// Package coveragepython is the coverage-python pack: the coverage of Python
// services, as a run with coverage exercises them, with coverage.py. A
// measured service runs under its own coverage.py (its coverage section's
// command), with axx's settings, which write its data into its folder as it
// exits, SIGTERM included; axx reads it with a coverage.py of its own, which
// runs nothing of the project's. It builds on coverage-core.
package coveragepython

import (
	"archive/zip"
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/mirror"
	"github.com/nimbusxr/axx/internal/npm"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// Name is the pack's name.
const Name = "coverage-python"

func init() { coveragecore.Register(pyRuntime{}) }

// Pack returns the coverage-python pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{coveragecore.Name},
		Steps: []core.StepDef{},
	}
}

const packDoc = `Measure the coverage of Python services in a run with coverage (` + "`axx run --coverage`" + `): a service whose coverage section names the runtime ` + "`python`" + `. Its ` + "`coverage.command`" + ` runs it under coverage.py, installed where it runs (` + "`python -m coverage run -m parcels`" + `), and axx gives it its settings through ` + "`COVERAGE_RCFILE`" + `: its data goes to the service's folder, a file per process, branches included, written as it exits, on SIGTERM too. They replace the project's own coverage.py settings in a run with coverage. Axx reads the data with a coverage.py of its own (` + coverageVersion + `, downloaded once from PyPI, or the index ` + "`PIP_INDEX_URL`" + ` names, and run with no setting or plugin of the project's), which needs Python 3.11 or later on the machine that runs axx. It counts lines, branches and functions. It builds on ` + "`coverage-core`" + `.

` + "```yaml" + `
services:
  claims:
    dir: ../claims
    command: python -m claims
    coverage:
      runtime: python
      command: python -m coverage run -m claims
` + "```" + `

In a container, its compose file mounts ` + "`${AXX_COVERAGE_DIR}`" + ` at the coverage section's ` + "`mount`" + ` and passes ` + "`COVERAGE_RCFILE`" + ` on, and ` + "`packs.coverage-core.parts.<name>.paths`" + ` maps the container's paths to the project's (` + "`{/app/: ../claims/}`" + `).`

// coverageVersion is the coverage.py axx reads the data with: its pure
// Python wheel, pinned by SHA-256.
const (
	coverageVersion = "7.17.3"
	coverageWheel   = "coverage-" + coverageVersion + "-py3-none-any.whl"
	coverageSum     = "21745c08f2835b847a16887b748d3084dddeb0571ae2a0d62e33644413568b99"
	pypiSimple      = "https://pypi.org/simple"
)

type pyRuntime struct{}

func (pyRuntime) Name() string { return "python" }

func (pyRuntime) Measured() istanbul.Measured {
	return istanbul.Measured{Branches: true, Functions: true}
}

// rcFile is axx's coverage.py settings, in the service's folder.
const rcFile = "coveragerc"

// Launch writes the settings the service's coverage.py reads: its data in
// the folder, a file per process, branches, saved on SIGTERM.
func (pyRuntime) Launch(_ context.Context, call coveragecore.LaunchCall) ([]string, error) {
	s := call.Service
	dir, join := s.Dir, filepath.Join
	if s.Mount != "" {
		dir, join = s.Mount, path.Join
	}
	rc := "[run]\ndata_file = " + join(dir, ".coverage") + "\nparallel = true\nbranch = true\nsigterm = true\n"
	if err := os.WriteFile(filepath.Join(s.Dir, rcFile), []byte(rc), 0o644); err != nil {
		return nil, err
	}
	return []string{"COVERAGE_RCFILE=" + join(dir, rcFile)}, nil
}

// Convert reads the service's data files with axx's coverage.py: its paths
// mapped to this machine's, its lcov report read back.
func (pyRuntime) Convert(ctx context.Context, call coveragecore.ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	measured := istanbul.Measured{Branches: true, Functions: true}
	data, _ := filepath.Glob(filepath.Join(call.End.Dir, ".coverage.*"))
	if len(data) == 0 {
		return nil, measured, nil
	}
	py, err := python(ctx)
	if err != nil {
		return nil, measured, err
	}
	lib, err := library(ctx)
	if err != nil {
		return nil, measured, err
	}
	out, err := os.MkdirTemp("", "axx-coveragepy-")
	if err != nil {
		return nil, measured, err
	}
	defer os.RemoveAll(out)
	// Not in the service's folder, nor the project: nothing of the
	// project's (its .coveragerc, its plugins) is read.
	cmd := exec.CommandContext(ctx, py, "-I", "-c", script, lib, call.End.Dir, out)
	cmd.Dir = out
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, measured, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, measured, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, measured, err
	}
	// It names the files measured, as the service named them; axx says
	// where they are here.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	var files []string
	if err == nil {
		err = json.Unmarshal([]byte(line), &files)
	}
	if err == nil {
		where := map[string]string{}
		for _, f := range files {
			where[f] = call.Path(f)
		}
		b, _ := json.Marshal(where)
		_, err = stdin.Write(append(b, '\n'))
	}
	_ = stdin.Close()
	_, _ = io.Copy(io.Discard, stdout)
	if werr := cmd.Wait(); err == nil && werr != nil {
		err = werr
	}
	if err != nil {
		return nil, measured, fmt.Errorf("coverage.py: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	f, err := os.Open(filepath.Join(out, "lcov.info"))
	if err != nil {
		return nil, measured, err
	}
	defer f.Close()
	got, m, err := istanbul.ReadLcov(f)
	return got, m, err
}

// script combines the service's data files, asks axx where its files are
// here, and writes an lcov report of them: args the library, the service's
// folder and where to write.
const script = `import json, os, sys
sys.path.insert(0, sys.argv[1])
import coverage
from coverage.data import CoverageData
raw, out = sys.argv[2], sys.argv[3]
combined = CoverageData(basename=os.path.join(out, "combined"))
for name in sorted(os.listdir(raw)):
    if name.startswith(".coverage."):
        d = CoverageData(basename=os.path.join(raw, name))
        d.read()
        combined.update(d)
print(json.dumps(sorted(combined.measured_files())), flush=True)
where = json.loads(sys.stdin.readline())
mapped = CoverageData(basename=os.path.join(out, ".coverage"))
mapped.update(combined, map_path=lambda p: where.get(p, p))
mapped.write()
cov = coverage.Coverage(data_file=os.path.join(out, ".coverage"), config_file=False)
cov.load()
cov.lcov_report(outfile=os.path.join(out, "lcov.info"), ignore_errors=True)
`

// python is a Python 3.11 or later on this machine.
func python(ctx context.Context) (string, error) {
	names := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		names = []string{"python", "py"}
	}
	for _, n := range names {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, p, "-I", "-c", "import sys; print(sys.version_info >= (3, 11))").Output()
		if err == nil && strings.TrimSpace(string(out)) == "True" {
			return p, nil
		}
	}
	return "", fmt.Errorf("coverage.py reads the Python services' coverage, and needs Python 3.11 or later on this machine: install one (python3 on PATH)")
}

// library is axx's coverage.py: its wheel, downloaded once from the index
// PIP_INDEX_URL names (PyPI's otherwise), checked, unpacked into axx's
// cache.
func library(ctx context.Context) (string, error) {
	dir := filepath.Join(npm.CacheDir("coverage"), "coverage-py-"+coverageVersion)
	if _, err := os.Stat(filepath.Join(dir, "coverage", "__init__.py")); err == nil {
		return dir, nil
	}
	index := strings.TrimRight(cmp.Or(os.Getenv("PIP_INDEX_URL"), pypiSimple), "/")
	page, err := npm.Fetch(ctx, index+"/coverage/")
	if err != nil {
		return "", fmt.Errorf("cannot read coverage.py's files in the package index: %w (set PIP_INDEX_URL to a mirror of PyPI's simple index)", err)
	}
	link, err := wheelLink(index+"/coverage/", page)
	if err != nil {
		return "", err
	}
	b, err := npm.Fetch(ctx, link)
	if err != nil {
		return "", fmt.Errorf("cannot download %s: %w", coverageWheel, err)
	}
	if s := sha256.Sum256(b); hex.EncodeToString(s[:]) != coverageSum {
		return "", fmt.Errorf("%s from %s: SHA-256 %x does not match the pinned %s", coverageWheel, mirror.Redact(link), s, coverageSum)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := unzip(b, tmp); err != nil {
		return "", err
	}
	_ = os.RemoveAll(dir)
	return dir, os.Rename(tmp, dir)
}

// anchor is a link of a simple index's page (PEP 503).
var anchor = regexp.MustCompile(`<a [^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)

// wheelLink is the address of coverage.py's pure wheel on an index's page,
// resolved against the page's.
func wheelLink(page string, html []byte) (string, error) {
	base, err := url.Parse(page)
	if err != nil {
		return "", err
	}
	for _, m := range anchor.FindAllSubmatch(html, -1) {
		if strings.TrimSpace(string(m[2])) != coverageWheel {
			continue
		}
		ref, err := url.Parse(strings.ReplaceAll(string(m[1]), "&amp;", "&"))
		if err != nil {
			return "", err
		}
		u := base.ResolveReference(ref)
		u.Fragment = ""
		if u.User == nil && base.User != nil && u.Host == base.Host {
			u.User = base.User
		}
		return u.String(), nil
	}
	return "", fmt.Errorf("the package index's page of coverage.py (%s) lists no %s", mirror.Redact(page), coverageWheel)
}

// unzip writes a wheel's files into dir.
func unzip(b []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) || strings.Contains(f.Name, `\`) {
			return fmt.Errorf("%s has a file outside its folder: %s", coverageWheel, f.Name)
		}
		to := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
