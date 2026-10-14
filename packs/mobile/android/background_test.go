package mobileandroid

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// workDB makes a WorkManager database with the tables and columns axx reads, as WorkManager 2.9
// and later have them: a periodic work with a unique name, a one-off work without one, and a
// finished work.
func workDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), workDatabase)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []string{
		`CREATE TABLE WorkSpec (id TEXT PRIMARY KEY, state INTEGER, worker_class_name TEXT, initial_delay INTEGER, interval_duration INTEGER, run_attempt_count INTEGER, last_enqueue_time INTEGER)`,
		`CREATE TABLE WorkName (name TEXT, work_spec_id TEXT, PRIMARY KEY (name, work_spec_id))`,
		`CREATE TABLE WorkTag (tag TEXT, work_spec_id TEXT, PRIMARY KEY (tag, work_spec_id))`,
		`CREATE TABLE SystemIdInfo (work_spec_id TEXT, generation INTEGER, system_id INTEGER, PRIMARY KEY (work_spec_id, generation))`,
		`INSERT INTO WorkSpec VALUES ('a1', 0, 'example.parcels.courier.RoundReminderWorker', 3600000, 3600000, 0, 1)`,
		`INSERT INTO WorkName VALUES ('round-reminder', 'a1')`,
		`INSERT INTO WorkTag VALUES ('example.parcels.courier.RoundReminderWorker', 'a1')`,
		`INSERT INTO SystemIdInfo VALUES ('a1', 0, 4), ('a1', 1, 7)`,
		`INSERT INTO WorkSpec VALUES ('b2', 0, 'example.parcels.courier.ProofUploadWorker', 0, 0, 2, 1)`,
		`INSERT INTO WorkTag VALUES ('example.parcels.courier.ProofUploadWorker', 'b2'), ('proofs', 'b2')`,
		`INSERT INTO WorkSpec VALUES ('c3', 2, 'example.parcels.courier.SignInSyncWorker', 0, 0, 0, 1)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestTheAppsWorkIsReadFromItsWorkManagerDatabase(t *testing.T) {
	works, err := readWorks(context.Background(), workDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 3 {
		t.Fatalf("works = %+v", works)
	}
	round, proof, done := works[0], works[1], works[2]
	if !round.periodic || round.retried || round.job.Int64 != 7 || round.title() != "round-reminder" {
		t.Errorf("the round reminder (the newest job of its generations) = %+v", round)
	}
	if proof.periodic || !proof.retried || proof.job.Valid || proof.title() != "ProofUploadWorker" {
		t.Errorf("the proof upload, not yet a job = %+v", proof)
	}
	if done.unfinished() || !round.unfinished() {
		t.Errorf("finished: %v, %v", done.unfinished(), round.unfinished())
	}
	for name, want := range map[string]bool{
		"round-reminder": true, "RoundReminderWorker": true, "example.parcels.courier.RoundReminderWorker": true,
		"round": false, "roundreminderworker": false, "proofs": false,
	} {
		if got := round.named(name); got != want {
			t.Errorf("round reminder named %q = %v, want %v", name, got, want)
		}
	}
	if !proof.named("proofs") {
		t.Error("a tag names the work")
	}
}

func TestTheJobSchedulersUnmetConditionsAreRead(t *testing.T) {
	dump := `JOB #u0a190/6: 1a2b3c4 example.parcels.courier/androidx.work.impl.background.systemjob.SystemJobService
    Required constraints: TIMING_DELAY
    Unsatisfied constraints: TIMING_DELAY
JOB #u0a190/7: 5d6e7f8 example.parcels.courier/androidx.work.impl.background.systemjob.SystemJobService
    Required constraints: TIMING_DELAY CONNECTIVITY
    Satisfied constraints: TIMING_DELAY
    Unsatisfied constraints: CONNECTIVITY
`
	if got := unsatisfied(dump, 7); got != "connectivity" {
		t.Errorf("job 7: %q", got)
	}
	if got := unsatisfied(dump, 70); got != "" {
		t.Errorf("job 70: %q", got)
	}
	// WorkManager 2.10's jobs, in their namespace (Android 14 and later).
	namespaced := `JOB #u0a190/androidx.work.systemjobscheduler:0: 9a8b7c6 example.parcels.courier/androidx.work.impl.background.systemjob.SystemJobService
    Required constraints: TIMING_DELAY CONNECTIVITY
    Unsatisfied constraints: TIMING_DELAY CONNECTIVITY
`
	if got := unsatisfied(namespaced, 0); got != "timing_delay connectivity" {
		t.Errorf("namespaced job 0: %q", got)
	}
}

// Work ran once it is no longer running, and finished or was enqueued again (periodic work, once
// it ran, is enqueued anew, with the time it was).
func TestWorkEndsWhenItIsNoLongerRunning(t *testing.T) {
	before := work{state: workEnqueued, lastEnqueue: 1000}
	for _, c := range []struct {
		name  string
		now   work
		ended bool
	}{
		{"not started yet", work{state: workEnqueued, lastEnqueue: 1000}, false},
		{"running", work{state: workRunning, lastEnqueue: 1000}, false},
		{"periodic, enqueued again", work{state: workEnqueued, lastEnqueue: 5000}, true},
		{"one-off, succeeded", work{state: 2, lastEnqueue: 1000}, true},
		{"one-off, failed", work{state: 3, lastEnqueue: 1000}, true},
	} {
		if got := c.now.endedSince(before); got != c.ended {
			t.Errorf("%s: ended = %v, want %v", c.name, got, c.ended)
		}
	}
}

// Work the app enqueued a moment ago is not yet a job: WorkManager hands it to Android's job
// scheduler a moment later, which axx waits for.
func TestWorkNotYetAJobIsWaitedFor(t *testing.T) {
	reads := 0
	read := func(context.Context) ([]work, error) {
		reads++
		w := work{id: "a1", class: "example.parcels.courier.RoundReminderWorker", names: []string{"round-reminder"}}
		if reads >= 3 {
			w.job = sql.NullInt64{Int64: 7, Valid: true}
		}
		return []work{w}, nil
	}
	works, err := scheduled(context.Background(), read, "round-reminder", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !works[0].job.Valid || reads != 3 {
		t.Errorf("after %d reads the work is %+v, want a job on the third", reads, works[0])
	}
	// Work that never becomes a job is given up on, as it was last read.
	reads = -100
	works, err = scheduled(context.Background(), read, "round-reminder", time.Second)
	if err != nil || works[0].job.Valid {
		t.Errorf("work never a job: %+v, %v", works, err)
	}
}
