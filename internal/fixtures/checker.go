package fixtures

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Check is one verifiable unit: drift of a managed file, the manifest's
// consistency, or conformance of a rule-matched file.
type Check struct {
	Name string
	run  func() error
}

// Failure is a failed check.
type Failure struct {
	Check   string `json:"check"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Checker is the read-only verification that committed fixtures are exactly
// what the factories generate (drift) and that fixture bytes conform to
// their governing schemas (conformance, for managed and unmanaged files).
type Checker struct {
	gen *Generator
	// unmaterialized are the ignored outputs this checkout does not have yet
	// (a fresh clone): `axx fixtures generate` writes them.
	unmaterialized []string
}

// Unmaterialized lists the ignored outputs the last Checks found absent, and
// in step with the committed manifest: a fresh checkout's, which generate
// writes.
func (c *Checker) Unmaterialized() []string { return c.unmaterialized }

// NewChecker loads the specs; errors here are expansion-independent (spec
// files, configuration, a hand-edited pairings lock).
func NewChecker(cfg Config, opts Options) (*Checker, error) {
	g, err := NewGenerator(cfg, opts)
	if err != nil {
		return nil, err
	}
	return &Checker{gen: g}, nil
}

// Checks lists every check: drift per produced file on disk, the manifest,
// and conformance per rule-matched file. An ignored output a checkout lacks
// passes when the committed manifest records what the sources produce (a
// fresh clone), and fails when it records something else (the sources
// changed, and the manifest was not generated again). Committed fixtures
// that are missing make one check.
func (c *Checker) Checks() ([]Check, error) {
	g := c.gen
	x, err := g.Expand(ModeVerify)
	if err != nil {
		return nil, err
	}
	committed, err := LoadManifest(g.baseDir)
	if err != nil {
		return nil, err
	}
	var checks []Check
	var missing, stale []string
	c.unmaterialized = nil
	for _, rel := range x.Paths() {
		expected := x.Files[rel]
		if _, err := os.Stat(filepath.Join(g.baseDir, filepath.FromSlash(rel))); os.IsNotExist(err) {
			switch {
			case !x.ignored(rel):
				missing = append(missing, rel)
			case !inManifest(committed, rel, expected):
				stale = append(stale, rel)
			default:
				c.unmaterialized = append(c.unmaterialized, rel)
			}
			continue
		}
		checks = append(checks, Check{Name: "drift: " + rel, run: func() error { return c.drift(rel, expected) }})
	}
	if len(missing) > 0 {
		checks = append(checks, Check{Name: "missing: committed fixtures", run: func() error {
			return checkError("%s the factories produce %s not on disk:\n%sfix: run `axx fixtures generate`, and commit them",
				plural(len(missing), "committed fixture"), are(len(missing)), listSome(missing, 5))
		}})
	}
	if len(stale) > 0 {
		checks = append(checks, Check{Name: "stale: the manifest", run: func() error {
			return checkError("the sources changed since the manifest was generated: %s would differ from what it records:\n%sfix: run `axx fixtures generate`, and commit the manifest",
				plural(len(stale), "ignored output"), listSome(stale, 5))
		}})
	}
	checks = append(checks, Check{Name: "manifest: consistency", run: func() error { return c.manifest(x) }})
	roots, err := sourceRoots(g.cfg)
	if err != nil {
		return nil, err
	}
	for _, rule := range g.cfg.Conformance {
		files, err := findFiles(g.baseDir, roots, rule.FilePatterns)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if isSourceFile(filepath.Base(file)) {
				continue // factory/prototype/fixture sources are never fixtures themselves
			}
			rel := relPath(g.baseDir, file)
			checks = append(checks, Check{Name: "conformance: " + rel, run: func() error { return c.conform(rule, file, rel) }})
		}
	}
	return checks, nil
}

// Run runs every check, collecting failures instead of stopping at the
// first. An expansion failure is reported as the single failure "expansion".
func (c *Checker) Run() (total int, failures []Failure) {
	checks, err := c.Checks()
	if err != nil {
		return 0, []Failure{{Check: "expansion", Code: codeOf(err), Message: errText(err)}}
	}
	for _, ch := range checks {
		if err := ch.run(); err != nil {
			failures = append(failures, Failure{Check: ch.Name, Code: codeOf(err), Message: errText(err)})
		}
	}
	return len(checks), failures
}

func (c *Checker) drift(rel string, expected []byte) error {
	path := filepath.Join(c.gen.baseDir, filepath.FromSlash(rel))
	committed, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return checkError("managed fixture missing on disk (for ignored outputs this means it has not been materialized yet).\nfix: run `axx fixtures generate`")
	}
	if err != nil {
		return ioError(err, "cannot read %s", path)
	}
	if !bytes.Equal(committed, expected) {
		return checkError("FIXTURE DRIFT: committed file differs from what the factory generates\n%sfix: edit the factory (not the file), then run `axx fixtures generate`",
			firstDifference(committed, expected))
	}
	return nil
}

func (c *Checker) manifest(x *Expansion) error {
	committed, err := LoadManifest(c.gen.baseDir)
	if err != nil {
		return err
	}
	var problems []string
	for _, e := range committed.Entries() {
		if _, ok := x.Files[e.Path]; !ok {
			problems = append(problems, "orphan: manifest lists "+e.Path+" but no factory produces it")
		}
	}
	for _, p := range x.Paths() {
		if !committed.Managed(p) {
			problems = append(problems, "unrecorded: "+p+" is produced but absent from the manifest")
		}
	}
	if len(problems) > 0 {
		return checkError("%s\nfix: run `axx fixtures generate` (or adopt/remove)", strings.Join(problems, "\n"))
	}
	return nil
}

func (c *Checker) conform(rule ConformanceRule, file, rel string) error {
	f, err := c.gen.FamilyByName(rule.SchemaType, "conformance rule '"+rule.Name+"'")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return ioError(err, "cannot read %s", file)
	}
	return f.Validate(data, c.gen.baseDir, rule.SchemaRef, rel)
}

// firstDifference reports the first differing line of committed and
// expected content.
func firstDifference(committed, expected []byte) string {
	cl := strings.Split(string(committed), "\n")
	el := strings.Split(string(expected), "\n")
	n := len(cl)
	if len(el) > n {
		n = len(el)
	}
	for i := 0; i < n; i++ {
		left, right := "<end of file>", "<end of file>"
		if i < len(cl) {
			left = cl[i]
		}
		if i < len(el) {
			right = el[i]
		}
		if left != right {
			return "  line " + itoa(i+1) + ":\n  --- committed: " + javaStrip(left) + "\n  +++ expected:  " + javaStrip(right) + "\n"
		}
	}
	return "  (content identical, byte-level difference: check line endings or encoding)\n"
}

// inManifest reports whether the committed manifest records a file with
// exactly this content.
func inManifest(m *Manifest, rel string, content []byte) bool {
	e, ok := m.Get(rel)
	return ok && e.SHA256 == SHA256(content)
}

// listSome lists up to n paths, one per line, and how many more there are.
func listSome(paths []string, n int) string {
	var b strings.Builder
	for i, p := range paths {
		if i == n {
			fmt.Fprintf(&b, "  and %d more\n", len(paths)-n)
			break
		}
		b.WriteString("  " + p + "\n")
	}
	return b.String()
}

func are(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func plural(n int, one string, many ...string) string {
	if n == 1 {
		return "1 " + one
	}
	if len(many) > 0 {
		return fmt.Sprintf("%d %s", n, many[0])
	}
	return fmt.Sprintf("%d %ss", n, one)
}
