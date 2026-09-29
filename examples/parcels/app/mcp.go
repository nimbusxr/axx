package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// parcelsMCP is the parcels MCP server, for the AI assistants of shops and
// recipients, at /mcp (streamable HTTP) and over stdio (`parcels mcp`):
//
//	track_parcel   where a parcel is: its status and its last scan (read-only)
//	hold_parcel    holds a parcel at its depot until a day, under the depot's rules
//	parcels://{reference}/label   a parcel's shipping label
//	delivery_update               a prompt for a message to the recipient
//
// The tools never say who the recipient is, or where they live.
type parcelsMCP struct {
	store    *store
	tracking *trackingStore
	labels   labeler
}

func (m *parcelsMCP) server() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "parcels", Title: "Parcels", Version: "1.4.0"},
		&sdk.ServerOptions{Instructions: "Track the parcels of a shop, and hold them at their depot. A parcel's reference looks like PX-1042."})
	str := map[string]any{"type": "string"}
	s.AddTool(&sdk.Tool{
		Name: "track_parcel", Title: "Track a parcel",
		Description: "Where a parcel is: its status, and where and when it was last scanned.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"reference": map[string]any{"type": "string", "description": "The parcel's reference, like PX-1042."},
		}, "required": []string{"reference"}},
		OutputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"reference": str, "status": str, "serviceLevel": str, "city": str,
			"lastLocation": map[string]any{"type": []string{"string", "null"}},
			"lastScanAt":   map[string]any{"type": []string{"string", "null"}},
		}, "required": []string{"reference", "status", "serviceLevel"}},
	}, m.track)
	s.AddTool(&sdk.Tool{
		Name: "hold_parcel", Title: "Hold a parcel at its depot",
		Description: "Holds a parcel at its depot until a day, for the recipient to collect it or have it delivered then. " +
			"A parcel out for delivery can no longer be held.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool)},
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"reference": map[string]any{"type": "string"},
			"until":     map[string]any{"type": "string", "format": "date", "description": "The day, like 2026-10-05."},
		}, "required": []string{"reference", "until"}},
		OutputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"reference": str, "status": str, "heldUntil": str,
		}, "required": []string{"reference", "status", "heldUntil"}},
	}, m.hold)
	s.AddResourceTemplate(&sdk.ResourceTemplate{
		Name: "label", Title: "Shipping label", URITemplate: "parcels://{reference}/label", MIMEType: "application/json",
		Description: "A parcel's shipping label: its barcode and its signature.",
	}, m.label)
	s.AddPrompt(&sdk.Prompt{
		Name: "delivery_update", Title: "Delivery update",
		Description: "A message that tells a parcel's recipient where it is.",
		Arguments:   []*sdk.PromptArgument{{Name: "reference", Description: "The parcel's reference.", Required: true}},
	}, m.deliveryUpdate)
	return s
}

// handler serves the server over streamable HTTP, without sessions.
func (m *parcelsMCP) handler() http.Handler {
	s := m.server()
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, &sdk.StreamableHTTPOptions{Stateless: true})
}

func toolError(format string, args ...any) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

func structured(v any) (*sdk.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &sdk.CallToolResult{StructuredContent: v, Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil
}

func (m *parcelsMCP) track(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	var in struct {
		Reference string `json:"reference"`
	}
	_ = json.Unmarshal(req.Params.Arguments, &in)
	p, err := m.store.Get(ctx, in.Reference)
	if errors.Is(err, errNotFound) {
		return toolError("There is no parcel %s.", in.Reference), nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"reference": p.Reference, "status": p.Status, "serviceLevel": p.ServiceLevel, "city": p.Recipient.City,
		"lastLocation": nil, "lastScanAt": nil,
	}
	if t, err := m.tracking.Get(ctx, p.Reference); err == nil {
		out["lastLocation"] = t.LastLocation
		if t.LastScanAt != nil {
			out["lastScanAt"] = t.LastScanAt.UTC().Format(time.RFC3339)
		}
	}
	return structured(out)
}

func (m *parcelsMCP) hold(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
	var in struct {
		Reference string `json:"reference"`
		Until     string `json:"until"`
	}
	_ = json.Unmarshal(req.Params.Arguments, &in)
	until, ok := day(in.Until)
	if !ok {
		return toolError("%q is not a day, like 2026-10-05.", in.Until), nil
	}
	var status string
	p, err := m.store.Hold(ctx, in.Reference, until, func(p *Parcel) error {
		if !holdable[p.Status] {
			status = p.Status
			return errNotChangeable
		}
		return nil
	})
	switch {
	case status != "":
		return toolError("%s is %s: it can no longer be held.", in.Reference, strings.ToLower(strings.ReplaceAll(status, "_", " "))), nil
	case errors.Is(err, errNotFound):
		return toolError("There is no parcel %s.", in.Reference), nil
	case err != nil:
		return nil, err
	}
	return structured(map[string]any{"reference": p.Reference, "status": p.Status, "heldUntil": in.Until})
}

// day reads a day, like 2026-10-05.
func day(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	return t, err == nil
}

func (m *parcelsMCP) label(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	ref := strings.TrimSuffix(strings.TrimPrefix(req.Params.URI, "parcels://"), "/label")
	p, err := m.store.Get(ctx, ref)
	if errors.Is(err, errNotFound) {
		return nil, sdk.ResourceNotFoundError(req.Params.URI)
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(m.labels.label(p))
	if err != nil {
		return nil, err
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)}}}, nil
}

func (m *parcelsMCP) deliveryUpdate(ctx context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
	ref := req.Params.Arguments["reference"]
	p, err := m.store.Get(ctx, ref)
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("there is no parcel %s", ref)
	}
	if err != nil {
		return nil, err
	}
	text := fmt.Sprintf("Write to the recipient of parcel %s, in two sentences, that it is %s, and when it should arrive (%s delivery). "+
		"Do not mention their address.", p.Reference, strings.ToLower(strings.ReplaceAll(p.Status, "_", " ")), strings.ToLower(p.ServiceLevel))
	return &sdk.GetPromptResult{
		Description: "A delivery update for " + p.Reference,
		Messages:    []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: text}}},
	}, nil
}
