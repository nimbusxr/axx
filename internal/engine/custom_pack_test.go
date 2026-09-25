package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/runner"
	"github.com/nimbusxr/axx/packs/rest"
)

// customPack is a pack a team would write: its steps work on the rest
// pack's context, the same objects the rest pack's steps use.
type customPack struct{}

func (customPack) Manifest() core.Manifest {
	return core.Manifest{Name: "custom", Steps: []core.StepDef{
		{
			ID: "custom.prepare", Keyword: "Given", Expr: "a custom step prepares a GET request to {word}",
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := rest.Context(sc).Service()
				if err != nil {
					return err
				}
				svc.AddRequest(http.MethodGet, a.String(0)).SetHeader("X-Prepared-By", "custom")
				return nil
			},
		},
		{
			ID: "custom.status", Keyword: "Then", Expr: "a custom step sees status {int} on the last response",
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := rest.Context(sc).Service()
				if err != nil {
					return err
				}
				reqs := svc.Requests()
				ex := reqs[len(reqs)-1].Exchange()
				if ex == nil {
					return errors.New("the last request was not executed")
				}
				if ex.Status != a.Int(0) || ex.RequestHeader.Get("X-Prepared-By") != "custom" {
					return core.Fail("status of the last response", a.Int(0), ex.Status)
				}
				return nil
			},
		},
	}}
}

const customFeature = `Feature: custom packs share the context

  Scenario: a custom step builds on the rest pack's steps
    Given the api service with the following properties:
      | url | ${sys:api.url} |
    And a custom step prepares a GET request to /health
    When the request is executed
    Then the response status code is 200
    And a custom step sees status 200 on the last response
`

func TestCustomPackSharesContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", "c.feature"), []byte(customFeature), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Dir: dir, Properties: map[string]string{"api.url": srv.URL}}
	packs := []NamedPack{{Name: "core", Pack: core.ParamsPack()}, {Name: "rest", Pack: rest.Pack()}, {Name: "custom", Pack: customPack{}}}
	ctx := context.Background()
	e, err := New(Options{Config: cfg, Packs: packs})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close(ctx) }()
	paths, _, _ := e.FeaturePaths(nil)
	set, err := e.LoadFeatures(paths)
	if err != nil {
		t.Fatal(err)
	}
	pickles, _ := set.Apply(feature.Filter{})
	r, err := runner.New(runner.Options{Registry: e.Registry, Hooks: e.Hooks, Suite: e.Suite, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(ctx, pickles)
	if s := res.Scenarios[0]; s.Status != runner.Passed {
		for _, st := range s.Steps {
			t.Logf("%s %s: %v", st.Status, st.Text, st.Err)
		}
		t.Fatalf("scenario %v", s.Status)
	}
}
