package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Lease is the services a host started for a run, until the run stops them.
type Lease struct {
	conn    net.Conn
	l       *lines
	run     int
	report  bool
	started []string
	out     io.Writer

	stopped chan *Failure // the host's stopped message; closed when the connection ends
}

// Open asks the host a announced for the services req names, and waits
// until they are ready. Their output goes to out, and what the run waits
// for (another run, an approval) to status. When nothing answers at a's
// address, the error has the code CodeNotListening.
func Open(ctx context.Context, a *Announcement, req Request, out, status io.Writer) (*Lease, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", a.Addr)
	if err != nil {
		return nil, hostErr(CodeNotListening, "the axx host that .axx/host.json names (%s, pid %d) does not answer: %v", a.Addr, a.PID, err).
			WithHint("start `axx host` again, or delete .axx/host.json to start the services here")
	}
	req.Protocol, req.Token = Protocol, a.Token
	ls := &Lease{conn: conn, l: newLines(conn), out: out, stopped: make(chan *Failure, 1)}
	if err := ls.l.write(req); err != nil {
		_ = conn.Close()
		return nil, lost(err)
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
			return nil, lost(err)
		}
		switch m.Type {
		case MsgHello:
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
			ls.started = m.Services
			go ls.follow()
			return ls, nil
		}
	}
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
			ls.stopped <- m.Error
			return
		}
	}
}

// Run is the host's number for the run.
func (ls *Lease) Run() int { return ls.run }

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
