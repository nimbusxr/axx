package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// servers starts the project servers of a router in this process, which has
// every pack built in, and remembers them.
type servers struct {
	mu      sync.Mutex
	started []string        // the project directories, in order
	ended   []chan struct{} // closed as each server ends
	fail    error           // when set, servers fail with it at once
}

func (s *servers) start(dir string) (*Backend, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, dir)
	ended := make(chan struct{})
	s.ended = append(s.ended, ended)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	if s.fail != nil {
		go func() { _, _ = io.Copy(io.Discard, inR) }()
		_ = outW.Close()
		fail := s.fail
		return &Backend{In: inW, Out: outR, Wait: func() error { close(ended); return fail }, Stop: func() { _ = inR.Close() }}, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), inR, outW, Options{Version: "test"})
		_ = outW.Close()
		close(ended)
	}()
	return &Backend{
		In: inW, Out: outR,
		Wait: func() error { return <-done },
		Stop: func() { _ = inR.Close() },
	}, nil
}

func (s *servers) dirs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.started...)
}

// allEnded waits for every server to end.
func (s *servers) allEnded(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	ended := append([]chan struct{}(nil), s.ended...)
	s.mu.Unlock()
	for i, e := range ended {
		select {
		case <-e:
		case <-time.After(10 * time.Second):
			t.Fatalf("the server of %s is still running", s.dirs()[i])
		}
	}
}

func startRouter(t *testing.T, s *servers) *client {
	t.Helper()
	return startWith(t, map[string]any{"workspace": map[string]any{"didChangeWatchedFiles": map[string]any{"dynamicRegistration": true}}},
		func(in io.Reader, out io.Writer) error {
			return Route(context.Background(), in, out, RouteOptions{Version: "test", Start: s.start})
		})
}

// project writes an axx project that lists packs, and returns its directory
// and the URI of a feature file in it.
func packsProject(t *testing.T, packs string) (string, string) {
	t.Helper()
	return writeProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [" + packs + "]\n"})
}

// mockedFeature uses a step of the mock pack.
const mockedFeature = `Feature: Addresses

  Scenario: an address is checked
    Given the mocked addresses service with the following properties:
      | url | http://localhost:8089 |
`

func codes(diags []diagnostic) []string {
	out := []string{}
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

func TestRouterGivesEachProjectTheStepsOfItsPacks(t *testing.T) {
	s := &servers{}
	c := startRouter(t, s)
	restDir, restURI := packsProject(t, "rest")
	mockDir, mockURI := packsProject(t, "rest, mock")

	if got := codes(c.open(restURI, mockedFeature)); len(got) != 1 || got[0] != "undefined" {
		t.Errorf("with the rest pack only: %v", got)
	}
	if got := codes(c.open(mockURI, mockedFeature)); len(got) != 0 {
		t.Errorf("with the mock pack: %v", got)
	}
	if got := s.dirs(); len(got) != 2 || got[0] != restDir || got[1] != mockDir {
		t.Errorf("servers started in %v, want %s and %s", got, restDir, mockDir)
	}

	// A request about a document gets its project's server's answer.
	var h *hover
	if err := json.Unmarshal(c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": mockURI}, "position": map[string]any{"line": 3, "character": 12},
	}), &h); err != nil || h == nil || !strings.Contains(h.Contents.Value, "mock") {
		t.Errorf("hover: %+v, %v", h, err)
	}

	c.request("shutdown", nil)
	c.notify("exit", nil)
	s.allEnded(t)
}

func TestRouterLoadsTheStepsAgainWhenThePacksChange(t *testing.T) {
	s := &servers{}
	c := startRouter(t, s)
	dir, uri := packsProject(t, "rest")
	if got := codes(c.open(uri, mockedFeature)); len(got) != 1 {
		t.Fatalf("with the rest pack only: %v", got)
	}

	packs := filepath.Join(dir, "axx-packs.yaml")
	if err := os.WriteFile(packs, []byte("packs: [rest, mock]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": fileLocation(packs, 0).URI, "type": 2}}})
	if got := codes(c.diagnostics(uri)); len(got) != 0 {
		t.Errorf("after adding the mock pack: %v", got)
	}
	if got := s.dirs(); len(got) != 2 || got[1] != dir {
		t.Errorf("servers started in %v", got)
	}
}

func TestRouterSaysWhyAProjectServerFailed(t *testing.T) {
	s := &servers{fail: errors.New("error[AXX-E0910]: preparing the packs failed")}
	c := startRouter(t, s)
	_, uri := packsProject(t, "./steps")
	diags := c.open(uri, mockedFeature)
	if len(diags) != 1 || diags[0].Severity != severityWarning ||
		diags[0].Message != "axx cannot load this project's steps: error[AXX-E0910]: preparing the packs failed" {
		t.Fatalf("diagnostics: %+v", diags)
	}
	// Requests about its documents are still answered.
	if got := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 3, "character": 12},
	}); string(got) != "null" {
		t.Errorf("hover: %s", got)
	}
}

func TestStepsComeFrom(t *testing.T) {
	dir, _ := writeProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest, ./steps]\n"})
	for path, want := range map[string]bool{
		"axx.yaml":             true,
		"axx.local.yaml":       true,
		"axx-packs.yaml":       true,
		"axx-packs.lock":       false,
		"steps/parcels.go":     true,
		"steps/go.mod":         true,
		"steps/README.md":      false,
		"features/a.feature":   false,
		"stepsmore/parcels.go": false,
		"other/axx.yaml":       false,
	} {
		if got := stepsComeFrom(dir, filepath.Join(dir, filepath.FromSlash(path))); got != want {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
}

func TestRouterKeepsTheStepsItHadWhenTheyCannotLoadAgain(t *testing.T) {
	s := &servers{}
	c := startRouter(t, s)
	dir, uri := packsProject(t, "rest, mock")
	if got := codes(c.open(uri, mockedFeature)); len(got) != 0 {
		t.Fatalf("with the mock pack: %v", got)
	}

	s.mu.Lock()
	s.fail = errors.New("error[AXX-E0910]: preparing the packs failed")
	s.mu.Unlock()
	packs := filepath.Join(dir, "axx-packs.yaml")
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": fileLocation(packs, 0).URI, "type": 2}}})
	for {
		m := c.next()
		if m.Method != "window/showMessage" {
			continue
		}
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if !strings.Contains(p.Message, "keeps the steps it had: error[AXX-E0910]: preparing the packs failed") {
			t.Errorf("message: %s", p.Message)
		}
		break
	}
	// The server before it still answers.
	var h *hover
	if err := json.Unmarshal(c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 3, "character": 12},
	}), &h); err != nil || h == nil {
		t.Errorf("hover: %+v, %v", h, err)
	}
}
