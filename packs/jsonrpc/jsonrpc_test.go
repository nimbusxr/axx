package jsonrpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// depots acts as the parcels service's depot API does: a parcel's status,
// and holding a parcel at the depot, which a parcel out for delivery can
// no longer be.
type depots struct {
	calls atomic.Int32
	auth  atomic.Value
}

func (d *depots) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.auth.Store(r.Header.Get("Authorization"))
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "<html>bad gateway</html>", http.StatusBadGateway)
		return
	}
	if req.Method != "rpc.discover" {
		d.calls.Add(1)
	}
	answer := func(v map[string]any) {
		v["jsonrpc"], v["id"] = "2.0", req.ID
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(code int, msg string, data any) {
		e := map[string]any{"code": code, "message": msg}
		if data != nil {
			e["data"] = data
		}
		answer(map[string]any{"error": e})
	}
	var ref, until string
	var byName struct{ Reference, Until string }
	var byPos []string
	switch {
	case json.Unmarshal(req.Params, &byName) == nil:
		ref, until = byName.Reference, byName.Until
	case json.Unmarshal(req.Params, &byPos) == nil && len(byPos) > 0:
		ref = byPos[0]
	}
	switch req.Method {
	case "rpc.discover":
		doc, _ := os.ReadFile("testdata/depots.openrpc.json")
		answer(map[string]any{"result": json.RawMessage(doc)})
	case "parcel.get":
		switch ref {
		case "PX-RPC-7199":
			answer(map[string]any{"result": map[string]any{"reference": ref, "status": "LOST"}})
		case "PX-RPC-7198":
			fail(4001, "the depot is closed", nil)
		default:
			answer(map[string]any{"result": map[string]any{"reference": ref, "status": "REGISTERED"}})
		}
	case "parcel.hold":
		if ref == "PX-RPC-7103" {
			fail(-32010, "PX-RPC-7103 is out for delivery", map[string]any{"status": "OUT_FOR_DELIVERY"})
			return
		}
		answer(map[string]any{"result": map[string]any{"reference": ref, "status": "ON_HOLD", "heldUntil": until}})
	case "depot.open":
		answer(map[string]any{"result": true})
	default:
		fail(-32601, "Method not found", nil)
	}
}

func serve(t *testing.T) (*depots, string) {
	t.Helper()
	d := &depots{}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	return d, srv.URL + "/rpc"
}

func harness(t *testing.T, config map[string]any) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.NewWith(t, config, Pack())
	if err := os.CopyFS(h.Dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCalls(t *testing.T) {
	d, url := serve(t)
	t.Setenv("DEPOTS_TOKEN", "dp-4711-secret")
	h := harness(t, nil)
	h.OK("the depots jsonrpc service with the following properties:", [][]string{{"url", url}, {"header.Authorization", "Bearer ${env:DEPOTS_TOKEN}"}})
	h.OK("the parcel.hold method is called on the depots jsonrpc service with the following params:",
		[][]string{{"reference", "PX-RPC-7101"}, {"until", "2026-10-02"}})
	h.OK("the depots jsonrpc service's result has the following properties:", [][]string{
		{"reference", "PX-RPC-7101"}, {"status", "ON_HOLD"}, {"heldUntil", "2026-10-02"}, {"reason", "undefined"},
	})
	_ = h.Fails("the depots jsonrpc service's result has the following properties:", "status", [][]string{{"status", "REGISTERED"}})
	_ = h.Fails("the depots jsonrpc service answered the error -32010", "answered parcel.hold with a result, not an error")
	if got := d.auth.Load(); got != "Bearer dp-4711-secret" {
		t.Errorf("authorization: %v", got)
	}

	h.OK("the parcel.get method is called on the depots jsonrpc service with the params:", `["PX-RPC-7102"]`)
	h.OK("the depots jsonrpc service's result has the following properties:", [][]string{{"reference", "PX-RPC-7102"}})
	h.OK("the depot.open method is called on the depots jsonrpc service")
	h.OK("the depots jsonrpc service's result is 'true'")
	_ = h.Fails("the depots jsonrpc service's result is 'false'", `The depots jsonrpc service's result of depot.open is true, not "false"`)

	// An error is the call's, for the checks.
	h.OK("the parcel.hold method is called on the depots jsonrpc service with the params:", `{"reference": "PX-RPC-7103", "until": "2026-10-02"}`)
	h.OK("the depots jsonrpc service answered the error -32010 with a message containing 'out for delivery'")
	h.OK("the depots jsonrpc service's error has the following properties:", [][]string{
		{"code", "-32010"}, {"data.status", "OUT_FOR_DELIVERY"}, {"data.reason", "undefined"},
	})
	_ = h.Fails("the depots jsonrpc service answered the error -32602", `The depots jsonrpc service answered the error -32010 ("PX-RPC-7103 is out for delivery") to parcel.hold, not -32602`)
	_ = h.Fails("the depots jsonrpc service answered the error -32010 with a message containing 'returned'", `whose message does not contain "returned"`)
	_ = h.Fails("the depots jsonrpc service's result is 'true'", "answered the error -32010")

	h.OK("the parcel.move method is called on the depots jsonrpc service")
	h.OK("the depots jsonrpc service answered the error -32601")
	_ = h.Fails("the parcel.get method is called on the depots jsonrpc service with the params:", "the params are an object (by name) or an array (by position)", `"PX-RPC-7104"`)
	_ = h.Fails("the parcel.get method is called on the payments jsonrpc service", `no jsonrpc service named "payments"`)
	for _, l := range h.Sink.Logs {
		if strings.Contains(l, "dp-4711-secret") {
			t.Errorf("the token is in the logs: %s", l)
		}
	}
}

func TestAnswersThatAreNotJSONRPC(t *testing.T) {
	_, url := serve(t)
	h := harness(t, nil)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			http.Error(w, "<html>bad gateway</html>", http.StatusBadGateway)
		case "/v1":
			fmt.Fprint(w, `{"result": true, "error": null, "id": 1}`)
		default:
			fmt.Fprint(w, `{"jsonrpc": "2.0", "result": true, "id": 99}`)
		}
	}))
	t.Cleanup(broken.Close)
	for path, want := range map[string]string{
		"/html":  "answered depot.open with HTTP 502, not a JSON-RPC response",
		"/v1":    `not a JSON-RPC 2.0 response (no "jsonrpc": "2.0")`,
		"/other": "with the id 99, not 1",
	} {
		h.NewScenario()
		h.OK("the depots jsonrpc service with the following properties:", [][]string{{"url", broken.URL + path}})
		_ = h.Fails("the depot.open method is called on the depots jsonrpc service", want)
	}
	_ = h.Fails("the other jsonrpc service with the following properties:", `the other jsonrpc service's url is http:// or https://, not "localhost:8400"`,
		[][]string{{"url", "localhost:8400"}})
	_ = h.Fails("the other jsonrpc service with the following properties:", `unknown jsonrpc service property "openapi"`,
		[][]string{{"url", url}, {"openapi", "depots.json"}})
}

func TestOpenRPC(t *testing.T) {
	for _, source := range []string{"depots.openrpc.json", "rpc.discover"} {
		t.Run(source, func(t *testing.T) {
			d, url := serve(t)
			h := harness(t, nil)
			h.OK("the depots jsonrpc service with the following properties:", [][]string{{"url", url}, {"openrpc", source}})
			h.OK("the parcel.hold method is called on the depots jsonrpc service with the following params:",
				[][]string{{"reference", "PX-RPC-7111"}, {"until", "2026-10-02"}})

			// Params that break the document are not sent.
			calls := d.calls.Load()
			for _, tc := range []struct {
				rows [][]string
				want string
			}{
				{[][]string{{"reference", "PX-RPC-7112"}}, `- validation.params.missing: the required param "until" is missing`},
				{[][]string{{"reference", "7113"}, {"until", "2026-10-02"}}, "- validation.params.schema.pattern: param reference $: "},
				{
					[][]string{{"reference", "PX-RPC-7114"}, {"until", "2026-10-02"}, {"depot", "LEJ"}},
					`- validation.params.unknown: parcel.hold has no param "depot"; it has reference, until, reason`,
				},
			} {
				err := h.Fails("the parcel.hold method is called on the depots jsonrpc service with the following params:", tc.want, tc.rows)
				if !strings.Contains(err.Error(), "the params of parcel.hold break "+source) {
					t.Errorf("err = %v", err)
				}
			}
			_ = h.Fails("the parcel.hold method is called on the depots jsonrpc service with the params:",
				"- validation.params.structure: parcel.hold takes its params by name, in an object", `["PX-RPC-7115", "2026-10-02"]`)
			_ = h.Fails("the parcel.move method is called on the depots jsonrpc service",
				"- validation.method.unknown: "+source+" has no method parcel.move; it has parcel.get, parcel.hold")
			if d.calls.Load() != calls {
				t.Errorf("%d calls that break the document were sent", d.calls.Load()-calls)
			}

			// What the service answers is checked too.
			_ = h.Fails("the parcel.get method is called on the depots jsonrpc service with the params:",
				"the result of parcel.get break "+source+":\n- validation.result.schema.enum: result $.status: ", `["PX-RPC-7199"]`)
			_ = h.Fails("the parcel.get method is called on the depots jsonrpc service with the params:",
				"- validation.error.unknown: parcel.get answered the error 4001, which "+source+" does not declare: it declares none", `["PX-RPC-7198"]`)
			// Its own errors, and JSON-RPC's, are fine.
			h.OK("the parcel.hold method is called on the depots jsonrpc service with the following params:",
				[][]string{{"reference", "PX-RPC-7103"}, {"until", "2026-10-02"}})
			h.OK("the depots jsonrpc service answered the error -32010")

			// A scenario relaxes a key and the keys below it, and only itself.
			h.OK("the OpenRPC validation levels are:", [][]string{{"validation.params", "WARN"}, {"validation.result", "IGNORE"}})
			h.OK("the parcel.hold method is called on the depots jsonrpc service with the following params:", [][]string{{"reference", "PX-RPC-7116"}})
			h.OK("the parcel.get method is called on the depots jsonrpc service with the params:", `["PX-RPC-7199"]`)
			logs := strings.Join(h.Sink.Logs, "\n")
			if !strings.Contains(logs, `OpenRPC WARN validation.params.missing: the required param "until" is missing`) || strings.Contains(logs, "validation.result.schema.enum") {
				t.Errorf("logs:\n%s", logs)
			}
			h.NewScenario()
			h.OK("the depots jsonrpc service with the following properties:", [][]string{{"url", url}, {"openrpc", source}})
			_ = h.Fails("the parcel.hold method is called on the depots jsonrpc service with the following params:", "validation.params.missing",
				[][]string{{"reference", "PX-RPC-7117"}})
		})
	}
}

func TestOpenRPCLevels(t *testing.T) {
	_, url := serve(t)
	h := harness(t, map[string]any{Name: map[string]any{"openrpc": map[string]any{"levels": map[string]string{"validation.error.unknown": "IGNORE"}}}})
	h.OK("the depots jsonrpc service with the following properties:", [][]string{{"url", url}, {"openrpc", "depots.openrpc.json"}})
	h.OK("the parcel.get method is called on the depots jsonrpc service with the params:", `["PX-RPC-7198"]`)
	h.OK("the depots jsonrpc service answered the error 4001")
	_ = h.Fails("the OpenRPC validation levels are:", `unknown OpenRPC validation key "validation.param"; did you mean validation.params?`,
		[][]string{{"validation.param", "WARN"}})
	_ = h.Fails("the OpenRPC validation levels are:", `invalid OpenRPC validation level "LOUD"`, [][]string{{"validation.params", "LOUD"}})

	bad := harness(t, map[string]any{Name: map[string]any{"openrpc": map[string]any{"levels": map[string]string{"validation.results": "WARN"}}}})
	_ = bad.Fails("the depots jsonrpc service with the following properties:",
		`packs.jsonrpc.openrpc.levels: unknown OpenRPC validation key "validation.results"; did you mean validation.result?`,
		[][]string{{"url", url}, {"openrpc", "depots.openrpc.json"}})
	_ = h.Fails("the other jsonrpc service with the following properties:", "not-openrpc.json is not an OpenRPC 1.x document",
		[][]string{{"url", url}, {"openrpc", "not-openrpc.json"}})
}
