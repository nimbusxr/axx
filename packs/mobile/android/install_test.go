package mobileandroid

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
)

// Appium's packages are those its lockfile pins, the flawed ones its driver
// bundles removed: no morgan older than 1.12.1, which forges log lines.
func TestAppiumHasNoFlawedMorgan(t *testing.T) {
	if _, err := npm.Packages(npm.InstallOptions{Lock: appiumLock, Unbundle: appiumUnbundled}); err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(appiumLock, &lock); err != nil {
		t.Fatal(err)
	}
	for path, p := range lock.Packages {
		if strings.HasSuffix(path, "/morgan") && older(p.Version, "1.12.1") {
			t.Errorf("%s is morgan %s, which forges log lines (GHSA-9f6g-j8ch-79g4): unbundle it", path, p.Version)
		}
	}
}

// older reports whether version a comes before b: major.minor.patch.
func older(a, b string) bool {
	var x, y [3]int
	_, _ = fmt.Sscanf(a, "%d.%d.%d", &x[0], &x[1], &x[2])
	_, _ = fmt.Sscanf(b, "%d.%d.%d", &y[0], &y[1], &y[2])
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}
