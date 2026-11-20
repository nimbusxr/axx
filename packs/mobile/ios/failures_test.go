package mobileios

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// keepScreens writes the screens a failed test's steps attached into the screens folder of dir,
// the last five, and tells where each is. A step that fails attaches the whole screen, with what
// iOS shows over the app (a banner, a dialog), which the app's page source leaves out; the
// integration tests' report printed the page source and dropped the screens. CI's job keeps the
// folder AXX_TEST_FAILURES names when it fails. Without a folder nothing is kept.
func keepScreens(test string, attached []cloudtest.Attachment, dir string) []string {
	if dir == "" {
		return nil
	}
	screens := slices.DeleteFunc(slices.Clone(attached), func(a cloudtest.Attachment) bool { return a.MediaType != "image/png" })
	screens = screens[max(0, len(screens)-5):]
	dir = filepath.Join(dir, "screens")
	var kept []string
	for i, a := range screens {
		name := fmt.Sprintf("%s-%d.png", strings.ReplaceAll(test, "/", "-"), i+1)
		err := os.MkdirAll(dir, 0o755)
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), a.Body, 0o644)
		}
		if err != nil {
			kept = append(kept, fmt.Sprintf("the screen a step attached (%s) could not be kept: %v", a.Name, err))
			continue
		}
		kept = append(kept, fmt.Sprintf("the screen a step attached (%s) is kept as %s", a.Name, filepath.Join("screens", name)))
	}
	return kept
}

// A failed test's screens are kept by the test's name, the last five, and only the screens: a
// step also attaches logs.
func TestAFailedTestsScreensAreKept(t *testing.T) {
	var attached []cloudtest.Attachment
	for i := range 7 {
		attached = append(attached,
			cloudtest.Attachment{MediaType: "image/png", Name: "the courier app", Body: []byte{byte(i)}},
			cloudtest.Attachment{MediaType: "text/plain", Name: "what the device logged", Body: []byte("a log")})
	}
	if kept := keepScreens("TestCouriersOnASimulator", attached, ""); kept != nil {
		t.Errorf("screens are kept without a folder: %v", kept)
	}
	dir := t.TempDir()
	kept := keepScreens("TestCouriersOnASimulator/a part", attached, dir)
	if len(kept) != 5 || kept[0] != "the screen a step attached (the courier app) is kept as "+filepath.Join("screens", "TestCouriersOnASimulator-a part-1.png") {
		t.Errorf("told of the screens kept: %q", kept)
	}
	files, err := os.ReadDir(filepath.Join(dir, "screens"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("kept %d files, want the last five screens", len(files))
	}
	// The last five of seven: the third screen is the first kept.
	for i, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, "screens", f.Name()))
		if err != nil || len(b) != 1 || int(b[0]) != i+2 {
			t.Errorf("%s holds %v (%v), want screen %d", f.Name(), b, err, i+3)
		}
	}
	// A folder that cannot be made is told, not failed on.
	blocked := filepath.Join(dir, "a file")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if kept := keepScreens("TestCouriersOnASimulator", attached[:1], blocked); len(kept) != 1 || !strings.Contains(kept[0], "could not be kept") {
		t.Errorf("a folder that cannot be made: %q", kept)
	}
}
