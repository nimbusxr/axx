package host

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func aQuestion() Question {
	return Question{Run: 4, Command: "axx run --compact", Approved: "services:\n  api: {}\n", Proposed: &Definition{
		Text:     "services:\n  api:\n    privileged: true\n",
		Warnings: []string{"compose service api is privileged"},
	}}
}

func TestIDEApprover(t *testing.T) {
	in, answers := io.Pipe()
	got := make(chan Event, 10)
	gone := make(chan struct{})
	a := NewIDE(in, func(e Event) { got <- e }, func() { close(gone) })

	result := make(chan bool, 1)
	go func() {
		ok, _ := a.Approve(context.Background(), aQuestion())
		result <- ok
	}()
	e := <-got
	if e.Event != "approval" || e.ID != 1 || e.Run != 4 || !strings.Contains(e.Diff, "+    privileged: true") || len(e.Warnings) != 1 || e.Proposed == "" || e.Approved == "" {
		t.Fatalf("approval: %+v", e)
	}
	_, _ = io.WriteString(answers, `{"approve": 7}`+"\n"+`{"approve": 1}`+"\n")
	if !<-result {
		t.Error("approved")
	}
	if e := <-got; e.Event != "answered" || e.ID != 1 || e.Answer != "approved" {
		t.Errorf("answered: %+v", e)
	}

	// A run that stops waiting abandons its question.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ok, _ := a.Approve(ctx, aQuestion())
		result <- ok
	}()
	<-got
	cancel()
	if <-result {
		t.Error("abandoned is not approved")
	}
	if e := <-got; e.Answer != "abandoned" {
		t.Errorf("answered: %+v", e)
	}

	// When the editor is gone, so are its answers: everything is refused.
	go func() {
		ok, _ := a.Approve(context.Background(), aQuestion())
		result <- ok
	}()
	<-got
	_ = answers.Close()
	if <-result {
		t.Error("refused once the editor is gone")
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("gone not called")
	}
	if ok, _ := a.Approve(context.Background(), aQuestion()); ok {
		t.Error("refused after the editor is gone")
	}
}

func TestTerminalApprover(t *testing.T) {
	in, typed := io.Pipe()
	var out syncBuffer
	a := NewTerminal(in, &out, false, true)
	result := make(chan bool, 1)
	go func() {
		ok, _ := a.Approve(context.Background(), aQuestion())
		result <- ok
	}()
	waitFor(t, "the prompt", func() bool { return strings.Contains(out.String(), "approve? [y/N] ") })
	_, _ = io.WriteString(typed, "maybe\nY\n")
	if !<-result {
		t.Error("y approves")
	}
	text := out.String()
	for _, want := range []string{"run 4 (axx run --compact): the services' definition changed", "  +    privileged: true", "warning: compose service api is privileged"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
	go func() {
		ok, _ := a.Approve(context.Background(), aQuestion())
		result <- ok
	}()
	// Its prompt: the first question's two, and this one.
	waitFor(t, "the prompt", func() bool { return strings.Count(out.String(), "approve? [y/N] ") == 3 })
	_, _ = io.WriteString(typed, "\n")
	if <-result {
		t.Error("the default is no")
	}

	var quiet syncBuffer
	if ok, _ := NewTerminal(strings.NewReader("y\n"), &quiet, false, false).Approve(context.Background(), aQuestion()); ok {
		t.Error("no one can answer a terminal that is not interactive")
	}
	if !strings.Contains(quiet.String(), "not approved: no one can answer here") {
		t.Errorf("says why:\n%s", quiet.String())
	}
}

// The terminal says which run a waiting run waits for: the one with the
// services, which a person may have to stop.
func TestTerminalSaysWhichRunHasTheServices(t *testing.T) {
	var out syncBuffer
	term := NewTerminal(strings.NewReader(""), &out, false, false)
	term.Event(Event{Event: "run", Run: 4, State: StateWaiting, Holder: 3, HolderCommand: "axx up"})
	if got := out.String(); got != "run 4: waits for run 3 (axx up), which has the services\n" {
		t.Errorf("the terminal says %q", got)
	}
}

func TestTerminalAsksAboutARestart(t *testing.T) {
	in, typed := io.Pipe()
	var out syncBuffer
	a := NewTerminal(in, &out, false, true)
	result := make(chan bool, 1)
	q := Question{
		Command: "axx.yaml", What: "the axx host again, on axx 0.8.0",
		Approved: "steps/pack.go 1111\n", Proposed: &Definition{Text: "steps/pack.go 2222\n"},
		Restart: &Next{Version: "0.8.0", From: "axx.yaml"},
	}
	go func() {
		ok, _ := a.Approve(context.Background(), q)
		result <- ok
	}()
	waitFor(t, "the prompt", func() bool { return strings.Contains(out.String(), "restart? [y/N] ") })
	_, _ = io.WriteString(typed, "y\n")
	if !<-result {
		t.Error("y approves")
	}
	text := out.String()
	for _, want := range []string{
		"axx.yaml now names axx 0.8.0: the host would restart on it, and asks you first\n",
		"a restart builds the project's packs again and runs the commands axx.yaml has around `axx host`, on this machine and as you; they changed since the host started:\n",
		"  -steps/pack.go 1111\n", "  +steps/pack.go 2222\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}

	// An older axx with nothing of the project's changed: the warning, no difference.
	out.b.Reset()
	q.Approved, q.Restart = q.Proposed.Text, &Next{Version: "0.6.0", From: "axx.yaml", Older: true}
	q.Proposed.Warnings = []string{"axx 0.6.0 is older than the axx 0.7.0 the host runs on"}
	go func() {
		ok, _ := a.Approve(context.Background(), q)
		result <- ok
	}()
	waitFor(t, "the prompt", func() bool { return strings.Contains(out.String(), "restart? [y/N] ") })
	_, _ = io.WriteString(typed, "n\n")
	if <-result {
		t.Error("n refuses")
	}
	if text := out.String(); text != "\naxx.yaml now names axx 0.6.0: the host would restart on it, and asks you first\nwarning: axx 0.6.0 is older than the axx 0.7.0 the host runs on\nrestart? [y/N] " {
		t.Errorf("the terminal says:\n%q", text)
	}

	out.b.Reset()
	a.Event(Event{Event: "version", State: VersionWaiting, Version: "0.8.0", Text: "axx.yaml now names axx 0.8.0: the host restarts on it once the runs connected to it have ended"})
	if got := out.String(); got != "axx host: axx.yaml now names axx 0.8.0: the host restarts on it once the runs connected to it have ended\n" {
		t.Errorf("the terminal says %q", got)
	}
}

func TestIDEAsksAboutARestart(t *testing.T) {
	in, answers := io.Pipe()
	got := make(chan Event, 10)
	a := NewIDE(in, func(e Event) { got <- e }, nil)
	result := make(chan bool, 1)
	go func() {
		ok, _ := a.Approve(context.Background(), Question{
			Command: "axx.yaml", What: "the axx host again, on axx 0.8.0",
			Approved: "steps/pack.go 1111\n", Proposed: &Definition{Text: "steps/pack.go 2222\n"},
			Restart: &Next{Version: "0.8.0", From: "axx.yaml"},
		})
		result <- ok
	}()
	// An editor from before hosts restarted words it from the text and the
	// command, as a pack's question; a later one reads the version.
	e := <-got
	if e.Event != "approval" || e.Version != "0.8.0" || e.Text != "the axx host again, on axx 0.8.0" || e.Command != "axx.yaml" || !strings.Contains(e.Diff, "+steps/pack.go 2222") {
		t.Fatalf("approval: %+v", e)
	}
	_, _ = io.WriteString(answers, `{"approve": 1}`+"\n")
	if !<-result {
		t.Error("approved")
	}
}
