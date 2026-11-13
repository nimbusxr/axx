//go:build integration

package coveragejvm

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/coverage/internal/coveragetest"
)

// A JVM service with JaCoCo's agent writes what ran as it exits, and the
// pack reads it against its classes: testdata/Desk.java, compiled here.
func TestAJVMServicesCoverage(t *testing.T) {
	java, err := javaProgram()
	if err != nil {
		coveragetest.Missing(t, err)
	}
	javac := filepath.Join(filepath.Dir(java), "javac")
	if _, err := os.Stat(javac); err != nil {
		coveragetest.Missing(t, "no javac beside", java)
	}
	project := t.TempDir()
	work := filepath.Join(project, "desk")
	src := filepath.Join(work, "src", "main", "java", "parcels")
	classes := filepath.Join(work, "build", "classes", "java", "main")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("testdata", "Desk.java"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Desk.java"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(javac, "-d", classes, filepath.Join(src, "Desk.java")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	dir := filepath.Join(project, ".axx", "coverage", "raw", "r1", "desk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_TOOL_OPTIONS", "")
	s := core.CoverageService{Name: "desk", Runtime: "jvm", Dir: dir, WorkDir: work}
	env, err := runtime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(java, "-cp", classes, "parcels.Desk")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	files, m, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 0"},
		Settings: func(string, any) error { return nil },
		Project:  project,
		Path:     func(p string) string { return filepath.Join(work, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(src, "Desk.java") || !m.Branches || !m.Functions {
		t.Fatalf("files %+v, measured %+v", files, m)
	}
	if s := istanbul.Total(files); s.Lines.Covered != 4 || s.Lines.Total != 7 || s.Branches.Covered != 1 || s.Functions.Covered != 2 {
		t.Errorf("lines %+v, branches %+v, functions %+v", s.Lines, s.Branches, s.Functions)
	}
}

const server = `package parcels;

public class Server {
    public static void main(String[] args) throws Exception {
        System.out.println(Desk.ship(1));
        System.out.println("ready");
        Thread.sleep(60_000);
    }
}
`

// A JVM service axx up keeps is read through its agent's port, on the port
// its settings name: reset as a run starts, taken as it ends.
func TestAJVMServiceKeptUp(t *testing.T) {
	java, err := javaProgram()
	if err != nil {
		coveragetest.Missing(t, err)
	}
	javac := filepath.Join(filepath.Dir(java), "javac")
	if _, err := os.Stat(javac); err != nil {
		coveragetest.Missing(t, "no javac beside", java)
	}
	work := t.TempDir()
	src := filepath.Join(work, "src", "main", "java", "parcels")
	classes := filepath.Join(work, "build", "classes", "java", "main")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	desk, err := os.ReadFile(filepath.Join("testdata", "Desk.java"))
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"Desk.java": string(desk), "Server.java": server} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command(javac, "-d", classes, filepath.Join(src, "Desk.java"), filepath.Join(src, "Server.java")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	settings := func(_ string, v any) error {
		return json.Unmarshal([]byte(`{"parts": {"desk": {"port": `+strconv.Itoa(port)+`}}}`), v)
	}

	dir := t.TempDir()
	t.Setenv("JAVA_TOOL_OPTIONS", "")
	s := core.CoverageService{Name: "desk", Runtime: "jvm", Dir: dir, WorkDir: work, Kept: true}
	call := coveragecore.LaunchCall{Service: s, Settings: settings}
	env, err := runtime{}.Launch(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env[0], "output=tcpserver,address=127.0.0.1,port="+strconv.Itoa(port)) {
		t.Errorf("env: %v", env)
	}
	cmd := exec.Command(java, "-cp", classes, "parcels.Server")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	sc := bufio.NewScanner(out)
	for sc.Scan() && sc.Text() != "ready" {
	}

	// What ran before the run is reset; Server.main has run since, ship not.
	if err := (runtime{}).Reset(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	into := t.TempDir()
	if err := (runtime{}).Take(context.Background(), call, into); err != nil {
		t.Fatal(err)
	}
	end := core.CoverageEnd{CoverageService: s}
	end.Dir = into
	files, _, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{End: end, Settings: settings, Project: work, Path: func(p string) string { return filepath.Join(work, p) }})
	if err != nil || len(files) != 2 {
		t.Fatalf("%d files, %v", len(files), err)
	}
	for _, fc := range files {
		if filepath.Base(fc.Path) == "Desk.java" {
			if s := istanbul.Total([]*istanbul.FileCoverage{fc}); s.Lines.Covered != 0 {
				t.Errorf("Desk ran since the reset: %+v", s.Lines)
			}
		}
	}
}

// A Spring Boot jar keeps the application's classes beside its libraries'
// jars: JaCoCo reads only the application's (a multi-release library's
// would stop it), and only files of the project's sources count.
func TestASpringBootJarsCoverage(t *testing.T) {
	java, err := javaProgram()
	if err != nil {
		coveragetest.Missing(t, err)
	}
	javac := filepath.Join(filepath.Dir(java), "javac")
	if _, err := os.Stat(javac); err != nil {
		coveragetest.Missing(t, "no javac beside", java)
	}
	project := t.TempDir()
	work := filepath.Join(project, "desk")
	src := filepath.Join(work, "src", "main", "java")
	classes := filepath.Join(t.TempDir(), "classes")
	compile := func(out string, files map[string]string) {
		t.Helper()
		var names []string
		dir := t.TempDir()
		for name, text := range files {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			names = append(names, p)
		}
		if o, err := exec.Command(javac, append([]string{"-d", out}, names...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, o)
		}
	}
	desk, err := os.ReadFile(filepath.Join("testdata", "Desk.java"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "parcels"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "parcels", "Desk.java"), desk, 0o644); err != nil {
		t.Fatal(err)
	}
	compile(classes, map[string]string{"parcels/Desk.java": string(desk)})
	// A library whose jar is multi-release: two classes of one name.
	lib, lib21 := t.TempDir(), t.TempDir()
	compile(lib, map[string]string{"lib/Helper.java": "package lib;\npublic class Helper { public static int one() { return 1; } }\n"})
	compile(lib21, map[string]string{"lib/Helper.java": "package lib;\npublic class Helper { public static int one() { return 21; } }\n"})
	zipOf := func(entries map[string]string) []byte {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		for name, from := range entries {
			data, err := os.ReadFile(from)
			if err != nil {
				t.Fatal(err)
			}
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write(data)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	libJar := filepath.Join(t.TempDir(), "helper.jar")
	if err := os.WriteFile(libJar, zipOf(map[string]string{
		"lib/Helper.class":                      filepath.Join(lib, "lib", "Helper.class"),
		"META-INF/versions/21/lib/Helper.class": filepath.Join(lib21, "lib", "Helper.class"),
	}), 0o644); err != nil {
		t.Fatal(err)
	}
	boot := filepath.Join(project, "build", "libs", "desk.jar")
	if err := os.MkdirAll(filepath.Dir(boot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boot, zipOf(map[string]string{
		"BOOT-INF/classes/parcels/Desk.class": filepath.Join(classes, "parcels", "Desk.class"),
		"BOOT-INF/lib/helper.jar":             libJar,
	}), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(project, ".axx", "coverage", "raw", "r1", "desk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_TOOL_OPTIONS", "")
	s := core.CoverageService{Name: "desk", Runtime: "jvm", Dir: dir, WorkDir: filepath.Join(project, "infra")}
	env, err := runtime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(java, "-cp", classes, "parcels.Desk")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	convert := func(settings string) ([]*istanbul.FileCoverage, error) {
		files, _, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{
			End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 0"},
			Settings: func(_ string, v any) error { return json.Unmarshal([]byte(settings), v) },
			Project:  project,
			Path:     func(p string) string { return p },
		})
		return files, err
	}
	files, err := convert(`{"parts": {"desk": {"classes": ["build/libs/desk.jar"], "sources": ["desk/src/main/java"]}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(src, "parcels", "Desk.java") {
		t.Fatalf("files %+v", files)
	}

	// Sources that aren't the project's: the run says so, of the service.
	var um *coveragecore.Unmatched
	if _, err := convert(`{"parts": {"desk": {"classes": ["build/libs/desk.jar"], "sources": ["elsewhere"]}}}`); !errors.As(err, &um) || !strings.Contains(um.Why, "packs.coverage-jvm.parts.desk.sources") {
		t.Errorf("sources elsewhere: %v", err)
	}
	// No classes where its build puts them: so too.
	if _, err := convert(`{}`); !errors.As(err, &um) || !strings.Contains(um.Why, "packs.coverage-jvm.parts.desk.classes") {
		t.Errorf("no classes: %v", err)
	}
}
