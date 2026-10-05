package mobilecore

import "testing"

// A path in a phone app's files starts at its sandbox, with ./ or ~/.
func TestSandboxPath(t *testing.T) {
	for in, want := range map[string]string{"./Documents/exports": "Documents/exports", "~/Library": "Library", ".": "", "./": "", "files/x": "files/x", "./a/../b": "b"} {
		if got, err := sandboxPath(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"/data/data/x", "../x", "./../x"} {
		if _, err := sandboxPath(bad); err == nil {
			t.Errorf("%q is no path in the app's files", bad)
		}
	}
}
