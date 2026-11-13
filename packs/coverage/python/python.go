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
		ConfigSchema: []byte(configSchema),
		Steps:        []core.StepDef{},
		Checks:       []core.Check{{Name: "Python 3.11+ (coverage-python)", Run: checkPython}},
	}
}

// checkPython finds the Python axx's coverage.py runs on, for axx doctor: a
// run with coverage needs it to read the Python services' coverage, other
// runs do not.
func checkPython(ctx context.Context) core.CheckResult {
	py, err := python(ctx)
	if err != nil {
		return core.CheckResult{Status: core.CheckWarn, Detail: "no Python 3.11 or later on this machine: a run with coverage cannot read the Python services' coverage", Hint: "install Python 3.11 or later, as python3 on PATH"}
	}
	return core.CheckResult{Status: core.CheckOK, Detail: py}
}

const packDoc = `Measure the coverage of Python services in a run with coverage (` + "`axx run --coverage`" + `): a service whose coverage section names the runtime ` + "`python`" + `. Its ` + "`coverage.command`" + ` runs it under coverage.py, installed where it runs (` + "`python -m coverage run -m parcels`" + `), and axx gives it its settings through ` + "`COVERAGE_RCFILE`" + `: its data goes to the service's folder, a file per process, branches included, written as it exits, on SIGTERM too. Every Python process of the service is measured: those it starts, such as uvicorn's workers, and those it forks, such as gunicorn's, wherever coverage.py is installed with them. Code installed as a package, in site-packages, is measured only when ` + "`parts.<name>.packages`" + ` names its packages. They replace the project's own coverage.py settings in a run with coverage. Axx reads the data with a coverage.py of its own (` + coverageVersion + `, downloaded once from PyPI, or the index ` + "`PIP_INDEX_URL`" + ` names, and run with no setting or plugin of the project's), which needs Python 3.11 or later on the machine that runs axx. It counts lines, branches and functions. It builds on ` + "`coverage-core`" + `.

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

// configSchema is the pack's section of axx.yaml, packs.coverage-python.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "parts": {
      "type": "object",
      "description": "Settings of each Python service, by its name in axx.yaml.",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "packages": {"type": "array", "items": {"type": "string"}, "description": "The service's packages, by their import names, measured wherever they are installed: code installed as a package, in site-packages, isn't measured otherwise. When set, only they are measured."}
        }
      }
    }
  }
}`

type config struct {
	Parts map[string]struct {
		Packages []string `json:"packages"`
	} `json:"parts"`
}

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
	var cfg config
	if call.Settings != nil {
		if err := call.Settings(Name, &cfg); err != nil {
			return nil, err
		}
	}
	rc := "[run]\ndata_file = " + join(dir, ".coverage") + "\nparallel = true\nbranch = true\nsigterm = true\npatch = " + patches(s) + "\n"
	if pkgs := cfg.Parts[s.Name].Packages; len(pkgs) > 0 {
		rc += "source_pkgs = " + strings.Join(pkgs, ", ") + "\n"
	}
	if err := os.WriteFile(filepath.Join(s.Dir, rcFile), []byte(rc), 0o644); err != nil {
		return nil, err
	}
	return []string{"COVERAGE_RCFILE=" + join(dir, rcFile)}, nil
}

// patches are coverage.py's patches a service runs with, so that every
// Python process of it is measured: those it starts, such as a server's
// workers (uvicorn's), those it forks (gunicorn's), those that replace it
// (execv), and those that end with os._exit, as a forked process does.
// coverage.py refuses fork and execv on Windows: a service in a container
// runs on Linux.
func patches(s core.CoverageService) string {
	if s.Mount == "" && runtime.GOOS == "windows" {
		return "_exit, subprocess"
	}
	return "_exit, execv, fork, subprocess"
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
	var read struct {
		Files   []string `json:"files"`
		Skipped []string `json:"skipped"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(line), &read)
	}
	files := read.Files
	if len(read.Skipped) > 0 && call.Logger != nil {
		// A process killed as it wrote its data: one of the container's,
		// which the kernel kills as its main process exits, say.
		call.Logger.Warn(fmt.Sprintf("coverage of %s: %d of its data files are not whole, like %s, and are left out: a process of it was killed while it wrote its coverage", call.End.Name, len(read.Skipped), read.Skipped[0]))
	}
	var unmatched *coveragecore.Unmatched
	if err == nil {
		where := map[string]string{}
		found := false
		for _, f := range files {
			where[f] = call.Path(f)
			if _, serr := os.Stat(where[f]); serr == nil {
				found = true
			}
		}
		switch {
		case len(files) == 0:
			unmatched = &coveragecore.Unmatched{Why: "coverage.py measured none of its code: code installed as a package, in site-packages, is measured only when packs.coverage-python.parts." + call.End.Name + ".packages names its packages"}
		case !found:
			unmatched = &coveragecore.Unmatched{Why: fmt.Sprintf("coverage.py measured %d of its files, like %s, and none of them is in the project where it looked, %s: say where they are in packs.coverage-core.parts.%s.paths", len(files), files[0], where[files[0]], call.End.Name)}
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
	if unmatched != nil {
		return nil, measured, unmatched
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
const script = `import json, os, sqlite3, sys
sys.path.insert(0, sys.argv[1])
import coverage
from coverage.data import CoverageData
from coverage.exceptions import CoverageException, NoDataError
raw, out = sys.argv[2], sys.argv[3]
combined = CoverageData(basename=os.path.join(out, "combined"))
skipped = []
for name in sorted(os.listdir(raw)):
    if name.startswith(".coverage."):
        d = CoverageData(basename=os.path.join(raw, name))
        try:
            d.read()
            combined.update(d)
        except (CoverageException, sqlite3.Error):
            skipped.append(name)
print(json.dumps({"files": sorted(combined.measured_files()), "skipped": skipped}), flush=True)
where = json.loads(sys.stdin.readline())
mapped = CoverageData(basename=os.path.join(out, ".coverage"))
mapped.update(combined, map_path=lambda p: where.get(p, p))
mapped.write()
cov = coverage.Coverage(data_file=os.path.join(out, ".coverage"), config_file=False)
cov.load()
try:
    cov.lcov_report(outfile=os.path.join(out, "lcov.info"), ignore_errors=True)
except NoDataError:
    open(os.path.join(out, "lcov.info"), "w").close()
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
