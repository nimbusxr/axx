package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// fakeApprover answers with answer (nil: never), and keeps the questions
// and their contexts.
type fakeApprover struct {
	mu        sync.Mutex
	questions []Question
	ctxs      []context.Context
	answer    func(Question) bool
}

func (f *fakeApprover) Approve(ctx context.Context, q Question) (bool, error) {
	f.mu.Lock()
	f.questions = append(f.questions, q)
	f.ctxs = append(f.ctxs, ctx)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return answer(q), nil
}

func (f *fakeApprover) ctx(i int) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctxs[i]
}

func (f *fakeApprover) asked() []Question {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Question(nil), f.questions...)
}

// events keeps a host's events.
type events struct {
	mu   sync.Mutex
	list []Event
}

func (e *events) add(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, ev)
}

// states are a run's states, in order.
func (e *events) states(run int) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.list {
		if ev.Event == "run" && ev.Run == run {
			out = append(out, ev.State)
		}
	}
	return out
}

func (e *events) find(match func(Event) bool) (Event, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.list {
		if match(ev) {
			return ev, true
		}
	}
	return Event{}, false
}

// fixture is a project of one service (the test binary, which prints
// "ready" and runs until stopped; its cleanup appends to cleanups) and a
// host serving it.
type fixture struct {
	dir       string
	cleanups  string
	approvals string
	approver  *fakeApprover
	events    *events
	srv       *Server
	stop      func()
}

func service(t *testing.T, dir, name, extra string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cleanups := filepath.Join(dir, "cleanups")
	return "  " + name + ":\n" +
		"    command: [" + quote(exe) + ", serve, " + name + " ready]\n" +
		"    cleanup: [" + quote(exe) + ", append, " + quote(cleanups) + ", " + name + "]\n" +
		"    ready:\n      log: " + name + " ready\n      timeout: 20s\n" + extra
}

func writeConfig(t *testing.T, dir, services string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("services:\n"+services), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T, approvals string) *fixture {
	t.Helper()
	dir := realPath(t.TempDir())
	writeConfig(t, dir, service(t, dir, "api", ""))
	if approvals == "" {
		approvals = filepath.Join(t.TempDir(), "approvals.json")
	}
	f := &fixture{dir: dir, cleanups: filepath.Join(dir, "cleanups"), approvals: approvals, approver: &fakeApprover{}, events: &events{}}
	f.start(t)
	return f
}

func (f *fixture) start(t *testing.T) {
	t.Helper()
	srv, err := New(context.Background(), Options{
		Load:      config.LoadOptions{WorkDir: f.dir},
		Approver:  f.approver,
		Events:    f.events.add,
		Report:    true,
		Approvals: f.approvals,
		Env:       append(os.Environ(), helperEnv+"=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = srv
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx) }()
	f.stop = func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}
	t.Cleanup(func() {
		if f.stop != nil {
			f.stop()
		}
	})
	waitFor(t, "the announcement", func() bool { a, _ := Find(f.dir); return a != nil })
}

func (f *fixture) announcement(t *testing.T) *Announcement {
	t.Helper()
	a, err := Find(f.dir)
	if err != nil || a == nil {
		t.Fatalf("announcement: %v %v", a, err)
	}
	return a
}

func (f *fixture) open(t *testing.T, req Request, out, status io.Writer) (*Lease, error) {
	t.Helper()
	if out == nil {
		out = io.Discard
	}
	if status == nil {
		status = io.Discard
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return Open(ctx, f.announcement(t), req, out, status)
}

func (f *fixture) cleanedUp() []string {
	b, _ := os.ReadFile(f.cleanups)
	return strings.Fields(string(b))
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// syncBuffer is a bytes.Buffer goroutines write to.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunGetsServicesAndTheirCleanup(t *testing.T) {
	f := newFixture(t, "")
	ann := f.announcement(t)
	if !strings.HasPrefix(ann.Addr, "127.0.0.1:") || ann.Token == "" || ann.PID != os.Getpid() {
		t.Errorf("announcement: %+v", ann)
	}
	if info, err := os.Stat(AnnouncementFile(f.dir)); err == nil && info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("the announcement's token is the user's: mode %v", info.Mode())
	}
	if ev, ok := f.events.find(func(e Event) bool { return e.Event == "serving" }); !ok || ev.Changed || !slices.Equal(ev.Services, []string{"api"}) {
		t.Errorf("serving: %+v", ev)
	}

	var out syncBuffer
	ls, err := f.open(t, Request{Command: "axx run features", Dir: "/sandbox/shop"}, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ls.Started(), []string{"api"}) || !ls.Reports() || ls.Run() != 1 {
		t.Errorf("lease: started %q, reports %v, run %d", ls.Started(), ls.Reports(), ls.Run())
	}
	if !strings.Contains(out.String(), "[api] api ready") {
		t.Errorf("the services' output: %q", out.String())
	}
	if len(f.cleanedUp()) != 0 {
		t.Error("cleaned up while the run had the services")
	}
	ls.Report("##teamcity[testingStarted]")
	ls.Result(Result{Passed: 3, Failed: 1})
	if err := ls.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.cleanedUp(); !slices.Equal(got, []string{"api"}) {
		t.Errorf("cleanups: %q", got)
	}
	waitFor(t, "the run's end", func() bool { s := f.events.states(1); return len(s) > 0 && s[len(s)-1] == StateEnded })
	if got := f.events.states(1); !slices.Equal(got, []string{StateQueued, StateStarting, StateStarted, StateStopping, StateEnded}) {
		t.Errorf("states: %q", got)
	}
	if ev, _ := f.events.find(func(e Event) bool { return e.State == StateQueued }); ev.Dir != "/sandbox/shop" || ev.Command != "axx run features" {
		t.Errorf("queued: %+v", ev)
	}
	if ev, _ := f.events.find(func(e Event) bool { return e.State == StateEnded }); ev.Result == nil || ev.Result.Passed != 3 || ev.Result.Failed != 1 || ev.Error != nil {
		t.Errorf("ended: %+v", ev)
	}
	if ev, ok := f.events.find(func(e Event) bool { return e.Event == "report" }); !ok || ev.Line != "##teamcity[testingStarted]" || ev.Run != 1 {
		t.Errorf("report: %+v", ev)
	}

	f.stop()
	f.stop = nil
	if _, err := os.Stat(AnnouncementFile(f.dir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stopped host leaves no announcement: %v", err)
	}
}

func TestRunsTakeTurns(t *testing.T) {
	f := newFixture(t, "")
	first, err := f.open(t, Request{Command: "first"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var status syncBuffer
	second := make(chan *Lease, 1)
	go func() {
		ls, err := f.open(t, Request{Command: "second"}, nil, &status)
		if err != nil {
			t.Error(err)
		}
		second <- ls
	}()
	waitFor(t, "the second run to wait", func() bool { return strings.Contains(status.String(), "waiting for run 1 (first)") })
	select {
	case <-second:
		t.Fatal("the second run had the services while the first had them")
	case <-time.After(200 * time.Millisecond):
	}
	if err := first.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	ls := <-second
	if ls == nil {
		t.FailNow()
	}
	if err := ls.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.cleanedUp(); !slices.Equal(got, []string{"api", "api"}) {
		t.Errorf("each run cleaned up after: %q", got)
	}
	if got := f.events.states(2); len(got) < 2 || got[1] != StateWaiting {
		t.Errorf("the second run's states: %q", got)
	}
}

func TestLostRunIsCleanedUp(t *testing.T) {
	f := newFixture(t, "")
	ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ls.conn.Close() // the sandbox's run was killed
	waitFor(t, "the cleanup", func() bool { return len(f.cleanedUp()) == 1 })
	waitFor(t, "the run's end", func() bool { s := f.events.states(1); return len(s) > 0 && s[len(s)-1] == StateEnded })
}

func TestStoppingTheHostCleansUp(t *testing.T) {
	f := newFixture(t, "")
	ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.stop()
	f.stop = nil
	if got := f.cleanedUp(); !slices.Equal(got, []string{"api"}) {
		t.Errorf("cleanups: %q", got)
	}
	err = ls.Stop(context.Background())
	var ae *axxerr.Error
	if err != nil && (!errors.As(err, &ae) || ae.Code != CodeLost) {
		t.Errorf("the run's stop, once the host stopped: %v", err)
	}
}

func TestChangedDefinitionWaitsForApproval(t *testing.T) {
	f := newFixture(t, "")
	dir := f.dir
	writeConfig(t, dir, service(t, dir, "api", "    env:\n      PARCELS_REGION: eu\n"))

	f.approver.answer = func(Question) bool { return false }
	var status syncBuffer
	_, err := f.open(t, Request{Command: "axx run --compact"}, nil, &status)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeNotApproved {
		t.Fatalf("refused: %v", err)
	}
	if !strings.Contains(status.String(), "waiting up to a minute for the person running the axx host to approve it") {
		t.Errorf("the run says what it waits for: %q", status.String())
	}
	q := f.approver.asked()[0]
	if q.Run != 1 || q.Command != "axx run --compact" || !strings.Contains(Diff(q.Approved, q.Proposed.Text), "+    env:\n+      PARCELS_REGION: eu") {
		t.Errorf("question: %+v\n%s", q, Diff(q.Approved, q.Proposed.Text))
	}
	if len(f.cleanedUp()) != 0 {
		t.Error("a refused definition started nothing")
	}

	// A refused definition is refused again, without asking: an agent that
	// runs again does not ask the person again.
	f.approver.answer = func(Question) bool { return true }
	_, err = f.open(t, Request{Command: "axx run --compact"}, nil, nil)
	if !errors.As(err, &ae) || ae.Code != CodeNotApproved || !strings.Contains(err.Error(), "refused this change") {
		t.Fatalf("refused again: %v", err)
	}
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times", n)
	}

	// Another change is asked about.
	writeConfig(t, dir, service(t, dir, "api", "    env:\n      PARCELS_REGION: ap\n"))
	ls, err := f.open(t, Request{Command: "axx run --compact"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ls.Stop(context.Background())
	if _, err := f.open(t, Request{Command: "axx run"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if qs := f.approver.asked(); len(qs) != 2 {
		t.Errorf("an approved definition is not asked about again: %d questions", len(qs))
	}

	// A host started later approves nothing new, and asks as it starts.
	f.stop()
	f.stop = nil
	writeConfig(t, dir, service(t, dir, "api", "    env:\n      PARCELS_REGION: us\n"))
	g := &fixture{dir: dir, cleanups: f.cleanups, approvals: f.approvals, approver: &fakeApprover{}, events: &events{}}
	g.start(t)
	if ev, _ := g.events.find(func(e Event) bool { return e.Event == "serving" }); !ev.Changed {
		t.Errorf("a definition changed while no host ran is not approved by starting one: %+v", ev)
	}
	waitFor(t, "the question as the host starts", func() bool { return len(g.approver.asked()) == 1 })
	if q := g.approver.asked()[0]; q.Run != 0 || q.Command != "" || !strings.Contains(Diff(q.Approved, q.Proposed.Text), "+      PARCELS_REGION: us") {
		t.Errorf("the question as it starts: %+v", q)
	}
}

// A question outlives the run that asked: a run gives up waiting, and the
// next one has the answer the person gave meanwhile.
func TestAQuestionOutlivesItsRun(t *testing.T) {
	f := newFixture(t, "")
	f.srv.wait = 200 * time.Millisecond
	answers := make(chan bool)
	f.approver.answer = func(Question) bool { return <-answers }
	writeConfig(t, f.dir, service(t, f.dir, "api", "    env:\n      PARCELS_REGION: eu\n"))

	_, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeNotAnswered {
		t.Fatalf("not answered: %v", err)
	}
	answers <- true
	ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ls.Stop(context.Background())
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

// A run that waits on a question asked for an earlier run has its answer;
// a question about a definition that changed again is no longer asked.
func TestAQuestionIsReplacedByANewerChange(t *testing.T) {
	f := newFixture(t, "")
	f.srv.wait = 200 * time.Millisecond
	f.approver.answer = nil // until the question is no longer asked
	writeConfig(t, f.dir, service(t, f.dir, "api", "    env:\n      PARCELS_REGION: eu\n"))
	if _, err := f.open(t, Request{Command: "axx run"}, nil, nil); err == nil {
		t.Fatal("started unapproved")
	}
	writeConfig(t, f.dir, service(t, f.dir, "api", "    env:\n      PARCELS_REGION: us\n"))
	if _, err := f.open(t, Request{Command: "axx run"}, nil, nil); err == nil {
		t.Fatal("started unapproved")
	}
	qs := f.approver.asked()
	if len(qs) != 2 || !strings.Contains(qs[1].Proposed.Text, "PARCELS_REGION: us") {
		t.Fatalf("questions: %d", len(qs))
	}
	if !errors.Is(context.Cause(f.approver.ctx(0)), errReplaced) {
		t.Errorf("the first question ended: %v", context.Cause(f.approver.ctx(0)))
	}
}

func TestStartingApprovesTheFirstDefinition(t *testing.T) {
	f := newFixture(t, "")
	if ev, _ := f.events.find(func(e Event) bool { return e.Event == "serving" }); ev.Changed {
		t.Errorf("serving: %+v", ev)
	}
	ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ls.Stop(context.Background())
	if n := len(f.approver.asked()); n != 0 {
		t.Errorf("questions: %d", n)
	}
}

// A run that stops waiting for an answer leaves the question open, and the
// turn to the next run.
func TestARunStopsWaitingForAnAnswer(t *testing.T) {
	f := newFixture(t, "")
	writeConfig(t, f.dir, service(t, f.dir, "api", "    env:\n      A: b\n"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Open(ctx, f.announcement(t), Request{Command: "axx run"}, io.Discard, io.Discard)
		done <- err
	}()
	waitFor(t, "the question", func() bool { return len(f.approver.asked()) == 1 })
	cancel()
	err := <-done
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeInterrupted {
		t.Fatalf("err = %v", err)
	}
	if f.approver.ctx(0).Err() != nil {
		t.Error("the question is still asked")
	}
	// The next run has its turn.
	writeConfig(t, f.dir, service(t, f.dir, "api", ""))
	ls, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = ls.Stop(context.Background())
}

func TestRefusals(t *testing.T) {
	f := newFixture(t, "")
	ann := *f.announcement(t)
	bad := ann
	bad.Token = "not-the-token"
	_, err := Open(context.Background(), &bad, Request{}, io.Discard, io.Discard)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeRefused {
		t.Errorf("a wrong token: %v", err)
	}

	_, err = f.open(t, Request{Config: "../elsewhere/axx.yaml"}, nil, nil)
	if !errors.As(err, &ae) || ae.Code != CodeRefused {
		t.Errorf("a configuration outside the project: %v", err)
	}

	_, err = f.open(t, Request{Services: []string{"billing"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown service "billing"`) {
		t.Errorf("an unknown service: %v", err)
	}

	gone := ann
	gone.Addr = "127.0.0.1:1"
	_, err = Open(context.Background(), &gone, Request{}, io.Discard, io.Discard)
	if !errors.As(err, &ae) || ae.Code != CodeNotListening {
		t.Errorf("no host there: %v", err)
	}

	second, err := New(context.Background(), Options{Load: config.LoadOptions{WorkDir: f.dir}, Approver: &fakeApprover{}, Approvals: "-"})
	if err != nil {
		t.Fatal(err)
	}
	err = second.Serve(context.Background())
	if !errors.As(err, &ae) || ae.Code != CodeRunning {
		t.Errorf("a second host: %v", err)
	}
}

// What fails reading a definition can quote this machine's files and
// environment: the person sees it, the run only what failed.
func TestARunIsNotToldWhyItsDefinitionCannotBeRead(t *testing.T) {
	env := fakeDocker(t, "")
	dir := realPath(t.TempDir())
	f := &fixture{dir: dir, approver: &fakeApprover{}, events: &events{}, approvals: filepath.Join(t.TempDir(), "a.json")}
	writeConfig(t, dir, service(t, dir, "api", ""))
	f.start(t)
	env = append(env, "AXX_HOST_MODEL="+filepath.Join(t.TempDir(), "secret-file-name"))
	f.srv.env = env
	// The agent has the service run a compose project of a file outside the project.
	writeConfig(t, dir, "  api:\n    command: docker compose -f /home/you/.netrc up\n")

	_, err := f.open(t, Request{Command: "axx run"}, nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeDefinition || strings.Contains(err.Error(), "secret-file-name") {
		t.Fatalf("the run's error: %v", err)
	}
	waitFor(t, "the run's end", func() bool { s := f.events.states(1); return len(s) > 0 && s[len(s)-1] == StateEnded })
	if ev, _ := f.events.find(func(e Event) bool { return e.State == StateEnded }); ev.Error == nil || !strings.Contains(ev.Error.Message, "secret-file-name") {
		t.Errorf("the person sees why: %+v", ev.Error)
	}
}
