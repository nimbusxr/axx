package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// parcelsAgent is the parcels A2A agent, which other agents (a shop's, a
// marketplace's) ask about parcels, at /a2a (JSON-RPC) and /a2a/rest
// (HTTP+JSON), with its card at /.well-known/agent-card.json. It has no
// model inside: it reads a parcel's reference, and whether it is asked to
// hold it.
//
//	"Where is PX-1042?"      a task that completes with the parcel's status
//	"Hold PX-1042"           a task that asks until which day, and holds it on the reply
//	a parcel beyond the EU   the partner carrier's agent is asked, and its answer passed on
type parcelsAgent struct {
	store    *store
	tracking *trackingStore
	partner  *partnerCarrier
	log      *slog.Logger
}

var (
	referenceIn = regexp.MustCompile(`PX-[A-Z0-9]+(?:-[A-Z0-9]+)*`)
	dayIn       = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

// routes serves the agent and its card on the service's mux.
func (a *parcelsAgent) routes(mux *http.ServeMux, publicURL string) {
	card := &a2a.AgentCard{
		Name:        "Parcels agent",
		Description: "Tracks parcels, and holds them at their depot until a day the recipient chooses.",
		Version:     "1.4.0",
		Provider:    &a2a.AgentProvider{Org: "Parcels", URL: publicURL},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(publicURL+"/a2a", a2a.TransportProtocolJSONRPC),
			a2a.NewAgentInterface(publicURL+"/a2a/rest", a2a.TransportProtocolHTTPJSON),
		},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills: []a2a.AgentSkill{
			{
				ID: "track-parcel", Name: "Track a parcel", Description: "Where a parcel is, and its status.", Tags: []string{"tracking"},
				Examples: []string{"Where is PX-1042?"},
			},
			{
				ID: "hold-parcel", Name: "Hold a parcel", Description: "Holds a parcel at its depot until a day.", Tags: []string{"delivery"},
				Examples: []string{"Hold PX-1042 until 2026-10-05"},
			},
		},
	}
	h := cancelWhenRunEnds{a2asrv.NewHandler(a)}
	mux.Handle("GET /.well-known/agent-card.json", a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("POST /a2a", a2asrv.NewJSONRPCHandler(h))
	mux.Handle("/a2a/rest/", http.StripPrefix("/a2a/rest", a2asrv.NewRESTHandler(h)))
}

// cancelWhenRunEnds cancels a task that is not over once the run that
// answered last has ended. a2a-go 2.6 hands a cancel that comes while that run
// still ends to the run (a TODO in its internal/taskexec), which fails it: the
// run's queue is closed, or the run left the task input-required. A hold that
// asked for its day is canceled right after it asked.
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

func (a *parcelsAgent) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	text := textOf(ec.Message)
	says := func(s string) *a2a.Message {
		return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart(s))
	}
	return func(yield func(a2a.Event, error) bool) {
		// A reply to a hold that asked for its day.
		if t := ec.StoredTask; t != nil && t.Status.State == a2a.TaskStateInputRequired && len(t.History) > 0 {
			a.holdUntil(ctx, ec, referenceIn.FindString(textOf(t.History[0])), text, says, yield)
			return
		}
		ref := referenceIn.FindString(text)
		if ref == "" {
			yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
				"I track parcels, and hold them at their depot. Tell me a parcel's reference, like PX-1042.")), nil)
			return
		}
		if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
			return
		}
		p, err := a.store.Get(ctx, ref)
		if errors.Is(err, errNotFound) {
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateRejected, says("There is no parcel "+ref+".")), nil)
			return
		}
		if err != nil {
			yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, says("The parcels cannot be looked up now; try again later.")), nil)
			return
		}
		if strings.Contains(strings.ToLower(text), "hold") {
			if !dayIn.MatchString(text) {
				yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, says("Until which day should "+ref+" be held? Like 2026-10-05.")), nil)
				return
			}
			a.holdUntil(ctx, ec, ref, text, says, yield)
			return
		}
		if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, says("Looking up "+ref)), nil) {
			return
		}
		status := map[string]any{"reference": p.Reference, "status": p.Status}
		answer := fmt.Sprintf("%s is %s.", p.Reference, words(p.Status))
		if t, err := a.tracking.Get(ctx, p.Reference); err == nil && t.LastLocation != nil {
			status["lastLocation"] = *t.LastLocation
			answer = fmt.Sprintf("%s is %s, last scanned in %s.", p.Reference, words(p.Status), *t.LastLocation)
		}
		if abroad(p.Recipient.Country) {
			// The partner carrier delivers it: its agent knows where it is.
			partnerSays, err := a.partner.ask(ctx, "Where is "+p.Reference+"?")
			if err != nil {
				a.log.Warn("the partner carrier's agent did not answer", "reference", ref, "err", err)
				partnerSays = "the partner carrier cannot be asked now"
			}
			status["carrier"], status["partner"] = "partner", partnerSays
			answer = fmt.Sprintf("%s is with our partner carrier, who says: %s", p.Reference, partnerSays)
		}
		ev := a2a.NewArtifactEvent(ec, a2a.NewDataPart(status))
		ev.Artifact.Name = "parcel-status"
		if !yield(ev, nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, says(answer)), nil)
	}
}

// holdUntil holds a parcel until the day a message names, under the
// depot's rules.
func (a *parcelsAgent) holdUntil(ctx context.Context, ec a2a.TaskInfoProvider, ref, text string, says func(string) *a2a.Message, yield func(a2a.Event, error) bool) {
	day := dayIn.FindString(text)
	until, err := time.Parse(time.DateOnly, day)
	if err != nil {
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateInputRequired, says("Which day? Like 2026-10-05.")), nil)
		return
	}
	var status string
	_, err = a.store.Hold(ctx, ref, until, func(p *Parcel) error {
		if !holdable[p.Status] {
			status = p.Status
			return errNotChangeable
		}
		return nil
	})
	switch {
	case status != "":
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, says(ref+" is "+words(status)+": it can no longer be held.")), nil)
	case err != nil:
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateFailed, says("The hold of "+ref+" did not work; try again later.")), nil)
	default:
		a.log.Info("parcel held", "reference", ref, "until", day, "by", "a2a")
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, says(ref+" is held at its depot until "+day+".")), nil)
	}
}

func (a *parcelsAgent) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
	}
}

// abroad reports whether a parcel goes beyond the EU, with the partner
// carrier.
func abroad(country string) bool {
	return country != homeCountry && !euCountries[country]
}

// words is a status as people say it: out for delivery.
func words(status string) string {
	return strings.ToLower(strings.ReplaceAll(status, "_", " "))
}

func textOf(m *a2a.Message) string {
	if m == nil {
		return ""
	}
	var out []string
	for _, p := range m.Parts {
		if t := p.Text(); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}
