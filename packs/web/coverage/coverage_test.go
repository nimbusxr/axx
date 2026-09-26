package coverage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/web/coverage/internal/v8cov"
)

func TestTheManifestIsSettingsAndStepHooks(t *testing.T) {
	m := Pack().Manifest()
	if len(m.Steps) != 0 {
		t.Errorf("%d steps", len(m.Steps))
	}
	for _, h := range m.Hooks {
		if h.Phase != core.BeforeStep && h.Phase != core.AfterStep {
			t.Errorf("hook %s runs %s", h.ID, h.Phase)
		}
	}
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(m.ConfigSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"folder", "sources"} {
		if schema.Properties[p].Description == "" {
			t.Errorf("the schema has no %s", p)
		}
	}
	if len(schema.Properties) != 2 {
		t.Errorf("settings: %v", schema.Properties)
	}
}

func TestSettings(t *testing.T) {
	project := t.TempDir()
	st, err := parseConfig(Config{}, project)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, ".axx", "web", "coverage"); st.folder != want {
		t.Errorf("folder %s, want %s", st.folder, want)
	}
	st, err = parseConfig(Config{Folder: "reports/js", Sources: map[string]string{
		"/portal/":       "../app/web",
		"/portal/js/":    "../app/web/js/",
		"/portal/assets": filepath.Join(project, "assets"),
	}}, project)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "reports", "js"); st.folder != want {
		t.Errorf("folder %s, want %s", st.folder, want)
	}
	var prefixes []string
	for _, sf := range st.sources {
		prefixes = append(prefixes, sf.prefix+"="+sf.folder)
	}
	if want := []string{"/portal/assets=assets", "/portal/js/=../app/web/js", "/portal/=../app/web"}; !slices.Equal(prefixes, want) {
		t.Errorf("sources %v, want %v", prefixes, want)
	}
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{Sources: map[string]string{"portal/js/": "web/js"}}, `packs.web-coverage.sources: "portal/js/" is not the start of a URL path: start it with /`},
		{Config{Sources: map[string]string{"/portal/js/": " "}}, `packs.web-coverage.sources: "/portal/js/" has no folder`},
	} {
		if _, err := parseConfig(c.cfg, project); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.cfg, err, c.want)
		}
	}
}

func TestWhatCountsAndItsName(t *testing.T) {
	all, _ := parseConfig(Config{}, "/work")
	some, _ := parseConfig(Config{Sources: map[string]string{"/portal/js/": "../app/web/js", "/portal/js/vendor/": "../vendor"}}, "/work")
	const origin = "http://localhost:8400"
	for _, c := range []struct {
		st     *settings
		script string
		mapped bool
		counts bool
	}{
		{all, "http://localhost:8400/portal/js/app.js", false, true},
		{all, "http://localhost:8400/other.js?v=2", false, true},
		{all, "http://127.0.0.1:8400/portal/js/app.js", false, false},
		{all, "https://cdn.example.com/chat.js", false, false},
		{all, "blob:http://localhost:8400/5f2c", false, false},
		{all, "data:text/javascript,1", false, false},
		{some, "http://localhost:8400/portal/js/app.js", false, true},
		{some, "http://localhost:8400/portal/app.js", false, false},
		{some, "http://localhost:8400/dist/app.js", true, true},
	} {
		if got := c.st.counts(origin, c.script, c.mapped); got != c.counts {
			t.Errorf("%s (source map %v) counts: %v, want %v", c.script, c.mapped, got, c.counts)
		}
	}
	for _, c := range []struct {
		st        *settings
		path, got string
		ok        bool
	}{
		{all, "/portal/js/app.js", "portal/js/app.js", true},
		{all, "/src/quote.ts", "src/quote.ts", true},
		{all, "/", "", false},
		{some, "/portal/js/app.js", "../app/web/js/app.js", true},
		{some, "/portal/js/parcels/list.js", "../app/web/js/parcels/list.js", true},
		{some, "/portal/js/vendor/lib.js", "../vendor/lib.js", true},
		{some, "/portal/css/app.js", "", false},
	} {
		got, ok := c.st.name(c.path)
		if got != c.got || ok != c.ok {
			t.Errorf("%s: %q %v, want %q %v", c.path, got, ok, c.got, c.ok)
		}
	}
	if o := originOf(mustParse(t, "HTTPS://Portal.example.com:443/x")); o != "https://portal.example.com" {
		t.Errorf("origin %s", o)
	}
}

func TestOriginalsAreReadFromTheSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web", "src", "quote.ts"), []byte("export {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := parseConfig(Config{Sources: map[string]string{"/src/": "web/src"}}, dir)
	if got, err := st.read("/src/quote.ts"); err != nil || got != "export {};\n" {
		t.Errorf("read %q, %v", got, err)
	}
	if _, err := st.read("/src/rates.ts"); err == nil || !strings.Contains(err.Error(), "the project has not web/src/rates.ts") {
		t.Errorf("a missing file: %v", err)
	}
	all, _ := parseConfig(Config{}, dir)
	if _, err := all.read("/src/quote.ts"); err == nil || !strings.Contains(err.Error(), "packs.web-coverage.sources does not say where it is") {
		t.Errorf("without sources: %v", err)
	}
}

func TestInlineSourceMaps(t *testing.T) {
	a := &appCoverage{}
	m := `{"version":3,"sources":["a.ts"],"mappings":"AAAA"}`
	for _, u := range []string{
		"data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(m)),
		"data:application/json;charset=utf-8;base64," + base64.RawStdEncoding.EncodeToString([]byte(m)),
		"data:application/json," + url.PathEscape(m),
	} {
		got, err := a.sourceMap("http://localhost:8400/app.js", u)
		if err != nil || string(got) != m {
			t.Errorf("%s: %q, %v", u, got, err)
		}
	}
	if _, err := a.sourceMap("http://localhost:8400/app.js", "file:///app.js.map"); err == nil {
		t.Error("a file: URL was fetched")
	}
}

func TestTakesThatDoNotFitTheSourceAreLeftOut(t *testing.T) {
	var takes [][]v8cov.Function
	_ = json.Unmarshal([]byte(`[[{"ranges":[{"startOffset":0,"endOffset":10,"count":1}]}],[{"ranges":[{"startOffset":0,"endOffset":12,"count":1}]}]]`), &takes)
	got, times := fitting(takes, []int{1, 3}, 10)
	if len(got) != 1 || !slices.Equal(times, []int{1}) {
		t.Errorf("%v %v", got, times)
	}
	if n := utf16Len("a📦é"); n != 4 {
		t.Errorf("UTF-16 length %d", n)
	}
}

// A run writes its reports when it ends, says so, and tells the IDE.
func TestTheRunWritesItsReports(t *testing.T) {
	dir := t.TempDir()
	var logs bytes.Buffer
	var announced []string
	s := core.NewSuite(core.SuiteOptions{
		ProjectDir: dir, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Announce: func(line string) { announced = append(announced, line) },
	})
	r, err := runFor(s)
	if err != nil {
		t.Fatal(err)
	}
	text := "function track(ref) {\n  if (!ref) {\n    return 'none';\n  }\n  return ref;\n}\ntrack('PX-1042');\n"
	take := `[{"functionName":"","ranges":[{"startOffset":0,"endOffset":93,"count":1}],"isBlockCoverage":true},` +
		`{"functionName":"track","ranges":[{"startOffset":0,"endOffset":74,"count":1},{"startOffset":34,"endOffset":59,"count":0}],"isBlockCoverage":true}]`
	for range 2 {
		r.add(scriptKey{"http://localhost:8400/js/track.js", "h1"}, json.RawMessage(take))
	}
	r.sources["h1"] = &source{text: text, ok: true}
	// A script whose source map is no source map: its own coverage counts.
	r.add(scriptKey{"http://localhost:8400/js/app.js", "h2"}, json.RawMessage(`[{"functionName":"","ranges":[{"startOffset":0,"endOffset":5,"count":1}],"isBlockCoverage":false}]`))
	r.sources["h2"] = &source{text: "go();", ok: true, sourceMap: []byte("<html>")}
	// A script whose source could not be read.
	r.add(scriptKey{"http://localhost:8400/js/gone.js", "h3"}, json.RawMessage(take))
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, ".axx", "web", "coverage")
	lcov, err := os.ReadFile(filepath.Join(folder, "lcov.info"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SF:js/app.js\n", "SF:js/track.js\n", "FNDA:2,track\n", "DA:3,0\n", "DA:4,0\n", "DA:7,2\n"} {
		if !strings.Contains(string(lcov), want) {
			t.Errorf("lcov lacks %q:\n%s", want, lcov)
		}
	}
	for _, f := range []string{"coverage-final.json", "coverage-summary.json"} {
		if _, err := os.Stat(filepath.Join(folder, f)); err != nil {
			t.Error(err)
		}
	}
	for _, want := range []string{
		`msg="the web apps' JavaScript coverage: lines 75%, statements 75%, functions 100%, branches 66.66%, in .axx/web/coverage/lcov.info"`,
		`the source map of http://localhost:8400/js/app.js is not one`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the logs lack %q:\n%s", want, logs.String())
		}
	}
	if want := "[AXX-IDE] coverage path=" + filepath.Join(folder, "lcov.info"); !slices.Equal(announced, []string{want}) {
		t.Errorf("announced %q, want %q", announced, want)
	}
}

func TestARunWithoutCoverageWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := core.NewSuite(core.SuiteOptions{ProjectDir: dir})
	if _, err := runFor(s); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx")); !os.IsNotExist(err) {
		t.Errorf("the run wrote %v", err)
	}
}

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
