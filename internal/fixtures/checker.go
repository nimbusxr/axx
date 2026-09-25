package fixtures

import (
	"bytes"
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
}

// NewChecker loads the specs; errors here are expansion-independent (spec
// files, configuration, a hand-edited pairings lock).
func NewChecker(cfg Config, opts Options) (*Checker, error) {
	g, err := NewGenerator(cfg, opts)
	if err != nil {
		return nil, err
	}
	return &Checker{gen: g}, nil
}

// Checks lists every check: drift per produced file, the manifest, and
// conformance per rule-matched file.
func (c *Checker) Checks() ([]Check, error) {
	g := c.gen
	x, err := g.Expand(ModeVerify)
	if err != nil {
		return nil, err
	}
	var checks []Check
	for _, rel := range x.Paths() {
		expected := x.Files[rel]
		checks = append(checks, Check{Name: "drift: " + rel, run: func() error { return c.drift(rel, expected) }})
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
