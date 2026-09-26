//go:build integration

package coverage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// site is the test site: a parcel's tracking page with scripts of its own
// and one of another origin, a quote form bundled with its source map, and
// pages to go to. Its HTML names the other origin, the same server under
// another name.
func site(t *testing.T) string {
	t.Helper()
	var other string
	files := http.FileServer(http.Dir(filepath.Join("testdata", "site")))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".html") {
			files.ServeHTTP(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", "site", filepath.FromSlash(r.URL.Path)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.ReplaceAll(string(b), "OTHER_ORIGIN", other)))
	}))
	t.Cleanup(srv.Close)
	other = strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	return srv.URL
}

// newHarness is a scenario with the test site as the tracking web app.
func newHarness(t *testing.T, url, engine string, cfg Config) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.NewWith(t, map[string]any{Name: cfg}, webcore.Pack(), Pack())
	app(h, url, engine)
	return h
}

func app(h *cloudtest.Harness, url, engine string) {
	h.OK("the tracking web app with the following properties:", [][]string{{"url", url}, {"engine", engine}})
}

// finish ends the scenario and the run, and returns the coverage it wrote.
func finish(t *testing.T, h *cloudtest.Harness) lcov {
	t.Helper()
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if err := h.Suite.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return readLcov(t, filepath.Join(h.Dir, ".axx", "web", "coverage", "lcov.info"))
}

// lcov is a report's files: each line's count, each function's.
type lcov map[string]*record

type record struct {
	lines     map[int]int
	functions map[string]int
}

func readLcov(t *testing.T, path string) lcov {
	t.Helper()
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := lcov{}
	var cur *record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		kind, value, _ := strings.Cut(sc.Text(), ":")
		switch kind {
		case "SF":
			cur = &record{lines: map[int]int{}, functions: map[string]int{}}
			out[value] = cur
		case "DA":
			l, n, _ := strings.Cut(value, ",")
			line, _ := strconv.Atoi(l)
			count, _ := strconv.Atoi(n)
			cur.lines[line] = count
		case "FNDA":
			n, name, _ := strings.Cut(value, ",")
			count, _ := strconv.Atoi(n)
			cur.functions[name] += count
		}
	}
	return out
}

func (l lcov) files() []string {
	var out []string
	for f := range l {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// has checks the counts of lines (line: count) and functions of a file.
func (l lcov) has(t *testing.T, file string, lines map[int]int, functions map[string]int) {
	t.Helper()
	r := l[file]
	if r == nil {
		t.Errorf("no coverage of %s; files: %v", file, l.files())
		return
	}
	for line, want := range lines {
		if got, ok := r.lines[line]; !ok || got != want {
			t.Errorf("%s line %d: ran %d times (known %v), want %d", file, line, got, ok, want)
		}
	}
	for name, want := range functions {
		if got := r.functions[name]; got != want {
			t.Errorf("%s function %s: ran %d times, want %d", file, name, got, want)
		}
	}
}

func TestTheScriptsOfTheAppAreCovered(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{})
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page shows "Enter a reference"`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "PX-1042: In transit, 400 g"`)
	got := finish(t, h)
	// Not the script of another origin, nor the page's inline scripts.
	if files := got.files(); !slices.Equal(files, []string{"js/format.js", "js/track.js"}) {
		t.Fatalf("files: %v", files)
	}
	got.has(t, "js/track.js", map[int]int{2: 1, 9: 1, 13: 0, 14: 0, 16: 1, 20: 0, 26: 0},
		map[string]int{"trackParcel": 1, "reportProblem": 0})
	got.has(t, "js/format.js", map[int]int{5: 1, 7: 0, 9: 0, 11: 0, 17: 1, 19: 0},
		map[string]int{"statusLabel": 1, "formatWeight": 1})
	for _, f := range []string{"coverage-final.json", "coverage-summary.json"} {
		b, err := os.ReadFile(filepath.Join(h.Dir, ".axx", "web", "coverage", f))
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if v["js/track.js"] == nil {
			t.Errorf("%s lacks js/track.js: %s", f, b)
		}
	}
	want := "[AXX-IDE] coverage path=" + filepath.Join(h.Dir, ".axx", "web", "coverage", "lcov.info")
	if !slices.Contains(h.Sink.Lines(), want) {
		t.Errorf("announced %q, want %q", h.Sink.Lines(), want)
	}
}

// What a page runs as it leaves counts: the hook takes it first.
func TestCountsSurviveNavigations(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{})
	h.OK(`the "/track.html" page is opened`)
	// The form's handler runs, then the page goes: in one step.
	h.OK(`the "Report a problem" button is clicked`)
	h.OK(`the page shows "Your claim was sent"`)
	h.OK(`the browser's back button is clicked`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "PX-1042: In transit, 400 g"`)
	h.OK(`the page is reloaded`)
	got := finish(t, h)
	// The tracking page loaded three times.
	got.has(t, "js/track.js", map[int]int{2: 3, 20: 1, 23: 0, 26: 1, 16: 1},
		map[string]int{"trackParcel": 1, "reportProblem": 1})
}

func TestTheScenariosAddUp(t *testing.T) {
	url := site(t)
	h := newHarness(t, url, "chromium", Config{})
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "In transit"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	h.NewScenario()
	app(h, url, "chromium")
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the "Reference" field is filled with "PX-2077"`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "PX-2077: Delivered, 12.5 kg"`)
	got := finish(t, h)
	got.has(t, "js/track.js", map[int]int{2: 2, 16: 2}, map[string]int{"trackParcel": 2})
	got.has(t, "js/format.js", map[int]int{5: 1, 7: 1, 9: 0, 17: 1, 19: 1}, map[string]int{"statusLabel": 2})
}

// Pages of one renderer share its coverage; a tab closes, and a page goes
// to another site and back.
func TestTabsAndSitesShareTheCounts(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{})
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the "Track another parcel" link is clicked`)
	h.OK(`the "/track.html?tab=new" page is shown`)
	h.OK(`the "Reference" field is filled with "PX-3310"`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "PX-3310: Returned to sender, 2.0 kg"`)
	h.OK(`the browser tab is closed`)
	h.OK(`the "/track.html" page is shown`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "PX-1042: In transit, 400 g"`)
	h.OK(`the "Help" link is clicked`)
	h.OK(`the page shows "How can we help?"`)
	h.OK(`the browser's back button is clicked`)
	h.OK(`the "Reference" field is filled with "PX-2077"`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the page shows "PX-2077: Delivered, 12.5 kg"`)
	got := finish(t, h)
	if files := got.files(); !slices.Equal(files, []string{"js/format.js", "js/track.js"}) {
		t.Fatalf("files: %v", files)
	}
	got.has(t, "js/track.js", map[int]int{16: 3}, map[string]int{"trackParcel": 3})
	got.has(t, "js/format.js", map[int]int{5: 1, 7: 1, 9: 1, 11: 0}, map[string]int{"statusLabel": 3})
}

// A bundle counts as its original files, through its source map.
func TestABundleCountsAsItsSources(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{})
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "Weight (grams)" field is filled with "1200"`)
	h.OK(`"France" is chosen in the "Destination country" field`)
	h.OK(`the "Get a quote" button is clicked`)
	h.OK(`the page shows "Price: 14.80 EUR"`)
	got := finish(t, h)
	if files := got.files(); !slices.Equal(files, []string{"src/quote.ts", "src/rates.ts"}) {
		t.Fatalf("files: %v", files)
	}
	got.has(t, "src/quote.ts", map[int]int{10: 0, 14: 1, 19: 0, 21: 0}, nil)
	got.has(t, "src/rates.ts", map[int]int{6: 0, 8: 1, 10: 0, 18: 1}, nil)
}

// With sources, the scripts are files of the project.
func TestSourcesNameTheProjectsFiles(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{Sources: map[string]string{"/js/": "../app/web/js", "/src/": "../app/web/src"}})
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "Get a quote" button is clicked`)
	h.OK(`the page shows "Enter a weight"`)
	got := finish(t, h)
	want := []string{"../app/web/js/format.js", "../app/web/js/track.js", "../app/web/src/quote.ts", "../app/web/src/rates.ts"}
	if files := got.files(); !slices.Equal(files, want) {
		t.Fatalf("files: %v, want %v", files, want)
	}
	got.has(t, "../app/web/src/quote.ts", map[int]int{10: 1, 14: 0}, nil)
}

// Only the scripts under sources count; a source map without the sources'
// content takes them from the project.
func TestOnlyTheSourcesCount(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{Sources: map[string]string{"/src/": "web/src"}})
	for _, f := range []string{"quote.ts", "rates.ts"} {
		b, err := os.ReadFile(filepath.Join("testdata", "site", "src", f))
		if err != nil {
			t.Fatal(err)
		}
		h.File("web/src/"+f, string(b))
	}
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the "/quote-lean.html" page is opened`)
	h.OK(`the "Weight (grams)" field is filled with "1200"`)
	h.OK(`the "Get a quote" button is clicked`)
	h.OK(`the page shows "Price: 6.90 EUR"`)
	got := finish(t, h)
	if files := got.files(); !slices.Equal(files, []string{"web/src/quote.ts", "web/src/rates.ts"}) {
		t.Fatalf("files: %v", files)
	}
	got.has(t, "web/src/rates.ts", map[int]int{6: 1, 8: 0}, nil)
}

// A dialog keeps the page's scripts waiting, and its renderer: the
// coverage waits for the answer, and a scenario that ends with a dialog
// open does not wait for it.
func TestDialogsHoldNothingUp(t *testing.T) {
	h := newHarness(t, site(t), "chromium", Config{})
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "Cancel the quote" button is clicked`)
	h.OK(`the dialog shows "Cancel this quote?"`)
	h.OK(`the dialog is accepted`)
	h.OK(`the page shows "Quote cancelled"`)
	h.OK(`the "Cancel the quote" button is clicked`)
	h.OK(`the dialog shows "Cancel this quote?"`)
	start := time.Now()
	got := finish(t, h)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the run took %s to end", took)
	}
	got.has(t, "src/quote.ts", map[int]int{19: 1}, nil)
}

func TestFirefoxRunsWithoutCoverage(t *testing.T) {
	h := newHarness(t, site(t), "firefox", Config{})
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the "Track" button is clicked`)
	h.OK(`the "/quote.html" page is opened`)
	if got := finish(t, h); got != nil {
		t.Errorf("coverage of firefox: %v", got.files())
	}
	want := "the tracking web app runs in firefox: JavaScript coverage is collected in Chromium, Chrome and Edge only"
	if n := slices.Index(h.Sink.Logs, want); n < 0 || slices.Index(h.Sink.Logs[n+1:], want) >= 0 {
		t.Errorf("logs %q, want %q once", h.Sink.Logs, want)
	}
}
