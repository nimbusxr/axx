package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/runner"
	"github.com/nimbusxr/axx/packs/rest"
)

// Two shops sign in to the portal at the same time. Each one's sign-in sends
// the shop's cookie, and the portal answers with a session cookie and a 303
// to the shop's parcels, which the pack follows. The last request sends no
// cookie at all.
const sessionsFeature = `Feature: portal sessions

  Background:
    Given the portal service with the following properties:
      | url | ${sys:portal.url} |

  Scenario: kestrel-books signs in
    Given a POST request to /portal/sign-in?shop=kestrel-books
    And the request header Cookie is 'shop=kestrel-books'
    When the request is executed
    Then the response status code is 200
    Given a 2nd ordered GET request to /portal/parcels?shop=kestrel-books
    When the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200

  Scenario: lark-ceramics signs in
    Given a POST request to /portal/sign-in?shop=lark-ceramics
    And the request header Cookie is 'shop=lark-ceramics'
    When the request is executed
    Then the response status code is 200
    Given a 2nd ordered GET request to /portal/parcels?shop=lark-ceramics
    When the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200
`

// TestParallelScenariosShareNoCookies runs two scenarios in parallel, as
// axx run does, and checks that no request carries a cookie the scenario
// did not set itself: the pack keeps no cookies, in a scenario or across
// scenarios.
func TestParallelScenariosShareNoCookies(t *testing.T) {
	var (
		mu      sync.Mutex
		cookies = map[string][]string{} // shop -> the Cookie headers of its requests, in order
		signIns = make(chan struct{}, 2)
	)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /portal/sign-in", func(w http.ResponseWriter, r *http.Request) {
		shop := r.URL.Query().Get("shop")
		mu.Lock()
		cookies[shop] = append(cookies[shop], strings.Join(r.Header.Values("Cookie"), "; "))
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "session-" + shop, Value: "signed-in", Path: "/portal"})
		// Answer once both shops are signing in: the scenarios overlap.
		signIns <- struct{}{}
		deadline := time.After(10 * time.Second)
		for len(signIns) < 2 {
			select {
			case <-deadline:
				http.Error(w, "the other scenario did not sign in", http.StatusRequestTimeout)
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		http.Redirect(w, r, "/portal/parcels?shop="+shop, http.StatusSeeOther)
	})
	mux.HandleFunc("GET /portal/parcels", func(w http.ResponseWriter, r *http.Request) {
		shop := r.URL.Query().Get("shop")
		mu.Lock()
		cookies[shop] = append(cookies[shop], strings.Join(r.Header.Values("Cookie"), "; "))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", "sessions.feature"), []byte(sessionsFeature), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e, err := engine.New(engine.Options{
		Config: &config.Config{Dir: dir, Properties: map[string]string{"portal.url": srv.URL}},
		Packs:  []engine.NamedPack{{Name: "core", Pack: core.ParamsPack()}, {Name: "rest", Pack: rest.Pack()}},
	})
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
	r, err := runner.New(runner.Options{Registry: e.Registry, Hooks: e.Hooks, Suite: e.Suite, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(ctx, pickles)
	if len(res.Scenarios) != 2 {
		t.Fatalf("ran %d scenarios", len(res.Scenarios))
	}
	for _, s := range res.Scenarios {
		if s.Status != runner.Passed {
			for _, st := range s.Steps {
				t.Logf("%s %s: %v", st.Status, st.Text, st.Err)
			}
			t.Fatalf("%s: %v", s.Pickle.Name, s.Status)
		}
	}
	if res.Scenarios[0].Worker == res.Scenarios[1].Worker {
		t.Fatalf("both scenarios ran on worker %d", res.Scenarios[0].Worker)
	}

	// Each shop's sign-in and the page it leads to carry the shop's own
	// cookie, never the session cookie the portal set nor the other shop's;
	// the next request carries none.
	for _, shop := range []string{"kestrel-books", "lark-ceramics"} {
		want := []string{"shop=" + shop, "shop=" + shop, ""}
		if got := cookies[shop]; !slices.Equal(got, want) {
			t.Errorf("%s: the portal got the cookies %q, want %q", shop, got, want)
		}
	}
}
