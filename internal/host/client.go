package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// Lease is the services a host started for a run, until the run stops them.
type Lease struct {
	conn    net.Conn
	l       *lines
	run     int
	report  bool
	started []string
	told    []lifecycle.ToldValues
	out     io.Writer
	// dir is the project, as the run sees it; exits, how the services
	// ended, which the host's stopped message says.
	dir   string
	exits []lifecycle.Exit

	stopped chan *Failure // the host's stopped message; closed when the connection ends
}

// Open asks the host a announced for the services req names, and waits
// until they are ready. Their output goes to out, and what the run waits
// for (another run, an approval) to status. When nothing answers at a's
// address, the error has the code CodeNotListening.
//
// The host is the one the project announces as Open connects: a host that
// restarted on another version of axx since a was read has another address,
// and one that is restarting is waited for.
func Open(ctx context.Context, a *Announcement, req Request, out, status io.Writer) (*Lease, error) {
	w := &waiter{status: status}
	for {
		ls, err := open(ctx, a, w, req, out, status)
		if w.again(ctx, err) {
			continue
		}
		return ls, err
	}
}

// waiter is a connection's wait for a host that restarts on another
// version of axx: it says so once, and gives up after moveWait.
type waiter struct {
	status io.Writer
	since  time.Time
	said   bool
}

// wait says, the first time, that the run waits for the host to restart.
func (w *waiter) wait(version string) {
	if w.since.IsZero() {
		w.since = time.Now()
	}
	if !w.said && w.status != nil {
		fmt.Fprintf(w.status, "axx: the axx host is restarting on axx %s; waiting for it\n", version)
	}
	w.said = true
}

// again reports whether err is a host's that left to restart as the run
// reached it, and the run should look for it again: after a moment.
func (w *waiter) again(ctx context.Context, err error) bool {
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeMoving {
		return false
	}
	if w.since.IsZero() {
		w.since = time.Now()
	}
	if time.Since(w.since) > moveWait {
		return false
	}
	return pause(ctx)
}

// left is the error of a connection that ended before the host accepted
// it: the host's leaving to restart, when the project's announcement says
// so (its answer may be lost with the connection), else that it was lost.
func left(a *Announcement, err error) error {
	if a.dir != "" {
		if now, _ := Find(a.dir); now != nil && now.restarting() != nil {
			return hostErr(CodeMoving, "the axx host is restarting on axx %s", now.Moving.Version).WithHint("run again in a moment")
		}
	}
	return lost(err)
}

// pause waits a moment, unless ctx ends first.
func pause(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(200 * time.Millisecond):
		return true
	}
}

// reach connects to the host of a's project as the project announces it
// now, waiting for one that restarts on another version of axx. It returns
// the announcement it connected by. When nothing answers, the error has the
// code CodeNotListening, and hint as its hint.
func reach(ctx context.Context, a *Announcement, w *waiter, hint string) (net.Conn, *Announcement, error) {
	for {
		cur := a
		if a.dir != "" {
			if now, _ := Find(a.dir); now != nil {
				cur = now
			}
		}
		if m := cur.restarting(); m != nil {
			w.wait(m.Version)
			if !pause(ctx) {
				return nil, nil, axxerr.Wrap(ctx.Err(), CodeInterrupted, exitcode.Interrupted, "waiting for the axx host to restart was interrupted")
			}
			continue
		}
		d := net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", cur.Addr)
		if err == nil {
			return conn, cur, nil
		}
		// It may have left to restart, or come back, between the reading
		// and the connecting: look again then.
		if a.dir != "" && ctx.Err() == nil {
			if now, _ := Find(a.dir); now != nil && (now.restarting() != nil || now.Token != cur.Token) {
				continue
			}
		}
		if cur.Moving != nil {
			return nil, nil, hostErr(CodeNotListening, "the axx host that .axx/host.json names (pid %d) left to restart on axx %s and did not come back", cur.PID, cur.Moving.Version).
				WithHint("%s", hint)
		}
		return nil, nil, hostErr(CodeNotListening, "the axx host that .axx/host.json names (%s, pid %d) does not answer: %v", cur.Addr, cur.PID, err).
			WithHint("%s", hint)
	}
}

func open(ctx context.Context, a *Announcement, w *waiter, req Request, out, status io.Writer) (*Lease, error) {
	conn, a, err := reach(ctx, a, w, "start `axx host` again, or delete .axx/host.json to start the services here")
	if err != nil {
		return nil, err
	}
	req.Protocol, req.Token = Protocol, a.Token
	ls := &Lease{conn: conn, l: newLines(conn), out: out, dir: req.Dir, stopped: make(chan *Failure, 1)}
	accepted := false
	if err := ls.l.write(req); err != nil {
		_ = conn.Close()
		return nil, left(a, err)
	}
	// Until the services are ready, cancelling the run ends the connection:
	// the host stops what it started.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	for {
		var m Message
		if err := ls.l.read(&m); err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return nil, axxerr.Wrap(ctx.Err(), CodeInterrupted, exitcode.Interrupted,
					"asking the axx host for the services was interrupted; it stops and cleans up what it started")
			}
			if !accepted {
				return nil, left(a, err)
			}
			return nil, lost(err)
		}
		switch m.Type {
		case MsgHello:
			accepted = true
			ls.run, ls.report = m.Run, m.Report
		case MsgWaiting:
			fmt.Fprintf(status, "axx: %s\n", m.Reason)
		case MsgOutput:
			_, _ = io.WriteString(out, m.Text+"\n")
		case MsgError:
			_ = conn.Close()
			if m.Error == nil {
				m.Error = &Failure{Message: "the axx host refused the run"}
			}
			return nil, m.Error.err()
		case MsgStarted:
			ls.started, ls.told = m.Services, m.Told
			go ls.follow()
			return ls, nil
		}
	}
}

// Dial opens a connection to pack's side in the host a announced
// (core.HostServer): what the pack's two sides send each other goes over
// it. command is what the run is, for the person.
func Dial(ctx context.Context, a *Announcement, pack, command string) (net.Conn, error) {
	return DialAs(ctx, a, Request{Pack: pack, Command: command})
}

// DialAs is Dial for a run that tells the host its configuration too (req's
// Config, Profile, Properties and Settings, with its Pack and Command), for
// a pack's side that works with a suite of the project's packs: the host
// loads it as the run loaded its own.
func DialAs(ctx context.Context, a *Announcement, req Request) (net.Conn, error) {
	w := &waiter{}
	for {
		conn, err := dialAs(ctx, a, w, req)
		if w.again(ctx, err) {
			continue
		}
		return conn, err
	}
}

func dialAs(ctx context.Context, a *Announcement, w *waiter, req Request) (net.Conn, error) {
	conn, a, err := reach(ctx, a, w, "start `axx host` again")
	if err != nil {
		return nil, err
	}
	l := newLines(conn)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	req.Protocol, req.Token, req.IDE = Protocol, a.Token, os.Getenv("AXX_IDE")
	if err := l.write(req); err != nil {
		_ = conn.Close()
		return nil, left(a, err)
	}
	var m Message
	if err := l.read(&m); err != nil {
		_ = conn.Close()
		return nil, left(a, err)
	}
	if m.Type != MsgHello {
		_ = conn.Close()
		if m.Error == nil {
			m.Error = &Failure{Message: "the axx host refused the " + req.Pack + " pack's connection"}
		}
		return nil, m.Error.err()
	}
	_ = conn.SetDeadline(time.Time{})
	return &packConn{Conn: conn, r: l.r}, nil
}

func lost(err error) error {
	return axxerr.Wrap(err, CodeLost, exitcode.Environment, "lost the connection to the axx host").
		WithHint("the host stops and cleans up the services of a run it loses; see its terminal")
}

// follow writes the services' output until the host has stopped them.
func (ls *Lease) follow() {
	defer close(ls.stopped)
	for {
		var m Message
		if err := ls.l.read(&m); err != nil {
			return
		}
		switch m.Type {
		case MsgOutput:
			_, _ = io.WriteString(ls.out, m.Text+"\n")
		case MsgStopped:
			ls.exits = m.Exits
			ls.stopped <- m.Error
			return
		}
	}
}

// Run is the host's number for the run.
func (ls *Lease) Run() int { return ls.run }

// Told are the properties the services told the host, once they are ready.
func (ls *Lease) Told() ([]lifecycle.ToldValues, error) { return ls.told, nil }

// Exits are how the services the host started for the run ended, once
// Stop has stopped them, with their coverage folders in the project as the
// run sees it.
func (ls *Lease) Exits() []lifecycle.Exit {
	out := slices.Clone(ls.exits)
	for i := range out {
		if d := out[i].CoverageDir; d != "" {
			out[i].CoverageDir = filepath.Join(ls.dir, filepath.FromSlash(d))
		}
	}
	return out
}

// Started are the services the host started for the run, in start order.
func (ls *Lease) Started() []string { return ls.started }

// Reports says whether the host wants the run's reports (an editor shows
// them).
func (ls *Lease) Reports() bool { return ls.report }

// Report sends a line of the run's TeamCity report.
func (ls *Lease) Report(line string) {
	if ls.report {
		_ = ls.l.write(Message{Type: MsgReport, Text: line})
	}
}

// Result sends how the run's scenarios ended.
func (ls *Lease) Result(r Result) { _ = ls.l.write(Message{Type: MsgResult, Result: &r}) }

// Stop asks the host to stop the services and clean up after them, and
// waits until it has.
func (ls *Lease) Stop(ctx context.Context) error {
	defer ls.conn.Close()
	// A host that stopped has sent its stopped message, which is the answer
	// even when this can no longer be sent.
	_ = ls.l.write(Message{Type: MsgStop})
	select {
	case f, ok := <-ls.stopped:
		if !ok {
			return lost(errors.New("the connection ended before the services were stopped"))
		}
		if f != nil {
			return f.err()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reporter is an io.Writer for a run's TeamCity report: each line goes to
// the lease, once there is one.
type Reporter struct {
	mu    sync.Mutex
	lease *Lease
	buf   []byte
}

// Use sends what is written from now on to ls.
func (r *Reporter) Use(ls *Lease) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lease = ls
}

func (r *Reporter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lease == nil || !r.lease.report {
		return len(p), nil
	}
	r.buf = append(r.buf, p...)
	for {
		i := strings.IndexByte(string(r.buf), '\n')
		if i < 0 {
			return len(p), nil
		}
		r.lease.Report(string(r.buf[:i]))
		r.buf = r.buf[i+1:]
	}
}
