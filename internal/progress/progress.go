// Package progress says what axx is doing while a step takes time: a
// download, a build, a check of the project on another version.
//
// Every such step starts with one line, for everyone: a person, an agent,
// CI. What follows depends on who reads:
//
//   - A person at a terminal sees that line live: it shows that axx works
//     (a mark that turns), how long it has, and how far a download is. When
//     the step ends, the line is left as it started.
//   - Anything else (an agent, a pipe, CI, --json) gets nothing that
//     rewrites a line. A step that goes on says so every half minute, in a
//     line of its own, so that neither a reader nor a harness that ends a
//     silent command takes it for stuck.
//
// So what is left on the screen is the same lines either way. It all goes
// where the Reporter writes, axx's standard error: a command's result on
// standard output is never mixed with it.
package progress

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// StillEvery is how often a step that goes on says so, where its line
	// is not live.
	StillEvery = 30 * time.Second
	// SlowAfter is how long a step that is quick most times (IfSlow) goes
	// on before it says anything.
	SlowAfter = time.Second
	// redrawEvery is how often a live line is written again.
	redrawEvery = 100 * time.Millisecond
)

// frames are the marks that turn on a live line.
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Options says how a Reporter writes.
type Options struct {
	// Live rewrites a step's line while it goes on: for a person at a
	// terminal, and no one else.
	Live bool
	// Width is the terminal's width, asked each time a live line is
	// written (nil, or 0: 80 columns). A live line never wraps.
	Width func() int
	// ASCII uses marks every terminal has.
	ASCII bool
	// Still and Slow are StillEvery and SlowAfter, when they are to be
	// others (0: those).
	Still, Slow time.Duration
}

// Reporter writes what axx is doing to one writer. It is a writer too:
// what else is written through it while a step's line is live goes above
// that line. A nil Reporter says nothing.
type Reporter struct {
	mu    sync.Mutex
	out   io.Writer
	opts  Options
	steps []*Step // those that go on, the latest last
	shown int     // the columns of the live line on the screen
	mid   bool    // what was written through it last did not end its line
	frame int
	stop  chan struct{}
	// pending is what a call writes, sent to out at once when it ends: a
	// live line is taken off and written again in one write, so that a
	// terminal never shows the moment between.
	pending bytes.Buffer
	// For tests: the time, and how often steps are looked at.
	now   func() time.Time
	every time.Duration
	still time.Duration
	slow  time.Duration
}

// New is a Reporter that writes to out.
func New(out io.Writer, opts Options) *Reporter {
	r := &Reporter{out: out, opts: opts, now: time.Now, every: redrawEvery, still: StillEvery, slow: SlowAfter}
	if !opts.Live {
		r.every = 250 * time.Millisecond
	}
	if opts.Still > 0 {
		r.still = opts.Still
	}
	if opts.Slow > 0 {
		r.slow = opts.Slow
	}
	// Often enough to say each in time.
	r.every = min(r.every, r.still/2, r.slow/2)
	return r
}

// On is the Reporter a writer is, or one that writes plain lines to it: for
// code that is handed a writer for what it says (a Log), which may be a
// Reporter.
func On(w io.Writer) *Reporter {
	if r, ok := w.(*Reporter); ok {
		return r
	}
	if w == nil {
		return nil
	}
	return New(w, Options{})
}

// Step is something axx does that can take a while, from Start to Done. A
// nil Step does nothing.
type Step struct {
	r     *Reporter
	line  string
	short string
	began time.Time
	said  time.Time // when it last said it goes on, where its line is not live
	done  atomic.Int64
	total atomic.Int64
	ended bool
	// written says that its line is on the screen to stay: a live line is
	// written so when its step ends, or when another starts inside it.
	written bool
	// quiet says that it has said nothing yet: a step that is quick most
	// times says what it does only once it has gone on for a while.
	quiet bool
}

// Start says that a step begins: line is the whole line, as it stays on the
// screen ("axx: downloading axx 0.8.0 (once; kept for later runs)"), and
// short what the step does, in a few words, for the line that says it still
// goes on ("downloading axx 0.8.0"). End it with Done.
func (r *Reporter) Start(line, short string) *Step {
	return r.start(line, short, false)
}

// IfSlow is Start for a step that is quick most times, like a question to
// GitHub: it says nothing unless the step goes on for SlowAfter, so that
// the usual case adds no line.
func (r *Reporter) IfSlow(line, short string) *Step {
	return r.start(line, short, true)
}

func (r *Reporter) start(line, short string, quiet bool) *Step {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.flush()
	now := r.now()
	s := &Step{r: r, line: line, short: short, began: now, said: now, quiet: quiet}
	switch {
	case quiet:
		r.steps = append(r.steps, s)
	case r.opts.Live:
		r.erase()
		// The step this one starts inside keeps its place above it, when
		// it has said what it does.
		if n := len(r.steps); n > 0 && !r.steps[n-1].written && !r.steps[n-1].quiet {
			r.steps[n-1].written = true
			fmt.Fprintln(&r.pending, r.steps[n-1].line)
		}
		r.steps = append(r.steps, s)
		r.draw()
	default:
		r.steps = append(r.steps, s)
		fmt.Fprintln(&r.pending, line)
	}
	if r.stop == nil {
		r.stop = make(chan struct{})
		go r.run(r.stop)
	}
	return s
}

// Bytes says how far a download is: done of total bytes (total 0 or less
// when the size is not known).
func (s *Step) Bytes(done, total int64) {
	if s == nil {
		return
	}
	s.done.Store(done)
	s.total.Store(total)
}

// Done ends the step. Its line stays as Start wrote it.
func (s *Step) Done() {
	if s == nil {
		return
	}
	r := s.r
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.flush()
	if s.ended {
		return
	}
	s.ended = true
	if r.opts.Live && !s.quiet {
		r.erase()
		if !s.written {
			fmt.Fprintln(&r.pending, s.line)
		}
	}
	for i, x := range r.steps {
		if x == s {
			r.steps = append(r.steps[:i], r.steps[i+1:]...)
			break
		}
	}
	if len(r.steps) == 0 {
		close(r.stop)
		r.stop = nil
		return
	}
	if r.opts.Live {
		r.draw()
	}
}

// Write writes what is not a step's line: above a live one, which is
// written again below it.
func (r *Reporter) Write(p []byte) (int, error) {
	if r == nil {
		return len(p), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.erase()
	r.pending.Write(p)
	if len(p) > 0 {
		r.mid = p[len(p)-1] != '\n'
	}
	r.draw()
	_, err := r.out.Write(r.pending.Bytes())
	r.pending.Reset()
	return len(p), err
}

// run looks at the steps that go on until none does: it writes the live
// line again, or says that a step still goes on.
func (r *Reporter) run(stop <-chan struct{}) {
	t := time.NewTicker(r.every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		r.mu.Lock()
		select {
		case <-stop:
			r.mu.Unlock()
			return
		default:
		}
		now := r.now()
		for _, s := range r.steps {
			// A step that was to be quick has gone on: it says what it does.
			if s.quiet && now.Sub(s.began) >= r.slow {
				s.quiet = false
				if !r.opts.Live {
					fmt.Fprintln(&r.pending, s.line)
				}
			}
		}
		if r.opts.Live {
			r.frame++
			r.erase()
			r.draw()
		} else {
			for _, s := range r.steps {
				if !s.quiet && now.Sub(s.said) >= r.still {
					s.said = now
					fmt.Fprintf(&r.pending, "axx: still %s (%s)\n", s.short, strings.Join(append([]string{elapsed(now.Sub(s.began))}, s.far()...), ", "))
				}
			}
		}
		r.flush()
		r.mu.Unlock()
	}
}

// flush sends what the call wrote to out. Callers hold mu.
func (r *Reporter) flush() {
	if r.pending.Len() > 0 {
		_, _ = r.out.Write(r.pending.Bytes())
		r.pending.Reset()
	}
}

// erase takes the live line off the screen. Callers hold mu.
func (r *Reporter) erase() {
	if r.shown == 0 {
		return
	}
	// Spaces over it, rather than a terminal's own way to clear a line:
	// every terminal has a carriage return.
	fmt.Fprintf(&r.pending, "\r%s\r", strings.Repeat(" ", r.shown))
	r.shown = 0
}

// draw writes the live line of the latest step that goes on. Callers hold
// mu, after erase.
func (r *Reporter) draw() {
	if !r.opts.Live || r.mid {
		return
	}
	var s *Step
	for _, x := range r.steps {
		if !x.quiet {
			s = x // the latest that says what it does
		}
	}
	if s == nil {
		return
	}
	mark := frames[r.frame%len(frames)]
	if r.opts.ASCII {
		mark = string(`|/-\`[r.frame%4])
	}
	tail := " " + mark
	var details []string
	details = append(details, s.far()...)
	if d := r.now().Sub(s.began); d >= time.Second {
		details = append(details, elapsed(d))
	}
	if len(details) > 0 {
		tail += " " + strings.Join(details, ", ")
	}
	width := 80
	if r.opts.Width != nil {
		if w := r.opts.Width(); w > 0 {
			width = w
		}
	}
	// One column short of the width: a line that fills it wraps on some
	// terminals. On a narrow one the line gives way, not how far the step is.
	text := s.line
	if room := width - 1 - utf8.RuneCountInString(tail); utf8.RuneCountInString(text) > room {
		if room < 2 {
			room = 2
		}
		text = string([]rune(text)[:room-1]) + "…"
	}
	text += tail
	fmt.Fprint(&r.pending, text)
	r.shown = utf8.RuneCountInString(text)
}

// far is how far the step's download is, when it is one.
func (s *Step) far() []string {
	done, total := s.done.Load(), s.total.Load()
	switch {
	case total > 0 && done <= total:
		return []string{fmt.Sprintf("%d%% of %s", done*100/total, size(total))}
	case done > 0:
		return []string{size(done)}
	}
	return nil
}

// elapsed is a duration to the second: 45s, 1m30s.
func elapsed(d time.Duration) string {
	return d.Truncate(time.Second).String()
}

// size is a number of bytes for a person: 840 KB, 5.1 MB, 38 MB.
func size(n int64) string {
	switch mb := float64(n) / (1 << 20); {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%d KB", n>>10)
	case mb < 10:
		return fmt.Sprintf("%.1f MB", mb)
	default:
		return fmt.Sprintf("%.0f MB", mb)
	}
}

// Reader counts what is read from r into a step's download of total bytes.
func (s *Step) Reader(r io.Reader, total int64) io.Reader {
	if s == nil {
		return r
	}
	s.Bytes(0, total)
	return &reader{r: r, s: s, total: total}
}

type reader struct {
	r     io.Reader
	s     *Step
	n     int64
	total int64
}

func (c *reader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.s.Bytes(c.n, c.total)
	return n, err
}
