package host

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// fakeFollower says the project runs on next, once a test sets it.
type fakeFollower struct {
	mu       sync.Mutex
	next     *Next
	nextErr  error
	starts   string
	readyErr error
	readied  []string
}

func (f *fakeFollower) set(change func(f *fakeFollower)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *fakeFollower) Next(context.Context) (*Next, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.next == nil {
		return nil, f.nextErr
	}
	n := *f.next
	return &n, f.nextErr
}

func (f *fakeFollower) Ready(_ context.Context, n *Next) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readied = append(f.readied, n.Version)
	return f.readyErr
}

func (f *fakeFollower) Starts() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, nil
}

// followed is a host that follows fol, and what closes when its Serve
// returned.
func followed(t *testing.T, fol *fakeFollower) (*fixture, <-chan struct{}) {
	t.Helper()
	dir := realPath(t.TempDir())
	writeConfig(t, dir, service(t, dir, "api", ""))
	f := &fixture{
		dir: dir, cleanups: filepath.Join(dir, "cleanups"), approvals: filepath.Join(t.TempDir(), "approvals.json"),
		approver: &fakeApprover{}, events: &events{}, follow: fol,
	}
	f.start(t)
	return f, f.returned
}

func versionEvents(e *events) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.list {
		if ev.Event == "version" {
			out = append(out, ev.State+" "+ev.Version+": "+ev.Text)
		}
	}
	return out
}

func stays(t *testing.T, gone <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-gone:
		t.Fatalf("the host restarted: %s", why)
	case <-time.After(300 * time.Millisecond):
	}
}

func restarts(t *testing.T, gone <-chan struct{}) {
	t.Helper()
	select {
	case <-gone:
	case <-time.After(20 * time.Second):
		t.Fatal("the host did not restart")
	}
}

func TestHostRestartsOnTheAxxTheProjectRunsOn(t *testing.T) {
	fol := &fakeFollower{starts: "packs: rest\n"}
	f, gone := followed(t, fol)
	stays(t, gone, "the project runs on the host's axx")

	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "1.1.0", From: "axx.yaml"} })
	restarts(t, gone)
	if n := f.srv.Moved(); n == nil || n.Version != "1.1.0" {
		t.Fatalf("moved: %+v", n)
	}
	// It says where runs look that it restarts, and on what; and gets the
	// axx before it stops answering.
	a := f.announcement(t)
	if a.Moving == nil || a.Moving.Version != "1.1.0" || time.Since(a.Moving.Since) > time.Minute {
		t.Errorf("announcement: %+v", a.Moving)
	}
	if len(fol.readied) != 1 || fol.readied[0] != "1.1.0" {
		t.Errorf("made ready: %q", fol.readied)
	}
	if got := versionEvents(f.events); len(got) != 1 || got[0] != "restarting 1.1.0: axx.yaml now names axx 1.1.0: restarting on it" {
		t.Errorf("events: %q", got)
	}
	if asked := f.approver.asked(); len(asked) != 0 {
		t.Errorf("asked about a restart with nothing of the project's changed: %+v", asked)
	}
}

func TestHostRestartsOnlyOnceItsRunsEnded(t *testing.T) {
	fol := &fakeFollower{}
	f, gone := followed(t, fol)
	ls, err := f.open(t, Request{Command: "axx up"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "1.1.0", From: Installed} })
	stays(t, gone, "a run has the services")
	if got := versionEvents(f.events); len(got) != 1 || got[0] != "waiting 1.1.0: the installed axx is now 1.1.0: the host restarts on it once the runs connected to it have ended" {
		t.Errorf("events: %q", got)
	}
	if len(fol.readied) != 0 {
		t.Error("got the axx while a run was connected")
	}
	if err := ls.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarts(t, gone)
	if got := f.cleanedUp(); len(got) != 1 {
		t.Errorf("the run's services were cleaned up before the restart: %q", got)
	}
}

func TestHostAsksBeforeARestartRunsWhatChanged(t *testing.T) {
	fol := &fakeFollower{starts: "steps/pack.go 1111\n"}
	f, gone := followed(t, fol)
	f.approver.answer = func(Question) bool { return false }
	fol.set(func(f *fakeFollower) {
		f.next = &Next{Version: "1.1.0", From: "axx.yaml"}
		f.starts = "steps/pack.go 2222\n"
	})
	waitFor(t, "the question", func() bool { return len(f.approver.asked()) == 1 })
	q := f.approver.asked()[0]
	if q.Restart == nil || q.Restart.Version != "1.1.0" || q.Approved != "steps/pack.go 1111\n" || q.Proposed.Text != "steps/pack.go 2222\n" || q.Command != "axx.yaml" {
		t.Errorf("question: %+v, proposed %+v", q, q.Proposed)
	}
	// Refused: it stays, says so, and does not ask about that version again.
	stays(t, gone, "the person refused")
	if got := versionEvents(f.events); len(got) != 1 || got[0] != "stays 1.1.0: not restarted: the host stays on axx 1.0.0 until you restart it" {
		t.Errorf("events: %q", got)
	}
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times about a version they refused", n)
	}
	// Another version is another question; approved, it restarts.
	f.approver.mu.Lock()
	f.approver.answer = func(Question) bool { return true }
	f.approver.mu.Unlock()
	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "1.2.0", From: "axx.yaml"} })
	restarts(t, gone)
	if n := f.srv.Moved(); n == nil || n.Version != "1.2.0" {
		t.Errorf("moved: %+v", n)
	}
	if n := len(f.approver.asked()); n != 2 {
		t.Errorf("asked %d times", n)
	}
}

func TestHostAsksBeforeARestartOnAnOlderAxx(t *testing.T) {
	fol := &fakeFollower{starts: "packs: rest\n"}
	f, gone := followed(t, fol)
	f.approver.answer = func(Question) bool { return true }
	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "0.9.0", From: "axx.yaml", Older: true} })
	restarts(t, gone)
	asked := f.approver.asked()
	if len(asked) != 1 || len(asked[0].Proposed.Warnings) != 1 || !strings.Contains(asked[0].Proposed.Warnings[0], "axx 0.9.0 is older than the axx 1.0.0 the host runs on") {
		t.Fatalf("asked: %+v", asked)
	}
}

func TestAYesHoldsWhileARunComesFirst(t *testing.T) {
	fol := &fakeFollower{starts: "a\n"}
	f, gone := followed(t, fol)
	// While the person reads the question, a run connects: the restart
	// waits for it, and does not ask again after.
	opened := make(chan *Lease, 1)
	f.approver.answer = func(Question) bool {
		ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
		if err != nil {
			t.Error(err)
		}
		opened <- ls
		return true
	}
	fol.set(func(f *fakeFollower) {
		f.next = &Next{Version: "1.1.0", From: "axx.yaml"}
		f.starts = "b\n"
	})
	ls := <-opened
	stays(t, gone, "a run has the services")
	if ls != nil {
		_ = ls.Stop(context.Background())
	}
	restarts(t, gone)
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

func TestHostDoesNotRestartWhatChangedWhileItAsked(t *testing.T) {
	fol := &fakeFollower{starts: "a\n"}
	f, gone := followed(t, fol)
	// The person answers yes to what they were shown; by then it changed again.
	f.approver.answer = func(q Question) bool {
		if q.Proposed.Text == "b\n" {
			fol.set(func(f *fakeFollower) { f.starts = "c\n" })
			return true
		}
		return false
	}
	fol.set(func(f *fakeFollower) {
		f.next = &Next{Version: "1.1.0", From: "axx.yaml"}
		f.starts = "b\n"
	})
	waitFor(t, "the second question", func() bool { return len(f.approver.asked()) == 2 })
	stays(t, gone, "what they approved is not what a restart would run")
	if q := f.approver.asked()[1]; q.Proposed.Text != "c\n" {
		t.Errorf("second question: %q", q.Proposed.Text)
	}
}

func TestHostStaysWhenItCannotGetTheAxx(t *testing.T) {
	fol := &fakeFollower{readyErr: errors.New("no release 7.7.7")}
	f, gone := followed(t, fol)
	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "7.7.7", From: "axx.yaml"} })
	stays(t, gone, "the axx cannot be had")
	got := versionEvents(f.events)
	if len(got) != 1 || got[0] != "stays 7.7.7: axx.yaml now names axx 7.7.7, which this machine cannot get, so the host stays on axx 1.0.0: no release 7.7.7" {
		t.Errorf("events: %q", got)
	}
	// It still serves.
	ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ls.Stop(context.Background())

	// What it cannot read of the version is said too, once.
	fol.set(func(f *fakeFollower) {
		f.next, f.nextErr = nil, errors.New("axx.yaml: `axx: latest` is not a version of axx")
	})
	waitFor(t, "the second notice", func() bool { return len(versionEvents(f.events)) == 2 })
	stays(t, gone, "the version cannot be read")
	if got := versionEvents(f.events); len(got) != 2 || got[1] != "stays : the host stays on axx 1.0.0: axx.yaml: `axx: latest` is not a version of axx" {
		t.Errorf("events: %q", got)
	}
}

func TestRunWaitsForAHostThatRestarts(t *testing.T) {
	fol := &fakeFollower{}
	f, gone := followed(t, fol)
	before := f.announcement(t)
	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "1.1.0", From: "axx.yaml"} })
	restarts(t, gone)
	f.stop()

	// A run that read the announcement before the restart, and one that
	// reads it now, both wait for the host and get the one that comes back.
	var status syncBuffer
	type result struct {
		ls  *Lease
		err error
	}
	opened := make(chan result, 2)
	for _, a := range []*Announcement{before, f.announcement(t)} {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ls, err := Open(ctx, a, Request{Command: "axx run"}, io.Discard, &status)
			opened <- result{ls, err}
		}()
	}
	select {
	case r := <-opened:
		t.Fatalf("a run did not wait for the host: %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
	f.follow = nil
	f.start(t)
	for range 2 {
		r := <-opened
		if r.err != nil {
			t.Fatal(r.err)
		}
		if err := r.ls.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	}
	if got := status.String(); strings.Count(got, "axx: the axx host is restarting on axx 1.1.0; waiting for it\n") != 2 {
		t.Errorf("status: %q", got)
	}
	// A pack's connection waits the same way.
	if a := f.announcement(t); a.Moving != nil || a.Token == before.Token {
		t.Errorf("the host that came back: %+v", a)
	}
}

func TestRunGivesUpOnAHostThatDidNotComeBack(t *testing.T) {
	fol := &fakeFollower{}
	f, gone := followed(t, fol)
	fol.set(func(f *fakeFollower) { f.next = &Next{Version: "1.1.0", From: "axx.yaml"} })
	restarts(t, gone)
	f.stop()
	old := moveWait
	moveWait = 200 * time.Millisecond
	t.Cleanup(func() { moveWait = old })

	_, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeNotListening || !strings.Contains(ae.Message, "left to restart on axx 1.1.0 and did not come back") {
		t.Fatalf("error: %v", err)
	}

	// An axx that failed before it served takes the announcement back: its
	// own only, and only one that says it left.
	Abandon(f.dir, os.Getpid()+1)
	if a, _ := Find(f.dir); a == nil {
		t.Fatal("another process's announcement was taken back")
	}
	Abandon(f.dir, os.Getpid())
	if a, _ := Find(f.dir); a != nil {
		t.Fatalf("the announcement stayed: %+v", a)
	}
	f.follow = nil
	f.start(t)
	Abandon(f.dir, os.Getpid())
	if a, _ := Find(f.dir); a == nil {
		t.Fatal("a serving host's announcement was taken back")
	}
}

func TestStarts(t *testing.T) {
	dir := realPath(t.TempDir())
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Starts(dir, nil)
	if err != nil || got != "# axx-packs.yaml: none\n" {
		t.Fatalf("no packs: %q, %v", got, err)
	}

	write("axx-packs.yaml", "packs:\n  - rest\n  - ./steps\n  - example.test/audit@v1.2.0\n")
	write("axx-packs.lock", "axx: 1.0.0\nmodules:\n  - path: example.test/audit\n    version: v1.2.0\n")
	write("steps/go.mod", "module example.test/steps\n")
	write("steps/pack.go", "package steps\n")
	write("steps/pack_test.go", "package steps\n")
	write("scripts/before.sh", "#!/bin/sh\necho before\n")
	hooks := []config.CommandHook{
		{Before: "host", Command: config.Command{Line: "./scripts/before.sh --all"}},
		{After: "host", Command: config.Command{Argv: []string{"echo", "done"}}, Shell: true},
	}
	got, err = Starts(dir, hooks)
	if err != nil {
		t.Fatal(err)
	}
	want := `# axx-packs.yaml: the packs the host's axx is built with
packs:
  - rest
  - ./steps
  - example.test/audit@v1.2.0

# axx-packs.lock: the versions of the packs that are Go modules
example.test/audit v1.2.0

# the project's own packs, built into it: each file they compile, by its SHA-256
steps/go.mod 5e2a7e2a8c7a1a53
steps/pack.go 0000000000000000

# axx.yaml runs before ` + "`axx host`" + `
./scripts/before.sh --all

# scripts/before.sh
#!/bin/sh
echo before

# axx.yaml runs after ` + "`axx host`" + `
echo done (through the shell)
`
	// The hashes are of the files' contents: compare all but them, then
	// that a change to a file changes its line and no other.
	strip := func(s string) string {
		var out []string
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "steps/") {
				line = line[:strings.LastIndex(line, " ")]
			}
			out = append(out, line)
		}
		return strings.Join(out, "\n")
	}
	if strip(got) != strip(want) {
		t.Errorf("starts:\n%s\nwant:\n%s", got, want)
	}
	// The lock's own axx version, which every start writes, and a test are
	// not what a start runs; a pack's source and a hook's script are.
	write("axx-packs.lock", "axx: 1.1.0\nmodules:\n  - path: example.test/audit\n    version: v1.2.0\n")
	write("steps/pack_test.go", "package steps\n\n// changed\n")
	if again, err := Starts(dir, hooks); err != nil || again != got {
		t.Errorf("changed with the lock's axx version or a test:\n%s\n%v", again, err)
	}
	write("steps/pack.go", "package steps\n\nfunc init() {}\n")
	again, err := Starts(dir, hooks)
	if err != nil || again == got || strip(again) != strip(got) {
		t.Errorf("a pack's source changed:\n%s\n%v", again, err)
	}
	write("scripts/before.sh", "#!/bin/sh\necho other\n")
	if third, err := Starts(dir, hooks); err != nil || !strings.Contains(third, "echo other\n") {
		t.Errorf("a hook's script changed:\n%s\n%v", third, err)
	}
}
