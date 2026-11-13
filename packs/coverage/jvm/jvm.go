// Package coveragejvm is the coverage-jvm pack: the coverage of JVM services
// (Kotlin, Java, Scala, Groovy), as a run with coverage exercises them, with
// JaCoCo. It gives a measured service JaCoCo's agent (JAVA_TOOL_OPTIONS),
// which writes what ran as the JVM exits, and reads it with JaCoCo's CLI
// against the service's classes. It builds on coverage-core.
package coveragejvm

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/mirror"
	"github.com/nimbusxr/axx/internal/npm"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// Name is the pack's name.
const Name = "coverage-jvm"

func init() { coveragecore.Register(runtime{}) }

// Pack returns the coverage-jvm pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{coveragecore.Name},
		ConfigSchema: []byte(configSchema), Steps: []core.StepDef{},
		Checks: []core.Check{{Name: "Java (coverage-jvm)", Run: checkJava}},
	}
}

// checkJava finds the Java JaCoCo's CLI runs on, for axx doctor: a run with
// coverage needs it to read the JVM services' coverage, other runs do not.
func checkJava(context.Context) core.CheckResult {
	java, err := javaProgram()
	if err != nil {
		return core.CheckResult{Status: core.CheckWarn, Detail: "no Java on this machine: a run with coverage cannot read the JVM services' coverage", Hint: "install a JDK or JRE, with JAVA_HOME set or java on PATH"}
	}
	return core.CheckResult{Status: core.CheckOK, Detail: java}
}

const packDoc = `Measure the coverage of JVM services, Kotlin, Java, Scala or Groovy, in a run with coverage (` + "`axx run --coverage`" + `): a service whose coverage section names the runtime ` + "`jvm`" + `. Axx gives it JaCoCo's agent through ` + "`JAVA_TOOL_OPTIONS`" + `, which every JVM it starts reads, and the agent writes what ran as the JVM exits (on its stop signal: a JVM runs its shutdown, unless killed). Axx reads it with JaCoCo's CLI against the service's classes: those of its build, ` + "`build/classes/*/main`" + ` (Gradle) or ` + "`target/classes`" + ` (Maven) in its ` + "`dir`" + `, or the folders and jars ` + "`parts.<name>.classes`" + ` names, which must be the classes it ran (of a Spring Boot jar or a web archive, its application's classes, not its libraries'). Only the files of its sources count. It counts lines, branches and methods, as JaCoCo does: Kotlin's generated code left out, an inline function counted where it is called. It needs Java on the machine that runs axx; JaCoCo ` + jacocoVersion + `, from Maven Central (or ` + "`AXX_MAVEN_URL`" + `), is downloaded once. It builds on ` + "`coverage-core`" + `.

` + "```yaml" + `
services:
  orders:
    dir: ../orders
    command: java -jar build/libs/orders.jar
    coverage:
      runtime: jvm
packs:
  coverage-jvm:
    parts:
      orders:
        classes: [../orders/build/classes/kotlin/main]
        sources: [../orders/src/main/kotlin]
` + "```" + `

Run the service's own JVM in its command (` + "`java -jar`" + `), not through a build tool, whose JVMs would get the agent too. ` + "`JAVA_TOOL_OPTIONS`" + ` replaces a service's own: put its options in its command. In a container, its compose file mounts ` + "`${AXX_COVERAGE_DIR}`" + ` at the coverage section's ` + "`mount`" + ` and passes ` + "`JAVA_TOOL_OPTIONS`" + ` on.`

// configSchema is the pack's section of axx.yaml, packs.coverage-jvm.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "parts": {
      "type": "object",
      "description": "Settings of each JVM service, by its name in axx.yaml.",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "classes": {"type": "array", "items": {"type": "string"}, "description": "The folders and jars of the classes the service runs, relative to the project (default: build/classes/*/main and target/classes in its dir). They must be the classes it ran: JaCoCo reads none of a class rebuilt since. Of a Spring Boot jar or a web archive, only its application's classes are read (BOOT-INF/classes, WEB-INF/classes), not its libraries'."},
          "sources": {"type": "array", "items": {"type": "string"}, "description": "The folders of its sources, relative to the project, which name its files (default: src/main/kotlin and src/main/java in its dir)."},
          "port": {"type": "integer", "minimum": 1, "maximum": 65535, "description": "The port JaCoCo's agent listens on when axx up keeps the service (axx up --coverage), for each run to read and reset its coverage while it runs: on 127.0.0.1, or published from its container on it. axx never picks one."}
        }
      }
    }
  }
}`

type config struct {
	Parts map[string]struct {
		Classes []string `json:"classes"`
		Sources []string `json:"sources"`
		Port    int      `json:"port"`
	} `json:"parts"`
}

// jacocoVersion is the JaCoCo axx downloads, its jars pinned by SHA-256.
const jacocoVersion = "0.8.16"

var jars = map[string]struct{ path, sum string }{
	"agent": {"/org/jacoco/org.jacoco.agent/" + jacocoVersion + "/org.jacoco.agent-" + jacocoVersion + "-runtime.jar", "37ae2f374ad77e85c59c6e5b46565226c3ac3cb460ac19de0bcb844ac2f43e33"},
	"cli":   {"/org/jacoco/org.jacoco.cli/" + jacocoVersion + "/org.jacoco.cli-" + jacocoVersion + "-nodeps.jar", "1f2a806225623fbd9f53d1ee0e42a6975323de0adebe01c12fc464ed147d756a"},
}

// jar is one of JaCoCo's jars, downloaded once into axx's cache and
// checked against its pinned sum.
func jar(ctx context.Context, which string) (string, error) {
	j := jars[which]
	p := filepath.Join(npm.CacheDir("coverage"), "jacoco-"+jacocoVersion, path.Base(j.path))
	if b, err := os.ReadFile(p); err == nil && sum(b) == j.sum {
		return p, nil
	}
	b, err := npm.Fetch(ctx, mirror.Maven(j.path))
	if err != nil {
		return "", fmt.Errorf("cannot download JaCoCo's %s: %w (%s)", which, err, mirror.MavenHelp())
	}
	if got := sum(b); got != j.sum {
		return "", fmt.Errorf("JaCoCo's %s (%s): SHA-256 %s does not match the pinned %s", which, path.Base(j.path), got, j.sum)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type runtime struct{}

func (runtime) Name() string { return "jvm" }

func (runtime) Measured() istanbul.Measured {
	return istanbul.Measured{Branches: true, Functions: true}
}

// execFile is what the agent writes, in the service's folder.
const execFile = "jacoco.exec"

// Launch puts JaCoCo's agent in the service's folder, where its container
// sees it too, and gives the JVM it through JAVA_TOOL_OPTIONS.
func (runtime) Launch(ctx context.Context, call coveragecore.LaunchCall) ([]string, error) {
	agent, err := jar(ctx, "agent")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(agent)
	if err != nil {
		return nil, err
	}
	s := call.Service
	if err := os.WriteFile(filepath.Join(s.Dir, "jacocoagent.jar"), b, 0o644); err != nil {
		return nil, err
	}
	dir, join := s.Dir, filepath.Join
	if s.Mount != "" {
		dir, join = s.Mount, path.Join // a path of the container's
	}
	if strings.ContainsAny(dir, " ,=") {
		return nil, fmt.Errorf("service %s's coverage folder %s has a space, comma or equals sign, which JAVA_TOOL_OPTIONS and JaCoCo's options cannot carry: move the project, or mount the folder elsewhere", s.Name, dir)
	}
	opts := "-javaagent:" + join(dir, "jacocoagent.jar") + "=destfile=" + join(dir, execFile) + ",output=file,append=true,dumponexit=true"
	if s.Kept {
		// axx up keeps it: each run reads and resets it through the
		// agent's port, while it runs.
		port, err := keptPort(call)
		if err != nil {
			return nil, err
		}
		address := "127.0.0.1"
		if s.Mount != "" {
			address = "*" // the container's port, which its compose file publishes
		}
		opts = fmt.Sprintf("-javaagent:%s=output=tcpserver,address=%s,port=%d", join(dir, "jacocoagent.jar"), address, port)
	}
	if own := strings.TrimSpace(os.Getenv("JAVA_TOOL_OPTIONS")); own != "" {
		opts = own + " " + opts
	}
	return []string{"JAVA_TOOL_OPTIONS=" + opts}, nil
}

// mismatch is JaCoCo's word for a class rebuilt since the service ran it.
var mismatch = regexp.MustCompile(`(?m)^\[WARN\] Execution data for class (\S+) does not match`)

// Convert reads what the agent wrote with JaCoCo's CLI, against the
// service's classes, and names the files of its sources.
func (runtime) Convert(ctx context.Context, call coveragecore.ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	measured := istanbul.Measured{Branches: true, Functions: true}
	exec_ := filepath.Join(call.End.Dir, execFile)
	if _, err := os.Stat(exec_); err != nil {
		return nil, measured, nil
	}
	var cfg config
	if err := call.Settings(Name, &cfg); err != nil {
		return nil, measured, err
	}
	s := call.End.CoverageService
	conf := cfg.Parts[s.Name]
	classes := folders(call.Project, conf.Classes, s.WorkDir, "build/classes/kotlin/main", "build/classes/java/main", "build/classes/scala/main", "build/classes/groovy/main", "target/classes")
	if len(classes) == 0 {
		return nil, measured, &coveragecore.Unmatched{Why: fmt.Sprintf("none of the classes it ran are where its build puts them (build/classes/*/main, target/classes in %s): name its classes or its jar in packs.%s.parts.%s.classes, the ones it ran (an image that builds them keeps them: copy its jar out)", s.WorkDir, Name, s.Name)}
	}
	sources := folders(call.Project, conf.Sources, s.WorkDir, "src/main/kotlin", "src/main/java", "src/main/scala", "src/main/groovy")
	java, err := javaProgram()
	if err != nil {
		return nil, measured, err
	}
	cli, err := jar(ctx, "cli")
	if err != nil {
		return nil, measured, err
	}
	tmp, err := os.MkdirTemp("", "axx-jacoco-")
	if err != nil {
		return nil, measured, err
	}
	defer os.RemoveAll(tmp)
	report := filepath.Join(tmp, "report.xml")
	args := []string{"-jar", cli, "report", exec_, "--xml", report}
	for i, c := range classes {
		own, err := ownClasses(c, filepath.Join(tmp, fmt.Sprintf("classes-%d", i)))
		if err != nil {
			return nil, measured, err
		}
		args = append(args, "--classfiles", own)
	}
	cmd := exec.CommandContext(ctx, java, args...)
	// Not the agent's options, nor anyone's: JaCoCo's CLI runs as it is.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return k == "JAVA_TOOL_OPTIONS" || k == "JDK_JAVA_OPTIONS" || k == "_JAVA_OPTIONS"
	})
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, measured, fmt.Errorf("JaCoCo's report: %w: %s", err, bytes.TrimSpace(out))
	}
	if m := mismatch.FindAllSubmatch(out, -1); len(m) > 0 && call.Logger != nil {
		call.Logger.Warn(fmt.Sprintf("coverage of %s: %d of its classes, like %s, are not those it ran: rebuilt since? JaCoCo counts none of them", s.Name, len(m), m[0][1]))
	}
	b, err := os.ReadFile(report)
	if err != nil {
		return nil, measured, err
	}
	// Only the files of the project's sources count: a shaded jar's
	// libraries, say, have none.
	var outside []string
	files, err := parseReport(b, func(pkg, name string) string {
		rel := path.Join(pkg, name)
		for _, src := range sources {
			p := filepath.Join(src, filepath.FromSlash(rel))
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		if p := call.Path(rel); p != rel {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		outside = append(outside, rel)
		return ""
	})
	if err == nil && len(files) == 0 && len(outside) > 0 {
		return nil, measured, &coveragecore.Unmatched{Why: fmt.Sprintf("JaCoCo measured %d of its source files, like %s, and none of them is in the project's sources (src/main/* in %s): name them in packs.%s.parts.%s.sources", len(outside), outside[0], s.WorkDir, Name, s.Name)}
	}
	return files, measured, err
}

// appClasses are where a Spring Boot jar and a web archive keep the
// application's own classes, beside its libraries' jars (BOOT-INF/lib,
// WEB-INF/lib), which JaCoCo would read too: their classes aren't the
// service's, and a multi-release jar's stop it.
var appClasses = []string{"BOOT-INF/classes/", "WEB-INF/classes/"}

// ownClasses is c, or, for an archive that keeps the application's classes
// beside its libraries, those classes, extracted into dir.
func ownClasses(c, dir string) (string, error) {
	if ext := strings.ToLower(filepath.Ext(c)); ext != ".jar" && ext != ".war" {
		return c, nil
	}
	z, err := zip.OpenReader(c)
	if err != nil {
		return c, nil // JaCoCo says what it is
	}
	defer z.Close()
	prefix := ""
	for _, f := range z.File {
		for _, p := range appClasses {
			if strings.HasPrefix(f.Name, p) {
				prefix = p
			}
		}
		if prefix != "" {
			break
		}
	}
	if prefix == "" {
		return c, nil
	}
	for _, f := range z.File {
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == f.Name || !strings.HasSuffix(rel, ".class") || strings.HasPrefix(rel, "META-INF/") || !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		if err := extract(f, filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return "", fmt.Errorf("%s: %w", c, err)
		}
	}
	return dir, nil
}

func extract(f *zip.File, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// folders are the folders the settings name, relative to the project, or
// those of defaults in the service's working directory that are there.
func folders(project string, named []string, work string, defaults ...string) []string {
	var out []string
	if len(named) > 0 {
		for _, n := range named {
			if !filepath.IsAbs(n) {
				n = filepath.Join(project, n)
			}
			out = append(out, n)
		}
		return out
	}
	for _, d := range defaults {
		p := filepath.Join(work, filepath.FromSlash(d))
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// javaProgram is the java JaCoCo's CLI runs on: JAVA_HOME's, else PATH's.
func javaProgram() (string, error) {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		p := filepath.Join(home, "bin", "java")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		if _, err := os.Stat(p + ".exe"); err == nil {
			return p + ".exe", nil
		}
	}
	p, err := exec.LookPath("java")
	if err != nil {
		return "", fmt.Errorf("JaCoCo reads the JVM services' coverage, and needs Java on this machine: install one (JAVA_HOME, or java on PATH)")
	}
	return p, nil
}

// report is JaCoCo's XML report: its packages' classes and source files.
type report struct {
	Packages []struct {
		Name    string `xml:"name,attr"`
		Classes []struct {
			Name       string `xml:"name,attr"`
			SourceFile string `xml:"sourcefilename,attr"`
			Methods    []struct {
				Name     string    `xml:"name,attr"`
				Line     int       `xml:"line,attr"`
				Counters []counter `xml:"counter"`
			} `xml:"method"`
		} `xml:"class"`
		SourceFiles []struct {
			Name  string `xml:"name,attr"`
			Lines []struct {
				Nr int `xml:"nr,attr"`
				MI int `xml:"mi,attr"`
				CI int `xml:"ci,attr"`
				MB int `xml:"mb,attr"`
				CB int `xml:"cb,attr"`
			} `xml:"line"`
		} `xml:"sourcefile"`
	} `xml:"package"`
}

type counter struct {
	Type    string `xml:"type,attr"`
	Covered int    `xml:"covered,attr"`
}

// parseReport is the coverage of JaCoCo's report's source files: each line
// with instructions a statement, run or not (JaCoCo says whether, not how
// often); each line's branches, the first covered ones counted; each method
// a function. file names a package's source file.
func parseReport(b []byte, file func(pkg, name string) string) ([]*istanbul.FileCoverage, error) {
	var r report
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false // its DOCTYPE names report.dtd, which is not here
	if err := d.Decode(&r); err != nil {
		return nil, fmt.Errorf("JaCoCo's report: %w", err)
	}
	var out []*istanbul.FileCoverage
	for _, p := range r.Packages {
		for _, sf := range p.SourceFiles {
			fc := &istanbul.FileCoverage{Path: file(p.Name, sf.Name)}
			if fc.Path == "" {
				continue // not the project's
			}
			for _, l := range sf.Lines {
				if l.MI+l.CI == 0 {
					continue
				}
				fc.StatementMap = append(fc.StatementMap, istanbul.Span(l.Nr, 0, l.Nr, 0))
				fc.S = append(fc.S, min(l.CI, 1))
				if n := l.MB + l.CB; n > 0 {
					loc := istanbul.Span(l.Nr, 0, l.Nr, 0)
					b := istanbul.BranchMeta{Type: "branch", Line: l.Nr, Loc: loc}
					hits := make([]int, n)
					for i := range n {
						b.Locations = append(b.Locations, loc)
						if i < l.CB {
							hits[i] = 1
						}
					}
					fc.BranchMap, fc.B = append(fc.BranchMap, b), append(fc.B, hits)
				}
			}
			for _, c := range p.Classes {
				if c.SourceFile != sf.Name {
					continue
				}
				class := path.Base(c.Name)
				for _, m := range c.Methods {
					if m.Line == 0 {
						continue
					}
					hit := 0
					for _, ct := range m.Counters {
						if ct.Type == "METHOD" {
							hit = ct.Covered
						}
					}
					loc := istanbul.Span(m.Line, 0, m.Line, 0)
					fc.FnMap = append(fc.FnMap, istanbul.FnMeta{Name: class + "." + m.Name, Decl: loc, Loc: loc, Line: m.Line})
					fc.F = append(fc.F, hit)
				}
			}
			out = append(out, fc)
		}
	}
	return out, nil
}

// keptPort is the port the agent of a service axx up keeps listens on.
func keptPort(call coveragecore.LaunchCall) (int, error) {
	var cfg config
	if err := call.Settings(Name, &cfg); err != nil {
		return 0, err
	}
	if p := cfg.Parts[call.Service.Name].Port; p != 0 {
		return p, nil
	}
	return 0, fmt.Errorf("axx up --coverage reads service %s's coverage while it runs, through JaCoCo's agent's port: set packs.%s.parts.%s.port (and publish it from its container)", call.Service.Name, Name, call.Service.Name)
}

// Reset starts a kept service's counts afresh, dropping what ran so far.
func (runtime) Reset(ctx context.Context, call coveragecore.LaunchCall) error {
	tmp, err := os.MkdirTemp("", "axx-jacoco-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	return dump(ctx, call, filepath.Join(tmp, execFile))
}

// Take writes what ran since the reset into into, as a stopped service's
// folder has it, and starts the counts afresh.
func (runtime) Take(ctx context.Context, call coveragecore.LaunchCall, into string) error {
	return dump(ctx, call, filepath.Join(into, execFile))
}

// dump has JaCoCo's CLI read a kept service's coverage from its agent's
// port into dest, and reset it.
func dump(ctx context.Context, call coveragecore.LaunchCall, dest string) error {
	port, err := keptPort(call)
	if err != nil {
		return err
	}
	java, err := javaProgram()
	if err != nil {
		return err
	}
	cli, err := jar(ctx, "cli")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, java, "-jar", cli, "dump", "--address", "127.0.0.1", "--port", fmt.Sprint(port), "--destfile", dest, "--reset", "--retry", "3")
	cmd.Env = append(os.Environ(), "JAVA_TOOL_OPTIONS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("JaCoCo's dump of service %s on port %d: %w: %s", call.Service.Name, port, err, bytes.TrimSpace(out))
	}
	return nil
}
