package asyncapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/contract"
)

// harness is a run with the pack and the v3 test document as the project's
// asyncapi.yaml, its other files beside it.
func harness(t *testing.T, config map[string]any) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.NewWith(t, config, Pack())
	if err := os.CopyFS(h.Dir, os.DirFS(filepath.Join("testdata", "v3"))); err != nil {
		t.Fatal(err)
	}
	h.Start(h.Plan())
	return h
}

var badScan = contract.Message{
	Protocol: "mqtt", Addresses: []string{"depots/LEJ/scans"}, Sent: true,
	Payload: []byte(`{"scanId": "SC-9", "parcelRef": "PX-API-7301"}`),
}

func TestLevels(t *testing.T) {
	h := harness(t, nil)
	c, err := contract.Open(h.SC, "asyncapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Check(h.SC, badScan)
	if err == nil || !strings.Contains(err.Error(), "the message to send breaks asyncapi.yaml:\n- validation.message.payload.schema.required: payload $: missing property 'status'") ||
		!strings.Contains(err.Error(), `"Given the AsyncAPI validation levels are:"`) {
		t.Fatalf("err = %v", err)
	}

	// A scenario relaxes a key and the keys below it, and only itself.
	h.OK("the AsyncAPI validation levels are:", [][]string{{"validation.message.payload", "WARN"}})
	if err := c.Check(h.SC, badScan); err != nil {
		t.Fatal(err)
	}
	if logs := strings.Join(h.Sink.Logs, "\n"); !strings.Contains(logs, "AsyncAPI WARN validation.message.payload.schema.required: payload $: missing property 'status'") {
		t.Errorf("logs lack the warning:\n%s", logs)
	}
	h.OK("the AsyncAPI validation levels are:", [][]string{{"validation.message.payload.schema.required", "ERROR"}})
	if err := c.Check(h.SC, badScan); err == nil {
		t.Fatal("the more specific key should win")
	}
	h.NewScenario()
	if err := c.Check(h.SC, badScan); err == nil {
		t.Fatal("another scenario's levels should not apply")
	}

	_ = h.Fails("the AsyncAPI validation levels are:", `unknown AsyncAPI validation key "validation.message.payload.schema.requird"; did you mean validation.message.payload.schema.required?`,
		[][]string{{"validation.message.payload.schema.requird", "WARN"}})
	_ = h.Fails("the AsyncAPI validation levels are:", `invalid AsyncAPI validation level "LOUD"`,
		[][]string{{"validation.channel", "LOUD"}})
}

func TestLevelsOfTheRun(t *testing.T) {
	h := harness(t, map[string]any{Name: map[string]any{"levels": map[string]string{"validation.message": "IGNORE"}}})
	c, err := contract.Open(h.SC, "asyncapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Check(h.SC, badScan); err != nil {
		t.Fatal(err)
	}
	if logs := strings.Join(h.Sink.Logs, "\n"); strings.Contains(logs, "AsyncAPI") {
		t.Errorf("an ignored finding was logged:\n%s", logs)
	}
	// The run's levels are the default the scenario's are merged over.
	h.OK("the AsyncAPI validation levels are:", [][]string{{"validation.message.payload.schema", "ERROR"}})
	if err := c.Check(h.SC, badScan); err == nil {
		t.Fatal("the scenario's level should win")
	}
}

func TestUnknownKeyOfTheRun(t *testing.T) {
	h := cloudtest.NewWith(t, map[string]any{Name: map[string]any{"levels": map[string]string{"validation.channels": "WARN"}}}, Pack())
	err := Pack().(pack).Init(t.Context(), h.Suite)
	if err == nil || !strings.Contains(err.Error(), `packs.asyncapi.levels: unknown AsyncAPI validation key "validation.channels"; did you mean validation.channel?`) {
		t.Fatalf("err = %v", err)
	}
}

func TestDocuments(t *testing.T) {
	h := harness(t, nil)
	if _, err := contract.Open(h.SC, "missing.yaml"); err == nil || !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("err = %v", err)
	}
	// A document at a URL, whose $refs are relative to it.
	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Join("testdata", "v3"))))
	t.Cleanup(srv.Close)
	c, err := contract.Open(h.SC, srv.URL+"/asyncapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	delivered := contract.Message{
		Protocol: "kafka", Addresses: []string{"parcels.events"},
		Payload: []byte(`{"parcelRef": "PX-API-7302", "type": "DELIVERED", "deliveredAt": "2026-09-28T10:00:00Z"}`),
	}
	if err := c.Check(h.SC, delivered); err != nil {
		t.Fatal(err)
	}
	if _, err := contract.Open(h.SC, srv.URL+"/nowhere.yaml"); err == nil || !strings.Contains(err.Error(), "answered 404") {
		t.Fatalf("err = %v", err)
	}
}

func TestWithoutThePack(t *testing.T) {
	h := cloudtest.New(t)
	if _, err := contract.Open(h.SC, "asyncapi.yaml"); !errors.Is(err, contract.ErrNoChecks) {
		t.Fatalf("err = %v", err)
	}
}
