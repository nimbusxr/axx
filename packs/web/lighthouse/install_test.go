package lighthouse

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
)

func TestTheLockfilePinsLighthouse(t *testing.T) {
	pkgs, err := npm.Packages(npm.InstallOptions{Lock: lockfile, LeaveOut: unused})
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]string{}
	for _, p := range pkgs {
		have[p.Path] = p.Version
		if strings.Contains(p.Path, "@sentry/") || strings.Contains(p.Path, "@opentelemetry/") {
			t.Errorf("%s is installed: only Sentry loads it", p.Path)
		}
	}
	if have["node_modules/lighthouse"] != lighthouseVersion {
		t.Errorf("the lockfile pins Lighthouse %q, not %s", have["node_modules/lighthouse"], lighthouseVersion)
	}
	if have["node_modules/puppeteer-core"] == "" {
		t.Error("the lockfile has no puppeteer-core, which the helper connects to the browser with")
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	b, err := os.ReadFile("npm/package.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil || manifest.Dependencies["lighthouse"] != lighthouseVersion {
		t.Errorf("npm/package.json asks for Lighthouse %q, not %s", manifest.Dependencies["lighthouse"], lighthouseVersion)
	}
}
