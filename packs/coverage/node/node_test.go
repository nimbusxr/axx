package coveragenode

import (
	"context"
	"os"
	"path/filepath"
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
