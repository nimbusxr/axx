package coveragenode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
)

func TestScriptPath(t *testing.T) {
	for u, want := range map[string]string{
		"file:///app/dist/server.js":                "/app/dist/server.js",
		"file:///app/node_modules/express/index.js": "",
		"node:internal/modules/cjs/loader":          "",
		"evalmachine.<anonymous>":                   "",
		"file:///axx-coverage/axx-coverage.cjs":     "",
	} {
		got, ok := scriptPath(u)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v", u, got, ok)
		}
	}
}

func TestLaunch(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "--max-old-space-size=512")
	dir := t.TempDir()
	env, err := nodeRuntime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: core.CoverageService{Name: "api", Dir: dir, Mount: "/axx-coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 || env[0] != "NODE_V8_COVERAGE=/axx-coverage/v8" || env[1] != "NODE_OPTIONS=--max-old-space-size=512 --require /axx-coverage/axx-coverage.cjs" {
		t.Errorf("%v", env)
	}
	if b, err := os.ReadFile(filepath.Join(dir, preloadFile)); err != nil || string(b) != string(preload) {
		t.Errorf("the preload is not in the service's folder: %v", err)
	}
	if _, err := (nodeRuntime{}).Launch(context.Background(), coveragecore.LaunchCall{Service: core.CoverageService{Dir: filepath.Join(t.TempDir(), "a b")}}); err == nil {
		t.Error("a folder with a space is given to NODE_OPTIONS")
	}
}

// A service that wrote nothing has no coverage, and no error.
func TestNoCoverageWritten(t *testing.T) {
	files, _, err := nodeRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{End: core.CoverageEnd{CoverageService: core.CoverageService{Dir: t.TempDir()}}})
	if err != nil || files != nil {
		t.Errorf("%v %v", files, err)
	}
}

// Node writes its coverage readable by its own user only: in a container
// of a user of its own, the file V8 writes as the process ends, after the
// preload took its coverage and marked it, is unreadable here, and only
// that one is left out. One that no mark explains is an error.
func TestAnUnreadableFileAfterTheExitTake(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a file this user cannot read")
	}
	dir := t.TempDir()
	app := filepath.Join(t.TempDir(), "server.js")
	if err := os.WriteFile(app, []byte("console.log(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v8dir := filepath.Join(dir, v8Folder)
	if err := os.MkdirAll(v8dir, 0o755); err != nil {
		t.Fatal(err)
	}
	took := `{"result":[{"scriptId":"1","url":"file://` + filepath.ToSlash(app) + `","functions":[{"functionName":"","ranges":[{"startOffset":0,"endOffset":15,"count":1}],"isBlockCoverage":true}]}]}`
	if err := os.WriteFile(filepath.Join(v8dir, "coverage-7-100-0.json"), []byte(took), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v8dir, "coverage-7-200-1.json"), []byte(took), 0o000); err != nil {
		t.Fatal(err)
	}
	call := coveragecore.ConvertCall{
		End:  core.CoverageEnd{CoverageService: core.CoverageService{Dir: dir}},
		Path: func(p string) string { return p },
	}
	if _, _, err := (nodeRuntime{}).Convert(context.Background(), call); err == nil || !strings.Contains(err.Error(), "NODE_OPTIONS") {
		t.Fatalf("an unreadable file no mark explains: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exited-7"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	files, _, err := nodeRuntime{}.Convert(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != app {
		t.Fatalf("files %+v", files)
	}
}
