package host

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
)

// scanner is a pack's side in the host: it answers each line a run's side
// sends with the line in capitals, and says so to the person.
type scanner struct {
	ended chan error
}

func (scanner) HostServes() string { return "the depot's scanners" }

func (s scanner) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	defer conn.Close()
	run.Tell("a scanner for " + filepath.Base(run.ProjectDir))
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		if _, err := io.WriteString(conn, strings.ToUpper(sc.Text())+"\n"); err != nil {
			break
		}
	}
	err := ctx.Err()
	if s.ended != nil {
		s.ended <- err
	}
	return err
}

func packFixture(t *testing.T, services string) (*fixture, scanner) {
	t.Helper()
	dir := realPath(t.TempDir())
	if services == "" {
		if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		writeConfig(t, dir, services)
	}
	sc := scanner{ended: make(chan error, 4)}
	f := &fixture{
		dir: dir, cleanups: filepath.Join(dir, "cleanups"), approvals: filepath.Join(t.TempDir(), "approvals.json"),
		approver: &fakeApprover{}, events: &events{}, packs: map[string]core.HostServer{"depot-scanners": sc},
	}
	f.start(t)
	return f, sc
}

func said(t *testing.T, conn net.Conn, line string) string {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, line+"\n"); err != nil {
		t.Fatal(err)
	}
	got, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("no answer to %q: %v", line, err)
	}
	return strings.TrimSpace(got)
}

// A run's pack reaches its side in the host, and the two speak their own
// protocol; the host starts no services for it, and tells the person what
// the pack says it does.
func TestAPacksTwoSidesTalkThroughTheHost(t *testing.T) {
	f, _ := packFixture(t, "")
	conn, err := Dial(context.Background(), f.announcement(t), "depot-scanners", "axx run features/scans")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := said(t, conn, "px-4101 at leeds"); got != "PX-4101 AT LEEDS" {
		t.Errorf("the pack's side answered %q", got)
	}
	if got := said(t, conn, "px-4102 at leeds"); got != "PX-4102 AT LEEDS" {
		t.Errorf("the pack's side answered %q the second time", got)
	}
	ev, ok := f.events.find(func(e Event) bool { return e.Event == "pack" })
	if !ok || ev.Pack != "depot-scanners" || ev.Command != "axx run features/scans" || !strings.HasPrefix(ev.Text, "a scanner for ") {
		t.Errorf("what the person is told: %+v", ev)
	}
	if _, ok := f.events.find(func(e Event) bool { return e.Event == "run" }); ok {
		t.Error("a pack's connection counted as a run with services")
	}
	serving, _ := f.events.find(func(e Event) bool { return e.Event == "serving" })
	if len(serving.Packs) != 0 {
		t.Errorf("the host says it serves %v: the test gave it no lines", serving.Packs)
	}
}

// What the run sends right after its request, in the same packet, is the
// pack's too.
func TestWhatFollowsTheRequestIsThePacks(t *testing.T) {
	f, _ := packFixture(t, "")
	ann := f.announcement(t)
	conn, err := net.Dial("tcp", ann.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	l := newLines(conn)
	req := `{"protocol":3,"token":"` + ann.Token + `","command":"axx run","pack":"depot-scanners"}` + "\n" + "px-4101 at leeds\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}
	var hello Message
	if err := l.read(&hello); err != nil || hello.Type != MsgHello {
		t.Fatalf("the host's hello: %+v %v", hello, err)
	}
	got, err := l.r.ReadString('\n')
	if err != nil || strings.TrimSpace(got) != "PX-4101 AT LEEDS" {
		t.Errorf("the pack's side answered %q (%v)", got, err)
	}
}

func TestAPacksConnectionIsRefused(t *testing.T) {
	f, _ := packFixture(t, service(t, t.TempDir(), "api", ""))
	ann := *f.announcement(t)
	var ae *axxerr.Error

	bad := ann
	bad.Token = strings.Repeat("0", len(ann.Token))
	if _, err := Dial(context.Background(), &bad, "depot-scanners", "axx run"); !errors.As(err, &ae) || ae.Code != CodeRefused {
		t.Errorf("without the host's token: %v", err)
	}
	_, err := Dial(context.Background(), &ann, "depot-forklifts", "axx run")
	if !errors.As(err, &ae) || ae.Code != CodeRefused || !strings.Contains(err.Error(), "no depot-forklifts pack") {
		t.Errorf("a pack the host does not have: %v", err)
	}
	gone := ann
	gone.Addr = "127.0.0.1:1"
	if _, err := Dial(context.Background(), &gone, "depot-scanners", "axx run"); !errors.As(err, &ae) || ae.Code != CodeNotListening {
		t.Errorf("no host there: %v", err)
	}
	// None of them started the project's service.
	if _, ok := f.events.find(func(e Event) bool { return e.Event == "run" }); ok {
		t.Error("a refused connection started a run")
	}
}

func TestStoppingTheHostEndsWhatAPackDoes(t *testing.T) {
	f, sc := packFixture(t, "")
	conn, err := Dial(context.Background(), f.announcement(t), "depot-scanners", "axx run")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := said(t, conn, "px-4101"); got != "PX-4101" {
		t.Fatalf("the pack's side answered %q", got)
	}
	f.stop()
	f.stop = nil
	select {
	case err := <-sc.ended:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the pack's side ended with %v, want the host's stop", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the pack's side goes on after the host stopped")
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Error("the run's connection is still open")
	}
}

// A run that goes ends what the pack did for it.
func TestARunThatGoesEndsItsPacksConnection(t *testing.T) {
	f, sc := packFixture(t, "")
	conn, err := Dial(context.Background(), f.announcement(t), "depot-scanners", "axx run")
	if err != nil {
		t.Fatal(err)
	}
	if got := said(t, conn, "px-4101"); got != "PX-4101" {
		t.Fatalf("the pack's side answered %q", got)
	}
	_ = conn.Close()
	select {
	case err := <-sc.ended:
		if err != nil {
			t.Errorf("the pack's side ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the pack's side goes on after the run closed its connection")
	}
}

// couriers is a pack's side in the host that starts a project's app for
// runs: each line a run sends names an app, which the person approves
// before it "starts". It answers the line "started", or why not.
type couriers struct{}

func (couriers) HostServes() string { return "the couriers' apps" }

func (couriers) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		subject, path, _ := strings.Cut(sc.Text(), "=")
		answer := "started"
		if err := run.Approve(ctx, core.HostApproval{
			Subject:  subject,
			Text:     "app: " + path,
			Warnings: []string{"the app is a process on this machine: it reads and writes your files"},
		}); err != nil {
			answer = err.Error()
			var ae *axxerr.Error
			if errors.As(err, &ae) {
				answer = ae.Code + " " + answer
			}
		}
		if _, err := io.WriteString(conn, answer+"\n"); err != nil {
			break
		}
	}
	return ctx.Err()
}

func appsFixture(t *testing.T, answer func(Question) bool) *fixture {
	t.Helper()
	dir := realPath(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		dir: dir, cleanups: filepath.Join(dir, "cleanups"), approvals: filepath.Join(t.TempDir(), "approvals.json"),
		approver: &fakeApprover{answer: answer}, events: &events{}, packs: map[string]core.HostServer{"depot-apps": couriers{}},
	}
	f.start(t)
	return f
}

func (f *fixture) apps(t *testing.T) net.Conn {
	t.Helper()
	conn, err := Dial(context.Background(), f.announcement(t), "depot-apps", "axx run features/round")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// A pack's side in the host asks the person before it starts something of
// the project's for runs. They are asked once for a text: the same text is
// approved from then on, by a host started again too, and a text that
// changed asks again, with what they approved before to compare.
func TestAPackAsksThePersonBeforeItStartsSomething(t *testing.T) {
	f := appsFixture(t, func(Question) bool { return true })
	conn := f.apps(t)
	if got := said(t, conn, "the courier app=build/Courier.app"); got != "started" {
		t.Fatalf("approved, and the pack answered %q", got)
	}
	asked := f.approver.asked()
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	q := asked[0]
	if q.What != "depot-apps: the courier app" || q.Command != "axx run features/round" || q.Approved != "" ||
		q.Proposed.Text != "depot-apps: the courier app\napp: build/Courier.app\n" || len(q.Proposed.Warnings) != 1 {
		t.Errorf("the question: %+v, proposed %+v", q, q.Proposed)
	}
	// The same text again, and on another connection: no question.
	if got := said(t, conn, "the courier app=build/Courier.app"); got != "started" {
		t.Errorf("the second time: %q", got)
	}
	if got := said(t, f.apps(t), "the courier app=build/Courier.app"); got != "started" {
		t.Errorf("another run: %q", got)
	}
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times for one text", n)
	}
	// Another path for the same app: asked again, with the approved text.
	if got := said(t, conn, "the courier app=build/Release/Courier.app"); got != "started" {
		t.Errorf("the changed app: %q", got)
	}
	asked = f.approver.asked()
	if len(asked) != 2 || asked[1].Approved != q.Proposed.Text || !strings.Contains(asked[1].Proposed.Text, "build/Release/Courier.app") {
		t.Errorf("the second question: %+v", asked)
	}
	// A host started again keeps the approval.
	f.stop()
	f.stop = nil
	f.approver = &fakeApprover{answer: func(Question) bool { return false }}
	f.start(t)
	if got := said(t, f.apps(t), "the courier app=build/Release/Courier.app"); got != "started" {
		t.Errorf("after the host started again: %q", got)
	}
	if n := len(f.approver.asked()); n != 0 {
		t.Errorf("the new host asked %d times about what was approved", n)
	}
}

// What the person refused is refused again without asking, and says so to
// the run; another app is still asked about.
func TestAPacksApprovalRefused(t *testing.T) {
	f := appsFixture(t, func(q Question) bool { return !strings.Contains(q.Proposed.Text, "Courier.app") })
	conn := f.apps(t)
	for range 2 {
		got := said(t, conn, "the courier app=build/Courier.app")
		if !strings.HasPrefix(got, CodeNotApproved) || !strings.Contains(got, "refused to start the courier app") {
			t.Fatalf("refused, and the pack answered %q", got)
		}
	}
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times about what was refused", n)
	}
	if got := said(t, conn, "the depot app=build/Depot.app"); got != "started" {
		t.Errorf("another app: %q", got)
	}
}

// Two apps asked about at once are each answered: the second question waits
// for the first, and neither is dropped.
func TestTwoPacksQuestionsAreAskedOneAfterTheOther(t *testing.T) {
	release := make(chan struct{})
	f := appsFixture(t, func(Question) bool { <-release; return true })
	answers := make(chan string, 2)
	for _, line := range []string{"the courier app=build/Courier.app", "the depot app=build/Depot.app"} {
		conn := f.apps(t)
		go func() { answers <- said(t, conn, line) }()
	}
	waitFor(t, "the first question", func() bool { return len(f.approver.asked()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.approver.asked()); n != 1 {
		t.Fatalf("%d questions open at once", n)
	}
	close(release)
	for range 2 {
		if got := <-answers; got != "started" {
			t.Errorf("an app answered %q", got)
		}
	}
	if n := len(f.approver.asked()); n != 2 {
		t.Errorf("asked %d times for two apps", n)
	}
}

// A person who does not answer in time leaves the question open: the run is
// told, and the next one has their answer.
func TestAPacksApprovalNotAnsweredYet(t *testing.T) {
	answer := make(chan bool, 1)
	f := appsFixture(t, func(Question) bool { return <-answer })
	f.srv.wait = 200 * time.Millisecond
	conn := f.apps(t)
	got := said(t, conn, "the courier app=build/Courier.app")
	if !strings.HasPrefix(got, CodeNotAnswered) || !strings.Contains(got, "has not answered yet whether runs may start the courier app") {
		t.Fatalf("not answered, and the pack answered %q", got)
	}
	answer <- true
	waitFor(t, "the approval kept", func() bool { return said(t, f.apps(t), "the courier app=build/Courier.app") == "started" })
	if n := len(f.approver.asked()); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

// The terminal says which run's pack asks, about what, and what it reaches.
func TestTheTerminalAsksAboutAPacksApproval(t *testing.T) {
	var out syncBuffer
	in, w := io.Pipe()
	term := NewTerminal(in, &out, false, true)
	done := make(chan bool, 1)
	go func() {
		ok, _ := term.Approve(context.Background(), Question{
			Command: "axx run features/round", What: "mobile-ios: the courier app",
			Proposed: &Definition{Text: "mobile-ios: the courier app\napp: build/Courier.app\n", Warnings: []string{"the app is a process on this Mac"}},
		})
		done <- ok
	}()
	waitFor(t, "the question", func() bool { return strings.Contains(out.String(), "approve? [y/N]") })
	_, _ = io.WriteString(w, "y\n")
	if !<-done {
		t.Error("y did not approve")
	}
	for _, want := range []string{
		"axx run features/round asks to start mobile-ios: the courier app on this machine",
		"+app: build/Courier.app", "warning: the app is a process on this Mac",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}
