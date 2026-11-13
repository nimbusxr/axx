package coveragepython

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
)

func TestLaunch(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ mount, want string }{
		{"", dir},
		{"/axx-coverage", "/axx-coverage"},
	} {
		env, err := pyRuntime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: core.CoverageService{Name: "claims", Dir: dir, Mount: c.mount}})
		if err != nil {
			t.Fatal(err)
		}
		sep := string(filepath.Separator)
		if c.mount != "" {
			sep = "/"
		}
		if len(env) != 1 || env[0] != "COVERAGE_RCFILE="+c.want+sep+"coveragerc" {
			t.Errorf("env: %v", env)
		}
		rc, _ := os.ReadFile(filepath.Join(dir, "coveragerc"))
		for _, want := range []string{"data_file = " + c.want + sep + ".coverage\n", "parallel = true", "branch = true", "sigterm = true"} {
			if !strings.Contains(string(rc), want) {
				t.Errorf("the settings lack %q:\n%s", want, rc)
			}
		}
	}
}

func TestWheelLink(t *testing.T) {
	page := `<html><body>
<a href="../../packages/aa/coverage-7.17.3-cp312-cp312-macosx_11_0_arm64.whl#sha256=x">coverage-7.17.3-cp312-cp312-macosx_11_0_arm64.whl</a>
<a href="../../packages/d1/8e/coverage-7.17.3-py3-none-any.whl#sha256=21745c" data-requires-python="&gt;=3.11">coverage-7.17.3-py3-none-any.whl</a>
</body></html>`
	got, err := wheelLink("https://ci:token@repo.example.com/pypi/simple/coverage/", []byte(page))
	if err != nil || got != "https://ci:token@repo.example.com/pypi/packages/d1/8e/coverage-7.17.3-py3-none-any.whl" {
		t.Errorf("%s %v", got, err)
	}
	if _, err := wheelLink("https://pypi.org/simple/coverage/", []byte("<a href=x>coverage-7.0.0.tar.gz</a>")); err == nil {
		t.Error("a page without the wheel")
	}
}

// A service that wrote no data file has no coverage, and no error.
func TestNoDataIsNoCoverage(t *testing.T) {
	files, _, err := pyRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{End: core.CoverageEnd{CoverageService: core.CoverageService{Dir: t.TempDir()}}})
	if err != nil || files != nil {
		t.Errorf("%v %v", files, err)
	}
}
