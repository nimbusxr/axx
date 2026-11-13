package coveragejvm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// JaCoCo's report of testdata/Desk.java, run with no argument: ship's first
// return ran, its second did not, close never ran.
func TestParseReport(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "report.xml"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := parseReport(b, func(pkg, name string) string { return pkg + "/" + name })
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "parcels/Desk.java" {
		t.Fatalf("%+v", files)
	}
	s := istanbul.Total(files)
	// As JaCoCo counts them: LINE 4 of 7, BRANCH 1 of 2, METHOD 2 of 4.
	if s.Lines.Covered != 4 || s.Lines.Total != 7 || s.Branches.Covered != 1 || s.Branches.Total != 2 || s.Functions.Covered != 2 || s.Functions.Total != 4 {
		t.Errorf("lines %+v, branches %+v, functions %+v", s.Lines, s.Branches, s.Functions)
	}
	var names []string
	for _, fn := range files[0].FnMap {
		names = append(names, fn.Name)
	}
	if strings.Join(names, ",") != "Desk.<init>,Desk.ship,Desk.close,Desk.main" {
		t.Errorf("functions: %v", names)
	}
}

func TestLaunch(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	p, err := jar(context.Background(), "agent")
	if err != nil {
		t.Skipf("JaCoCo's agent cannot be had here: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_TOOL_OPTIONS", "-Xmx256m")
	dir := t.TempDir()
	env, err := runtime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: core.CoverageService{Name: "orders", Dir: dir, Mount: "/axx-coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "JAVA_TOOL_OPTIONS=-Xmx256m -javaagent:/axx-coverage/jacocoagent.jar=destfile=/axx-coverage/jacoco.exec,output=file,append=true,dumponexit=true"
	if len(env) != 1 || env[0] != want {
		t.Errorf("got %v\nwant %s", env, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "jacocoagent.jar")); err != nil {
		t.Errorf("the agent is not in the service's folder: %v", err)
	}
	if _, err := (runtime{}).Launch(context.Background(), coveragecore.LaunchCall{Service: core.CoverageService{Name: "orders", Dir: filepath.Join(t.TempDir(), "a b")}}); err == nil {
		t.Error("a folder with a space is given to JAVA_TOOL_OPTIONS")
	}
}

// A service that wrote no jacoco.exec has no coverage, and no error.
func TestNoExecIsNoCoverage(t *testing.T) {
	files, _, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{End: core.CoverageEnd{CoverageService: core.CoverageService{Dir: t.TempDir()}}})
	if err != nil || files != nil {
		t.Errorf("%v %v", files, err)
	}
}

// axx doctor says whether the machine has the Java JaCoCo's CLI runs on.
func TestCheckJava(t *testing.T) {
	t.Setenv("JAVA_HOME", "")
	t.Setenv("PATH", t.TempDir())
	if r := checkJava(context.Background()); r.Status != core.CheckWarn || !strings.Contains(r.Detail, "no Java") {
		t.Errorf("without Java: %+v", r)
	}
	home := t.TempDir()
	java := filepath.Join(home, "bin", "java")
	if err := os.MkdirAll(filepath.Dir(java), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(java, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_HOME", home)
	if r := checkJava(context.Background()); r.Status != core.CheckOK || r.Detail != java {
		t.Errorf("with JAVA_HOME: %+v", r)
	}
}
