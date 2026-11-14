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
