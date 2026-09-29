package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// assistant is the parcel assistant, which asks a model through OpenAI's
// Chat Completions API (which OpenAI-compatible servers speak too):
//
//	POST /api/assistant/address    {"note": "..."}      the recipient's address in a shop's note
//	POST /api/assistant/questions  {"question": "..."}  an answer to a recipient's question
//
// To answer a question, the model looks the parcel up with the track_parcel
// tool, which tells it the parcel's status and its last scan: never the
// recipient's name or street.
type assistant struct {
	client   openai.Client
	model    string
	parcels  *store
	tracking *trackingStore
	log      *slog.Logger
}

func newAssistant(cfg config, parcels *store, tracking *trackingStore, log *slog.Logger) *assistant {
	return &assistant{
		client: openai.NewClient(
			option.WithBaseURL(cfg.ModelURL),
			option.WithAPIKey(cfg.ModelAPIKey),
			// A busy model gets one more try, after the wait it asks for.
			option.WithMaxRetries(1),
			option.WithRequestTimeout(30*time.Second),
		),
		model:    cfg.Model,
		parcels:  parcels,
		tracking: tracking,
		log:      log,
	}
}

// addressSchema is the structured output the address is asked in.
var addressSchema = func() map[string]any {
	var s map[string]any
	if err := json.Unmarshal([]byte(`{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "street": {"type": "string"},
    "postcode": {"type": "string"},
    "city": {"type": "string"},
    "country": {"type": "string", "description": "ISO 3166-1 alpha-2"}
  },
  "required": ["name", "street", "postcode", "city", "country"],
  "additionalProperties": false
}`), &s); err != nil {
		panic(err)
	}
	return s
}()

func (a *assistant) address(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil || strings.TrimSpace(in.Note) == "" {
		problem(w, r, http.StatusBadRequest, "the request is a JSON object with the shop's note")
		return
	}
	res, err := a.client.Chat.Completions.New(r.Context(), openai.ChatCompletionNewParams{
		Model: a.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You read delivery addresses in the notes shops attach to their parcels. " +
				"Answer with the recipient's name and address; the country is its ISO 3166-1 alpha-2 code."),
			openai.UserMessage(in.Note),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
			JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{Name: "recipient_address", Schema: addressSchema, Strict: openai.Bool(true)},
		}},
	})
	if err != nil {
		a.failed(w, r, err)
		return
	}
	var addr Recipient
	choice := res.Choices[0]
	if choice.FinishReason != "stop" || json.Unmarshal([]byte(choice.Message.Content), &addr) != nil ||
		addr.Name == "" || addr.Street == "" || addr.Postcode == "" || addr.Country == "" {
		a.log.Warn("the model read no address in a note", "finish", choice.FinishReason, "answer", choice.Message.Content)
		problem(w, r, http.StatusUnprocessableEntity, "the assistant cannot read an address in the note: ask the shop to check it")
		return
	}
	writeJSON(w, http.StatusOK, addr)
}

var trackParcel = openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
	Name:        "track_parcel",
	Description: openai.String("Where a parcel is: its status, and where and when it was last scanned."),
	Parameters: shared.FunctionParameters{
		"type":       "object",
		"properties": map[string]any{"reference": map[string]any{"type": "string", "description": "The parcel's reference, like PX-1042."}},
		"required":   []string{"reference"},
	},
})

func (a *assistant) question(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil || strings.TrimSpace(in.Question) == "" {
		problem(w, r, http.StatusBadRequest, "the request is a JSON object with the recipient's question")
		return
	}
	params := openai.ChatCompletionNewParams{
		Model: a.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You answer recipients' questions about their parcels, briefly. " +
				"Look a parcel up with track_parcel before you say where it is."),
			openai.UserMessage(in.Question),
		},
		Tools: []openai.ChatCompletionToolUnionParam{trackParcel},
	}
	// The model calls tools until it answers; a few rounds are plenty.
	for range 4 {
		res, err := a.client.Chat.Completions.New(r.Context(), params)
		if err != nil {
			a.failed(w, r, err)
			return
		}
		msg := res.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			writeJSON(w, http.StatusOK, map[string]string{"answer": msg.Content})
			return
		}
		params.Messages = append(params.Messages, msg.ToParam())
		for _, call := range msg.ToolCalls {
			params.Messages = append(params.Messages, openai.ToolMessage(a.run(r.Context(), call), call.ID))
		}
	}
	problem(w, r, http.StatusBadGateway, "the assistant did not answer")
}

// run runs a tool the model called, and returns its result for the model.
func (a *assistant) run(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) string {
	result := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	if call.Function.Name != trackParcel.OfFunction.Function.Name {
		return result(map[string]string{"error": "there is no tool " + call.Function.Name})
	}
	var args struct {
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || args.Reference == "" {
		return result(map[string]string{"error": "track_parcel needs a parcel's reference"})
	}
	p, err := a.parcels.Get(ctx, args.Reference)
	if errors.Is(err, errNotFound) {
		return result(map[string]string{"error": fmt.Sprintf("there is no parcel %s", args.Reference)})
	}
	if err != nil {
		return result(map[string]string{"error": "the parcel cannot be looked up now"})
	}
	// What the model may know: never the recipient's name or street.
	out := map[string]any{"reference": p.Reference, "status": p.Status, "serviceLevel": p.ServiceLevel, "city": p.Recipient.City}
	if t, err := a.tracking.Get(ctx, p.Reference); err == nil {
		out["lastLocation"], out["lastScanAt"] = t.LastLocation, t.LastScanAt
	}
	return result(out)
}

// failed answers when the model did: a model that is busy or failing, even
// after one more try, makes the recipient try again later.
func (a *assistant) failed(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500) {
		a.log.Warn("the model is busy", "status", apiErr.StatusCode)
		w.Header().Set("Retry-After", "30")
		problem(w, r, http.StatusServiceUnavailable, "the assistant is busy: try again in a moment")
		return
	}
	a.log.Error("the model failed", "err", err)
	problem(w, r, http.StatusBadGateway, "the assistant is unavailable")
}
