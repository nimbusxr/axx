package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// project is a temporary suite: axx.yaml at the root, data files below.
type project struct {
	t   *testing.T
	dir string
}

func newProject(t *testing.T) *project {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &project{t: t, dir: dir}
}

func (p *project) write(name, content string) *project {
	p.t.Helper()
	path := filepath.Join(p.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
	return p
}

// config loads axx.yaml with the given lint section (YAML, unindented).
func (p *project) config(lintYAML string) (*config.Config, error) {
	p.t.Helper()
	body := "version: 1\n"
	if lintYAML != "" {
		body += "lint:\n" + indent(lintYAML, "  ")
	}
	p.write("axx.yaml", body)
	return config.Load(config.LoadOptions{Path: filepath.Join(p.dir, "axx.yaml"), WorkDir: p.dir, LookupEnv: func(string) (string, bool) { return "", false }})
}

func (p *project) load(lintYAML string) (*Set, error) {
	p.t.Helper()
	cfg, err := p.config(lintYAML)
	if err != nil {
		p.t.Fatalf("config: %v", err)
	}
	return Load(cfg)
}

func (p *project) run(lintYAML string, opts ...Options) *Report {
	p.t.Helper()
	set, err := p.load(lintYAML)
	if err != nil {
		p.t.Fatalf("load: %v", err)
	}
	o := Options{WorkDir: p.dir}
	if len(opts) > 0 {
		o = opts[0]
		if o.WorkDir == "" {
			o.WorkDir = p.dir
		}
	}
	return set.Run(o)
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// locations renders every duplicate as `value: file:line, file:line`.
func locations(rep *Report) []string {
	var out []string
	for _, rr := range rep.Rules {
		for _, f := range rr.Findings {
			var ls []string
			for _, l := range f.Locations {
				ls = append(ls, fmt.Sprintf("%s:%d", l.File, l.Line))
			}
			out = append(out, f.Code+" "+f.Value+": "+strings.Join(ls, ", "))
		}
	}
	return out
}

func codeOf(err error) string {
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

const ruleTemplate = `config:
  baseDir: data
  mode: %s
rules:
  - name: unique-ids
    filePatterns: ["*.yaml"]
    regex: 'id: (\S+)'
    validation: %s
`

// Uniqueness, modes and ignore lists.
func TestValidations(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		mode       string
		validation string
		want       []string // duplicates as "code value: file:line, ..."
		ok         bool
	}{
		{
			name:  "global-unique value repeats across files",
			files: map[string]string{"one.yaml": "id: order-1\n", "two.yaml": "name: x\nid: order-1\n"},
			mode:  "error", validation: GlobalUnique,
			want: []string{"AXX-E0820 order-1: data/one.yaml:1, data/two.yaml:2"},
		},
		{
			name:  "global-unique value repeats within one file",
			files: map[string]string{"one.yaml": "id: a\nid: a\n"},
			mode:  "error", validation: GlobalUnique,
			want: []string{"AXX-E0820 a: data/one.yaml:1, data/one.yaml:2"},
		},
		{
			name:  "all values unique",
			files: map[string]string{"one.yaml": "id: a\nid: b\n", "two.yaml": "id: c\n"},
			mode:  "error", validation: GlobalUnique, ok: true,
		},
		{
			name:  "file-unique value repeats within a file",
			files: map[string]string{"one.yaml": "id: a\nid: a\n", "two.yaml": "id: a\n"},
			mode:  "error", validation: FileUnique,
			want: []string{"AXX-E0820 a: data/one.yaml:1, data/one.yaml:2"},
		},
		{
			name:  "file-unique value repeats only across files",
			files: map[string]string{"one.yaml": "id: a\n", "two.yaml": "id: a\n"},
			mode:  "error", validation: FileUnique, ok: true,
		},
		{
			name:  "cross-file-unique value in two files",
			files: map[string]string{"one.yaml": "id: a\nid: a\n", "two.yaml": "id: a\n"},
			mode:  "error", validation: CrossFileUnique,
			want: []string{"AXX-E0820 a: data/one.yaml:1, data/one.yaml:2, data/two.yaml:1"},
		},
		{
			name:  "cross-file-unique value repeats only within one file",
			files: map[string]string{"one.yaml": "id: a\nid: a\n"},
			mode:  "error", validation: CrossFileUnique, ok: true,
		},
		{
			name:  "warn mode reports but passes",
			files: map[string]string{"one.yaml": "id: a\nid: a\n"},
			mode:  "warn", validation: GlobalUnique, ok: true,
			want: []string{"AXX-E0820 a: data/one.yaml:1, data/one.yaml:2"},
		},
		{
			name:  "line numbers are exact deep in a file",
			files: map[string]string{"one.yaml": "# header\n# comment\nid: a\n# more\nid: a\n"},
			mode:  "error", validation: GlobalUnique,
			want: []string{"AXX-E0820 a: data/one.yaml:3, data/one.yaml:5"},
		},
		{
			name:  "binary files are skipped",
			files: map[string]string{"blob.yaml": "id: a\x00id: a"},
			mode:  "error", validation: GlobalUnique, ok: true,
		},
		{
			name:  "files in subdirectories match a bare file-name glob",
			files: map[string]string{"one.yaml": "id: a\n", "deep/er/two.yaml": "id: a\n"},
			mode:  "error", validation: CrossFileUnique,
			want: []string{"AXX-E0820 a: data/deep/er/two.yaml:1, data/one.yaml:1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProject(t)
			for name, content := range tt.files {
				p.write("data/"+name, content)
			}
			rep := p.run(fmt.Sprintf(ruleTemplate, tt.mode, tt.validation))
			if got := locations(rep); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			if rep.OK() != tt.ok {
				t.Errorf("ok = %v, want %v", rep.OK(), tt.ok)
			}
		})
	}
}

func TestModesAndIgnoreValues(t *testing.T) {
	p := newProject(t).write("data/one.yaml", "id: shared\nid: shared\nid: unique-1\nid: 7\nid: 7\n")
	rep := p.run(`config: {baseDir: data, mode: error}
rules:
  - name: rule-warn
    filePatterns: ["*.yaml"]
    regex: 'id: (\S+)'
    mode: warn
  - name: ignored
    filePatterns: ["*.yaml"]
    regex: 'id: (\S+)'
    ignoreValues: [shared, 7]
`)
	if !rep.OK() || rep.Summary.Warnings != 2 || rep.Summary.Errors != 0 {
		t.Fatalf("rule-level warn overrides the global mode: %+v", rep.Summary)
	}
	if n := len(rep.Rules[1].Findings); n != 0 {
		t.Errorf("ignoreValues (strings and numbers) must suppress: %+v", rep.Rules[1].Findings)
	}
	if rep.Rules[0].Findings[0].Severity != SeverityWarning || rep.Rules[0].Mode != ModeWarn {
		t.Errorf("rule mode: %+v", rep.Rules[0])
	}

	// --mode overrides every rule, including per-rule modes.
	set, err := p.load(`config: {baseDir: data, mode: warn}
rules:
  - {name: r, filePatterns: ["*.yaml"], regex: 'id: (\S+)', mode: warn}
`)
	if err != nil {
		t.Fatal(err)
	}
	if rep := set.Run(Options{WorkDir: p.dir, Mode: ModeError}); rep.OK() {
		t.Error("--mode error must turn warn rules into errors")
	}
	if rep := set.Run(Options{WorkDir: p.dir}); !rep.OK() {
		t.Error("warn rules pass")
	}
}

func TestFileHandling(t *testing.T) {
	p := newProject(t).
		write("data/big.yaml", "id: a\nid: a\n"+strings.Repeat("#", 200)).
		write("data/small.yaml", "id: b\nid: b\n").
		write("data/many.yaml", "id: c\nid: c\nid: c\nid: d\nid: d\nid: e\nid: e\n")
	rep := p.run(`config:
  baseDir: data
  maxFileSize: 100
  maxReportedValues: 2
  maxReportedLocations: 2
rules:
  - name: unique-ids
    filePatterns: ["*.yaml"]
    regex: 'id: (\S+)'
`)
	got := locations(rep)
	want := []string{
		"AXX-E0812 : data/big.yaml:0",
		"AXX-E0820 c: data/many.yaml:1, data/many.yaml:2, data/many.yaml:3",
		"AXX-E0820 d: data/many.yaml:4, data/many.yaml:5",
		"AXX-E0820 e: data/many.yaml:6, data/many.yaml:7",
		"AXX-E0820 b: data/small.yaml:1, data/small.yaml:2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if f := rep.Rules[0].Findings[0]; f.Severity != SeverityWarning || !strings.Contains(f.Message, "exceeds lint.config.maxFileSize 100") {
		t.Errorf("oversized files are skipped with a warning: %+v", f)
	}
	var b strings.Builder
	if err := WriteHuman(&b, rep, HumanOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"... and 3 more findings", "... and 1 more occurrence\n"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("human output should truncate (%q):\n%s", want, b.String())
		}
	}
}

func TestRegexSemantics(t *testing.T) {
	p := newProject(t).
		write("data/a.txt", "  \n  - id: \"x-1\"\n  - ref: \"x-2\"\n\tname=\"é-3\" id: \"x-4\"\n").
		write("data/b.txt", "  - id: \"x-1\"\n  - ref: \"x-2\"\nname=\"é-3\"\nid: \"\"\nid: \"\"\n")
	// First participating group wins; MULTILINE ^; \s+ may start on an earlier
	// blank line, but the location is the captured value's line and column.
	rep := p.run(`config: {baseDir: data}
rules:
  - name: ids
    filePatterns: ["*.txt"]
    regex: '^\s+-?\s*(?:id|ref):\s*"([^"]*)"|name="([^"]+)"|^id: "([^"]*)"'
`)
	var got []string
	for _, f := range rep.Rules[0].Findings {
		var ls []string
		for _, l := range f.Locations {
			ls = append(ls, l.String())
		}
		got = append(got, fmt.Sprintf("%q %s", f.Value, strings.Join(ls, " ")))
	}
	want := []string{
		`"x-1" data/a.txt:2:10 data/b.txt:1:10`,
		`"x-2" data/a.txt:3:11 data/b.txt:2:11`,
		`"é-3" data/a.txt:4:8 data/b.txt:3:7`,
		`"" data/b.txt:4:6 data/b.txt:5:6`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if l := rep.Rules[0].Findings[0].Locations[0]; l.Text != `- id: "x-1"` {
		t.Errorf("location text is the trimmed line: %q", l.Text)
	}
}

// Matching files.
func TestFindFiles(t *testing.T) {
	p := newProject(t)
	base := filepath.Join(p.dir, "acceptance/src/test/resources")
	p.write("acceptance/src/test/resources/features/sale/seeds/order.yaml", "id: a\n").
		write("acceptance/src/test/resources/features/sale/seeds/order.fixture.yaml", "id: a\n").
		write("acceptance/src/test/resources/top.yaml", "id: b\n").
		write("acceptance/src/test/resources/.git/config.yaml", "x").
		write("infra/wiremock/aurus/__files/ok.json", "{}").
		write("infra/wiremock/aurus/mappings/stubs.json", "{}")
	tests := []struct {
		patterns []string
		want     []string
	}{
		{[]string{"../../../../infra/wiremock/aurus/__files/*.json"}, []string{"../../../../infra/wiremock/aurus/__files/ok.json"}},
		{[]string{"../../../../infra/wiremock/**/*.json"}, []string{"../../../../infra/wiremock/aurus/__files/ok.json", "../../../../infra/wiremock/aurus/mappings/stubs.json"}},
		{[]string{"../../../../infra/wiremock/aurus/mappings/stubs.json"}, []string{"../../../../infra/wiremock/aurus/mappings/stubs.json"}},
		{[]string{"features/**/seeds/*.yaml"}, []string{"features/sale/seeds/order.fixture.yaml", "features/sale/seeds/order.yaml"}},
		{[]string{"*.yaml"}, []string{"features/sale/seeds/order.fixture.yaml", "features/sale/seeds/order.yaml", "top.yaml"}},
		{[]string{"*.yaml", "top.yaml", "features/sale/seeds/*.yaml"}, []string{"features/sale/seeds/order.fixture.yaml", "features/sale/seeds/order.yaml", "top.yaml"}},
		{[]string{"../does-not-exist/**/*.json"}, nil},
		{[]string{"**/seeds/order.{yaml,json}"}, []string{"features/sale/seeds/order.yaml"}},
		{[]string{"features/sale/seeds/order.[!f]*"}, []string{"features/sale/seeds/order.yaml"}},
		{[]string{"**.fixture.yaml"}, []string{"features/sale/seeds/order.fixture.yaml"}},
		{[]string{"*/seeds/*.yaml"}, []string{"features/sale/seeds/order.fixture.yaml", "features/sale/seeds/order.yaml"}}, // matched against path tails
		{[]string{"sale/seeds/*.yaml"}, nil},                                                                               // the static prefix is a directory under baseDir
		{[]string{"resources/top.yaml"}, nil},                                                                              // likewise
	}
	for _, tt := range tests {
		var fps []*filePattern
		for _, s := range tt.patterns {
			fp, err := compileFilePattern(s)
			if err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			fps = append(fps, fp)
		}
		files, err := findFiles(base, fps)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, f := range files {
			got = append(got, relSlash(base, f))
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%v: got %v, want %v", tt.patterns, got, tt.want)
		}
	}
}

func TestGlobToRegex(t *testing.T) {
	tests := []struct{ glob, regex, err string }{
		{glob: "*.json", regex: `^[^/]*\.json$`},
		{glob: "**/*.json", regex: `^.*/[^/]*\.json$`},
		{glob: "**.fixture.yaml", regex: `^.*\.fixture\.yaml$`},
		{glob: "a?c", regex: `^a[^/]c$`},
		{glob: "{a,b}.json", regex: `^(?:(?:a)|(?:b))\.json$`},
		{glob: "x,y}", regex: `^x,y}$`},
		{glob: "[abc]", regex: `^[[^/]&&[abc]]$`},
		{glob: "[!a-c]", regex: `^[[^/]&&[^a-c]]$`},
		{glob: "[^a]", regex: `^[[^/]&&[\^a]]$`},
		{glob: "[-a]", regex: `^[[^/]&&[-a]]$`},
		{glob: `a\*b`, regex: `^a\*b$`},
		{glob: "(x)+|$", regex: `^\(x\)\+\|\$$`},
		{glob: "{a,{b}}", err: "cannot nest groups"},
		{glob: "{a,b", err: "missing '}'"},
		{glob: "[ab", err: "missing ']'"},
		{glob: "[a/b]", err: "explicit 'name separator' in class"},
		{glob: "[z-a]", err: "invalid range"},
		{glob: `a\`, err: "no character to escape"},
	}
	for _, tt := range tests {
		got, err := globToRegex(tt.glob)
		switch {
		case tt.err != "":
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s: err = %v, want %q", tt.glob, err, tt.err)
			}
		case err != nil:
			t.Errorf("%s: %v", tt.glob, err)
		case got != tt.regex:
			t.Errorf("%s: %s, want %s", tt.glob, got, tt.regex)
		}
	}
	m, err := compileGlob("[!.]*.{yml,yaml}")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{"a.yaml": true, "b.yml": true, ".hidden.yaml": false, "d/a.yaml": false} {
		if m.match(path) != want {
			t.Errorf("match(%s) = %v", path, !want)
		}
	}
}

// excludePatterns.
func TestExcludePatterns(t *testing.T) {
	p := newProject(t).
		write("data/foo.yaml", "id: \"same-1\"\n").
		write("data/foo.fixture.yaml", "id: \"same-1\"\n").
		write("data/sub/bar.yaml", "id: \"same-2\"\n").
		write("data/sub/bar.fixture.yaml", "id: \"same-2\"\n").
		write("infra/__files/body.json", `{"id": "same-3"}`).
		write("infra/__files/body.fixture.yaml", "data:\n  id: \"same-3\"\n")
	rep := p.run(`config: {baseDir: data}
rules:
  - name: ids
    filePatterns: ["*.yaml", "../infra/__files/*.json", "../infra/__files/*.yaml"]
    excludePatterns: ["**.fixture.yaml"]
    regex: 'id.{2,4}"([a-z0-9-]+)"'
    validation: cross-file-unique
`)
	if !rep.OK() || rep.Rules[0].Files != 3 {
		t.Fatalf("fixture sources (also outside baseDir) must be excluded: files=%d %v", rep.Rules[0].Files, locations(rep))
	}
}

// JSONPath rules, with Jayway paths.
func TestJSONPathRules(t *testing.T) {
	cfg := func(path, validation string, extra ...string) string {
		return fmt.Sprintf(`config: {baseDir: data}
rules:
  - name: structural-ids
    filePatterns: ["*.json"]
    type: jsonpath
    jsonPath: %q
    validation: %s
%s`, path, validation, strings.Join(extra, "\n"))
	}
	tests := []struct {
		name   string
		files  map[string]string
		config string
		want   []string
	}{
		{
			name:   "duplicates across files carry exact line numbers",
			files:  map[string]string{"a.json": "{\n  \"order\": {\n    \"id\": \"ord-1\"\n  }\n}\n", "b.json": "{\n  \"order\": {\n    \"id\": \"ord-1\"\n  }\n}\n"},
			config: cfg("order.id", GlobalUnique),
			want:   []string{"AXX-E0820 ord-1: data/a.json:3, data/b.json:3"},
		},
		{
			name: "same-named key at another depth never matches",
			files: map[string]string{
				"a.json": `{"order": {"id": "ord-a"}, "metadata": {"id": "shared"}}`,
				"b.json": `{"order": {"id": "ord-b"}, "metadata": {"id": "shared"}}`,
			},
			config: cfg("order.id", GlobalUnique),
		},
		{
			name: "[*] matches every element",
			files: map[string]string{
				"a.json": `{"payments": [{"id": "p-1"}, {"id": "p-dup"}]}`,
				"b.json": `{"payments": [{"id": "p-2"}, {"id": "p-dup"}]}`,
			},
			config: cfg("payments[*].id", GlobalUnique),
			want:   []string{"AXX-E0820 p-dup: data/a.json:1, data/b.json:1"},
		},
		{
			name: "a fixed index matches only its element",
			files: map[string]string{
				"a.json": `{"payments": [{"id": "p-1"}, {"id": "p-dup"}]}`,
				"b.json": `{"payments": [{"id": "p-2"}, {"id": "p-dup"}]}`,
			},
			config: cfg("payments[0].id", GlobalUnique),
		},
		{
			name:   "a plain field does not match array elements",
			files:  map[string]string{"a.json": `{"payments": [{"id": "p-dup"}]}`, "b.json": `{"payments": [{"id": "p-dup"}]}`},
			config: cfg("payments.id", GlobalUnique),
		},
		{
			name:   "file-unique allows cross-file reuse but not repeats in a file",
			files:  map[string]string{"a.json": `{"payments": [{"id": "p-1"}, {"id": "p-1"}]}`, "b.json": `{"payments": [{"id": "p-1"}]}`},
			config: cfg("payments[*].id", FileUnique),
			want:   []string{"AXX-E0820 p-1: data/a.json:1, data/a.json:1"},
		},
		{
			name:   "cross-file-unique allows repeats within a file",
			files:  map[string]string{"a.json": `{"payments": [{"id": "p-1"}, {"id": "p-1"}]}`},
			config: cfg("payments[*].id", CrossFileUnique),
		},
		{
			name:   "cross-file-unique rejects reuse across files",
			files:  map[string]string{"a.json": `{"payments": [{"id": "p-1"}, {"id": "p-1"}]}`, "b.json": `{"payments": [{"id": "p-1"}]}`},
			config: cfg("payments[*].id", CrossFileUnique),
			want:   []string{"AXX-E0820 p-1: data/a.json:1, data/a.json:1, data/b.json:1"},
		},
		{
			name:   "ignoreValues",
			files:  map[string]string{"a.json": `{"order": {"id": "shared"}}`, "b.json": `{"order": {"id": "shared"}}`},
			config: cfg("order.id", GlobalUnique, `    ignoreValues: ["shared"]`),
		},
		{
			name:   "numbers and booleans participate as written",
			files:  map[string]string{"a.json": `{"order": {"id": 12345, "ok": true, "n": null}}`, "b.json": "{\"order\": {\"id\": 12345}}\n{\"order\": {\"id\": 1.50}}", "c.json": `{"order": {"id": 1.5}}`},
			config: cfg("order.id", GlobalUnique),
			want:   []string{"AXX-E0820 12345: data/a.json:1, data/b.json:1"},
		},
		{
			name:   "$. prefix",
			files:  map[string]string{"a.json": `{"order": {"id": "dup"}}`, "b.json": `{"order": {"id": "dup"}}`},
			config: cfg("$.order.id", GlobalUnique),
			want:   []string{"AXX-E0820 dup: data/a.json:1, data/b.json:1"},
		},
		{
			name:   "root arrays never match the subset",
			files:  map[string]string{"a.json": `[{"id": "x"}, {"id": "x"}]`},
			config: cfg("id", GlobalUnique),
		},
		{
			name: "deep scan through the Jayway port keeps line numbers",
			files: map[string]string{
				"a.json": "{\n  \"a\": {\"id\": \"x\"},\n  \"list\": [\n    {\"id\": \"x\"}\n  ]\n}",
				"b.json": "{\"id\": \"y\"}",
			},
			config: cfg("$..id", GlobalUnique),
			want:   []string{"AXX-E0820 x: data/a.json:2, data/a.json:4"},
		},
		{
			name: "filters and wildcards through the Jayway port",
			files: map[string]string{
				"a.json": "{\"items\": [\n{\"kind\": \"k\", \"sku\": \"s-1\"},\n{\"kind\": \"z\", \"sku\": \"s-1\"}\n]}",
				"b.json": "{\"items\": [{\"kind\": \"k\", \"sku\": \"s-1\"}]}",
				"c.json": "{\"items\": {\"one\": {\"kind\": \"k\", \"sku\": \"s-1\"}}}",
			},
			config: cfg("$.items[?(@.kind == 'k')].sku", GlobalUnique),
			want:   []string{"AXX-E0820 s-1: data/a.json:2, data/b.json:1"},
		},
		{
			name:   "object wildcard through the Jayway port",
			files:  map[string]string{"a.json": `{"items": {"one": {"sku": "s-1"}, "two": {"sku": "s-1"}}}`},
			config: cfg("items.*.sku", GlobalUnique),
			want:   []string{"AXX-E0820 s-1: data/a.json:1, data/a.json:1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProject(t)
			for name, content := range tt.files {
				p.write("data/"+name, content)
			}
			rep := p.run(tt.config)
			if got := locations(rep); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestJSONPathInvalidFile(t *testing.T) {
	p := newProject(t).write("data/a.json", "{\n  \"order\": {\"id\": 'x'}\n}").write("data/b.json", "not json at all {")
	rep := p.run(`config: {baseDir: data, mode: warn}
rules:
  - {name: s, filePatterns: ["*.json"], type: jsonpath, jsonPath: order.id}
`)
	got := locations(rep)
	if len(got) != 2 || rep.OK() {
		t.Fatalf("a non-JSON file fails the rule even in warn mode: %v", got)
	}
	f := rep.Rules[0].Findings[0]
	if f.Code != CodeUnparsable || f.Locations[0].String() != "data/a.json:2:19" || !strings.Contains(f.Message, "data/a.json") {
		t.Errorf("finding: %+v", f)
	}
	if f := rep.Rules[0].Findings[1]; !strings.Contains(f.Message, "unrecognized token 'not'") {
		t.Errorf("finding: %+v", f)
	}
}

func TestParseJSON(t *testing.T) {
	good := map[string]string{
		"":                                      "",
		"\ufeff {}":                             "",
		`{"a": [1, -2.5e3, true, false, null]}`: "",
		"{\"a\":\"\\u00e9\\ud83d\\ude00\\/\"}":  "",
		"{}{}\n[]":                              "",
		"1 2":                                   "",
		`{"a":1,"a":2}`:                         "",
	}
	bad := map[string]string{
		"{\"a\": 01}":       "invalid number '01'",
		"{\"a\": 1.}":       "invalid number '1.'",
		"{\"a\": -}":        "invalid number '-'",
		"{\"a\": NaN}":      "unrecognized token 'NaN'",
		"{'a': 1}":          "expected a double-quoted field name",
		"{\"a\": 1,}":       "expected a double-quoted field name",
		"[1,]":              "expected a value",
		"// c\n{}":          "comments are not allowed",
		"{\"a\": \"x\ty\"}": "unescaped control character",
		"{\"a\": \"\\x\"}":  "unrecognized character escape",
		"{\"a\": 1":         "unexpected end of input",
		"{\"a\": truex}":    "unrecognized token 'truex'",
		"12{}":              "expected space separating root-level values",
		"{\"a\" 1}":         "expected ':'",
		"[1 2]":             "expected ',' or ']'",
		"{\"a\": \"unterm}": "unexpected end of input in a string",
	}
	for in := range good {
		if _, err := parseJSON(in); err != nil {
			t.Errorf("%q: %v", in, err)
		}
	}
	for in, want := range bad {
		_, err := parseJSON(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", in, err, want)
		}
	}
	roots, _ := parseJSON("{\r\n  \"a\": \"\\u00e9\\ud83d\\ude00\",\r  \"b\": [\n    \"é\", 2]}")
	a, b := roots[0].kids[0], roots[0].kids[1].kids
	if a.text != "é😀" || a.line != 2 || a.col != 8 || b[0].line != 4 || b[0].col != 5 || b[1].col != 10 {
		t.Errorf("positions: a=%+v b0=%+v b1=%+v", a, b[0], b[1])
	}
}

func TestCompileJSONPath(t *testing.T) {
	tests := []struct {
		path   string
		subset bool
		err    string
	}{
		{path: "mission_id", subset: true},
		{path: "$.order.id", subset: true},
		{path: "payments[*].id", subset: true},
		{path: "a[12].b-c.$ref", subset: true},
		{path: "order id.x", subset: true},
		{path: "$..id"},
		{path: "a.*.id"},
		{path: "$['a b'].c"},
		{path: "a[-1]"},
		{path: "items[?(@.k == 1)].id"},
		{path: "$"},
		{path: "$.", err: "at least one field"},
		{path: "a[?(", err: "Could not parse"},
	}
	for _, tt := range tests {
		jp, err := compileJSONPath(tt.path)
		switch {
		case tt.err != "":
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s: err = %v, want %q", tt.path, err, tt.err)
			}
		case err != nil:
			t.Errorf("%s: %v", tt.path, err)
		case (jp.segments != nil) != tt.subset:
			t.Errorf("%s: subset = %v, want %v", tt.path, jp.segments != nil, tt.subset)
		}
	}
}

// Includes.
func TestIncludes(t *testing.T) {
	const generated = `rules:
  - name: generated-rule
    filePatterns: ["*.json"]
    type: jsonpath
    jsonPath: "order.id"
    validation: cross-file-unique
`
	t.Run("included rules follow the including file's rules", func(t *testing.T) {
		p := newProject(t).write("data/a.yaml", "id: one\n").write("axx-lint.generated.yaml", generated)
		set, err := p.load(`config: {baseDir: data}
include: [axx-lint.generated.yaml]
rules:
  - name: hand-written
    filePatterns: ["*.yaml"]
    regex: 'id: (\S+)'
`)
		if err != nil {
			t.Fatal(err)
		}
		if len(set.Rules) != 2 || set.Rules[0].Name != "hand-written" || set.Rules[1].Name != "generated-rule" {
			t.Fatalf("rules: %+v", set.Rules)
		}
		if got := set.Rules[1].Source.String(); got != "axx-lint.generated.yaml:2:9" && !strings.HasSuffix(got, "axx-lint.generated.yaml:2:9") {
			t.Errorf("included rule source = %s", got)
		}
	})
	t.Run("include-only config and nested relative includes", func(t *testing.T) {
		p := newProject(t).
			write("lint/mid.yaml", "include: [deeper/leaf.yaml]\n").
			write("lint/deeper/leaf.yaml", generated)
		set, err := p.load("include: [lint/mid.yaml]\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(set.Rules) != 1 || set.Rules[0].Name != "generated-rule" {
			t.Fatalf("rules: %+v", set.Rules)
		}
	})
	errCases := []struct {
		name  string
		files map[string]string
		lint  string
		code  string
		want  []string
	}{
		{
			name: "missing include names both files",
			lint: "include: [nope.yaml]\nrules:\n  - {name: r, filePatterns: [\"*\"], regex: \"(x)\"}\n",
			code: CodeIncludeMissing, want: []string{"nope.yaml", "axx.yaml", "axx.yaml:3:"},
		},
		{
			name:  "include with a config block",
			files: map[string]string{"gen.yaml": "config: { baseDir: elsewhere }\n" + generated},
			lint:  "include: [gen.yaml]\n",
			code:  CodeIncludeInvalid, want: []string{"config", "gen.yaml:1:"},
		},
		{
			name:  "include cycle",
			files: map[string]string{"a.yaml": "include: [b.yaml]\n", "b.yaml": "include: [a.yaml]\n"},
			lint:  "include: [a.yaml]\n",
			code:  CodeIncludeCycle, want: []string{"cycle", "a.yaml is already loaded", "b.yaml:1:"},
		},
		{
			name:  "include including axx.yaml",
			files: map[string]string{"a.yaml": "include: [axx.yaml]\n"},
			lint:  "include: [a.yaml]\n",
			code:  CodeIncludeCycle, want: []string{"axx.yaml is already loaded"},
		},
		{
			name:  "include with an unknown key",
			files: map[string]string{"a.yaml": "rules:\n  - name: r\n    filePatterns: [x]\n    regexp: '(x)'\n"},
			lint:  "include: [a.yaml]\n",
			code:  CodeIncludeInvalid, want: []string{"a.yaml:2:", "regexp"},
		},
		{
			name:  "include that is not YAML",
			files: map[string]string{"a.yaml": "rules: [\n"},
			lint:  "include: [a.yaml]\n",
			code:  CodeIncludeInvalid, want: []string{"invalid YAML"},
		},
	}
	for _, tt := range errCases {
		t.Run(tt.name, func(t *testing.T) {
			p := newProject(t)
			for n, c := range tt.files {
				p.write(n, c)
			}
			_, err := p.load(tt.lint)
			if codeOf(err) != tt.code {
				t.Fatalf("code = %s (%v), want %s", codeOf(err), err, tt.code)
			}
			msg := err.Error()
			var ae *axxerr.Error
			if errors.As(err, &ae) && ae.Location != nil {
				msg += " @" + ae.Location.String()
			}
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q should mention %q", msg, w)
				}
			}
		})
	}
	t.Run("empty include file contributes nothing", func(t *testing.T) {
		p := newProject(t).write("empty.yaml", "# nothing yet\n").write("data/x", "")
		_, err := p.load("config: {baseDir: data}\ninclude: [empty.yaml]\n")
		if codeOf(err) != CodeNoRules {
			t.Fatalf("an empty lint section is an error: %v", err)
		}
	})
}

func TestConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		lint string
		code string
		want []string
	}{
		{
			name: "jsonpath rule without jsonPath",
			lint: "rules:\n  - name: broken\n    filePatterns: [\"*.json\"]\n    type: jsonpath\n",
			code: CodeInvalidRule, want: []string{"jsonPath", "axx.yaml:6:"},
		},
		{
			name: "regex rule without regex",
			lint: "rules:\n  - name: broken\n    filePatterns: [\"*.json\"]\n",
			code: CodeInvalidRule, want: []string{"regex", "jsonpath"},
		},
		{
			name: "invalid Java regex",
			lint: "rules:\n  - name: broken\n    filePatterns: [\"*.json\"]\n    regex: 'id: ([a-z'\n",
			code: CodeInvalidRule, want: []string{"invalid regex", "Unclosed character class", "axx.yaml:6:"},
		},
		{
			name: "regex without a capture group",
			lint: "rules:\n  - name: broken\n    filePatterns: [\"*.json\"]\n    regex: 'id: \\w+'\n",
			code: CodeInvalidRule, want: []string{"no capture group"},
		},
		{
			name: "invalid glob",
			lint: "rules:\n  - name: broken\n    filePatterns: [\"{a,{b}}\"]\n    regex: '(x)'\n",
			code: CodeInvalidRule, want: []string{"cannot nest groups", "axx.yaml:5:"},
		},
		{
			name: "no filePatterns",
			lint: "rules:\n  - name: broken\n    filePatterns: []\n    regex: '(x)'\n",
			code: CodeInvalidRule, want: []string{"no filePatterns"},
		},
		{
			name: "invalid jsonPath",
			lint: "rules:\n  - name: broken\n    filePatterns: [\"*.json\"]\n    type: jsonpath\n    jsonPath: 'a[?('\n",
			code: CodeInvalidRule, want: []string{"invalid jsonPath"},
		},
		{
			name: "baseDir does not exist",
			lint: "config: {baseDir: nowhere}\nrules:\n  - {name: r, filePatterns: [x], regex: '(x)'}\n",
			code: CodeInvalidRule, want: []string{"nowhere is not a directory", "axx.yaml:3:"},
		},
		{
			name: "several problems are reported together",
			lint: "rules:\n  - {name: a, filePatterns: [x]}\n  - {name: b, filePatterns: [x], regex: '(x'}\n",
			code: CodeInvalidRule, want: []string{"2 problems", `rule "a" defines no regex`, `rule "b": invalid regex`},
		},
		{
			name: "no rules",
			lint: "config: {baseDir: .}\n",
			code: CodeNoRules, want: []string{"defines no rules"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProject(t)
			_, err := p.load(tt.lint)
			if codeOf(err) != tt.code {
				t.Fatalf("code = %s (%v), want %s", codeOf(err), err, tt.code)
			}
			msg := err.Error()
			var ae *axxerr.Error
			if errors.As(err, &ae) && ae.Location != nil {
				msg += " @" + ae.Location.String()
			}
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q should mention %q", msg, w)
				}
			}
		})
	}

	// Schema problems are reported by config.Load with their line.
	p := newProject(t)
	_, err := p.config("rules:\n  - name: r\n    filePatterns: [x]\n    regex: '(x)'\n    validation: globally-unique\n")
	if codeOf(err) != config.CodeInvalid || !strings.Contains(err.Error(), "axx.yaml:7:") {
		t.Errorf("an unknown validation is a schema error with its line: %v", err)
	}
}

func TestNoLintSection(t *testing.T) {
	p := newProject(t)
	set, err := p.load("")
	if err != nil {
		t.Fatal(err)
	}
	rep := set.Run(Options{WorkDir: p.dir})
	if set.Configured || len(rep.Rules) != 0 || !rep.OK() || len(rep.Notes) != 1 {
		t.Fatalf("no lint section: %+v", rep)
	}
}

func TestPathsFilter(t *testing.T) {
	p := newProject(t).
		write("data/a.yaml", "id: x\nid: y\n").
		write("data/b.yaml", "id: x\n").
		write("data/sub/c.yaml", "id: y\n")
	const lint = "config: {baseDir: data}\nrules:\n  - {name: r, filePatterns: ['*.yaml'], regex: 'id: (\\S+)'}\n"
	set, err := p.load(lint)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		paths []string
		want  string
	}{
		{nil, "x,y"},
		{[]string{"data/b.yaml"}, "x"},
		{[]string{filepath.Join(p.dir, "data/sub")}, "y"},
		{[]string{"data"}, "x,y"},
		{[]string{"data/su"}, ""},
	} {
		rep := set.Run(Options{WorkDir: p.dir, Paths: tt.paths})
		var got []string
		for _, f := range rep.Rules[0].Findings {
			got = append(got, f.Value)
		}
		if strings.Join(got, ",") != tt.want || rep.OK() != (tt.want == "") {
			t.Errorf("%v: got %v (ok=%v), want %s", tt.paths, got, rep.OK(), tt.want)
		}
	}
}

func TestRuleIDs(t *testing.T) {
	p := newProject(t)
	set, err := p.load(`rules:
  - {name: "Seed IDs!", filePatterns: [x], regex: '(x)'}
  - {name: "seed ids", filePatterns: [x], regex: '(x)'}
  - {name: "seed-ids-2", filePatterns: [x], regex: '(x)'}
  - {name: "***", filePatterns: [x], regex: '(x)'}
`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range set.Rules {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "seed-ids,seed-ids-2,seed-ids-2-2,rule" {
		t.Errorf("ids = %v", ids)
	}
}
