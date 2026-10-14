package cloudtest

import "testing"

// A scenario a test leaves open (a step failed, or the test never ended it) ends as the test does:
// what it holds is let go of before the next test.
func TestAScenarioATestLeavesOpenEndsWithIt(t *testing.T) {
	closed, status := false, ""
	t.Run("leaves it open", func(t *testing.T) {
		h := New(t)
		sc := h.SC
		sc.OnClose("device", func() error {
			closed, status = true, sc.Status()
			return nil
		})
	})
	if !closed {
		t.Fatal("the scenario the test left open did not end")
	}
	if status != "passed" {
		t.Errorf("it ended %q, want passed", status)
	}
}
