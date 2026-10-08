//go:build darwin

package ax

import "testing"

// An app's bundle identifier is its .app, as LaunchServices has it, without
// Spotlight; one no app has is none.
func TestAppPath(t *testing.T) {
	if p, err := AppPath("com.apple.Preview"); err != nil || p != "/System/Applications/Preview.app" {
		t.Errorf("Preview: %q, %v", p, err)
	}
	if p, err := AppPath("us.nimbusxr.no-such-app"); err != nil || p != "" {
		t.Errorf("no app: %q, %v", p, err)
	}
}
