// Package coveragenode is the coverage-node pack: the coverage of Node.js
// services, as a run with coverage exercises them, from V8's own coverage.
// It has Node write it (NODE_V8_COVERAGE) when the service exits, on its stop
// signal too, through a preload of axx's (NODE_OPTIONS=--require), and
// converts it as web-coverage converts the browsers', through source maps.
// It builds on coverage-core.
package coveragenode

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/internal/v8cov"
)

// Name is the pack's name.
const Name = "coverage-node"

func init() { coveragecore.Register(nodeRuntime{}) }

// Pack returns the coverage-node pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{coveragecore.Name},
		Steps: []core.StepDef{},
	}
}

const packDoc = `Measure the coverage of Node.js services in a run with coverage (` + "`axx run --coverage`" + `): a service whose coverage section names the runtime ` + "`node`" + `. Axx has Node write V8's own coverage of it (` + "`NODE_V8_COVERAGE`" + `) when it exits, and gives it a preload (` + "`NODE_OPTIONS=--require`" + `) that makes it exit through ` + "`process.exit`" + ` on its stop signal when it does not handle the signal itself, so that Node writes it then too. Its scripts count, through their source maps (TypeScript and all); ` + "`node_modules`" + ` and Node's own do not. It counts lines, branches and functions, as Node's tools (c8) do. No port and no inspector are opened. It builds on ` + "`coverage-core`" + `.

` + "```yaml" + `
services:
  portal-api:
    dir: ../portal-api
    command: node dist/server.js
    coverage:
      runtime: node
` + "```" + `

Its processes' children count too: they inherit ` + "`NODE_OPTIONS`" + `, which replaces a service's own (put its options in its command). In a container, its compose file mounts ` + "`${AXX_COVERAGE_DIR}`" + ` at the coverage section's ` + "`mount`" + ` and passes ` + "`NODE_V8_COVERAGE`" + ` and ` + "`NODE_OPTIONS`" + ` on, and ` + "`packs.coverage-core.parts.<name>.paths`" + ` maps the container's paths to the project's.`

//go:embed preload.cjs
var preload []byte

type nodeRuntime struct{}

func (nodeRuntime) Name() string { return "node" }

func (nodeRuntime) Measured() istanbul.Measured {
	return istanbul.Measured{Branches: true, Functions: true}
}

// The preload, and the folder V8 writes its coverage into, in the service's
// coverage folder.
const (
	preloadFile = "axx-coverage.cjs"
	v8Folder    = "v8"
)

// Launch puts the preload in the service's folder, where its container sees
// it too, and has Node write V8's coverage beside it.
func (nodeRuntime) Launch(_ context.Context, call coveragecore.LaunchCall) ([]string, error) {
	s := call.Service
	if err := os.WriteFile(filepath.Join(s.Dir, preloadFile), preload, 0o644); err != nil {
		return nil, err
	}
	// The folder Node writes into, made here: one a container's user made
	// would be its, whose files this user could not remove.
	if err := os.MkdirAll(filepath.Join(s.Dir, v8Folder), 0o755); err != nil {
		return nil, err
	}
	dir, join := s.Dir, filepath.Join
	if s.Mount != "" {
		dir, join = s.Mount, path.Join
	}
	if strings.ContainsAny(dir, ` "'`) {
		return nil, fmt.Errorf("service %s's coverage folder %s has a space or a quote, which NODE_OPTIONS cannot carry: move the project, or mount the folder elsewhere", s.Name, dir)
	}
	opts := "--require " + join(dir, preloadFile)
	if own := strings.TrimSpace(os.Getenv("NODE_OPTIONS")); own != "" {
		opts = own + " " + opts
	}
	return []string{"NODE_V8_COVERAGE=" + join(dir, v8Folder), "NODE_OPTIONS=" + opts}, nil
}

// tail reports whether name, which reading failed with err, is the file V8
// writes as a process ends after the preload took its coverage and marked
// it: of what ran after, and readable by the process's user only, which in
// a container of a user of its own this machine's user is not. Its name is
// "coverage-", the pid, the time and a count.
func tail(dir, name string, err error) bool {
	if !errors.Is(err, fs.ErrPermission) {
		return false
	}
	pid, _, ok := strings.Cut(strings.TrimPrefix(filepath.Base(name), "coverage-"), "-")
	if !ok || pid == "" {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "exited-"+pid))
	return err == nil
}

// v8File is a file Node writes into NODE_V8_COVERAGE: its scripts' coverage,
// and the source maps of those that have one.
type v8File struct {
	Result []struct {
		URL       string           `json:"url"`
		Functions []v8cov.Function `json:"functions"`
	} `json:"result"`
	SourceMapCache map[string]struct {
		Data json.RawMessage `json:"data"`
	} `json:"source-map-cache"`
}

// Convert reads what Node wrote: each script of the service's (not
// node_modules', not Node's own), its takes merged, through its source map.
func (nodeRuntime) Convert(_ context.Context, call coveragecore.ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	measured := istanbul.Measured{Branches: true, Functions: true}
	names, _ := filepath.Glob(filepath.Join(call.End.Dir, v8Folder, "coverage-*.json"))
	if len(names) == 0 {
		return nil, measured, nil
	}
	takes := map[string][][]v8cov.Function{}
	maps := map[string]json.RawMessage{}
	for _, n := range names {
		b, err := os.ReadFile(n)
		if tail(call.End.Dir, n, err) {
			continue
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, measured, fmt.Errorf("%w: Node writes its coverage readable by its own user only, and the process that wrote it, of a user of its own, ran without coverage-node's preload, which makes it readable: does its command keep NODE_OPTIONS?", err)
		}
		if err != nil {
			return nil, measured, err
		}
		var f v8File
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, measured, fmt.Errorf("%s: %w", n, err)
		}
		for _, sc := range f.Result {
			if p, ok := scriptPath(sc.URL); ok {
				takes[p] = append(takes[p], sc.Functions)
			}
		}
		for u, m := range f.SourceMapCache {
			if p, ok := scriptPath(u); ok && m.Data != nil {
				maps[p] = m.Data
			}
		}
	}
	read := func(p string) (string, error) {
		b, err := os.ReadFile(p)
		return string(b), err
	}
	var files []*istanbul.FileCoverage
	for _, p := range slices.Sorted(func(yield func(string) bool) {
		for k := range takes {
			if !yield(k) {
				return
			}
		}
	}) {
		local := call.Path(p)
		src, err := os.ReadFile(local)
		if err != nil {
			continue // not in the project
		}
		var sm *v8cov.SourceMap
		if m, ok := maps[p]; ok {
			if sm, err = v8cov.ParseSourceMap(m); err != nil {
				sm = nil
			}
		}
		c, err := v8cov.NewConverter(local, string(src), sm, read)
		if err != nil && sm != nil {
			// Its source map's originals are not here: the script counts.
			c, err = v8cov.NewConverter(local, string(src), nil, read)
		}
		if err != nil {
			continue
		}
		c.Apply(v8cov.Merge(takes[p]))
		for _, fc := range c.Istanbul() {
			if !strings.Contains(filepath.ToSlash(fc.Path), "/node_modules/") {
				files = append(files, fc)
			}
		}
	}
	return files, measured, nil
}

// scriptPath is the file of a script's URL, as the service names it: a
// file:// URL's, outside node_modules, other than axx's preload.
func scriptPath(u string) (string, bool) {
	if !strings.HasPrefix(u, "file://") {
		return "", false // node:internal, eval, data:
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", false
	}
	p := parsed.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	if strings.Contains(p, "/node_modules/") || path.Base(p) == preloadFile {
		return "", false // a dependency's, or axx's own preload
	}
	return p, true
}
