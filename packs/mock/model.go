package mock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/schemadoc"
	"github.com/nimbusxr/axx/internal/secrets"
)

// modelSince is the axx version that introduced the model checks.
const modelSince = "0.1.5"

// A mocked model is a WireMock with the axx image's model mock: its stubs
// (mapping files) answer a model's requests in the format they come in.
// These steps check what the service asked the model, read from WireMock's
// journal: every request since the run started, each read in its API's
// format. A scenario's requests are the ones about its own data.
func modelSteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mock.model.asked", Keyword: "Then", Since: modelSince,
			Expr: "the mocked {mockedService} model was asked about {string}[[ {int} time(s)]]",
			Doc: "Check that the mocked model was asked about a text: that a request to it had the text in its conversation, " +
				"at least once, or as many times as the step says.\n\n" + modelDoc,
			Examples: []string{
				"Then the mocked models model was asked about 'PX-AI-8102'",
				"Then the mocked models model was asked about 'PX-AI-8104' 2 times",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, about := a.Value(0).(*Service), a.String(1)
				reqs, err := askedAbout(sc, svc, about)
				if err != nil {
					return err
				}
				if !a.Present(2) {
					if len(reqs) == 0 {
						return neverAsked(sc, svc, about)
					}
					return nil
				}
				if n := a.Int(2); len(reqs) != n {
					return core.Fail(fmt.Sprintf("the mocked %s model was asked about %q %d time(s), not %d", svc.Name, about, len(reqs), n), n, len(reqs))
				}
				return nil
			},
		},
		{
			ID: "mock.model.contains", Keyword: "Then", Since: modelSince,
			Expr: "the mocked {mockedService} model's request about {string} contains {string}",
			Doc: "Check that a request the mocked model was asked about a text sent another: in its messages, its tools' results, " +
				"its tools or its settings.\n\n" + modelDoc,
			Examples: []string{"Then the mocked models model's request about 'PX-AI-8102' contains 'OUT_FOR_DELIVERY'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, about, want := a.Value(0).(*Service), a.String(1), a.String(2)
				reqs, err := about1(sc, svc, about)
				if err != nil {
					return err
				}
				for _, r := range reqs {
					if strings.Contains(r.sent(), want) {
						return nil
					}
				}
				return secrets.Hide(sc, core.Failf("no request the mocked %s model was asked about %q contains %q; the latest was:\n%s",
					svc.Name, about, want, excerpt(reqs[len(reqs)-1].text())))
			},
		},
		{
			ID: "mock.model.notContains", Keyword: "Then", Since: modelSince, Absence: true,
			Expr: "the mocked {mockedService} model's request about {string} does not contain {string}",
			Doc: "Check that no request the mocked model was asked about a text sent another, anywhere: in its messages, its " +
				"tools' results, its tools or its settings. The model must have been asked about the text, or the check " +
				"proves nothing and fails.\n\n" + modelDoc,
			Examples: []string{"Then the mocked models model's request about 'PX-AI-8102' does not contain 'Lindenweg 14'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, about, unwanted := a.Value(0).(*Service), a.String(1), a.String(2)
				reqs, err := about1(sc, svc, about)
				if err != nil {
					return err
				}
				for _, r := range reqs {
					if s := r.sent(); strings.Contains(s, unwanted) {
						return secrets.Hide(sc, core.Failf("a request the mocked %s model was asked about %q contains %q:\n%s",
							svc.Name, about, unwanted, around(s, unwanted)))
					}
				}
				return nil
			},
		},
		{
			ID: "mock.model.tool", Keyword: "Then", Since: modelSince,
			Expr:     "the mocked {mockedService} model was offered the {word} tool in the request about {string}",
			Doc:      "Check that a request the mocked model was asked about a text offered it the tool, by its name.\n\n" + modelDoc,
			Examples: []string{"Then the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8102'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, tool, about := a.Value(0).(*Service), a.String(1), a.String(2)
				reqs, err := about1(sc, svc, about)
				if err != nil {
					return err
				}
				offered := map[string]bool{}
				for _, r := range reqs {
					for _, t := range r.tools {
						if t == tool {
							return nil
						}
						offered[t] = true
					}
				}
				if len(offered) == 0 {
					return core.Failf("the mocked %s model was offered no tool in the requests about %q", svc.Name, about)
				}
				return core.Failf("the mocked %s model was not offered the %s tool in the requests about %q, only: %s",
					svc.Name, tool, about, strings.Join(sortedKeys(offered), ", "))
			},
		},
		{
			ID: "mock.model.schema", Keyword: "Then", Since: modelSince,
			Expr: "the mocked {mockedService} model was asked for the {filepath} schema in the request about {string}",
			Doc: "Check that a request the mocked model was asked about a text asked for its answer in the JSON Schema of a file " +
				"of the project (JSON or YAML): the structured output the request asks for is that schema, whatever the order of " +
				"its keys.\n\n" + modelDoc,
			Examples: []string{"Then the mocked models model was asked for the schemas/address.json schema in the request about 'Lindenweg 14'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, file, about := a.Value(0).(*Service), a.String(1), a.String(2)
				p, err := sc.Suite().ResolvePath(file)
				if err != nil {
					return err
				}
				raw, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				want, err := schemadoc.Decode(raw)
				if err != nil {
					return fmt.Errorf("the schema %s is not JSON or YAML: %w", file, err)
				}
				reqs, err := about1(sc, svc, about)
				if err != nil {
					return err
				}
				var asked []string
				for _, r := range reqs {
					if r.schema == nil {
						continue
					}
					if sameSchema(want, r.schema) {
						return nil
					}
					asked = append(asked, compact(r.schema))
				}
				if len(asked) == 0 {
					return core.Failf("no request the mocked %s model was asked about %q asked for a schema: it asked for no structured output",
						svc.Name, about)
				}
				return core.Fail(fmt.Sprintf("the requests the mocked %s model was asked about %q asked for another schema than %s", svc.Name, about, file),
					compact(want), strings.Join(asked, "\n"))
			},
		},
	}
}

const modelDoc = "- The model is a WireMock with the axx image's model mock: its stubs are mapping files, and it answers in the " +
	"format of the request, whichever provider's API the service speaks (OpenAI's and the servers that speak it, Anthropic, " +
	"Gemini, Bedrock, Ollama). Register it as any mocked service.\n" +
	"- A request is about a text when its conversation has it: its system prompt, its messages, and the tools' calls and " +
	"results, JSON-escaped text included. Every request since the run started counts, other scenarios' too: name the " +
	"scenario's own data, like its parcel's reference.\n" +
	"- A request that continues an earlier response of OpenAI's Responses API (`previous_response_id`) is about what that " +
	"response's requests were about too."

// askedAbout returns the chat requests the mocked model received since the
// run started that are about the text, oldest first.
func askedAbout(sc *core.Scenario, svc *Service, about string) ([]*modelRequest, error) {
	all, err := svc.c.modelRequests(sc.Context(), runContracts(sc.Suite()).start)
	if err != nil {
		return nil, err
	}
	var out []*modelRequest
	for _, r := range all {
		if strings.Contains(r.text(), about) {
			out = append(out, r)
		}
	}
	return out, nil
}

// about1 is askedAbout, which fails when the model was never asked about
// the text.
func about1(sc *core.Scenario, svc *Service, about string) ([]*modelRequest, error) {
	reqs, err := askedAbout(sc, svc, about)
	if err == nil && len(reqs) == 0 {
		err = neverAsked(sc, svc, about)
	}
	return reqs, err
}

// neverAsked says what the model was asked instead.
func neverAsked(sc *core.Scenario, svc *Service, about string) error {
	all, err := svc.c.modelRequests(sc.Context(), runContracts(sc.Suite()).start)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return core.Failf("the mocked %s model was never asked about %q: it received no request to a model since the run started "+
			"(is the service pointed at it?)", svc.Name, about)
	}
	var latest []string
	for i := len(all) - 1; i >= 0 && len(latest) < 3; i-- {
		latest = append(latest, fmt.Sprintf("  - %s: %s", all[i].api, excerpt(all[i].lastUser())))
	}
	return secrets.Hide(sc, core.Failf("the mocked %s model was never asked about %q; of its %d requests since the run started, the latest were about:\n%s",
		svc.Name, about, len(all), strings.Join(latest, "\n")))
}

var responseID = regexp.MustCompile(`"id"\s*:\s*"(resp_[^"]*)"`)

// served is a request WireMock received, and the body of its answer.
type servedCall struct {
	request  logged
	response []byte
}

// journal returns the requests WireMock received since t, oldest first,
// with their answers.
func (c *client) journal(ctx context.Context, since time.Time) ([]servedCall, error) {
	var out struct {
		Requests []struct {
			Request  logged `json:"request"`
			Response struct {
				BodyAsBase64 string `json:"bodyAsBase64"`
			} `json:"response"`
		} `json:"requests"`
	}
	q := url.Values{"since": {since.UTC().Format("2006-01-02T15:04:05.000Z")}}
	if err := c.get(ctx, "/__admin/requests?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	calls := make([]servedCall, 0, len(out.Requests))
	for i := len(out.Requests) - 1; i >= 0; i-- { // the journal lists newest first
		e := out.Requests[i]
		answer, _ := base64.StdEncoding.DecodeString(e.Response.BodyAsBase64)
		calls = append(calls, servedCall{request: e.Request, response: answer})
	}
	return calls, nil
}

// modelRequests returns the requests to a model's chat endpoint WireMock
// received since t, oldest first. A request to OpenAI's Responses API that
// continues an earlier response comes with that response's conversation.
func (c *client) modelRequests(ctx context.Context, since time.Time) ([]*modelRequest, error) {
	calls, err := c.journal(ctx, since)
	if err != nil {
		return nil, err
	}
	var reqs []*modelRequest
	byID := map[string]*modelRequest{}
	for _, e := range calls {
		r := readModelRequest(e.request.Method, e.request.URL, e.request.body())
		if r == nil {
			continue
		}
		if r.api == "OpenAI's Responses" {
			if m := responseID.FindSubmatch(e.response); m != nil {
				r.responseID = string(m[1])
				byID[r.responseID] = r
			}
		}
		reqs = append(reqs, r)
	}
	for _, r := range reqs {
		r.earlier = earlier(r, byID, map[string]bool{})
	}
	return reqs, nil
}

// earlier is the conversation of the responses a request continues, oldest
// first.
func earlier(r *modelRequest, byID map[string]*modelRequest, seen map[string]bool) *conversation {
	prev, ok := byID[r.previous]
	if r.previous == "" || !ok || seen[r.previous] {
		return nil
	}
	seen[r.previous] = true
	c := conversation{}
	if e := earlier(prev, byID, seen); e != nil {
		c.messages = append(c.messages, e.messages...)
	}
	c.messages = append(c.messages, message{role: "system", text: prev.conversation.system})
	c.messages = append(c.messages, prev.conversation.messages...)
	return &c
}

// sameSchema compares two JSON Schemas as JSON values: keys in any order,
// numbers by value, and type names in any case, since Gemini's
// responseSchema writes them in capitals (OBJECT, STRING).
func sameSchema(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok {
				return false
			}
			if k == "type" {
				if s, ok := v.(string); ok {
					if t, ok := w.(string); ok && strings.EqualFold(s, t) {
						continue
					}
				}
			}
			if !sameSchema(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameSchema(x[i], y[i]) {
				return false
			}
		}
		return true
	case json.Number, float64:
		n, ok1 := number(a)
		m, ok2 := number(b)
		return ok1 && ok2 && n.Cmp(m) == 0
	}
	return a == b
}

func number(v any) (*big.Float, bool) {
	switch x := v.(type) {
	case json.Number:
		f, ok := new(big.Float).SetString(string(x))
		return f, ok
	case float64:
		return big.NewFloat(x), true
	}
	return nil, false
}

// excerpt is a text for a failure: on one line, and not too long.
func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// around is the text around the first place a text has another.
func around(s, sub string) string {
	i := strings.Index(s, sub)
	start, end := max(0, i-80), min(len(s), i+len(sub)+80)
	return "…" + strings.Join(strings.Fields(s[start:end]), " ") + "…"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
