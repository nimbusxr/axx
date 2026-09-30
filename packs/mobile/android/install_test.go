package mobileandroid

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
)

// Appium's packages are those its lockfile pins, the flawed ones its driver
// bundles removed: no morgan older than 1.12.1, which forges log lines, and
// no brace-expansion 5 older than 5.0.12, which crafted patterns stall.
func TestAppiumHasNoFlawedPackages(t *testing.T) {
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
	fixed := map[string]string{"morgan": "1.12.1", "brace-expansion": "5.0.12"}
	for path, p := range lock.Packages {
		name := path[strings.LastIndex(path, "/")+1:]
		if want, ok := fixed[name]; ok && sameMajor(p.Version, want) && older(p.Version, want) {
			t.Errorf("%s is %s %s, which has a flaw fixed in %s: unbundle it", path, name, p.Version, want)
		}
	}
}

// sameMajor reports whether versions a and b have the same major version.
func sameMajor(a, b string) bool {
	return strings.SplitN(a, ".", 2)[0] == strings.SplitN(b, ".", 2)[0]
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
