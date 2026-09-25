package fixtures

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// workspace is a temporary resource root for one test.
type workspace struct {
	t    *testing.T
	dir  string
	cfg  Config
	opts Options
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	dir := t.TempDir()
	return &workspace{t: t, dir: dir, cfg: DefaultConfig(dir)}
}

func corpusDir() string {
	return filepath.Join(repoRoot(), "internal", "fixtures", "testdata", "corpus")
}

// seed copies corpus directories into the workspace root; "x-corpus" copies
// that corpus's content, anything else the directory itself.
func (w *workspace) seed(parts ...string) *workspace {
	w.t.Helper()
	for _, p := range parts {
		src := filepath.Join(corpusDir(), p)
		dst := w.dir
		if !strings.HasSuffix(p, "-corpus") {
			dst = filepath.Join(w.dir, p)
		}
		copyTree(w.t, src, dst)
	}
	return w
}

// seedKafka copies the default resources: the order-payments avro factory
// and its schema.
func (w *workspace) seedKafka() *workspace { return w.seed("kafka", "schemas") }

func (w *workspace) path(rel string) string { return filepath.Join(w.dir, filepath.FromSlash(rel)) }

func (w *workspace) write(rel, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.path(rel)), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(w.path(rel), []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) read(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(w.path(rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

func (w *workspace) exists(rel string) bool {
	_, err := os.Stat(w.path(rel))
	return err == nil
}

func (w *workspace) remove(rel string) {
	w.t.Helper()
	if err := os.Remove(w.path(rel)); err != nil {
		w.t.Fatal(err)
	}
}

// replace rewrites a file, failing when old is absent.
func (w *workspace) replace(rel, old, new string) {
	w.t.Helper()
	s := w.read(rel)
	if !strings.Contains(s, old) {
		w.t.Fatalf("%s does not contain %q:\n%s", rel, old, s)
	}
	w.write(rel, strings.Replace(s, old, new, 1))
}

func (w *workspace) generator() (*Generator, error) { return NewGenerator(w.cfg, w.opts) }

func (w *workspace) mustGenerator() *Generator {
	w.t.Helper()
	g, err := w.generator()
	if err != nil {
		w.t.Fatal(err)
	}
	return g
}

// expand generates every file in verify mode.
func (w *workspace) expand() map[string][]byte {
	w.t.Helper()
	x, err := w.mustGenerator().Expand(ModeVerify)
	if err != nil {
		w.t.Fatal(err)
	}
	return x.Files
}

func (w *workspace) expandErr() error {
	g, err := w.generator()
	if err != nil {
		return err
	}
	_, err = g.Expand(ModeVerify)
	if err == nil {
		return errUnexpectedSuccess
	}
	return err
}

func (w *workspace) generate() *GenerationResult {
	w.t.Helper()
	res, err := w.mustGenerator().Generate()
	if err != nil {
		w.t.Fatal(err)
	}
	return res
}

func (w *workspace) generateErr() error {
	g, err := w.generator()
	if err != nil {
		return err
	}
	if _, err := g.Generate(); err != nil {
		return err
	}
	return errUnexpectedSuccess
}

// check runs the checker and returns the failures as "name\n  message".
func (w *workspace) check() []string {
	w.t.Helper()
	c, err := NewChecker(w.cfg, w.opts)
	if err != nil {
		return []string{"setup\n" + errText(err)}
	}
	_, failures := c.Run()
	out := make([]string, len(failures))
	for i, f := range failures {
		out[i] = f.Check + "\n  " + strings.ReplaceAll(f.Message, "\n", "\n  ")
	}
	return out
}

func (w *workspace) checks() int {
	w.t.Helper()
	c, err := NewChecker(w.cfg, w.opts)
	if err != nil {
		w.t.Fatal(err)
	}
	checks, err := c.Checks()
	if err != nil {
		w.t.Fatal(err)
	}
	return len(checks)
}

func (w *workspace) adopter() *Adopter {
	w.t.Helper()
	a, err := NewAdopter(w.cfg, w.opts)
	if err != nil {
		w.t.Fatal(err)
	}
	return a
}

func (w *workspace) conformance(rules ...ConformanceRule) { w.cfg.Conformance = rules }

func filtered(failures []string, part string) []string {
	var out []string
	for _, f := range failures {
		if strings.Contains(f, part) {
			out = append(out, f)
		}
	}
	return out
}

func mustErrContain(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil || errors.Is(err, errUnexpectedSuccess) {
		t.Fatalf("expected an error containing %q, got none", parts)
	}
	mustContain(t, err.Error(), parts...)
}

func keysOf(files map[string][]byte) []string {
	var out []string
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// jsonOf parses a generated JSON file.
func jsonOf(t *testing.T, data []byte) *jsonx.Object {
	t.Helper()
	v, err := parseJSON(data, "test")
	if err != nil {
		t.Fatal(err)
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		t.Fatalf("not an object: %s", data)
	}
	return o
}

// at walks a parsed JSON value by keys and indices.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			o, _ := v.(*jsonx.Object)
			v = get(o, k)
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func str(v any) string { return valueOf(v) }

// Test expression functions (last4, join and tokenize).

type pureFn struct {
	name string
	fn   func([]string) string
}

func (p pureFn) Name() string                                          { return p.name }
func (pureFn) Version() string                                         { return "1" }
func (pureFn) Recorded() bool                                          { return false }
func (p pureFn) Apply(args []string, _ *InstanceScope) (string, error) { return p.fn(args), nil }

var last4 = pureFn{"last4", func(a []string) string {
	r := []rune(a[0])
	return string(r[max(0, len(r)-4):])
}}

var join = pureFn{"join", func(a []string) string { return strings.Join(a, "|") }}

// tokenize is a recorded function standing in for a service-backed
// tokenizer: it counts invocations and lifecycle calls, and its version is
// mutable to prove version bumps invalidate recorded pairings.
type tokenize struct {
	calls, beforeAll, afterAll atomic.Int32
	version                    string
	failApply                  bool
}

func (t *tokenize) Name() string    { return "tokenize" }
func (t *tokenize) Version() string { return t.version }
func (t *tokenize) Recorded() bool  { return true }
func (t *tokenize) BeforeAll() error {
	t.beforeAll.Add(1)
	return nil
}

func (t *tokenize) AfterAll() error {
	t.afterAll.Add(1)
	return nil
}

func (t *tokenize) Apply(args []string, _ *InstanceScope) (string, error) {
	if t.failApply {
		return "", errString("stack exploded mid-run (test)")
	}
	t.calls.Add(1)
	r := []rune(args[0])
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	variant := "DEFAULT"
	if len(args) > 1 {
		variant = args[1]
	}
	return string(r) + "-" + variant, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// propertiesFamily is a tiny consumer-authored family that renders sorted
// key=value lines, honors options.header and claims declared identities.
type propertiesFamily struct{}

func (propertiesFamily) Name() string { return "properties" }

func (propertiesFamily) Expand(spec *Spec, _ string, ctx *ExpansionContext) (map[string]map[string][]byte, error) {
	out := map[string]map[string][]byte{}
	for _, key := range spec.Fixtures.SortedKeys() {
		tree := deepMerge(spec.Prototype, spec.Fixtures.Get(key).Data)
		for _, id := range spec.Identity {
			v := ctx.identities.derive(id, key)
			if err := ctx.identities.claim(v, spec.SourceName, key, id.Path); err != nil {
				return nil, err
			}
			if err := pathSet(tree, id.Path, v); err != nil {
				return nil, err
			}
		}
		var b strings.Builder
		if h := get(spec.Options, "header"); h != nil {
			b.WriteString("# " + valueOf(h) + "\n")
		}
		flat := map[string]string{}
		var flatten func(prefix string, o *jsonx.Object)
		flatten = func(prefix string, o *jsonx.Object) {
			for _, k := range o.Keys() {
				p := joinPath(prefix, k)
				if m, ok := get(o, k).(*jsonx.Object); ok {
					flatten(p, m)
				} else {
					flat[p] = valueOf(get(o, k))
				}
			}
		}
		flatten("", tree)
		for _, k := range sortedMapKeys(flat) {
			b.WriteString(k + "=" + flat[k] + "\n")
		}
		out[key] = map[string][]byte{key + ".properties": []byte(b.String())}
	}
	return out, nil
}

func (propertiesFamily) Validate(data []byte, _, _, fixtureName string) error {
	if !strings.Contains(string(data), "=") {
		return genError("%s is not a properties file", fixtureName)
	}
	return nil
}

func (propertiesFamily) LintFilePatterns(*Spec) []string       { return nil }
func (propertiesFamily) LintJSONPath(_ *Spec, p string) string { return p }
