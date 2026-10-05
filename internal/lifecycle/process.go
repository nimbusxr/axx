package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"

	"github.com/nimbusxr/axx/internal/proc"
)

// drainWait bounds how long output is read after a process has exited
// (a grandchild that escaped the process group can hold the pipes open).
const drainWait = time.Second

// pipes connects a command's stdout and stderr to line handlers through OS
// pipes. Unlike exec's own copying, Wait then returns as soon as the process
// exits, even if a grandchild still holds the pipes open.
type pipes struct {
	r, w [2]*os.File
	done chan struct{} // closed when both streams hit EOF
}

func newPipes(cmd *exec.Cmd) (*pipes, error) {
	p := &pipes{done: make(chan struct{})}
	for i := range p.r {
		r, w, err := os.Pipe()
		if err != nil {
			p.close()
			return nil, err
		}
		p.r[i], p.w[i] = r, w
	}
	cmd.Stdout, cmd.Stderr = p.w[0], p.w[1]
	return p, nil
}

// started closes the parent's copies of the write ends (so EOF arrives when
// the process tree is gone) and pumps lines to the handlers.
func (p *pipes) started(onStdout, onStderr func(string)) {
	for _, w := range p.w {
		_ = w.Close()
	}
	var wg sync.WaitGroup
	for i, each := range [2]func(string){onStdout, onStderr} {
		wg.Go(func() { pump(p.r[i], each) })
	}
	go func() {
		wg.Wait()
		close(p.done)
	}()
}

// wait waits up to d for both streams to reach EOF.
func (p *pipes) wait(d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-p.done:
	case <-t.C:
	}
}

// drain waits up to d for the remaining output, then closes the pipes.
func (p *pipes) drain(d time.Duration) {
	p.wait(d)
	p.close()
}

func (p *pipes) close() {
	for _, f := range append(p.r[:], p.w[:]...) {
		if f != nil {
			_ = f.Close()
		}
	}
}

// process is a launched service.
type process struct {
	cmd       *exec.Cmd
	group     *proc.Group
	pipes     *pipes
	argv      []string
	startedAt time.Time
	// done is closed once the main process has exited; state is valid then.
	done  chan struct{}
	state *os.ProcessState
	// logMatched is closed when an output line matches ready.log.
	logMatched chan struct{}
	// stopping is set when axx stops the service, so its exit is expected.
	stopping atomic.Bool
}

// launch starts argv for a, streaming its output with the service's prefix into
// the console and the service's tail.
func (m *Manager) launch(a *service, argv []string) (*process, error) {
	name := a.cfg.Name
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:noctx // the service outlives Start's ctx; Stop terminates its process group
	cmd.Dir, cmd.Env = a.dir, a.env
	proc.Setup(cmd)
	pp, err := newPipes(cmd)
	if err != nil {
		return nil, envErr(CodeLaunchFailed, "service %s could not be started: %v", name, err)
	}
	if err := cmd.Start(); err != nil {
		pp.close()
		return nil, envErr(CodeLaunchFailed, "service %s could not be started: %v", name, err).
			WithHint("check services.%s.command: `%s` must be an executable on PATH or relative to %s", name, argv[0], a.dir)
	}
	group, err := proc.NewGroup(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		pp.close()
		return nil, envErr(CodeLaunchFailed, "service %s could not be put in its own process group: %v", name, err)
	}
	p := &process{
		cmd: cmd, group: group, pipes: pp, argv: argv,
		startedAt: time.Now(), done: make(chan struct{}),
	}

	var watchLog func(string)
	if re := a.ready.log; re != nil {
		p.logMatched = make(chan struct{})
		var matched atomic.Bool
		watchLog = func(line string) {
			if !matched.Load() && re.MatchString(line) && matched.CompareAndSwap(false, true) {
				close(p.logMatched)
			}
		}
	}
	handler := func(write func(string)) func(string) {
		return func(line string) {
			a.tail.add(line)
			if watchLog != nil {
				watchLog(line)
			}
			write(line)
		}
	}
	pp.started(
		handler(m.console.prefixed(m.console.stdout, name)),
		handler(m.console.prefixed(m.console.stderr, name)),
	)
	go func() {
		_ = cmd.Wait()
		p.state = cmd.ProcessState
		close(p.done)
		if !p.stopping.Load() {
			m.log.Warn("service exited on its own", "service", name, "status", describeExit(p.state))
		}
	}()
	return p, nil
}

// stopService stops a launched service's process group and runs its cleanup, which
// always runs, even if the service crashed or never became ready.
func (m *Manager) stopService(ctx context.Context, a *service) error {
	p := a.proc
	if p == nil {
		return nil // attached: not ours to stop
	}
	name := a.cfg.Name
	var errs []error
	p.stopping.Store(true)
	if p.group.Alive() {
		m.log.Info("stopping service", "service", name)
	}
	if err := terminate(ctx, p.group, a.cfg.Stop.Signal, a.cfg.Stop.Grace.Or(defaultGrace)); err != nil {
		_, pgid := p.group.IDs()
		errs = append(errs, envErr(CodeStopFailed, "service %s could not be stopped: %v", name, err).
			WithHint("stop its processes by hand (process group %d)", pgid))
	}
	t := time.NewTimer(killWait)
	select {
	case <-p.done:
	case <-t.C:
	}
	t.Stop()
	p.pipes.drain(drainWait)
	p.group.Release()

	var cleanupErr error
	if a.cleanup != nil {
		m.log.Info("running cleanup", "service", name, "command", displayArgv(a.cleanup))
		if cleanupErr = runCleanup(ctx, m.console, a.tail, name, a.cleanup, a.dir, a.env); cleanupErr != nil {
			errs = append(errs, cleanupErr)
		}
	}
	m.mu.Lock()
	a.cleaned = true
	// A failed cleanup stays in the state file, for `axx down` to run again.
	m.unclean = slices.DeleteFunc(m.unclean, func(sa stateService) bool { return sa.Name == name })
	if cleanupErr != nil {
		m.unclean = append(m.unclean, uncleaned(stateService{Name: name, Dir: a.dir, Cleanup: a.cleanup, Env: a.cfg.Env}))
	}
	m.saveStateLocked()
	m.mu.Unlock()
	return joinErrs(errs)
}

// runCleanup runs a cleanup command to completion, streaming its output
// with the service's prefix.
func runCleanup(ctx context.Context, con *console, tail *ring, name string, argv []string, dir string, env []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dir, env
	// Its own process group keeps a second Ctrl-C in the terminal from
	// interrupting it.
	proc.Setup(cmd)
	fail := func(err error) *axxerr.Error {
		return envErr(CodeCleanupFailed, "cleanup of service %s failed: `%s` %v", name, displayArgv(argv), err).
			WithHint("run it by hand in %s, and check services.%s.cleanup", dir, name)
	}
	pp, err := newPipes(cmd)
	if err != nil {
		return fail(err)
	}
	if err := cmd.Start(); err != nil {
		pp.close()
		return fail(err)
	}
	own := newRing(errorTailLines)
	handler := func(write func(string)) func(string) {
		return func(line string) {
			own.add(line)
			if tail != nil {
				tail.add(line)
			}
			write(line)
		}
	}
	pp.started(handler(con.prefixed(con.stdout, name)), handler(con.prefixed(con.stderr, name)))
	err = cmd.Wait()
	pp.drain(drainWait)
	if err != nil {
		e := fail(errors.New(describeExit(cmd.ProcessState)))
		if lines := own.snapshot(); len(lines) > 0 {
			e.Message += "; last output:" + formatTail(lines, errorTailLines)
		}
		return e
	}
	return nil
}

// describeExit says how a process ended.
func describeExit(ps *os.ProcessState) string {
	switch {
	case ps == nil:
		return "exited"
	case ps.ExitCode() >= 0:
		return fmt.Sprintf("exited with code %d", ps.ExitCode())
	default:
		return "was killed (" + ps.String() + ")"
	}
}

// joinErrs returns nil, the only error, or all errors joined.
func joinErrs(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errors.Join(errs...)
	}
}
