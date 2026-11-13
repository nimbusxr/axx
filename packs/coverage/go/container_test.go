//go:build integration

package coveragego

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/coverage/internal/coveragetest"
)

// A service that stops on SIGTERM as main returns, as a Go service should.
const shipmentsServer = `package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
)

func ship(n int) string {
	if n > 0 {
		return "shipped"
	}
	return "held"
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	fmt.Println(ship(1))
	fmt.Println("ready")
	<-ctx.Done()
}
`

// A Go service in a container, a build for coverage on distroless as its
// nonroot user and PID 1, writes its coverage into the folder of this
// machine it mounts as compose stops it; the run reads it, its files named
// as they are in the project.
func TestAGoServiceInAContainer(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		coveragetest.Missing(t, "no go on PATH")
	}
	t.Setenv("AXX_GO", goBin) // axx's Go is this one: nothing to download
	project := t.TempDir()
	mod := filepath.Join(project, "shipments")
	write(t, filepath.Join(mod, "go.mod"), "module example.com/shipments\n\ngo 1.21\n")
	write(t, filepath.Join(mod, "main.go"), shipmentsServer)
	bin := filepath.Join(t.TempDir(), "shipments")
	build := exec.Command(goBin, "build", "-cover", "-o", bin, ".")
	build.Dir = mod
	// For the container: Linux, on Docker's machine, which is this one's.
	build.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off", "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+goruntime.GOARCH)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	dir := filepath.Join(project, ".axx", "coverage", "raw", "r1", "shipments")
	s := core.CoverageService{Name: "shipments", Runtime: "go", Dir: dir, Mount: "/axx-coverage", WorkDir: mod}
	env, err := coveragecore.Pack().(core.CoverageLauncher).LaunchCoverage(context.Background(), func(string, any) error { return nil }, s)
	if err != nil {
		t.Fatal(err)
	}
	code, out := coveragetest.Run(t, coveragetest.Service{
		Image: "gcr.io/distroless/static-debian12:nonroot", Cmd: []string{"/shipments"}, Env: env, Ready: "ready",
		Binds: []string{bin + ":/shipments:ro", dir + ":/axx-coverage"},
	})
	if code != 0 {
		t.Errorf("exit code %d, want 0 (main returned):\n%s", code, out)
	}
	files, _, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 0"},
		Settings: func(string, any) error { return nil },
		Project:  project,
		Path:     func(p string) string { return p },
	})
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(mod, "main.go") {
		t.Fatalf("files %+v:\n%s", files, out)
	}
	if s := istanbul.Total(files); s.Lines.Covered == 0 || s.Lines.Covered == s.Lines.Total || !strings.Contains(out, "shipped") {
		t.Errorf("lines %+v:\n%s", s.Lines, out)
	}
}
