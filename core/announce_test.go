package core

import "testing"

func TestAnnounceWritesIDELines(t *testing.T) {
	var lines []string
	s := NewSuite(SuiteOptions{Announce: func(l string) { lines = append(lines, l) }})
	s.Announce("paused", "url", "http://127.0.0.1:53211/", "location", "features/shop portal.feature:24")
	if len(lines) != 1 || lines[0] != "[AXX-IDE] paused url=http://127.0.0.1:53211/ location=features/shop%20portal.feature:24" {
		t.Errorf("%q", lines)
	}
	NewSuite(SuiteOptions{}).Announce("paused", "url", "x") // no IDE: nothing, no panic
}
