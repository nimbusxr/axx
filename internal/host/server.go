package host

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// Options configures a host.
type Options struct {
	// Load is how the host loads the project's configuration itself (its
	// own --config, --profile, -D and --set): the definition starting it
	// approves. WorkDir is the project.
	Load config.LoadOptions
	// Addr is where it listens: a loopback address (default 127.0.0.1:0,
	// a port the system picks).
	Addr string
	// Approver asks the person about a changed definition.
	Approver Approver
	// Events receives what happens, for the host's terminal or editor. Nil
	// discards.
	Events func(Event)
	// Report asks runs for their reports: an editor shows them.
	Report bool
	// Approvals is the file approvals are kept in ("" for the default,
	// ApprovalsFile; "-" for none, in memory only).
	Approvals string
	// Env is the environment services start with (default os.Environ()).
	Env []string
	// Version is this axx's version, which the announcement carries.
	Version string
	// Logger receives the services' progress. Nil discards.
	Logger *slog.Logger
}

// Approver asks the person whether runs may start services by a changed
// definition.
type Approver interface {
	// Approve returns their answer, or ctx's error once the run that asked
	// no longer waits for it.
	Approve(ctx context.Context, q Question) (bool, error)
}

// Question is what an approval asks.
type Question struct {
	Run     int
	Command string
	// Approved is the last approved definition of the same configuration
	// ("" when there is none); Proposed the one the run would start.
	Approved string
	Proposed *Definition
}

// Event is something that happened in the host, for its terminal or editor
// (`axx host --ide` writes each one as a line of JSON).
type Event struct {
	// Event is serving, run, report, approval or answered.
	Event string `json:"event"`

	// serving: where the host listens, and the definition starting it
	// approved, or that changed since the last approved one (Changed: the
	// first run asks).
	Addr     string   `json:"addr,omitempty"`
	Dir      string   `json:"dir,omitempty"`
	Services []string `json:"services,omitempty"`
	Sources  []string `json:"sources,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Changed  bool     `json:"changed,omitempty"`

	// run: a run's state (queued, waiting, approval, starting, started,
	// stopping, ended), with its error and result once it ended. report:
	// a line of its TeamCity report. Dir is its project directory, which
	// the report's locations are in.
	Run     int      `json:"run,omitempty"`
	Command string   `json:"command,omitempty"`
	IDE     string   `json:"ide,omitempty"`
	State   string   `json:"state,omitempty"`
	Error   *Failure `json:"error,omitempty"`
	Result  *Result  `json:"result,omitempty"`
	Seconds float64  `json:"seconds,omitempty"`
	Line    string   `json:"line,omitempty"`

	// approval: a question for the person (an editor answers it on the
	// host's standard input); answered: its answer (approved, refused, or
	// abandoned when the run stopped waiting).
	ID       int    `json:"id,omitempty"`
	Approved string `json:"approved,omitempty"`
	Proposed string `json:"proposed,omitempty"`
	Diff     string `json:"diff,omitempty"`
	Answer   string `json:"answer,omitempty"`
}

// Run states.
const (
	StateQueued   = "queued"
	StateWaiting  = "waiting"
	StateApproval = "approval"
	StateStarting = "starting"
	StateStarted  = "started"
	StateStopping = "stopping"
	StateEnded    = "ended"
)

// Server is a host: it serves one project's runs.
type Server struct {
	opts      Options
	dir       string
	env       []string
	approvals *approvals
	start     *Definition
	changed   bool
	log       *slog.Logger
	token     string

	turn chan struct{} // held by the run that has the services
	// wait is how long a run waits for the person's answer; the question
	// stays open after.
	wait time.Duration
	// serving ends when the host stops: the questions it asks end then.
	serving context.Context

	mu     sync.Mutex
	runs   int
	holder struct {
		run     int
		command string
	}
	open    *question       // the question the person is asked, until they answer it
	refused map[string]bool // the definitions they refused, refused again until the host restarts
	events  sync.Mutex
}

// question is a changed definition the person is asked about: open until
// they answer, or a newer definition replaces it, whether or not a run
// still waits for the answer.
type question struct {
	text     string
	done     chan struct{}
	cancel   context.CancelCauseFunc
	answered bool // done by their answer, ok
	ok       bool
}

// Why a question is no longer asked.
var (
	errReplaced = errors.New("the services' definition changed again; the next run asks about it")
	errStopping = errors.New("the axx host is stopping")
)

// New prepares a host for the project opts.Load finds: it reads the
// definition of its services and approves it, unless the person approved
// another of the same configuration before (then the first run asks).
func New(ctx context.Context, opts Options) (*Server, error) {
	if opts.Approver == nil {
		return nil, errors.New("host: no approver")
	}
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	cfg, err := config.Load(opts.Load)
	if err != nil {
		return nil, err
	}
	s := &Server{
		opts: opts, dir: cfg.Dir, env: opts.Env, log: logger, turn: make(chan struct{}, 1),
		wait: time.Minute, serving: context.Background(), refused: map[string]bool{},
	}
	path := opts.Approvals
	switch path {
	case "":
		if path, err = ApprovalsFile(cfg.Dir); err != nil {
			return nil, err
		}
	case "-":
		path = ""
	}
	s.approvals = loadApprovals(path, cfg.Dir)
	if s.start, err = Define(ctx, cfg, key(cfg, opts.Load.Profile), s.env); err != nil {
		return nil, err
	}
	switch _, ok := s.approvals.last(s.start.Key); {
	case s.approvals.has(s.start.Text):
	case !ok:
		if err := s.approvals.add(s.start.Key, s.start.Text); err != nil {
			return nil, fmt.Errorf("cannot keep the approval: %w", err)
		}
	default:
		s.changed = true
	}
	return s, nil
}

// Dir is the project the host serves.
func (s *Server) Dir() string { return s.dir }

// key names a configuration: its file, relative to the project, and its
// profile.
func key(cfg *config.Config, profile string) string {
	k := "axx.yaml"
	if cfg.File != "" {
		if rel, err := filepath.Rel(cfg.Dir, cfg.File); err == nil {
			k = filepath.ToSlash(rel)
		}
	}
	if profile != "" {
		k += ", profile " + profile
	}
	return k
}

// Serve listens, announces the host in the project and serves runs until
// ctx ends; then it waits for every run's services to be stopped and
// cleaned up.
func (s *Server) Serve(ctx context.Context) error {
	file := AnnouncementFile(s.dir)
	if a, _ := Find(s.dir); a != nil && Answers(ctx, a.Addr) {
		return hostErr(CodeRunning, "an axx host already serves %s (pid %d, %s)", s.dir, a.PID, a.Addr).
			WithHint("its runs are yours too: stop it first to start another")
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.opts.Addr)
	if err != nil {
		return hostErr(CodeNotListening, "cannot listen on %s: %v", s.opts.Addr, err).
			WithHint("pick another address with --addr")
	}
	defer ln.Close()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	s.token = hex.EncodeToString(b)
	addr := ln.Addr().(*net.TCPAddr)
	if addr.IP.IsUnspecified() {
		addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: addr.Port}
	}
	ann := Announcement{Addr: addr.String(), Token: s.token, PID: os.Getpid(), Version: s.opts.Version, Started: time.Now().UTC()}
	data, _ := json.MarshalIndent(ann, "", "  ")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(file, data, 0o600); err != nil {
		return err
	}
	defer func() {
		// Only its own: another host may have announced itself since.
		if a, _ := Find(s.dir); a != nil && a.Token == s.token {
			_ = os.Remove(file)
		}
	}()
	serving, stop := context.WithCancelCause(ctx)
	defer stop(errStopping)
	s.serving = serving
	s.event(Event{
		Event: "serving", Addr: ann.Addr, Dir: s.dir,
		Services: s.start.Services, Sources: s.start.Sources, Warnings: s.start.Warnings, Changed: s.changed,
	})
	if s.changed {
		// It changed since the person last approved one: ask now, while they
		// are here, rather than when a run needs it.
		s.ask(s.start, 0, "")
	}

	var wg sync.WaitGroup
	conns := make(chan net.Conn)
	go func() {
		defer close(conns)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- c
		}
	}()
	for {
		select {
		case <-ctx.Done():
			_ = ln.Close()
			for c := range conns {
				_ = c.Close()
			}
			wg.Wait()
			return nil
		case c, ok := <-conns:
			if !ok {
				wg.Wait()
				return ctx.Err()
			}
			wg.Go(func() { s.handle(ctx, c) })
		}
	}
}

// Answers reports whether something listens at addr.
func Answers(ctx context.Context, addr string) bool {
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func (s *Server) event(e Event) {
	if s.opts.Events == nil {
		return
	}
	s.events.Lock()
	defer s.events.Unlock()
	s.opts.Events(e)
}

// handle serves one run: its turn with the services, from its request to
// their cleanup.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	l := newLines(conn)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if err := l.read(&req); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	refuse := func(err error) { _ = l.write(Message{Type: MsgError, Error: failure(err)}) }
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) != 1 {
		refuse(hostErr(CodeRefused, "the axx host refused the run: it did not give the token of %s", AnnouncementFile(s.dir)).
			WithHint("runs of the project the host serves read it there"))
		return
	}
	if req.Protocol != Protocol {
		refuse(hostErr(CodeRefused, "the axx host speaks protocol %d, and this axx %d", Protocol, req.Protocol).
			WithHint("use the same version of axx for the host and the runs"))
		return
	}
	s.mu.Lock()
	s.runs++
	run := s.runs
	s.mu.Unlock()
	began := time.Now()
	ev := func(state string, e Event) {
		e.Event, e.Run, e.Command, e.IDE, e.State = "run", run, req.Command, req.IDE, state
		s.event(e)
	}
	if err := l.write(Message{Type: MsgHello, Run: run, Report: s.opts.Report}); err != nil {
		return
	}
	ev(StateQueued, Event{Dir: req.Dir})

	// What the run sends: its report and result, and when it is done with
	// the services (it asks, or its connection ends).
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var result struct {
		sync.Mutex
		r *Result
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		for {
			var m Message
			if err := l.read(&m); err != nil {
				return
			}
			switch m.Type {
			case MsgReport:
				if s.opts.Report {
					s.event(Event{Event: "report", Run: run, Line: m.Text})
				}
			case MsgResult:
				result.Lock()
				result.r = m.Result
				result.Unlock()
			case MsgStop:
				return
			}
		}
	}()
	// ended reports how the run ended; Seconds is how long since, the
	// services' stopping once they started.
	ended := func(err error, since time.Time) {
		e := Event{Seconds: time.Since(since).Seconds()}
		if err != nil {
			e.Error = failure(err)
		}
		result.Lock()
		e.Result = result.r
		result.Unlock()
		ev(StateEnded, e)
	}

	if !s.take(runCtx, run, req.Command, l, func() { ev(StateWaiting, Event{}) }) {
		ended(hostErr(CodeInterrupted, "the run stopped waiting for its turn"), began)
		return
	}
	defer func() { <-s.turn }()

	cfg, def, err := s.define(runCtx, req)
	if err != nil {
		refuse(private(err))
		ended(err, began)
		return
	}
	if err := s.approved(runCtx, def, run, req.Command, l, func() { ev(StateApproval, Event{}) }); err != nil {
		refuse(err)
		ended(err, began)
		return
	}

	out := &outputLines{send: func(line string) { _ = l.write(Message{Type: MsgOutput, Text: line}) }}
	mgr, err := lifecycle.New(cfg.Services, lifecycle.Options{
		ConfigDir: cfg.Dir, Stdout: out, Stderr: out, Logger: s.log,
		Attach: set(req.Attach), Debug: set(req.Debug), DebugAll: req.DebugAll,
		StateFile: lifecycle.StateFile(cfg.Dir), Env: s.env,
	})
	if err != nil {
		refuse(err)
		ended(err, began)
		return
	}
	names := req.Services
	if names == nil {
		for _, svc := range cfg.Services {
			if svc.IsEnabled() {
				names = append(names, svc.Name)
			}
		}
	}
	ev(StateStarting, Event{Services: names})
	starting := time.Now()
	if err := mgr.Start(runCtx, names); err != nil {
		out.flush()
		refuse(err)
		ended(err, began)
		return
	}
	_ = l.write(Message{Type: MsgStarted, Services: mgr.Started()})
	ev(StateStarted, Event{Services: mgr.Started(), Seconds: time.Since(starting).Seconds()})

	<-runCtx.Done()
	if ctx.Err() == nil {
		<-done // its result, which it sends before it stops
	}
	ev(StateStopping, Event{})
	stopping := time.Now()
	err = mgr.Stop(context.WithoutCancel(ctx))
	out.flush()
	m := Message{Type: MsgStopped}
	if err != nil {
		m.Error = failure(err)
	}
	_ = l.write(m)
	ended(err, stopping)
}

// approved returns nil once the person approved def: before, or now. A run
// waits for their answer up to s.wait; the question stays open after, so
// that the next run with the same definition has the answer. A definition
// they refused is refused again until the host restarts.
func (s *Server) approved(ctx context.Context, def *Definition, run int, command string, l *lines, waiting func()) error {
	if s.approvals.has(def.Text) {
		return nil
	}
	s.mu.Lock()
	refused := s.refused[def.Text]
	s.mu.Unlock()
	if refused {
		return hostErr(CodeNotApproved, "the person running the axx host refused this change of the services' definition").
			WithHint("change axx.yaml's services and compose files back, or ask them; the host asks again about a newer change, or after it restarts")
	}
	q := s.ask(def, run, command)
	_ = l.write(Message{Type: MsgWaiting, Reason: fmt.Sprintf(
		"the services' definition changed since it was approved: waiting up to %s for the person running the axx host to approve it", about(s.wait))})
	waiting()
	timer := time.NewTimer(s.wait)
	defer timer.Stop()
	select {
	case <-q.done:
	case <-timer.C:
		return hostErr(CodeNotAnswered, "the person running the axx host has not answered yet whether runs may start the services' changed definition").
			WithHint("the question stays open where the host runs; tell them, and run again once they answered")
	case <-ctx.Done():
		return axxerr.Wrap(ctx.Err(), CodeInterrupted, exitcode.Interrupted, "the run stopped waiting for the approval; the question stays open where the host runs")
	}
	switch {
	case !q.answered:
		why := context.Cause(s.serving)
		if why == nil {
			why = errReplaced
		}
		return hostErr(CodeInterrupted, "the axx host no longer asks: %v", why)
	case !q.ok:
		return hostErr(CodeNotApproved, "the person running the axx host refused the services' changed definition").
			WithHint("they saw what changed where they run the host; change axx.yaml's services and compose files back, or ask them")
	}
	return nil
}

// about says how long d is, for a person: "a minute", "30 seconds".
func about(d time.Duration) string {
	switch {
	case d == time.Minute:
		return "a minute"
	case d%time.Minute == 0:
		return fmt.Sprintf("%d minutes", d/time.Minute)
	case d >= time.Second:
		return fmt.Sprintf("%d seconds", d/time.Second)
	}
	return d.String()
}

// ask asks the person about def, unless they are asked about it already:
// a question about another definition is no longer asked. Run is the run
// that asks (0: the host, as it starts).
func (s *Server) ask(def *Definition, run int, command string) *question {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open != nil && s.open.text == def.Text {
		return s.open
	}
	ctx, cancel := context.WithCancelCause(s.serving)
	if prev := s.open; prev != nil {
		prev.cancel(errReplaced)
	}
	q := &question{text: def.Text, done: make(chan struct{}), cancel: cancel}
	s.open = q
	approved, _ := s.approvals.last(def.Key)
	go func() {
		ok, err := s.opts.Approver.Approve(ctx, Question{Run: run, Command: command, Approved: approved, Proposed: def})
		asked := err == nil && ctx.Err() == nil // an answer, not a question no longer asked
		cancel(nil)
		s.mu.Lock()
		if s.open == q {
			s.open = nil
		}
		if asked {
			q.answered, q.ok = true, ok
			if ok {
				if err := s.approvals.add(def.Key, def.Text); err != nil {
					s.log.Warn("cannot keep the approval", "error", err)
				}
			} else {
				s.refused[def.Text] = true
			}
		}
		s.mu.Unlock()
		close(q.done)
	}()
	return q
}

// take waits for the run's turn with the services, telling it who has
// them meanwhile. It reports false when the run stopped waiting.
func (s *Server) take(ctx context.Context, run int, command string, l *lines, waiting func()) bool {
	held := func() {
		s.mu.Lock()
		s.holder.run, s.holder.command = run, command
		s.mu.Unlock()
	}
	select {
	case s.turn <- struct{}{}:
		held()
		return true
	default:
	}
	s.mu.Lock()
	other, cmd := s.holder.run, s.holder.command
	s.mu.Unlock()
	_ = l.write(Message{Type: MsgWaiting, Reason: fmt.Sprintf("waiting for run %d (%s) to finish with the services", other, cmd)})
	waiting()
	select {
	case s.turn <- struct{}{}:
		held()
		return true
	case <-ctx.Done():
		return false
	}
}

// define loads the configuration the run loaded, as the host's machine
// gives it, and reads its definition.
func (s *Server) define(ctx context.Context, req Request) (*config.Config, *Definition, error) {
	opts := config.LoadOptions{
		WorkDir: s.dir, Profile: req.Profile, Properties: req.Properties, Settings: req.Settings,
		// The run's profile, not the host's environment's.
		LookupEnv: func(k string) (string, bool) {
			if k == "AXX_PROFILE" {
				return "", false
			}
			return os.LookupEnv(k)
		},
	}
	if req.Config != "" {
		if !filepath.IsLocal(filepath.FromSlash(req.Config)) {
			return nil, nil, hostErr(CodeRefused, "the run's configuration %s is not in the project the axx host serves (%s)", req.Config, s.dir)
		}
		opts.Path = filepath.Join(s.dir, filepath.FromSlash(req.Config))
	}
	cfg, err := config.Load(opts)
	if err != nil {
		return nil, nil, axxerr.Wrap(err, CodeDefinition, exitcode.Environment,
			"axx.yaml cannot be loaded on the axx host's machine with this run's configuration, profile and settings")
	}
	if realPath(cfg.Dir) != realPath(s.dir) {
		return nil, nil, hostErr(CodeRefused, "the run's configuration is in %s, not in the project the axx host serves (%s)", cfg.Dir, s.dir)
	}
	def, err := Define(ctx, cfg, key(cfg, req.Profile), s.env)
	if err != nil {
		return nil, nil, err
	}
	return cfg, def, nil
}

// private is what a run is told of an error reading its definition: what
// failed, not the error itself, which can quote this machine's environment
// and its files outside the project (a compose file's include, a -f). The
// person sees the error where the host runs.
func private(err error) error {
	var ae *axxerr.Error
	switch {
	case errors.As(err, &ae) && ae.Code == CodeRefused:
		return err // the host's own refusal: nothing of the machine in it
	case errors.As(err, &ae) && ae.Code == CodeDefinition:
		// What failed, which the project says, without why.
		return hostErr(CodeDefinition, "%s; where the axx host runs, it shows why", ae.Message).
			WithHint("the host reads axx.yaml and the compose files on its own machine, with its environment: ask the person running it")
	}
	return hostErr(CodeDefinition, "the axx host could not read the services' definition with this run's configuration; where the host runs, it shows why").
		WithHint("the host reads axx.yaml and the compose files on its own machine, with its environment: ask the person running it")
}

func set(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// outputLines sends what the services print to the run, a line at a time.
type outputLines struct {
	mu   sync.Mutex
	buf  []byte
	send func(string)
}

var _ io.Writer = (*outputLines)(nil)

func (o *outputLines) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, p...)
	for {
		i := strings.IndexByte(string(o.buf), '\n')
		if i < 0 {
			break
		}
		o.send(strings.TrimSuffix(string(o.buf[:i]), "\r"))
		o.buf = o.buf[i+1:]
	}
	return len(p), nil
}

func (o *outputLines) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.buf) > 0 {
		o.send(string(o.buf))
		o.buf = nil
	}
}
