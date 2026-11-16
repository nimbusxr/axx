package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Follower keeps the host on the axx its project runs on: the version
// axx.yaml names, or the installed axx. The host asks it every few seconds
// and each time a run ends, and restarts on that axx once no run is
// connected to it.
//
// A restart builds the project's packs again and runs the commands axx.yaml
// has around `axx host`, on this machine and as the person, as a start
// does. The host restarts by itself only when all of that is as it started
// with (Starts) and the axx is no older than its own; otherwise it asks the
// person first, where it asks about the services.
type Follower interface {
	// Next is the axx a start of the host would run on now, when that is
	// another than this one; nil while the host runs on it.
	Next(ctx context.Context) (*Next, error)
	// Ready gets that axx onto this machine (a release the project names
	// is downloaded once), so that a restart does not fail for it.
	Ready(ctx context.Context, n *Next) error
	// Starts is what a start runs of the project's own besides its
	// services, as text: see [Starts].
	Starts() (string, error)
}

// Next is another axx the host should run on.
type Next struct {
	// Version is its version.
	Version string
	// From is what names it, in words: "axx.yaml", or Installed.
	From string
	// Older says that it is older than the axx the host runs on.
	Older bool
	// ByHand says that this host cannot restart by itself (on Windows):
	// it tells the person, who restarts it.
	ByHand bool
}

// Installed is Next.From for the installed axx.
const Installed = "the installed axx"

// why is what changed, as a sentence's start: "axx.yaml now names axx 0.8.0".
func (n *Next) why() string {
	switch n.From {
	case "":
		return "the project now runs on axx " + n.Version
	case Installed:
		return "the installed axx is now " + n.Version
	}
	return fmt.Sprintf("%s now names axx %s", n.From, n.Version)
}

// Version states: what the host does about another axx.
const (
	// VersionWaiting: it restarts on it once its runs have ended.
	VersionWaiting = "waiting"
	// VersionRestarting: it restarts on it now.
	VersionRestarting = "restarting"
	// VersionStays: it stays on its own (Text says why).
	VersionStays = "stays"
)

const (
	// followEvery is how often the host asks its follower.
	followEvery = 2 * time.Second
	// followQuiet is how long no run may have been connected before the
	// host restarts: a run whose services the host does not start connects
	// for each scenario's browser or device, and nothing in between.
	followQuiet = 3 * time.Second
	// followRetry is how long the host waits before it tries again to get
	// an axx it could not get.
	followRetry = 30 * time.Second
)

// follow asks the follower until ctx ends, and sends the serve loop the axx
// to restart on, which it does when no run is connected then.
func (s *Server) follow(ctx context.Context, move chan<- *Next) {
	f := s.opts.Follow
	tick := time.NewTicker(s.followEvery)
	defer tick.Stop()
	said := ""
	say := func(state string, n *Next, text string) {
		if text == said {
			return
		}
		said = text
		e := Event{Event: "version", State: state, Text: text}
		if n != nil {
			e.Version = n.Version
		}
		s.event(e)
	}
	refused := ""  // the version the person refused: asked again when another is named
	approved := "" // the version and what a start runs that they said yes to
	ready := ""    // the version on this machine
	var failed struct {
		version string // the version it could not get
		at      time.Time
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.left:
		}
		n, err := f.Next(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			say(VersionStays, nil, fmt.Sprintf("the host stays on axx %s: %v", s.opts.Version, err))
			continue
		}
		if n == nil {
			said, refused = "", ""
			continue
		}
		if n.ByHand {
			say(VersionStays, n, fmt.Sprintf("%s: restart the host to run on it (here it does not restart by itself)", n.why()))
			continue
		}
		if n.Version == refused || (n.Version == failed.version && time.Since(failed.at) < s.followRetry) {
			continue
		}
		if connected, quiet := s.idle(); connected > 0 {
			say(VersionWaiting, n, fmt.Sprintf("%s: the host restarts on it once the runs connected to it have ended", n.why()))
			continue
		} else if !quiet {
			continue
		}
		if ready != n.Version {
			if err := f.Ready(ctx, n); err != nil {
				if ctx.Err() != nil {
					return
				}
				failed.version, failed.at = n.Version, time.Now()
				say(VersionStays, n, fmt.Sprintf("%s, which this machine cannot get, so the host stays on axx %s: %v", n.why(), s.opts.Version, err))
				continue
			}
			ready, failed.version = n.Version, ""
		}
		starts, err := f.Starts()
		if err != nil {
			say(VersionStays, n, fmt.Sprintf("%s, but what a restart would run cannot be read, so the host stays on axx %s: %v", n.why(), s.opts.Version, err))
			continue
		}
		if id := n.Version + "\n" + starts; (n.Older || starts != s.started) && id != approved {
			ok, asked := s.askRestart(n, starts)
			if ctx.Err() != nil {
				return
			}
			if !asked {
				continue // another question is open, or this one was dropped: asked again
			}
			if !ok {
				refused = n.Version
				say(VersionStays, n, fmt.Sprintf("not restarted: the host stays on axx %s until you restart it", s.opts.Version))
				continue
			}
			// What they approved is what restarts: the next round asks about
			// anything that changed while they read. Their yes holds when a
			// run comes first and the restart waits for it.
			approved = id
			if again, err := f.Starts(); err != nil || again != starts {
				continue
			}
		}
		select {
		case move <- n:
		case <-ctx.Done():
			return
		}
	}
}

// idle is how many connections the host serves, and whether none has been
// open for long enough to restart.
func (s *Server) idle() (connected int, quiet bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected, s.connected == 0 && time.Since(s.lastLeft) >= s.followQuiet
}

// errAfterServices is why a question about a restart is no longer asked
// when one about the services takes its place.
var errAfterServices = errors.New("the services come first; asked again after")

// askRestart asks the person whether the host may restart on n with what a
// start runs now, which is not what it started with, or on an older axx.
// It reports their answer, and whether they gave one: no question is asked
// while another is open.
func (s *Server) askRestart(n *Next, starts string) (ok, asked bool) {
	s.mu.Lock()
	if s.open != nil {
		s.mu.Unlock()
		return false, false
	}
	ctx, cancel := context.WithCancelCause(s.serving)
	q := &question{text: "restart on axx " + n.Version + "\n" + starts, done: make(chan struct{}), cancel: cancel, restart: true}
	s.open = q
	s.mu.Unlock()

	def := &Definition{Key: "restart", Text: starts}
	if n.Older {
		def.Warnings = append(def.Warnings, fmt.Sprintf("axx %s is older than the axx %s the host runs on: what later releases fixed in the host is not in it", n.Version, s.opts.Version))
	}
	ok, err := s.opts.Approver.Approve(ctx, Question{
		Command: n.From, Approved: s.started, Proposed: def,
		What: "the axx host again, on axx " + n.Version, Restart: n,
	})
	asked = err == nil && ctx.Err() == nil
	cancel(nil)
	s.mu.Lock()
	if s.open == q {
		s.open = nil
	}
	s.mu.Unlock()
	close(q.done)
	return ok && asked, asked
}

// Moved is the axx the host stopped serving to restart on, once Serve
// returned for it; nil when it stopped for another reason.
func (s *Server) Moved() *Next { return s.moved }

// Abandon takes back the announcement of the host of the project in dir
// that left to restart on another version of axx (the process pid, which a
// restart keeps) and did not come back: the axx it restarted on failed
// before it served. Runs then find no host, rather than wait for one.
func Abandon(dir string, pid int) {
	a, err := Find(dir)
	if err != nil || a == nil || a.Moving == nil || a.PID != pid {
		return
	}
	_ = os.Remove(AnnouncementFile(dir))
}
