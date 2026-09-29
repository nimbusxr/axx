package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

var reference = regexp.MustCompile(`PX-A2A-\d+`)

// parcels is a rule-based parcels agent: it tracks parcels, and holds them
// until a day it asks for.
type parcels struct {
	mu      sync.Mutex
	headers http.Header // of the last request
}

func (p *parcels) Execute(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	text := messageText(ec.Message)
	ref := reference.FindString(text)
	agentSays := func(s string) *a2a.Message {
		return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart(s))
	}
	return func(yield func(a2a.Event, error) bool) {
		// A reply to a hold that asked for its day.
		if ec.StoredTask != nil && ec.StoredTask.Status.State == a2a.TaskStateInputRequired {
			held := reference.FindString(messageText(ec.StoredTask.History[0]))
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, agentSays(held+" is held until "+strings.TrimSpace(text))), nil)
			return
		}
		switch {
		case ref == "":
			yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("I track parcels and hold them at their depot.")), nil)
			return
		case !yield(a2a.NewSubmittedTask(ec, ec.Message), nil):
			return
		}
		switch {
		case strings.HasPrefix(text, "Hold"):
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, agentSays("Until which day should "+ref+" be held?")), nil)
			if ref == "PX-A2A-9208" {
				// A run that is slow to end after it answered.
				time.Sleep(200 * time.Millisecond)
			}
		case ref == "PX-A2A-9299":
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateRejected, agentSays("There is no parcel "+ref+".")), nil)
		default:
			if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, agentSays("Looking up "+ref)), nil) {
				return
			}
			if ref == "PX-A2A-9204" {
				time.Sleep(300 * time.Millisecond)
			}
			ev := a2a.NewArtifactEvent(ec, a2a.NewDataPart(map[string]any{"reference": ref, "status": "OUT_FOR_DELIVERY"}), a2a.NewTextPart(ref+" is out for delivery"))
			ev.Artifact.Name = "parcel-status"
			if !yield(ev, nil) {
				return
			}
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, agentSays(ref+" is out for delivery, in Leipzig.")), nil)
		}
	}
}

func (p *parcels) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
	}
}

// cancelWhenRunEnds cancels a task that is not over once the run that
// answered last has ended. a2a-go 2.6 hands a cancel that comes while that run
// still ends to the run (a TODO in its internal/taskexec), which fails it: the
// run's queue is closed, or the run left the task input-required.
type cancelWhenRunEnds struct{ a2asrv.RequestHandler }

func (h cancelWhenRunEnds) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		t, err := h.RequestHandler.CancelTask(ctx, req)
		if err == nil || time.Now().After(deadline) {
			return t, err
		}
		task, gerr := h.GetTask(ctx, &a2a.GetTaskRequest{ID: req.ID})
		if gerr != nil || task.Status.State.Terminal() {
			return t, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func serve(t *testing.T) (*parcels, string) {
	t.Helper()
	return serveWith(t, func(w http.ResponseWriter) http.ResponseWriter { return w })
}

// serveWith serves the agent through a wrapper of its response writers.
func serveWith(t *testing.T, wrap func(http.ResponseWriter) http.ResponseWriter) (*parcels, string) {
	t.Helper()
	p := &parcels{}
	handler := cancelWhenRunEnds{a2asrv.NewHandler(p)}
	mux := http.NewServeMux()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.headers = r.Header.Clone()
		p.mu.Unlock()
		mux.ServeHTTP(wrap(w), r)
	}))
	t.Cleanup(srv.Close)
	card := &a2a.AgentCard{
		Name: "Parcels agent", Description: "Tracks parcels, and holds them at their depot.", Version: "1.4.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(srv.URL+"/a2a", a2a.TransportProtocolJSONRPC),
			a2a.NewAgentInterface(srv.URL+"/rest", a2a.TransportProtocolHTTPJSON),
		},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills:             []a2a.AgentSkill{{ID: "track-parcel", Name: "Track a parcel", Description: "Where a parcel is.", Tags: []string{"tracking"}}},
	}
	mux.Handle("/.well-known/agent-card.json", a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/a2a", a2asrv.NewJSONRPCHandler(handler))
	mux.Handle("/rest/", http.StripPrefix("/rest", a2asrv.NewRESTHandler(handler)))
	return p, srv.URL
}

func TestTasks(t *testing.T) {
	for _, transport := range []string{"JSONRPC", "HTTP+JSON"} {
		t.Run(transport, func(t *testing.T) {
			_, url := serve(t)
			h := cloudtest.New(t, Pack())
			h.OK("the parcels a2a agent with the following properties:", [][]string{{"url", url}, {"transport", transport}})
			h.OK("the parcels a2a agent's card has the following properties:",
				[][]string{{"name", "Parcels agent"}, {"capabilities.streaming", "true"}, {"skills[0].id", "track-parcel"}})
			h.OK("the parcels a2a agent has the track-parcel skill")
			_ = h.Fails("the parcels a2a agent has the hold-parcel skill", "has no skill hold-parcel; its skills are: track-parcel")

			h.OK("a message is sent to the parcels a2a agent:", "Where is PX-A2A-9201?")
			h.OK("the parcels a2a agent's task is completed")
			h.OK("the parcels a2a agent's answer contains 'out for delivery, in Leipzig'")
			h.OK("the parcels a2a agent's task has an artifact where:", [][]string{{"name", "parcel-status"}, {"data.status", "OUT_FOR_DELIVERY"}, {"text", "PX-A2A-9201 is out for delivery"}})
			_ = h.Fails("the parcels a2a agent's task has an artifact where:", "no artifact of the parcels a2a agent's task has those properties", [][]string{{"data.status", "DELIVERED"}})
			_ = h.Fails("the parcels a2a agent's task is failed", "the parcels a2a agent's task is completed, not failed")

			// A task that asks for input gets it in a reply.
			h.OK("a message is sent to the parcels a2a agent:", "Hold PX-A2A-9202, please")
			h.OK("the parcels a2a agent's task is input-required")
			h.OK("the parcels a2a agent's answer contains 'Until which day'")
			h.OK("a reply is sent to the parcels a2a agent:", "2026-10-05")
			h.OK("the parcels a2a agent's task is completed")
			h.OK("the parcels a2a agent's answer contains 'PX-A2A-9202 is held until 2026-10-05'")

			// A reply, not a task.
			h.OK("a message is sent to the parcels a2a agent:", "What do you do?")
			h.OK("the parcels a2a agent's answer contains 'I track parcels'")
			_ = h.Fails("the parcels a2a agent's task is completed", "answered with a message, not a task")

			h.OK("a message is sent to the parcels a2a agent:", "Where is PX-A2A-9299?")
			h.OK("the parcels a2a agent's task is rejected")

			// A task is canceled.
			h.OK("a message is sent to the parcels a2a agent:", "Hold PX-A2A-9205")
			h.OK("the parcels a2a agent is asked to cancel its task")
			h.OK("the parcels a2a agent's task is canceled")
			// A task is canceled while the run that asked for input still ends.
			h.OK("a message is sent to the parcels a2a agent:", "Hold PX-A2A-9208")
			h.OK("the parcels a2a agent is asked to cancel its task")
			h.OK("the parcels a2a agent's task is canceled")
			// A task that ended cannot be canceled.
			h.OK("a message is sent to the parcels a2a agent:", "Where is PX-A2A-9207?")
			_ = h.Fails("the parcels a2a agent is asked to cancel its task", "the parcels a2a agent did not cancel its task")
		})
	}
}

func TestStreams(t *testing.T) {
	for _, transport := range []string{"JSONRPC", "HTTP+JSON"} {
		t.Run(transport, func(t *testing.T) { testStream(t, transport) })
	}
}

func testStream(t *testing.T, transport string) {
	_, url := serve(t)
	h := cloudtest.New(t, Pack())
	h.OK("the parcels a2a agent with the following properties:", [][]string{{"url", url}, {"transport", transport}})
	h.OK("a message is streamed to the parcels a2a agent:", "Where is PX-A2A-9204?")
	h.OK("the parcels a2a agent's stream received an update where:", [][]string{{"kind", "status"}, {"state", "working"}, {"message", "Looking up PX-A2A-9204"}})
	h.OK("within 5s the parcels a2a agent's task is completed")
	h.OK("the parcels a2a agent's stream received an update where:", [][]string{{"kind", "artifact"}, {"artifact.name", "parcel-status"}, {"artifact.data.reference", "PX-A2A-9204"}})
	h.OK("the parcels a2a agent's task has an artifact where:", [][]string{{"data.status", "OUT_FOR_DELIVERY"}})
	// The stream has ended: a check of what it never sent fails at once.
	start := time.Now()
	_ = h.Fails("within 30s the parcels a2a agent's stream received an update where:", "the parcels a2a agent's stream sent no update with those properties", [][]string{{"state", "failed"}})
	if time.Since(start) > 5*time.Second {
		t.Error("a check of an ended stream waited")
	}
	_ = h.Fails("within 1s the parcels a2a agent's task is working", "the parcels a2a agent's task is completed, not working")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// noFlush is a response writer that cannot stream, as a middleware's
// wrapper that hides the writer's Flush.
type noFlush struct{ http.ResponseWriter }

func TestAStreamTheAgentCannotSendFails(t *testing.T) {
	for _, c := range []struct{ transport, why string }{
		{"JSONRPC", "the agent answered the stream with the JSON-RPC error -32603: streaming not supported"},
		{"HTTP+JSON", "streaming not supported"},
	} {
		t.Run(c.transport, func(t *testing.T) {
			_, url := serveWith(t, func(w http.ResponseWriter) http.ResponseWriter { return noFlush{w} })
			h := cloudtest.New(t, Pack())
			h.OK("the parcels a2a agent with the following properties:", [][]string{{"url", url}, {"transport", c.transport}})
			h.OK("a message is streamed to the parcels a2a agent:", "Where is PX-A2A-9204?")
			err := h.Fails("within 5s the parcels a2a agent's stream received an update where:",
				"the parcels a2a agent's stream failed: ", [][]string{{"kind", "status"}})
			// Reports show an assertion's own message: the cause is in it.
			if ae, ok := errors.AsType[*core.AssertionError](err); !ok || !strings.Contains(ae.Message, c.why) {
				t.Errorf("the cause is not in the assertion's message: %v", err)
			}
		})
	}
}

func TestMessagesFromFilesAndHeaders(t *testing.T) {
	p, url := serve(t)
	h := cloudtest.New(t, Pack())
	card, err := http.Get(url + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	_ = json.NewDecoder(card.Body).Decode(&raw)
	card.Body.Close()
	h.File("a2a/parcels-card.json", string(raw))
	h.File("a2a/where-is.json", `{"role": "ROLE_USER", "parts": [{"text": "Where is PX-A2A-9206?"}, {"data": {"channel": "shop-app"}}]}`)
	t.Setenv("PARCELS_AGENT_TOKEN", "agent-4711-secret")
	h.OK("the parcels a2a agent with the following properties:", [][]string{{"card", "a2a/parcels-card.json"}, {"header.Authorization", "Bearer ${env:PARCELS_AGENT_TOKEN}"}})
	h.OK("the a2a/where-is.json message is sent to the parcels a2a agent")
	h.OK("the parcels a2a agent's task is completed")
	p.mu.Lock()
	got := p.headers.Get("Authorization")
	p.mu.Unlock()
	if got != "Bearer agent-4711-secret" {
		t.Errorf("authorization: %q", got)
	}
	for _, l := range h.Sink.Logs {
		if strings.Contains(l, "agent-4711-secret") {
			t.Errorf("the token is in the logs: %s", l)
		}
	}
}

func TestRegistrationErrors(t *testing.T) {
	_, url := serve(t)
	h := cloudtest.New(t, Pack())
	h.File("a2a/incomplete.json", `{"name": "Parcels agent", "skills": []}`)
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"timeout", "5s"}}, `needs a "url"`},
		{[][]string{{"url", "ftp://agents"}}, "is http:// or https://"},
		{[][]string{{"url", url}, {"transport", "SOAP"}}, "transport is JSONRPC, HTTP+JSON or GRPC"},
		{[][]string{{"url", url}, {"push token", "x"}}, `push token goes with a "push url"`},
		{[][]string{{"uri", url}}, `unknown a2a agent property "uri"`},
		{[][]string{{"url", "http://127.0.0.1:1"}}, "cannot read the parcels a2a agent's card from http://127.0.0.1:1/.well-known/agent-card.json"},
		{[][]string{{"card", "a2a/incomplete.json"}}, "is not an A2A 1.0 agent card:\n- it has no description"},
		{[][]string{{"url", url}, {"transport", "GRPC"}}, "the parcels a2a agent's card lists no GRPC interface; it lists JSONRPC, HTTP+JSON"},
	} {
		_ = h.Fails("the parcels a2a agent with the following properties:", c.want, c.rows)
	}
	_ = h.Fails("a reply is sent to the depots a2a agent:", `no a2a agent named "depots"`, "2026-10-05")
	h.OK("the parcels a2a agent with the following properties:", [][]string{{"url", url}})
	_ = h.Fails("a reply is sent to the parcels a2a agent:", "has nothing to reply to: send it a message first", "2026-10-05")
	_ = h.Fails("the parcels a2a agent's stream received an update where:", "was sent no message as a stream", [][]string{{"state", "working"}})
	_ = h.Fails("the parcels a2a agent's task is completed", "has answered no message of this scenario yet")
	_ = os.Remove(filepath.Join(h.Dir, "a2a/incomplete.json"))
}
