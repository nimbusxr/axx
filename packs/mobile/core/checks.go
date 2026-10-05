package mobilecore

import (
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// value is what a field holds: nothing while it shows only its label.
func value(n *Node) string {
	t := clean(n.Text)
	if t == n.Hint || t == n.Label {
		return ""
	}
	return t
}

// shownTexts is what a screen shows, for a failure.
func shownTexts(s *Screen) string {
	if s == nil {
		return ""
	}
	return cloudstep.Shown("texts", shownList(s), 30)
}

// shownList is what a screen shows: its texts, each once.
func shownList(s *Screen) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range s.Visible() {
		for _, t := range []string{clean(n.Text), clean(n.Label)} {
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}
