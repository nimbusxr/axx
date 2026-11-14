package webcore

import (
	"strings"
	"testing"
)

func TestChromiumBuild(t *testing.T) {
	// Where the whole of Chromium starts, it runs, watched or not.
	for _, headless := range []bool{true, false} {
		if channel, shell, err := chromiumBuild("", headless); channel != "chromium" || shell || err != nil {
			t.Errorf("headless %v: %q, %v, %v", headless, channel, shell, err)
		}
	}
	// In a sandbox that keeps it from starting, the headless shell runs a
	// headless run, and a run that shows its browsers cannot run.
	why := "this sandbox refuses a read"
	if channel, shell, err := chromiumBuild(why, true); channel != "" || !shell || err != nil {
		t.Errorf("sandboxed: %q, %v, %v", channel, shell, err)
	}
	_, _, err := chromiumBuild(why, false)
	if err == nil || !strings.Contains(err.Error(), why) || !strings.Contains(err.Error(), "run without watching or pausing") ||
		!strings.Contains(err.Error(), "/guides/run-from-a-sandbox/#browsers-in-the-sandbox") {
		t.Errorf("sandboxed with windows: %v", err)
	}
}
