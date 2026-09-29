package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/secrets"
)

// update is something a stream sent, as the checks read it: a status
// ({"kind": "status", "state": "working", "message": "..."}), an artifact
// ({"kind": "artifact", "artifact": {...}}), the task, or a reply.
type update map[string]any

// words are the states as people say them.
var words = map[string]a2a.TaskState{
	"submitted":      a2a.TaskStateSubmitted,
	"working":        a2a.TaskStateWorking,
	"completed":      a2a.TaskStateCompleted,
	"input-required": a2a.TaskStateInputRequired,
	"auth-required":  a2a.TaskStateAuthRequired,
	"failed":         a2a.TaskStateFailed,
	"canceled":       a2a.TaskStateCanceled,
	"rejected":       a2a.TaskStateRejected,
}

func stateWord(s a2a.TaskState) string {
	for w, st := range words {
		if st == s {
			return w
		}
	}
	return string(s)
}

func stateOf(word string) (a2a.TaskState, error) {
	if word == "cancelled" {
		word = "canceled"
	}
	if s, ok := words[word]; ok {
		return s, nil
	}
	names := make([]string, 0, len(words))
	for w := range words {
		names = append(names, w)
	}
	sort.Strings(names)
	return "", fmt.Errorf("a task's state is %s, not %q", strings.Join(names, ", "), word)
}

// settled reports whether a task will not change without a message: it
// ended, or it waits for input.
func settled(s a2a.TaskState) bool {
	switch s {
	case a2a.TaskStateCompleted, a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateRejected,
		a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired:
		return true
	}
	return false
}

// message is a message of the user: a text, or a file's message.
func message(sc *core.Scenario, text, file string) (*a2a.Message, error) {
	if file == "" {
		text, err := secrets.Resolve(sc, text)
		if err != nil {
			return nil, err
		}
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text)), nil
	}
	p, err := sc.Suite().ResolvePath(file)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	body, err := secrets.Resolve(sc, string(raw))
	if err != nil {
		return nil, err
	}
	var m a2a.Message
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return nil, fmt.Errorf("the message %s is not an A2A message: %w", file, err)
	}
	if len(m.Parts) == 0 {
		return nil, fmt.Errorf("the message %s has no parts", file)
	}
	if m.ID == "" {
		m.ID = a2a.NewMessageID()
	}
	if m.Role == "" {
		m.Role = a2a.MessageRoleUser
	}
	return &m, nil
}

// send sends a message: a new one starts a conversation, a reply continues
// the agent's last task. A stream runs until the agent ends it, or the
// scenario ends.
func send(sc *core.Scenario, name string, m *a2a.Message, reply, stream bool) error {
	a, err := get(sc, name)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.stream != nil {
		a.stream.cancel()
		<-a.stream.done
		a.stream = nil
	}
	if reply {
		switch {
		case a.task != nil:
			m.TaskID, m.ContextID = a.task.ID, a.task.ContextID
		case a.context != "":
			m.ContextID = a.context
		default:
			a.mu.Unlock()
			return fmt.Errorf("the %s a2a agent has nothing to reply to: send it a message first", a.name)
		}
	} else {
		a.task, a.reply, a.context = nil, nil, ""
	}
	a.updates = nil
	a.mu.Unlock()
	req := &a2a.SendMessageRequest{Message: m}
	if !stream {
		ctx, cancel := context.WithTimeout(sc.Context(), a.timeout)
		defer cancel()
		res, err := a.client.SendMessage(ctx, req)
		if err != nil {
			return secrets.Hide(sc, fmt.Errorf("the %s a2a agent refused the message: %w", a.name, err))
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		switch r := res.(type) {
		case *a2a.Task:
			a.task, a.reply, a.context = r, nil, r.ContextID
			sc.Log("the %s a2a agent's task is %s", a.name, stateWord(r.Status.State))
		case *a2a.Message:
			a.reply, a.context = r, r.ContextID
			sc.Log("the %s a2a agent replied", a.name)
		}
		return nil
	}
	// The stream outlives the step: it is not tied to the step's context.
	ctx, cancel := context.WithCancel(context.Background())
	s := &streaming{cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	a.stream = s
	a.mu.Unlock()
	go func() {
		defer close(s.done)
		for ev, err := range a.client.SendStreamingMessage(ctx, req) {
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.mu.Lock()
					s.err = err
					a.mu.Unlock()
				}
				return
			}
			a.apply(ev)
		}
	}()
	return nil
}

// apply takes in what a stream sent.
func (a *agent) apply(ev a2a.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch e := ev.(type) {
	case *a2a.Message:
		a.reply, a.context = e, e.ContextID
		a.updates = append(a.updates, update{"kind": "message", "message": messageText(e)})
	case *a2a.Task:
		a.task, a.context = e, e.ContextID
		a.updates = append(a.updates, update{"kind": "task", "state": stateWord(e.Status.State), "message": statusText(e)})
	case *a2a.TaskStatusUpdateEvent:
		if a.task == nil {
			a.task = &a2a.Task{ID: e.TaskID, ContextID: e.ContextID}
		}
		a.task.Status = e.Status
		a.updates = append(a.updates, update{"kind": "status", "state": stateWord(e.Status.State), "message": statusText(a.task)})
	case *a2a.TaskArtifactUpdateEvent:
		if a.task == nil {
			a.task = &a2a.Task{ID: e.TaskID, ContextID: e.ContextID}
		}
		a.task.Artifacts = merge(a.task.Artifacts, e.Artifact, e.Append)
		a.updates = append(a.updates, update{"kind": "artifact", "artifact": artifactView(e.Artifact)})
	}
}

// merge adds an artifact, or its parts to the one of its id.
func merge(all []*a2a.Artifact, ar *a2a.Artifact, appendParts bool) []*a2a.Artifact {
	for i, x := range all {
		if x.ID == ar.ID {
			if appendParts {
				c := *x
				c.Parts = append(append(a2a.ContentParts{}, x.Parts...), ar.Parts...)
				all[i] = &c
			} else {
				all[i] = ar
			}
			return all
		}
	}
	return append(all, ar)
}

// cancelTask cancels the agent's last task.
func cancelTask(sc *core.Scenario, name string) error {
	a, err := get(sc, name)
	if err != nil {
		return err
	}
	a.mu.Lock()
	t := a.task
	a.mu.Unlock()
	if t == nil {
		return fmt.Errorf("the %s a2a agent runs no task to cancel: send it a message first", a.name)
	}
	ctx, cancel := context.WithTimeout(sc.Context(), a.timeout)
	defer cancel()
	res, err := a.client.CancelTask(ctx, &a2a.CancelTaskRequest{ID: t.ID})
	if err != nil {
		return secrets.Hide(sc, fmt.Errorf("the %s a2a agent did not cancel its task: %w", a.name, err))
	}
	a.mu.Lock()
	a.task = res
	a.mu.Unlock()
	return nil
}

// wait checks until check passes: until the wait is over, or at once when
// what the agent answered will not change any more (its stream ended, or
// its task settled).
func wait(sc *core.Scenario, a *agent, within time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(within)
	for {
		ok, failure := check()
		if ok {
			return nil
		}
		a.mu.Lock()
		s, t := a.stream, a.task
		ended := s == nil || isDone(s)
		var streamErr error
		if s != nil {
			streamErr = s.err
		}
		a.mu.Unlock()
		switch {
		case streamErr != nil:
			return secrets.Hide(sc, more(failure, "\nthe %s a2a agent's stream failed: %v", a.name, streamErr))
		case ended && (t == nil || settled(t.Status.State)):
			return secrets.Hide(sc, failure)
		case time.Now().After(deadline):
			return secrets.Hide(sc, more(failure, " (after %s)", within))
		}
		if ended && t != nil {
			// A task the agent works on without a stream: ask how it is.
			ctx, cancel := context.WithTimeout(sc.Context(), a.timeout)
			res, err := a.client.GetTask(ctx, &a2a.GetTaskRequest{ID: t.ID})
			cancel()
			if err == nil {
				a.mu.Lock()
				a.task = res
				a.mu.Unlock()
			}
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// more adds to a check's failure. It goes into the assertion's own message:
// reports show an assertion's message, and what wraps it only before it.
func more(failure error, format string, args ...any) error {
	if ae, ok := errors.AsType[*core.AssertionError](failure); ok {
		c := *ae
		c.Message += fmt.Sprintf(format, args...)
		return &c
	}
	return fmt.Errorf("%w%s", failure, fmt.Sprintf(format, args...))
}

func isDone(s *streaming) bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func checkState(sc *core.Scenario, name, word string, within time.Duration) error {
	a, err := get(sc, name)
	if err != nil {
		return err
	}
	want, err := stateOf(word)
	if err != nil {
		return err
	}
	return wait(sc, a, within, func() (bool, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case a.task != nil && a.task.Status.State == want:
			return true, nil
		case a.task != nil:
			return false, core.Fail(fmt.Sprintf("the %s a2a agent's task is %s, not %s: %s", a.name, stateWord(a.task.Status.State), word, statusText(a.task)),
				word, stateWord(a.task.Status.State))
		case a.reply != nil:
			return false, core.Failf("the %s a2a agent answered with a message, not a task: %s", a.name, messageText(a.reply))
		}
		return false, core.Failf("the %s a2a agent has answered no message of this scenario yet", a.name)
	})
}

func checkAnswer(sc *core.Scenario, name, want string) error {
	a, err := get(sc, name)
	if err != nil {
		return err
	}
	return wait(sc, a, defaultWait, func() (bool, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		got := a.answer()
		if strings.Contains(got, want) {
			return true, nil
		}
		return false, core.Fail(fmt.Sprintf("the %s a2a agent's answer does not contain the text", a.name), want, got)
	})
}

// answer is the text of what the agent answered: its reply, or its task's
// status message and artifacts.
func (a *agent) answer() string {
	if a.task == nil {
		if a.reply != nil {
			return messageText(a.reply)
		}
		return ""
	}
	parts := []string{statusText(a.task)}
	for _, ar := range a.task.Artifacts {
		parts = append(parts, partsText(ar.Parts))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func checkArtifact(sc *core.Scenario, name string, t *core.Table) error {
	a, err := get(sc, name)
	if err != nil {
		return err
	}
	return wait(sc, a, defaultWait, func() (bool, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.task == nil || len(a.task.Artifacts) == 0 {
			return false, core.Failf("the %s a2a agent's task has no artifact", a.name)
		}
		var seen []string
		for _, ar := range a.task.Artifacts {
			b, _ := json.Marshal(artifactView(ar))
			if jsonassert.Properties(string(b), t, false) == nil {
				return true, nil
			}
			seen = append(seen, string(b))
		}
		return false, core.Failf("no artifact of the %s a2a agent's task has those properties; its artifacts:\n  %s", a.name, strings.Join(seen, "\n  "))
	})
}

func checkUpdate(sc *core.Scenario, name string, t *core.Table, within time.Duration) error {
	a, err := get(sc, name)
	if err != nil {
		return err
	}
	a.mu.Lock()
	streamed := a.stream != nil
	a.mu.Unlock()
	if !streamed {
		return fmt.Errorf("the %s a2a agent was sent no message as a stream; stream one with \"a message is streamed to the %s a2a agent:\"", a.name, a.name)
	}
	return wait(sc, a, within, func() (bool, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		var seen []string
		for _, u := range a.updates {
			b, _ := json.Marshal(u)
			if jsonassert.Properties(string(b), t, false) == nil {
				return true, nil
			}
			seen = append(seen, string(b))
		}
		return false, core.Failf("the %s a2a agent's stream sent no update with those properties; it sent:\n  %s", a.name, strings.Join(seen, "\n  "))
	})
}

// artifactView is an artifact as the checks read it: its name, its
// description, its text, its first data part, and its parts.
func artifactView(ar *a2a.Artifact) map[string]any {
	v := map[string]any{"name": ar.Name, "description": ar.Description, "text": partsText(ar.Parts)}
	for _, p := range ar.Parts {
		if d := p.Data(); d != nil {
			v["data"] = d
			break
		}
	}
	var parts any
	if b, err := json.Marshal(ar.Parts); err == nil && json.Unmarshal(b, &parts) == nil {
		v["parts"] = parts
	}
	return v
}

func messageText(m *a2a.Message) string {
	if m == nil {
		return ""
	}
	return partsText(m.Parts)
}

func statusText(t *a2a.Task) string {
	return messageText(t.Status.Message)
}

func partsText(parts a2a.ContentParts) string {
	var out []string
	for _, p := range parts {
		if s := p.Text(); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}
