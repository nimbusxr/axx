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
