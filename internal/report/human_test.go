package report

import "testing"

// A page attached to a scenario (a desktop app's trace) is for a browser:
// the console names it, and prints text only.
func TestPagesAreNotPrinted(t *testing.T) {
	for mt, want := range map[string]bool{
		"text/plain": true, "application/json": true, "text/html": false, "text/html; charset=utf-8": false,
		"application/xhtml+xml": false, "image/png": false,
	} {
		if got := isTextMedia(mt); got != want {
			t.Errorf("isTextMedia(%q) = %v, want %v", mt, got, want)
		}
	}
}
