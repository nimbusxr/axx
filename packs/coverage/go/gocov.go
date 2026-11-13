// Package coveragego is the coverage-go pack: the coverage of Go services, as
// a run with coverage exercises them. A service built with go build -cover
// writes its coverage into the folder GOCOVERDIR names when it exits, which
// the pack sets; axx converts it with its own Go (go tool covdata) and
// names the files of the service's modules. It builds on coverage-core.
package coveragego

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/gotool"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// Name is the pack's name.
const Name = "coverage-go"

func init() { coveragecore.Register(runtime{}) }

// Pack returns the coverage-go pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{coveragecore.Name},
		ConfigSchema: []byte(configSchema), Steps: []core.StepDef{},
	}
}

const packDoc = `Measure the coverage of Go services in a run with coverage (` + "`axx run --coverage`" + `): a service whose coverage section names the runtime ` + "`go`" + `. Its ` + "`coverage.command`" + ` runs a build of it for coverage, ` + "`go build -cover`" + ` (with ` + "`-coverpkg=./...`" + ` to count every package of its module, not only main's), which writes its coverage into the folder ` + "`GOCOVERDIR`" + ` names when it exits: on its stop signal, it must exit as ` + "`main`" + ` returns or ` + "`os.Exit`" + `, not be killed. Axx converts it with its own Go (` + "`go tool covdata`" + `), and names the files of the service's module, found by its ` + "`go.mod`" + ` from the service's ` + "`dir`" + ` up, or of the modules ` + "`parts.<name>.modules`" + ` names. It counts lines and functions; Go's coverage has no branches. It builds on ` + "`coverage-core`" + `.

` + "```yaml" + `
services:
  shipments:
    dir: ../shipments
    command: ./bin/shipments
    shell: true
    coverage:
      runtime: go
      command: go build -cover -coverpkg=./... -o bin/shipments-cover ./cmd/shipments && bin/shipments-cover
` + "```" + `

In a container, its compose file mounts ` + "`${AXX_COVERAGE_DIR}`" + ` at the coverage section's ` + "`mount`" + ` and passes ` + "`GOCOVERDIR`" + ` on, and its image runs the build for coverage.`

// configSchema is the pack's section of axx.yaml, packs.coverage-go.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "parts": {
      "type": "object",
      "description": "Settings of each Go service, by its name in axx.yaml.",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "modules": {"type": "array", "items": {"type": "string"}, "description": "The folders of the Go modules whose files count, relative to the project, each with its go.mod (default: the module of the service's dir)."}
        }
      }
    }
  }
}`

type config struct {
	Parts map[string]struct {
		Modules []string `json:"modules"`
	} `json:"parts"`
}

type runtime struct{}

func (runtime) Name() string { return "go" }

// Measured: Go's coverage counts statements, by block; the pack finds the
// functions they are in. It has no branches.
func (runtime) Measured() istanbul.Measured { return istanbul.Measured{Functions: true} }

// Launch names the folder Go writes its coverage into: inside the
// service's container, for one in a container.
func (runtime) Launch(_ context.Context, call coveragecore.LaunchCall) ([]string, error) {
	dir := call.Service.Dir
	if call.Service.Mount != "" {
		dir = call.Service.Mount
	}
	return []string{"GOCOVERDIR=" + dir}, nil
}

// Convert reads what the service wrote with axx's own Go, and names the
// files of its modules.
func (runtime) Convert(ctx context.Context, call coveragecore.ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	measured := istanbul.Measured{Functions: true}
	if !hasCoverData(call.End.Dir) {
		return nil, measured, nil
	}
	var cfg config
	if err := call.Settings(Name, &cfg); err != nil {
		return nil, measured, err
	}
	modules, err := modulesOf(call.End.CoverageService, cfg.Parts[call.End.Name].Modules, call.Project)
	if err != nil {
		return nil, measured, err
	}
	profile, err := textProfile(ctx, call.End.Dir)
	if err != nil {
		return nil, measured, err
	}
	blocks, err := parseProfile(profile)
	if err != nil {
		return nil, measured, err
	}
	var files []*istanbul.FileCoverage
	for _, imp := range slices.Sorted(func(yield func(string) bool) {
		for k := range blocks {
			if !yield(k) {
				return
			}
		}
	}) {
		file, ok := fileOf(modules, imp)
		if !ok {
			continue // a package of another module: a dependency
		}
		src, err := os.ReadFile(file)
		if err != nil {
			continue // not in the project: the service's build is not of it
		}
		files = append(files, fileCoverage(file, src, blocks[imp]))
	}
	return files, measured, nil
}

// hasCoverData reports whether dir has what a -cover build writes: its
// meta-data files (covmeta.*).
func hasCoverData(dir string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, "covmeta.*"))
	return len(m) > 0
}

// module is a Go module: its path and its folder.
type module struct{ path, dir string }

// modulesOf are the modules whose files count: those the settings name,
// else the module of the service's working directory.
func modulesOf(s core.CoverageService, dirs []string, project string) ([]module, error) {
	if len(dirs) == 0 {
		dir := s.WorkDir
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				dirs = []string{dir}
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return nil, fmt.Errorf("service %s: no go.mod in %s or above: name its module's folder in packs.%s.parts.%s.modules", s.Name, s.WorkDir, Name, s.Name)
			}
			dir = parent
		}
	}
	var out []module
	for _, d := range dirs {
		if !filepath.IsAbs(d) {
			d = filepath.Join(project, d)
		}
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err != nil {
			return nil, fmt.Errorf("service %s: %w", s.Name, err)
		}
		p := modfile.ModulePath(b)
		if p == "" {
			return nil, fmt.Errorf("service %s: %s names no module", s.Name, filepath.Join(d, "go.mod"))
		}
		out = append(out, module{path: p, dir: d})
	}
	// The longest path first: a nested module's files are its own.
	slices.SortFunc(out, func(a, b module) int { return len(b.path) - len(a.path) })
	return out, nil
}

// fileOf is the file of a module an import path names.
func fileOf(modules []module, imp string) (string, bool) {
	for _, m := range modules {
		if rest, ok := strings.CutPrefix(imp, m.path+"/"); ok {
			return filepath.Join(m.dir, filepath.FromSlash(path.Clean(rest))), true
		}
	}
	return "", false
}

// textProfile is the coverage in dir as a text profile, as go test
// -coverprofile writes one, by axx's own Go.
func textProfile(ctx context.Context, dir string) ([]byte, error) {
	tc, err := gotool.Ensure(ctx, gotool.Options{})
	if err != nil {
		return nil, fmt.Errorf("axx's Go converts Go's coverage, and cannot be had: %w", err)
	}
	// Not in dir, which a container may have made its own (root's, on Linux).
	tmp, err := os.MkdirTemp("", "axx-gocover-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "profile.txt")
	cmd := exec.CommandContext(ctx, tc.Go, "tool", "covdata", "textfmt", "-i="+dir, "-o="+out)
	cmd.Env = tc.Env
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go tool covdata textfmt: %w: %s (a service built with a newer Go than axx's: set AXX_GO to it)", err, bytes.TrimSpace(b))
	}
	return os.ReadFile(out)
}

// block is a block of a text profile: its place, statements and count.
type block struct {
	startLine, startCol, endLine, endCol, stmts, count int
}

// parseProfile reads a text profile: "mode: set", then
// "<import path>/<file>.go:<line>.<col>,<line>.<col> <statements> <count>".
func parseProfile(b []byte) (map[string][]block, error) {
	out := map[string][]block{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if n == 1 && strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		name, rest, ok := cutLast(line, ":")
		f := strings.Fields(rest)
		if !ok || len(f) != 3 {
			return nil, fmt.Errorf("the Go coverage profile, line %d: %q", n, line)
		}
		var bl block
		var err error
		if _, err = fmt.Sscanf(f[0], "%d.%d,%d.%d", &bl.startLine, &bl.startCol, &bl.endLine, &bl.endCol); err == nil {
			if bl.stmts, err = strconv.Atoi(f[1]); err == nil {
				bl.count, err = strconv.Atoi(f[2])
			}
		}
		if err != nil {
			return nil, fmt.Errorf("the Go coverage profile, line %d: %q: %w", n, line, err)
		}
		out[name] = append(out[name], bl)
	}
	return out, sc.Err()
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

// fileCoverage is a file's coverage: each line of a block a statement, as
// lcov's converters of Go's coverage count them, but for lines that hold no
// code (blank, a comment, a lone brace); each function declared in it a
// function, run when a block in its body ran.
func fileCoverage(file string, src []byte, blocks []block) *istanbul.FileCoverage {
	lines := strings.Split(string(src), "\n")
	code := func(l int) bool {
		if l < 1 || l > len(lines) {
			return false
		}
		t := strings.TrimSpace(lines[l-1])
		return t != "" && t != "}" && t != "{" && !strings.HasPrefix(t, "//")
	}
	hits := map[int]int{}
	for _, b := range blocks {
		for l := b.startLine; l <= b.endLine; l++ {
			if code(l) {
				hits[l] = max(hits[l], b.count)
			}
		}
	}
	fc := &istanbul.FileCoverage{Path: file}
	for _, l := range slices.Sorted(func(yield func(int) bool) {
		for k := range hits {
			if !yield(k) {
				return
			}
		}
	}) {
		fc.StatementMap = append(fc.StatementMap, istanbul.Span(l, 0, l, len(lines[l-1])))
		fc.S = append(fc.S, hits[l])
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return fc
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start, end := fset.Position(fn.Body.Lbrace), fset.Position(fn.Body.Rbrace)
		count := -1
		for _, b := range blocks {
			if b.startLine >= start.Line && b.endLine <= end.Line {
				count = max(count, b.count)
			}
		}
		if count < 0 {
			continue // no block of it was compiled for coverage
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			name = receiver(fn.Recv.List[0].Type) + "." + name
		}
		at := fset.Position(fn.Pos())
		loc := istanbul.Span(at.Line, at.Column-1, end.Line, end.Column)
		fc.FnMap = append(fc.FnMap, istanbul.FnMeta{Name: name, Decl: loc, Loc: loc, Line: at.Line})
		fc.F = append(fc.F, count)
	}
	return fc
}

// receiver is a method's receiver type's name.
func receiver(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiver(t.X)
	case *ast.IndexExpr:
		return receiver(t.X)
	case *ast.IndexListExpr:
		return receiver(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}
