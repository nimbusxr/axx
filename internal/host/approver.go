package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Terminal is the terminal the person started the host in: it writes the
// host's events, and asks about a changed definition with what changed,
// reading y or n. A line that comes while it asks is written above the
// question, which it asks again below.
type Terminal struct {
	out         io.Writer
	color       bool
	interactive bool
	lines       chan string

	mu     sync.Mutex
	prompt string // the question it asks, while it asks one
}

// NewTerminal writes to out and reads answers from in. A terminal that is
// not interactive (no one types into in) refuses every change, and says so
// when the host starts.
func NewTerminal(in io.Reader, out io.Writer, color, interactive bool) *Terminal {
	t := &Terminal{out: out, color: color, interactive: interactive, lines: make(chan string)}
	if interactive {
		go func() {
			defer close(t.lines)
			sc := bufio.NewScanner(in)
			for sc.Scan() {
				t.lines <- sc.Text()
			}
		}()
	}
	return t
}

// println writes lines, above the question it asks.
func (t *Terminal) println(lines ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prompt != "" {
		if t.color {
			fmt.Fprint(t.out, "\r\x1b[2K") // the question's line, written again below
		} else {
			fmt.Fprintln(t.out)
		}
	}
	for _, l := range lines {
		fmt.Fprintln(t.out, l)
	}
	if t.prompt != "" {
		fmt.Fprint(t.out, t.prompt)
	}
}

func (t *Terminal) ask(prompt string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prompt = prompt
	fmt.Fprint(t.out, prompt)
}

func (t *Terminal) asked(after string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if after != "" {
		fmt.Fprintln(t.out)
		fmt.Fprintln(t.out, after)
	}
	t.prompt = ""
}

func (t *Terminal) warning(s string) string {
	if t.color {
		return "\x1b[1;31mwarning:\x1b[0m " + s
	}
	return "warning: " + s
}

// Approve shows the change and waits for y or n, or until the question is
// no longer asked (context.Cause says why).
func (t *Terminal) Approve(ctx context.Context, q Question) (bool, error) {
	head := "the services' definition changed since you last approved it"
	if q.Run > 0 {
		head = fmt.Sprintf("run %d (%s): %s", q.Run, q.Command, head)
	}
	out := []string{"", head}
	for _, line := range strings.Split(strings.TrimSuffix(Diff(q.Approved, q.Proposed.Text), "\n"), "\n") {
		switch {
		case t.color && strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			line = "\x1b[32m" + line + "\x1b[0m"
		case t.color && strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			line = "\x1b[31m" + line + "\x1b[0m"
		case t.color && strings.HasPrefix(line, "@@"):
			line = "\x1b[36m" + line + "\x1b[0m"
		}
		out = append(out, "  "+line)
	}
	for _, w := range q.Proposed.Warnings {
		out = append(out, t.warning(w))
	}
	if !t.interactive {
		t.println(append(out, "not approved: no one can answer here; start `axx host` in a terminal to approve changes")...)
		return false, nil
	}
	t.println(out...)
	t.drain()
	t.ask("approve? [y/N] ")
	for {
		select {
		case <-ctx.Done():
			t.asked(fmt.Sprintf("no longer asked: %v", context.Cause(ctx)))
			return false, ctx.Err()
		case line, ok := <-t.lines:
			if !ok {
				t.asked("")
				return false, nil
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes":
				t.asked("")
				return true, nil
			case "", "n", "no":
				t.asked("")
				return false, nil
			}
			t.mu.Lock()
			fmt.Fprint(t.out, t.prompt)
			t.mu.Unlock()
		}
	}
}

// drain drops what was typed before the question: a y typed then answers
// nothing.
func (t *Terminal) drain() {
	for {
		select {
		case _, ok := <-t.lines:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// Event writes what happened in the host, a line or a few.
func (t *Terminal) Event(e Event) {
	switch e.Event {
	case "serving":
		lines := []string{fmt.Sprintf("axx host: serving %s on %s (Ctrl-C stops it)", e.Dir, e.Addr)}
		what := "axx.yaml's services: " + strings.Join(e.Services, ", ")
		if len(e.Sources) > 0 {
			what += "; " + strings.Join(e.Sources, ", ")
		}
		if !e.Changed { // else it asks, with the warnings
			lines = append(lines, "  approved: "+what)
			for _, w := range e.Warnings {
				lines = append(lines, t.warning(w))
			}
		}
		if !t.interactive {
			lines = append(lines, t.warning("no one can answer here (this is not a terminal): runs whose services' definition changed are refused; start `axx host` in a terminal to approve changes"))
		}
		t.println(append(lines, "waiting for runs")...)
	case "run":
		switch e.State {
		case StateQueued:
			t.println(fmt.Sprintf("run %d: %s", e.Run, e.Command))
		case StateWaiting:
			t.println(fmt.Sprintf("run %d: waits for the run before it", e.Run))
		case StateApproval:
			t.println(fmt.Sprintf("run %d: waits for your answer", e.Run))
		case StateStarting:
			t.println(fmt.Sprintf("run %d: starting %s", e.Run, strings.Join(e.Services, ", ")))
		case StateStarted:
			t.println(fmt.Sprintf("run %d: ready in %.1fs", e.Run, e.Seconds))
		case StateEnded:
			var parts []string
			if r := e.Result; r != nil {
				parts = append(parts, fmt.Sprintf("%d scenarios: %d passed, %d failed", r.Passed+r.Failed+r.Other, r.Passed, r.Failed))
			}
			switch {
			case e.Error != nil && e.Error.Code != "":
				parts = append(parts, fmt.Sprintf("%s [%s]", e.Error.Message, e.Error.Code))
			case e.Error != nil:
				parts = append(parts, e.Error.Message)
			default:
				parts = append(parts, fmt.Sprintf("stopped and cleaned up in %.1fs", e.Seconds))
			}
			t.println(fmt.Sprintf("run %d: %s", e.Run, strings.Join(parts, "; ")))
		}
	}
}

// IDE asks through the editor that started the host (`axx host --ide`):
// an approval event for each question, answered on the host's standard
// input with {"approve": id} or {"refuse": id}. Only the editor holds that
// input; a run cannot answer.
type IDE struct {
	events func(Event)

	mu      sync.Mutex
	next    int
	pending map[int]chan bool
	closed  bool
}

// NewIDE asks with events and reads the answers from in. When in ends (the
// editor is gone), every question is refused, and gone is called.
func NewIDE(in io.Reader, events func(Event), gone func()) *IDE {
	a := &IDE{events: events, pending: map[int]chan bool{}}
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var ans struct {
				Approve int `json:"approve"`
				Refuse  int `json:"refuse"`
			}
			if json.Unmarshal(sc.Bytes(), &ans) != nil {
				continue
			}
			if ans.Approve != 0 {
				a.answer(ans.Approve, true)
			}
			if ans.Refuse != 0 {
				a.answer(ans.Refuse, false)
			}
		}
		a.mu.Lock()
		a.closed = true
		for id, ch := range a.pending {
			ch <- false
			delete(a.pending, id)
		}
		a.mu.Unlock()
		if gone != nil {
			gone()
		}
	}()
	return a
}

func (a *IDE) answer(id int, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ch, found := a.pending[id]; found {
		ch <- ok
		delete(a.pending, id)
	}
}

// Approve sends the question and waits for the editor's answer.
func (a *IDE) Approve(ctx context.Context, q Question) (bool, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false, nil
	}
	a.next++
	id := a.next
	ch := make(chan bool, 1)
	a.pending[id] = ch
	a.mu.Unlock()
	a.events(Event{
		Event: "approval", ID: id, Run: q.Run, Command: q.Command,
		Approved: q.Approved, Proposed: q.Proposed.Text, Diff: Diff(q.Approved, q.Proposed.Text), Warnings: q.Proposed.Warnings,
	})
	select {
	case ok := <-ch:
		answer := "refused"
		if ok {
			answer = "approved"
		}
		a.events(Event{Event: "answered", ID: id, Run: q.Run, Answer: answer})
		return ok, nil
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		a.events(Event{Event: "answered", ID: id, Run: q.Run, Answer: "abandoned"})
		return false, ctx.Err()
	}
}
