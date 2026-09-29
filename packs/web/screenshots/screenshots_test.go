package screenshots

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSettings(t *testing.T) {
	st, err := parseConfig(Config{}, "/work/acceptance")
	if err != nil {
		t.Fatal(err)
	}
	if st.folder != filepath.Join("/work/acceptance", "screenshots") || st.tolerance != 0 || st.update || len(st.platforms) != 0 {
		t.Errorf("defaults: %+v", st)
	}
	st, err = parseConfig(Config{Folder: "looks", Platforms: []string{"linux"}, Tolerance: 0.01, Update: true}, "/work/acceptance")
	if err != nil {
		t.Fatal(err)
	}
	if st.folder != filepath.Join("/work/acceptance", "looks") || st.tolerance != 0.01 || !st.update || st.platforms[0] != "linux" {
		t.Errorf("set: %+v", st)
	}
	if _, err := parseConfig(Config{Tolerance: 2}, "/p"); err == nil || !strings.Contains(err.Error(), "packs.web-screenshots.tolerance: 2 is not a share between 0 and 1") {
		t.Errorf("tolerance: %v", err)
	}
}

func TestScreenshotNames(t *testing.T) {
	for _, ok := range []string{"quote", "quote result", "parcel-PX-4101", "portal.home"} {
		if !screenshotName.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "../quote", "quote/result", ".hidden", `a\b`} {
		if screenshotName.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
