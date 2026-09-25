package report

import (
	"encoding/json"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// messagesReporter writes Cucumber Messages as NDJSON: one envelope per
// line, in the order received.
type messagesReporter struct {
	out *writer
	enc *json.Encoder
	err error
}

func newMessages(out *writer) *messagesReporter {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return &messagesReporter{out: out, enc: enc}
}

func (m *messagesReporter) Envelope(e *messages.Envelope) {
	if err := m.enc.Encode(e); err != nil && m.err == nil {
		m.err = err
	}
}

func (m *messagesReporter) ScenarioFinished(*runner.ScenarioResult) {}

func (m *messagesReporter) Finish(*runner.RunResult) error {
	if m.out.err != nil {
		return m.out.err
	}
	return m.err
}
