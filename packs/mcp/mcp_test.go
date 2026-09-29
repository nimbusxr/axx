package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/proc"
)

// stdioEnv makes the test binary an MCP server over stdio, or a process
// that sleeps; childEnv makes the server start one first and write its pid.
const (
	stdioEnv = "AXX_MCP_TEST_SERVER"
	childEnv = "AXX_MCP_TEST_CHILD"
)

func TestMain(m *testing.M) {
	switch os.Getenv(stdioEnv) {
	case "broken":
		fmt.Fprintln(os.Stderr, "the parcels database is not reachable")
		os.Exit(1)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "stdio":
		if pidfile := os.Getenv(childEnv); pidfile != "" {
			c := exec.Command(os.Args[0])
			c.Env = append(os.Environ(), stdioEnv+"=sleep")
			if err := c.Start(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			_ = os.WriteFile(pidfile, []byte(strconv.Itoa(c.Process.Pid)), 0o600)
		}
		if err := parcelsServer().Run(context.Background(), &sdk.StdioTransport{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var str = map[string]any{"type": "string"}

// parcelsServer is the parcels MCP server the tests call.
func parcelsServer() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "parcels", Version: "1.4.0"}, nil)
	args := func(req *sdk.CallToolRequest) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &m)
		return m
	}
	s.AddTool(&sdk.Tool{
		Name: "track_parcel", Title: "Track a parcel", Description: "Where a parcel is: its status and its last scan.",
		Annotations:  &sdk.ToolAnnotations{ReadOnlyHint: true},
		InputSchema:  object(map[string]any{"reference": str}, "reference"),
		OutputSchema: object(map[string]any{"status": str, "lastLocation": str}, "status"),
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		ref, _ := args(req)["reference"].(string)
		if ref != "PX-MCP-9101" {
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "there is no parcel " + ref}}}, nil
		}
		out := map[string]any{"status": "OUT_FOR_DELIVERY", "lastLocation": "Leipzig"}
		b, _ := json.Marshal(out)
		return &sdk.CallToolResult{StructuredContent: out, Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil
	})
	s.AddTool(&sdk.Tool{
		Name: "hold_parcel", Description: "Holds a parcel at its depot until a day.",
		InputSchema: object(map[string]any{"reference": str, "until": map[string]any{"type": "string", "format": "date"}}, "reference", "until"),
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		a := args(req)
		if a["reference"] == "PX-MCP-9102" {
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "PX-MCP-9102 is already out for delivery"}}}, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf(`{"reference": %q, "status": "ON_HOLD", "heldUntil": %q}`, a["reference"], a["until"])}}}, nil
	})
	s.AddTool(&sdk.Tool{
		Name: "count_scans", Description: "How often a parcel was scanned in a depot.",
		InputSchema: object(map[string]any{"postcode": str, "depot": map[string]any{"type": "integer"}, "express": map[string]any{"type": "boolean"}}, "postcode", "depot"),
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		// The arguments as they arrived, typed.
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(req.Params.Arguments)}}}, nil
	})
	s.AddTool(&sdk.Tool{
		Name: "depot_status", Description: "The state of a depot.",
		InputSchema:  object(map[string]any{}),
		OutputSchema: object(map[string]any{"open": map[string]any{"type": "boolean"}}, "open"),
	}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		// It breaks its own output schema.
		return &sdk.CallToolResult{StructuredContent: map[string]any{"open": "yes"}, Content: []sdk.Content{&sdk.TextContent{Text: `{"open": "yes"}`}}}, nil
	})
	s.AddResourceTemplate(&sdk.ResourceTemplate{Name: "label", URITemplate: "parcels://{reference}/label", MIMEType: "application/json"},
		func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			ref := strings.TrimSuffix(strings.TrimPrefix(req.Params.URI, "parcels://"), "/label")
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{
				URI: req.Params.URI, MIMEType: "application/json", Text: fmt.Sprintf(`{"reference": %q, "service": "EXPRESS"}`, ref),
			}}}, nil
		})
	s.AddPrompt(&sdk.Prompt{Name: "delivery_update", Arguments: []*sdk.PromptArgument{{Name: "reference", Required: true}}},
		func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{
				Text: "Tell the recipient where parcel " + req.Params.Arguments["reference"] + " is, briefly.",
			}}}}, nil
		})
	return s
}

func harness(t *testing.T) (*cloudtest.Harness, string) {
	t.Helper()
	srv := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return parcelsServer() }, nil))
	t.Cleanup(srv.Close)
	h := cloudtest.New(t, Pack())
	return h, srv.URL + "/mcp"
}

func TestToolsOverHTTP(t *testing.T) {
	h, url := harness(t)
	h.OK("the parcels mcp server with the following properties:", [][]string{{"url", url}})
	h.OK("the parcels mcp server has the track_parcel tool")
	h.OK("the parcels mcp server has the track_parcel tool with the following properties:",
		[][]string{{"title", "Track a parcel"}, {"annotations.readOnlyHint", "true"}, {"inputSchema.required[0]", "reference"}})
	h.OK("the parcels mcp server does not have the cancel_parcel tool")
	_ = h.Fails("the parcels mcp server does not have the hold_parcel tool", "the parcels mcp server has the hold_parcel tool")
	_ = h.Fails("the parcels mcp server has the cancel_parcel tool", "has no tool cancel_parcel; its tools are: count_scans, depot_status, hold_parcel, track_parcel")

	h.OK("the track_parcel tool is called on the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9101"}})
	h.OK("the track_parcel tool's result is not an error")
	h.OK("the track_parcel tool's result on the parcels mcp server has the following properties:",
		[][]string{{"status", "OUT_FOR_DELIVERY"}, {"lastLocation", "Leipzig"}})
	h.OK("the track_parcel tool's result contains 'Leipzig'")
	_ = h.Fails("the track_parcel tool's result is an error", "the track_parcel tool's result is not an error")

	h.OK("the hold_parcel tool is called on the parcels mcp server with the following arguments:",
		[][]string{{"reference", "PX-MCP-9102"}, {"until", "2026-10-05"}})
	h.OK("the hold_parcel tool's result is an error")
	h.OK("the hold_parcel tool's result contains 'already out for delivery'")
	_ = h.Fails("the hold_parcel tool's result is not an error", "the hold_parcel tool's result is an error: PX-MCP-9102 is already out for delivery")

	// A tool without structured output answers JSON as text.
	h.File("mcp/hold-parcel.json", `{"reference": "PX-MCP-9105", "until": "2026-10-06"}`)
	h.OK("the hold_parcel tool is called on the parcels mcp server with the mcp/hold-parcel.json arguments")
	h.OK("the hold_parcel tool's result has the following properties:", [][]string{{"status", "ON_HOLD"}, {"heldUntil", "2026-10-06"}})

	// A call the server refuses.
	h.OK("the cancel_parcel tool is called on the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9101"}})
	h.OK("the cancel_parcel tool's call failed with the error code -32602")
	_ = h.Fails("the cancel_parcel tool's call failed with the error code -32603", "failed with another error code")
	_ = h.Fails("the cancel_parcel tool's result is not an error", "refused the call of the cancel_parcel tool with the error -32602")
	_ = h.Fails("the track_parcel tool's call failed with the error code -32602", "the track_parcel tool's call did not fail")
}

func TestArgumentsTakeTheirSchemasTypes(t *testing.T) {
	h, url := harness(t)
	h.OK("the parcels mcp server with the following properties:", [][]string{{"url", url}})
	h.OK("the count_scans tool is called on the parcels mcp server with the following arguments:",
		[][]string{{"postcode", "01067"}, {"depot", "4"}, {"express", "true"}})
	h.OK("the count_scans tool's result has the following properties:", [][]string{{"postcode", "01067"}, {"depot", "4"}, {"express", "true"}})
	h.OK("the count_scans tool's result contains '\"depot\":4'")
	h.OK("the count_scans tool's result contains '\"postcode\":\"01067\"'")
}

func TestTheSchemasAreTheContract(t *testing.T) {
	h, url := harness(t)
	h.OK("the parcels mcp server with the following properties:", [][]string{{"url", url}})
	err := h.Fails("the hold_parcel tool is called on the parcels mcp server with the following arguments:",
		"validation.arguments.schema.required", [][]string{{"reference", "PX-MCP-9106"}})
	if !strings.Contains(err.Error(), `Given the MCP validation levels are:`) {
		t.Errorf("the failure should say how to relax the check: %v", err)
	}
	_ = h.Fails("the depot_status tool is called on the parcels mcp server", "validation.result.schema.type")

	h.OK("the MCP validation levels are:", [][]string{{"validation.result", "WARN"}})
	h.OK("the depot_status tool is called on the parcels mcp server")
	h.OK("the depot_status tool's result has the following properties:", [][]string{{"open", "yes"}})
	_ = h.Fails("the MCP validation levels are:", "validation.arguments.schema.requird", [][]string{{"validation.arguments.schema.requird", "IGNORE"}})
}

func TestResourcesAndPrompts(t *testing.T) {
	h, url := harness(t)
	h.OK("the parcels mcp server with the following properties:", [][]string{{"url", url}})
	h.OK("the parcels://PX-MCP-9103/label resource is read from the parcels mcp server")
	h.OK("the parcels://PX-MCP-9103/label resource contains 'PX-MCP-9103'")
	h.OK("the parcels://PX-MCP-9103/label resource has the following properties:", [][]string{{"reference", "PX-MCP-9103"}, {"service", "EXPRESS"}})
	_ = h.Fails("the parcels://PX-MCP-9199/label resource contains 'x'", "was not read in this scenario")
	h.OK("the delivery_update prompt is requested from the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9104"}})
	h.OK("the delivery_update prompt contains 'where parcel PX-MCP-9104 is'")
	_ = h.Fails("the delivery_update prompt contains 'PX-MCP-9199'", "does not contain the text")
}

func TestOverStdio(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.OK("the parcels mcp server with the following properties:",
		[][]string{{"command", `"` + os.Args[0] + `"`}, {"env." + stdioEnv, "stdio"}, {"protocol version", "2025-06-18"}})
	if !strings.Contains(strings.Join(h.Sink.Logs, "\n"), "parcels 1.4.0, protocol 2025-06-18") {
		t.Errorf("the session should speak the version asked for: %v", h.Sink.Logs)
	}
	h.OK("the track_parcel tool is called on the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9101"}})
	h.OK("the track_parcel tool's result has the following properties:", [][]string{{"status", "OUT_FOR_DELIVERY"}})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// A process the command started, even before the session began, is stopped
// with the scenario.
func TestTheCommandsProcessesEndWithTheScenario(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child")
	h := cloudtest.New(t, Pack())
	h.OK("the parcels mcp server with the following properties:",
		[][]string{{"command", `"` + os.Args[0] + `"`}, {"env." + stdioEnv, "stdio"}, {"env." + childEnv, pidfile}})
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(string(b))
	if !proc.ProcessAlive(child) {
		t.Fatalf("the server's child %d is not running", child)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); proc.ProcessAlive(child); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the server's child %d still runs", child)
		}
	}
}

func TestTwoServers(t *testing.T) {
	h, url := harness(t)
	h.OK("the parcels mcp server with the following properties:", [][]string{{"url", url}})
	h.OK("the depots mcp server with the following properties:", [][]string{{"url", url}})
	h.OK("the track_parcel tool is called on the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9101"}})
	h.OK("the track_parcel tool is called on the depots mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9107"}})
	_ = h.Fails("the track_parcel tool's result is not an error", "was called on more than one mcp server: name the one to check")
	h.OK("the track_parcel tool's result on the parcels mcp server is not an error")
	h.OK("the track_parcel tool's result on the depots mcp server is an error")
	_ = h.Fails("the track_parcel tool's result on the shops mcp server is an error", "was not called on the shops mcp server")
}

func TestRegistrationErrors(t *testing.T) {
	h, url := harness(t)
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"timeout", "5s"}}, `needs a "command" (stdio) or a "url" (streamable HTTP)`},
		{[][]string{{"url", url}, {"command", "parcels mcp"}}, `has a "command" or a "url", not both`},
		{[][]string{{"url", "ftp://parcels"}}, "is http:// or https://"},
		{[][]string{{"url", url}, {"env.TOKEN", "x"}}, "env.<NAME> and dir are for a command"},
		{[][]string{{"command", "parcels mcp"}, {"header.Authorization", "Bearer x"}}, "header.<name> is for a url"},
		{[][]string{{"uri", url}}, `unknown mcp server property "uri"`},
		{[][]string{{"url", url}, {"timeout", "soon"}}, "is not a duration"},
		{[][]string{{"url", "http://127.0.0.1:1/mcp"}}, "cannot connect to the parcels mcp server at http://127.0.0.1:1/mcp"},
		{[][]string{{"command", "axx-no-such-mcp-server"}}, "cannot run the parcels mcp server (axx-no-such-mcp-server)"},
		{[][]string{{"command", `"` + os.Args[0] + `"`}, {"env." + stdioEnv, "broken"}}, "its error output:\nthe parcels database is not reachable"},
	} {
		_ = h.Fails("the parcels mcp server with the following properties:", c.want, c.rows)
	}
	_ = h.Fails("the track_parcel tool is called on the depots mcp server", `no mcp server named "depots" in this scenario`)
}
