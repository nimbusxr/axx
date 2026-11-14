package mock

import (
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// later adds a request to the fake's journal after a while, as a service's job
// would send it in the background.
func (f *fakeWireMock) later(t *testing.T, after time.Duration, r journaled) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { <-done })
	go func() {
		defer close(done)
		time.Sleep(after)
		f.mu.Lock()
		f.journal = append(f.journal, r)
		f.mu.Unlock()
	}()
}

func TestWaitsForRequests(t *testing.T) {
	reg, sc, fw, url := setup(t)
	run := func(text string) error {
		t.Helper()
		return step(t, reg, sc, text, nil)
	}
	if err := run("the mocked shops service with the following properties:"); err == nil {
		t.Fatal("a service needs its url")
	}
	if err := step(t, reg, sc, "the mocked shops service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}

	t.Run("a request sent in the background", func(t *testing.T) {
		fw.later(t, 700*time.Millisecond, journaled{Method: "POST", URL: "/webhooks/linden-and-lace"})
		start := time.Now()
		if err := run("within 10s the mocked POST request to /webhooks/linden-and-lace named delivered was received by shops"); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 5*time.Second {
			t.Error("the wait should end as soon as the request is there")
		}
		// Named for the steps that follow, as the step without a wait names it.
		if err := run("the mocked request named delivered on shops was received exactly 1 time"); err != nil {
			t.Error(err)
		}
	})

	t.Run("no request when the time is up", func(t *testing.T) {
		err := run("within 1s the mocked POST request to /webhooks/heron named never was received by shops")
		if !core.IsAssertion(err) || !strings.Contains(err.Error(), "received 0") {
			t.Errorf("a request that never came should fail the step: %v", err)
		}
	})

	t.Run("counts that grow", func(t *testing.T) {
		if err := run("the mocked GET request to /launches named launches was received by shops"); err != nil {
			t.Fatal(err)
		}
		fw.later(t, 500*time.Millisecond, journaled{Method: "GET", URL: "/launches"})
		fw.later(t, 900*time.Millisecond, journaled{Method: "GET", URL: "/launches"})
		start := time.Now()
		if err := run("within 10s the mocked request named launches on shops was received at least 4 times"); err != nil {
			t.Fatal(err)
		}
		if err := run("within 10s the mocked request named launches was received exactly 4 times"); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 5*time.Second {
			t.Error("the wait should end as soon as the count is there")
		}
	})

	t.Run("past an exact count, at once", func(t *testing.T) {
		start := time.Now()
		err := run("within 10s the mocked request named launches was received exactly 3 times")
		if !core.IsAssertion(err) || !strings.Contains(err.Error(), "received 4") {
			t.Errorf("a count past the one wanted should fail the step: %v", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Error("a count only grows: past the one wanted, no wait helps")
		}
	})

	t.Run("fewer when the time is up", func(t *testing.T) {
		err := run("within 1s the mocked request named launches on shops was received at least 5 times")
		if !core.IsAssertion(err) || !strings.Contains(err.Error(), "at least 5") {
			t.Errorf("fewer requests than wanted should fail the step: %v", err)
		}
	})
}
