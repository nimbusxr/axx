package version

import "testing"

func TestChannel(t *testing.T) {
	for v, want := range map[string]string{
		"0.1.0":                  "beta",
		"0.2.3":                  "beta",
		"1.0.0":                  "stable",
		"1.1.0-rc.1":             "rc",
		"0.0.0-dev":              "dev",
		"0.0.0-SNAPSHOT-59db4db": "dev",
		"nightly":                "dev",
		// Builds of a commit (go build in a checkout, go install ...@main):
		// Go pseudo-versions, before and after the first tag, and local changes.
		"0.0.0-20260924120000-abcdefabcdef":         "dev",
		"0.1.1-0.20260925184804-3bf0dd25927c":       "dev",
		"0.1.1-0.20260925184804-3bf0dd25927c+dirty": "dev",
		"0.2.0-rc.1.0.20260925184804-3bf0dd25927c":  "dev",
		"0.1.0+dirty": "dev",
	} {
		if got := channel(v); got != want {
			t.Errorf("channel(%q) = %q, want %q", v, got, want)
		}
	}
}
