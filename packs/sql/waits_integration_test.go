//go:build integration

package sql

import (
	dbsql "database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// run runs a statement on the test database, as a service would.
func run(t *testing.T, url, q string, args ...any) {
	t.Helper()
	tgt, _ := Resolve(url, "space", "secret")
	db, err := dbsql.Open("pgx", tgt.DSN)
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	if _, err := db.Exec(q, args...); err != nil {
		t.Error(err)
	}
}

// later runs a statement on the test database after a while, in the
// background, as a service's job would.
func later(t *testing.T, url string, after time.Duration, q string, args ...any) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { <-done })
	go func() {
		defer close(done)
		time.Sleep(after)
		run(t, url, q, args...)
	}()
}

func TestJSONBPolling(t *testing.T) {
	h := newHarness(t)
	url := postgresURL(t)
	h.db(url)
	id := uniq(t)
	later(t, url, 700*time.Millisecond,
		`INSERT INTO space.missions (id, metadata) VALUES ($1, $2::jsonb), ($1 || '-2', $2::jsonb)`,
		id, fmt.Sprintf(`{"programme": %q, "crewed": true}`, id))
	start := time.Now()
	h.ok("within 10s a selection of at least 2 rows is retrieved from the space.missions table where the metadata jsonb column contains:",
		[]string{"programme", id}, []string{"crewed", "true"})
	if time.Since(start) > 5*time.Second {
		t.Errorf("the wait should end as soon as the rows are there")
	}
	h.ok("the selection has 2 rows")
	h.ok("the 1st row metadata property for the selection json properties are:", []string{"crewed", "true"})

	// Fewer rows than asked when the time is up: the selection kept is what there was.
	if err := h.step("within 1s a 2nd selection of at least 3 rows is retrieved from the space.missions table on space-db where the metadata jsonb column contains:",
		[]string{"programme", id}); !core.IsAssertion(err) || !strings.Contains(err.Error(), "at least 3 row(s)") {
		t.Errorf("a wait that ends with fewer rows should fail: %v", err)
	}
	h.ok("the 2nd selection has 2 rows")
	// A typed value means what it says: the string "true" is not true.
	if err := h.step("within 1s a 3rd selection of at least 1 row is retrieved from the space.missions table where the metadata jsonb column contains:",
		[]string{"programme", id}, []string{"crewed", `"true"`}); !core.IsAssertion(err) {
		t.Errorf(`"true" should not select true: %v`, err)
	}
}

func TestNoRows(t *testing.T) {
	url := postgresURL(t)
	t.Run("rows a job deletes", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		id := uniq(t)
		run(t, url, `INSERT INTO space.missions (id, status) VALUES ($1, 'scrubbed'), ($1 || '-2', 'scrubbed')`, id)
		later(t, url, 700*time.Millisecond, `DELETE FROM space.missions WHERE id LIKE $1 || '%'`, id)
		start := time.Now()
		h.ok("within 10s the space.missions table has no rows where:", []string{"status", "scrubbed"}, []string{"id", id})
		if time.Since(start) > 5*time.Second {
			t.Errorf("the wait should end as soon as the rows are gone")
		}
	})
	t.Run("rows that no longer match", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		id := uniq(t)
		run(t, url, `INSERT INTO space.missions (id, status) VALUES ($1, 'pending')`, id)
		later(t, url, 700*time.Millisecond, `UPDATE space.missions SET status = 'launched' WHERE id = $1`, id)
		h.ok("within 10s the space.missions table on space-db has no rows where:", []string{"id", id}, []string{"status", "pending"})
	})
	t.Run("rows still there when the time is up", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		id := uniq(t)
		run(t, url, `INSERT INTO space.missions (id, status) VALUES ($1, 'pending'), ($1 || '-2', 'pending')`, id)
		err := h.step("within 1s the space.missions table has no rows where:", []string{"status", "pending"}, []string{"name", "null"}, []string{"budget", "null"})
		if !core.IsAssertion(err) || !strings.Contains(err.Error(), "still do") {
			t.Errorf("rows that still match should fail the step: %v", err)
		}
	})
	t.Run("a value of the wrong type fails at once", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		start := time.Now()
		if err := h.step("within 10s the space.missions table has no rows where:", []string{"budget", `"lots"`}); err == nil || core.IsAssertion(err) {
			t.Errorf("a text for an integer column should be an error: %v", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("no wait can mend a value of the wrong type")
		}
	})
}

func TestTriggerCountWaits(t *testing.T) {
	url := postgresURL(t)
	t.Run("a retry in the background", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		name := "retry-" + uniq(t)
		h.ok("a before insert trigger on the space.missions table will raise a 40001 exception 1 time where:", []string{"name", name})
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(700 * time.Millisecond)
			_ = insertMission(t, url, name+"-a", name)
		}()
		t.Cleanup(func() { <-done })
		start := time.Now()
		h.ok("within 10s the before insert trigger on the space.missions table was raised 1 time")
		if time.Since(start) > 5*time.Second {
			t.Errorf("the wait should end as soon as the trigger raised")
		}
	})
	t.Run("fewer when the time is up", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		name := "never-" + uniq(t)
		h.ok("a before update trigger on the space.missions table will raise a 40001 exception where:", []string{"name", name})
		err := h.step("within 1s the before update trigger on the space.missions table on space-db was raised 2 times")
		if !core.IsAssertion(err) || !strings.Contains(err.Error(), "raised 0 time(s) within 1s") {
			t.Errorf("a trigger that did not raise should fail: %v", err)
		}
	})
	t.Run("more fails at once", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		name := "twice-" + uniq(t)
		h.ok("a 1st ordered before delete trigger on the space.missions table will raise a 40001 exception where:", []string{"name", name})
		run(t, url, `INSERT INTO space.missions (id, name) VALUES ($1, $2), ($1 || '-2', $2)`, name, name)
		for _, id := range []string{name, name + "-2"} {
			tgt, _ := Resolve(url, "space", "secret")
			db, _ := dbsql.Open("pgx", tgt.DSN)
			_, _ = db.Exec("DELETE FROM space.missions WHERE id = $1", id)
			_ = db.Close()
		}
		start := time.Now()
		err := h.step("within 10s the 1st ordered before delete trigger on the space.missions table was raised 1 time")
		if !core.IsAssertion(err) || !strings.Contains(err.Error(), "raised 2 time(s)") {
			t.Errorf("a trigger raised more times should fail: %v", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("past the count, no wait helps")
		}
	})
}
