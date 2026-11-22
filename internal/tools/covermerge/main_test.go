// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

const unit = `mode: atomic
github.com/nimbusxr/axx/core/value.go:10.2,12.3 2 5
github.com/nimbusxr/axx/core/value.go:14.2,20.3 4 0
github.com/nimbusxr/axx/packs/sql/sql.go:30.2,31.3 1 0
github.com/nimbusxr/axx/packs/web/core/browser.go:5.2,9.3 3 0
`

// Another job's: it ran what the first did not, and knows a file the first never built.
const integration = `mode: atomic
github.com/nimbusxr/axx/core/value.go:10.2,12.3 2 0
github.com/nimbusxr/axx/core/value.go:14.2,20.3 4 0
github.com/nimbusxr/axx/packs/sql/sql.go:30.2,31.3 1 7
github.com/nimbusxr/axx/packs/web/core/browser.go:5.2,9.3 3 2
github.com/nimbusxr/axx/internal/cli/run_windows.go:8.2,9.3 10 0
github.com/nimbusxr/axx/examples/parcels/app/shipments/main.go:4.2,9.3 50 3
`

func merged(t *testing.T, profiles ...string) map[string]block {
	t.Helper()
	blocks := map[string]block{}
	for _, p := range profiles {
		if err := merge(blocks, strings.NewReader(p)); err != nil {
			t.Fatal(err)
		}
	}
	return blocks
}

func TestAStatementIsCoveredWhenAnyJobRanIt(t *testing.T) {
	var out strings.Builder
	got := report(&out, merged(t, unit, integration), 2)
	// 2 + 1 + 3 of 2 + 4 + 1 + 3 + 10.
	if want := 100 * 6.0 / 20.0; got != want {
		t.Errorf("the whole is %.2f%%, want %.2f%%", got, want)
	}
	text := out.String()
	for _, want := range []string{
		"30.0% of statements: 6 of 20, from 2 profiles.",
		"| internal/cli | 10 | 0.0% | 10 |",
		"| core | 6 | 33.3% | 4 |",
		"| packs/sql | 1 | 100.0% | 0 |",
		"| packs/web/core | 3 | 100.0% | 0 |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
	// The parts that lack the most come first.
	if strings.Index(text, "| internal/cli |") > strings.Index(text, "| core |") {
		t.Errorf("internal/cli lacks more than core, and comes after it:\n%s", text)
	}
}

func TestTheMergedProfileIsOneGoReads(t *testing.T) {
	got := profile(merged(t, unit, integration))
	want := `mode: set
github.com/nimbusxr/axx/core/value.go:10.2,12.3 2 1
github.com/nimbusxr/axx/core/value.go:14.2,20.3 4 0
github.com/nimbusxr/axx/internal/cli/run_windows.go:8.2,9.3 10 0
github.com/nimbusxr/axx/packs/sql/sql.go:30.2,31.3 1 1
github.com/nimbusxr/axx/packs/web/core/browser.go:5.2,9.3 3 1
`
	if got != want {
		t.Errorf("merged profile:\n%s\nwant:\n%s", got, want)
	}
}

func TestParts(t *testing.T) {
	for file, want := range map[string]string{
		"github.com/nimbusxr/axx/core/value.go":                         "core",
		"github.com/nimbusxr/axx/fixtures/fixtures.go":                  "fixtures",
		"github.com/nimbusxr/axx/internal/cli/run.go":                   "internal/cli",
		"github.com/nimbusxr/axx/internal/cli/sub/x.go":                 "internal/cli",
		"github.com/nimbusxr/axx/packs/sql/internal/h2/h2.go":           "packs/sql",
		"github.com/nimbusxr/axx/packs/web/core/browser.go":             "packs/web/core",
		"github.com/nimbusxr/axx/packs/models/core/format/onnx/onnx.go": "packs/models/core",
		"github.com/nimbusxr/axx/packs/internal/video/video.go":         "packs/internal",
		"github.com/nimbusxr/axx/cmd/axx/main.go":                       "cmd",
	} {
		if got := part(file); got != want {
			t.Errorf("part(%s) = %s, want %s", file, got, want)
		}
	}
}

func TestWhatIsNoProfileIsRefused(t *testing.T) {
	for _, text := range []string{"mode: set\nnot a profile's line\n", "mode: set\nfile.go:1.1,2.2 x 1\n", "mode: set\nfile.go:1.1,2.2 3 -1\n"} {
		if err := merge(map[string]block{}, strings.NewReader(text)); err == nil {
			t.Errorf("%q was read as a profile", text)
		}
	}
}
