package core

import "time"

// Exchange is something a step sent to what the scenario talks to, or saw of
// it: a request and its answer, an event published or consumed, the rows a
// selection found. A pack says it as its step does it ([Scenario.Exchanged]),
// and a pack that shows a run to people reads it ([Scenario.Exchanges]): a
// demo's diagram draws the peers as boxes and what went between them.
//
// It is for showing, never for checking: a step's checks read the pack's own
// state. What a pack says here a person may see on a screen, so it says what
// its logs and attachments say, and nothing it keeps out of them (headers
// that carry credentials, say).
type Exchange struct {
	// Peer is the name the scenario gave what it talked to, as its steps
	// name it: "parcels", "parcels-db". Kind is what that is, in a word:
	// "service", "database", "topic", "mock", or a pack's own.
	Peer string `json:"peer"`
	Kind string `json:"kind"`
	// From names who sent, when that was not the scenario: a mock's exchange
	// is a call a service made to it. Empty is the scenario itself.
	From string `json:"from,omitempty"`
	// Sent is what went to the peer, Seen what came from it. Either may be
	// nil: a published event has no answer, a consumed one was not asked
	// for. With neither, the exchange says only that the scenario now talks
	// to the peer, as a step that registers a service does.
	Sent *Message `json:"sent,omitempty"`
	Seen *Message `json:"seen,omitempty"`

	// Pack, Line and At are the scenario's to fill in: the pack of the step
	// that ran, the step's line (0 outside a step) and the time.
	Pack string    `json:"pack,omitempty"`
	Line int       `json:"line,omitempty"`
	At   time.Time `json:"at"`
}

// FromSystem is an [Exchange]'s From when the system under test sent, and the
// pack cannot tell which of its services it was: a mock knows the calls it
// received, not who made them.
const FromSystem = "system"

// Message is one side of an [Exchange].
type Message struct {
	// Title is it in a line: "POST /api/parcels", "201 Created", "1 row of
	// parcels.holds".
	Title string `json:"title"`
	// MediaType and Body are its content, when it has one worth showing:
	// "application/json" and a payload. A pack cuts a long body where its
	// logs would.
	MediaType string `json:"mediaType,omitempty"`
	Body      string `json:"body,omitempty"`
}

// Exchanged says that the running step sent something to a peer or saw
// something of it. The scenario notes the step and the time.
func (sc *Scenario) Exchanged(x Exchange) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.step != nil {
		x.Line = sc.step.Line
		if x.Pack == "" {
			x.Pack = sc.step.Pack
		}
	}
	if x.At.IsZero() {
		x.At = time.Now()
	}
	sc.exchanges = append(sc.exchanges, x)
}

// Exchanges returns what the scenario's steps exchanged so far, in order: a
// copy, safe to keep.
func (sc *Scenario) Exchanges() []Exchange {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return append([]Exchange(nil), sc.exchanges...)
}
