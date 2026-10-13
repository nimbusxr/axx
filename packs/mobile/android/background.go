package mobileandroid

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"

	_ "modernc.org/sqlite" // the app's WorkManager database, read on the Mac
)

// The app's WorkManager database, in its data folder: WorkManager keeps its work there, and
// hands each piece to Android's job scheduler as a job.
const (
	workFolder   = "no_backup"
	workDatabase = "androidx.work.workdb"
)

// workNamespace is the namespace WorkManager 2.10 and later give their jobs, from Android 14 on.
const workNamespace = "androidx.work.systemjobscheduler"

// WorkManager's states of a piece of work (WorkInfo.State).
const (
	workEnqueued = 0
	workRunning  = 1
	workBlocked  = 4
)

// backgroundWorkRun is how long axx waits for background work it ran to end: WorkManager gives a
// worker ten minutes, and the work a scenario runs ends in seconds.
const backgroundWorkRun = time.Minute

// work is a piece of the app's WorkManager work, as its database has it.
type work struct {
	id, class         string
	state             int
	periodic, retried bool
	names, tags       []string
	job               sql.NullInt64 // the job Android's job scheduler runs it as
	lastEnqueue       int64         // when it was last enqueued, in milliseconds since 1970
}

// endedSince reports whether the work ran since it was as before: it is not running, and it
// finished (succeeded, failed or was cancelled) or was enqueued again, as periodic work is once it
// ran.
func (w work) endedSince(before work) bool {
	if w.state == workRunning {
		return false
	}
	return w.state != workEnqueued || w.lastEnqueue > before.lastEnqueue
}

// unfinished is work still to run: waiting for its time, running, or waiting for the work before it.
func (w work) unfinished() bool {
	return w.state == workEnqueued || w.state == workRunning || w.state == workBlocked
}

// named reports whether the name names the work: one of its unique names, one of its tags (one is
// its worker's class), or its worker's class without its package.
func (w work) named(name string) bool {
	return slices.Contains(w.names, name) || slices.Contains(w.tags, name) || simpleName(w.class) == name
}

// title is what the work is called, for a list: its unique name, or its worker's class.
func (w work) title() string {
	if len(w.names) > 0 {
		return w.names[0]
	}
	return simpleName(w.class)
}

func simpleName(class string) string { return class[strings.LastIndex(class, ".")+1:] }

// RunBackgroundTask has Android run the app's background work with this name now, as it does when
// the work's time comes. The work is WorkManager's, which hands it to Android's job scheduler as a
// job: axx asks the job scheduler to run the job now (cmd jobscheduler run), and Android runs it
// only if the conditions the app set for it (a network, say) are met. WorkManager itself runs
// periodic work only once its period has passed: axx tells it the period has (the work's last
// enqueue time, in its database), as the time passing would. It returns once the work ended, as
// WorkManager's database has it.
func (d *running) RunBackgroundTask(ctx context.Context, name string) error {
	if d.dev == nil {
		return fmt.Errorf("axx runs the %s app's background tasks on devices adb reaches, not on a device an Appium server runs", d.app.name)
	}
	works, err := d.works(ctx)
	if err != nil {
		return err
	}
	var named []work
	for _, w := range works {
		if w.unfinished() && w.named(name) {
			named = append(named, w)
		}
	}
	if len(named) == 0 {
		var titles []string
		for _, w := range works {
			if w.unfinished() && !slices.Contains(titles, w.title()) {
				titles = append(titles, w.title())
			}
		}
		return mobilecore.UnknownBackgroundTask(d.app.name, name, titles)
	}
	for _, w := range named {
		if err := d.runWork(ctx, name, w); err != nil {
			return err
		}
	}
	return nil
}

func (d *running) runWork(ctx context.Context, name string, w work) error {
	switch {
	case w.state == workRunning:
		return fmt.Errorf("the %s app's %q background task is running already", d.app.name, name)
	case w.state == workBlocked:
		return fmt.Errorf("the %s app's %q background task waits for the work before it, which has not finished", d.app.name, name)
	case !w.job.Valid:
		return fmt.Errorf("the %s app's %q background task is not yet a job Android's job scheduler runs", d.app.name, name)
	}
	if w.periodic || w.retried {
		if err := d.due(ctx, name, w); err != nil {
			return err
		}
	}
	job := strconv.FormatInt(w.job.Int64, 10)
	out, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "jobscheduler", "run", d.pkg, job)
	if strings.Contains(out, "Could not find job") {
		// WorkManager 2.10 and later keep their jobs apart, from Android 14 on.
		out, err = d.sdk.shell(ctx, d.dev.serial, "cmd", "jobscheduler", "run", "-n", workNamespace, d.pkg, job)
	}
	switch {
	case strings.Contains(out, "Running job"):
		return d.waitWorkEnded(ctx, name, w)
	case strings.Contains(out, "constraints"):
		return fmt.Errorf("the %s app's %q background task does not run now: the conditions the app set for it are not met (%s)",
			d.app.name, name, d.unmet(ctx, w.job.Int64))
	case err != nil:
		return fmt.Errorf("cannot run the %s app's %q background task: %w", d.app.name, name, err)
	default:
		return fmt.Errorf("cannot run the %s app's %q background task: Android's job scheduler answered %q", d.app.name, name, out)
	}
}

// waitWorkEnded waits for the work to end, once it ran, as WorkManager's database has it.
func (d *running) waitWorkEnded(ctx context.Context, name string, before work) error {
	deadline := time.Now().Add(backgroundWorkRun)
	for {
		works, err := d.works(ctx)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(works, func(w work) bool { return w.id == before.id })
		if i < 0 || works[i].endedSince(before) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the %s app's %q background task did not end in %s", d.app.name, name, backgroundWorkRun)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// due tells WorkManager the work's time has come: that its period, its first delay, or the wait
// before it tries again, has passed since it was last enqueued. WorkManager runs periodic work and
// work it retries no sooner, whatever runs its job; a run sets the time anew. The app's database is
// changed on the phone, as the app, by the phone's sqlite3.
func (d *running) due(ctx context.Context, name string, w work) error {
	// Five hours is the longest WorkManager waits before it tries work again.
	update := fmt.Sprintf("UPDATE WorkSpec SET last_enqueue_time = CAST(strftime('%%s', 'now') AS INTEGER) * 1000 - "+
		"(initial_delay + interval_duration + 18000000) WHERE id = '%s';", strings.ReplaceAll(w.id, "'", "''"))
	out, err := d.sdk.shell(ctx, d.dev.serial, "run-as "+q(d.pkg)+" sqlite3 "+q(workFolder+"/"+workDatabase)+" "+q(update))
	switch {
	case strings.Contains(out, "not debuggable"):
		return fmt.Errorf("the %s app's %q background task can be run only in a debuggable build, and Android refuses this one (run-as: package not debuggable)", d.app.name, name)
	case strings.Contains(out, "sqlite3") && (strings.Contains(out, "not found") || strings.Contains(out, "No such file")):
		return fmt.Errorf("the %s app's %q background task runs again only once its period has passed, which axx tells WorkManager "+
			"with the phone's sqlite3, and this phone has none (an Android image for users, like a Google Play one): run it on an "+
			"emulator with a Google APIs image", d.app.name, name)
	case err != nil:
		return fmt.Errorf("cannot make the %s app's %q background task due: %w", d.app.name, name, err)
	}
	return nil
}

// unmet is the conditions of a job that keep Android from running it, as the job scheduler says.
func (d *running) unmet(ctx context.Context, job int64) string {
	out, _ := d.sdk.shell(ctx, d.dev.serial, "dumpsys", "jobscheduler", d.pkg)
	if c := unsatisfied(out, job); c != "" {
		return c
	}
	return "Android does not say which"
}

// unsatisfied is the "Unsatisfied constraints" the job scheduler's dump lists for a job.
func unsatisfied(dump string, job int64) string {
	head := regexp.MustCompile(`JOB #[^/\s]+/(?:[^\s:]+:)?` + strconv.FormatInt(job, 10) + `(?::|\s)`)
	at := head.FindStringIndex(dump)
	if at == nil {
		return ""
	}
	rest := dump[at[1]:]
	if next := strings.Index(rest, "JOB #"); next >= 0 {
		rest = rest[:next]
	}
	for _, line := range strings.Split(rest, "\n") {
		if c, ok := strings.CutPrefix(strings.TrimSpace(line), "Unsatisfied constraints:"); ok {
			return strings.ToLower(strings.TrimSpace(c))
		}
	}
	return ""
}

// works are the app's WorkManager work, read from its database: copied from the phone, as the
// app, with the log of what it wrote last (SQLite's -wal and -shm files).
func (d *running) works(ctx context.Context) ([]work, error) {
	files := adbFiles{sdk: d.sdk, serial: d.dev.serial, pkg: d.pkg, dir: workFolder, app: d.app.name}
	dir, err := os.MkdirTemp("", "axx-workdb-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		body, found, err := files.Read(ctx, workDatabase+suffix)
		if err != nil {
			return nil, fmt.Errorf("cannot read the %s app's background work: %w", d.app.name, err)
		}
		if !found {
			if suffix == "" {
				return nil, nil // no WorkManager database: the app has no background work
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, workDatabase+suffix), body, 0o600); err != nil {
			return nil, err
		}
	}
	return readWorks(ctx, filepath.Join(dir, workDatabase))
}

// readWorks reads the work in a WorkManager database.
func readWorks(ctx context.Context, path string) ([]work, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT w.id, w.worker_class_name, w.state, w.interval_duration > 0, w.run_attempt_count > 0, w.last_enqueue_time,
		(SELECT group_concat(name, char(31)) FROM WorkName WHERE work_spec_id = w.id),
		(SELECT group_concat(tag, char(31)) FROM WorkTag WHERE work_spec_id = w.id),
		(SELECT system_id FROM SystemIdInfo WHERE work_spec_id = w.id ORDER BY rowid DESC LIMIT 1)
		FROM WorkSpec w ORDER BY w.rowid`)
	if err != nil {
		return nil, fmt.Errorf("cannot read the app's WorkManager database: %w", err)
	}
	defer rows.Close()
	var works []work
	for rows.Next() {
		var w work
		var names, tags sql.NullString
		if err := rows.Scan(&w.id, &w.class, &w.state, &w.periodic, &w.retried, &w.lastEnqueue, &names, &tags, &w.job); err != nil {
			return nil, fmt.Errorf("cannot read the app's WorkManager database: %w", err)
		}
		w.names, w.tags = split(names), split(tags)
		works = append(works, w)
	}
	return works, rows.Err()
}

func split(s sql.NullString) []string {
	if !s.Valid || s.String == "" {
		return nil
	}
	return strings.Split(s.String, "\x1f")
}
