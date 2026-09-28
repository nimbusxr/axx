package cloudstep

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

func hasStatus(status string) func(Message) (bool, error) {
	return func(m Message) (bool, error) { return strings.Contains(string(m.Body), status), nil }
}

// What arrived before the stream ended still counts; a check that finds
// nothing fails at once, saying why, instead of waiting.
func TestAnEndedInboxFailsAtOnce(t *testing.T) {
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "live tracking"}, core.NewSuite(core.SuiteOptions{}), nil)
	in := &Inbox{}
	in.Add(Message{Body: []byte(`{"status":"IN_TRANSIT"}`)})
	in.End("closed by the server: 1008 no parcel to follow")
	if err := ExpectMatch(sc, time.Minute, in, hasStatus("IN_TRANSIT"), "event", "event", "the tracking event stream"); err != nil {
		t.Errorf("an event that arrived before the end: %v", err)
	}
	start := time.Now()
	err := ExpectMatch(sc, time.Minute, in, hasStatus("DELIVERED"), "event", "event", "the tracking event stream")
	if err == nil || !core.IsAssertion(err) || !strings.Contains(err.Error(), "No event on the tracking event stream met the conditions: closed by the server: 1008 no parcel to follow") {
		t.Errorf("err: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("it waited %s", d)
	}
	if why, ended := in.Ended(); !ended || why != "closed by the server: 1008 no parcel to follow" {
		t.Errorf("ended: %q %v", why, ended)
	}
	in.End("a second reason")
	if why, _ := in.Ended(); why != "closed by the server: 1008 no parcel to follow" {
		t.Errorf("the first reason was replaced: %q", why)
	}
}

func TestAnOpenInboxWaitsWithinItsDuration(t *testing.T) {
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "live tracking"}, core.NewSuite(core.SuiteOptions{}), nil)
	in := &Inbox{}
	go func() {
		time.Sleep(300 * time.Millisecond)
		in.Add(Message{Body: []byte(`{"status":"DELIVERED"}`)})
	}()
	if err := ExpectMatch(sc, 5*time.Second, in, hasStatus("DELIVERED"), "event", "event", "the tracking event stream"); err != nil {
		t.Error(err)
	}
	err := ExpectMatch(sc, 300*time.Millisecond, in, hasStatus("RETURNED"), "attribute", "message", "the scans sqs queue")
	if err == nil || !strings.Contains(err.Error(), "No message on the scans sqs queue met the conditions within 300ms") {
		t.Errorf("err: %v", err)
	}
}
