package coveragecore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

func TestConfigSchemaIsJSON(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(configSchema), &v); err != nil {
		t.Fatal(err)
	}
}

// file is a file of n lines, the first ran of them.
func file(path string, n, ran int) *istanbul.FileCoverage {
	fc := &istanbul.FileCoverage{Path: path}
	for l := 1; l <= n; l++ {
		fc.StatementMap = append(fc.StatementMap, istanbul.Span(l, 0, l, 0))
		hits := 0
		if l <= ran {
			hits = 1
		}
		fc.S = append(fc.S, hits)
	}
	return fc
}

func float(f float64) *float64 { return &f }

func TestEvaluate(t *testing.T) {
	data := runData{Parts: []part{
		{Name: "orders", Runtime: "jvm", Measured: istanbul.Measured{Branches: true, Functions: true}, Files: []*istanbul.FileCoverage{file("orders/A.kt", 10, 9)}},
		{Name: "shipments", Runtime: "go", Files: []*istanbul.FileCoverage{file("shipments/main.go", 10, 5)}},
	}}
	cov, err := evaluate(config{Minimum: minimum{Lines: float(70)}, Parts: map[string]partConf{"orders": {Minimum: minimum{Lines: float(95)}}}}, data)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Check != core.CoverageUnder || cov.Totals.Lines.Pct != 70 || cov.Totals.Branches == nil || len(cov.Unmet) != 1 || cov.Unmet[0].Part != "orders" || cov.Unmet[0].Actual != 90 {
		t.Errorf("coverage: %+v, unmet %+v", cov, cov.Unmet)
	}
	if p := cov.Parts[1]; p.Totals.Branches != nil || p.Totals.Functions != nil {
		t.Errorf("Go measures no branches or functions: %+v", p.Totals)
	}

	// No minimum: nothing checked; a partial run or a shard: skipped.
	if cov, _ := evaluate(config{}, data); cov.Check != core.CoverageUnchecked {
		t.Errorf("no minimum: %q", cov.Check)
	}
	for _, d := range []runData{{Parts: data.Parts, Partial: "--tags"}, {Parts: data.Parts, Shard: "1/2"}} {
		if cov, _ := evaluate(config{Minimum: minimum{Lines: float(99)}}, d); cov.Check != core.CoverageSkipped || len(cov.Unmet) != 0 {
			t.Errorf("%+v: %q %+v", d, cov.Check, cov.Unmet)
		}
	}

	// A minimum of what a runtime does not measure is the settings' mistake.
	var ae *axxerr.Error
	for name, cfg := range map[string]config{
		"a part's":  {Parts: map[string]partConf{"shipments": {Minimum: minimum{Branches: float(50)}}}},
		"the run's": {Minimum: minimum{Functions: float(50)}},
	} {
		only := data
		if name == "the run's" {
			only.Parts = data.Parts[1:]
		}
		if _, err := evaluate(cfg, only); !errors.As(err, &ae) || ae.Code != CodeSettings {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPathOf(t *testing.T) {
	project := filepath.FromSlash("/work/shop")
	s := core.CoverageService{Name: "orders", WorkDir: filepath.Join(project, "orders")}
	cfg := config{Parts: map[string]partConf{"orders": {Paths: map[string]string{"/app/": "../orders-src", "/app/vendor/": "vendor"}}}}
	for in, want := range map[string]string{
		"/app/src/main.py":       "/work/orders-src/src/main.py",
		"/app/vendor/lib.py":     "/work/shop/vendor/lib.py",
		"src/main.py":            "/work/shop/orders/src/main.py",
		"/elsewhere/src/main.py": "/elsewhere/src/main.py",
	} {
		if got := cfg.pathOf(project, s, in); got != filepath.FromSlash(want) {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func TestMergeShards(t *testing.T) {
	shard := func(s string, ran int) runData {
		return runData{Version: dataVersion, Axx: "1.0.0", Shard: s, Selected: "abc", Parts: []part{{Name: "orders", Runtime: "go", Files: []*istanbul.FileCoverage{file("orders/main.go", 4, ran)}}}}
	}
	got, err := mergeShards([]runData{shard("1/2", 1), shard("2/2", 3)}, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if s := istanbul.Total(got.Parts[0].Files); got.Shard != "" || len(got.Parts) != 1 || s.Lines.Covered != 3 || s.Lines.Total != 4 {
		t.Errorf("merged: %+v %+v", got, s.Lines)
	}
	other := shard("2/2", 1)
	other.Selected = "xyz"
	older := shard("2/2", 1)
	older.Axx = "0.9.0"
	partial := shard("2/2", 1)
	partial.Partial = "--tags"
	for name, runs := range map[string][]runData{
		"a shard missing":    {shard("1/3", 1), shard("2/3", 1)},
		"a shard twice":      {shard("1/2", 1), shard("1/2", 1)},
		"other scenarios":    {shard("1/2", 1), other},
		"another axx":        {shard("1/2", 1), older},
		"a partial shard":    {shard("1/2", 1), partial},
		"a whole run, twice": {shard("", 1), shard("", 1)},
	} {
		from := make([]string, len(runs))
		for i := range from {
			from[i] = string(rune('a' + i))
		}
		var ae *axxerr.Error
		if _, err := mergeShards(runs, from); !errors.As(err, &ae) || ae.Code != CodeShards {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got, err := mergeShards([]runData{shard("", 2)}, []string{"a"}); err != nil || got.Shard != "" {
		t.Errorf("a whole run: %+v %v", got, err)
	}
}

// Each run keeps its own raw folders and the empty one of unmeasured
// services; earlier runs' go.
func TestKeepOnly(t *testing.T) {
	raw := t.TempDir()
	for _, d := range []string{"old", "current", "unmeasured"} {
		if err := os.MkdirAll(filepath.Join(raw, d, "orders"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	keepOnly(raw, map[string]bool{filepath.Join(raw, "current"): true})
	entries, _ := os.ReadDir(raw)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "current,unmeasured" {
		t.Errorf("left %v", names)
	}
}

func TestSilence(t *testing.T) {
	for want, end := range map[string]core.CoverageEnd{
		"killed it after its stop grace": {Killed: true, Status: "was killed (signal: killed)"},
		"Run it in a container":          {Killed: true, NoStopSignal: true, Status: "exited with code 1"},
		"exited on its own":              {OnItsOwn: true, Status: "exited with code 1"},
		"check how its coverage section": {Status: "exited with code 0"},
	} {
		if got := silence(end); !strings.Contains(got, want) {
			t.Errorf("%+v: %s", end, got)
		}
	}
}
