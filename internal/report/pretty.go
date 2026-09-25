package report

import (
	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// pretty prints every scenario as one block when it finishes (so parallel
// runs never interleave), then a summary.
type pretty struct{ h *human }

func (p *pretty) Envelope(*messages.Envelope) {}

func (p *pretty) ScenarioFinished(r *runner.ScenarioResult) {
	p.h.out.put(p.h.block(r))
}

func (p *pretty) Finish(res *runner.RunResult) error {
	p.h.out.put(p.h.summary(res))
	return p.h.out.err
}

// progress prints one character per scenario, then the failure details and
// summary of pretty.
type progress struct {
	h *human
	n int
}

var progressChars = map[runner.Status]string{
	runner.Passed:    ".",
	runner.Failed:    "F",
	runner.Undefined: "U",
	runner.Ambiguous: "A",
	runner.Pending:   "P",
	runner.Skipped:   "S",
}

func (p *progress) Envelope(*messages.Envelope) {}

func (p *progress) ScenarioFinished(r *runner.ScenarioResult) {
	p.n++
	p.h.out.put(p.h.st.status(r.Status, progressChars[r.Status]))
}

func (p *progress) Finish(res *runner.RunResult) error {
	if p.n > 0 {
		p.h.out.put("\n\n")
	}
	for _, r := range res.Scenarios {
		if failing(r.Status) {
			p.h.out.put(p.h.block(r))
		}
	}
	p.h.out.put(p.h.summary(res))
	return p.h.out.err
}
