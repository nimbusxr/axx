// Package core is the public API for axx step packs. Every pack, the ones
// axx publishes (rest, mock, sql, ...) and anyone else's, implements the
// same Pack interface.
package core

import (
	"context"
	"encoding/json"
	"time"
)

// Pack is a bundle of steps, parameter types and hooks.
//
// A pack may also implement Preparer (setup before the apps start),
// Initializer (suite-scoped setup such as connection pools), Finisher
// (checks of the run as a whole) and Closer (suite teardown). A run calls
// them in that order: Prepare, the apps start, Init, the scenarios, Finish,
// the apps stop, Close.
type Pack interface {
	Manifest() Manifest
}

// Preparer is implemented by packs that must set something up before the
// apps start, such as a listener the apps connect to when they start. The
// plan lists the scenarios the run will execute (for `axx up`, every
// scenario of the configured feature paths), so a pack can find what its
// steps will need. Close what Prepare opens with Suite.OnClose.
type Preparer interface {
	Prepare(ctx context.Context, s *Suite, plan *Plan) error
}

// Plan describes the scenarios a run is about to execute.
type Plan struct {
	Scenarios []PlannedScenario
}

// PlannedScenario is one scenario of a plan.
type PlannedScenario struct {
	ScenarioInfo
	Steps []PlannedStep
}

// PlannedStep is one step of a planned scenario and the definition it
// matched.
type PlannedStep struct {
	Text string
	// Pack and Definition identify the matched step definition; both are
	// empty when the step is undefined or ambiguous.
	Pack       string
	Definition string
	// Args are the matched arguments, untransformed (Raw is the step text).
	Args  []Arg
	Table *Table
}

// Steps returns the planned steps, in every scenario, that matched one of
// the given definition IDs.
func (p *Plan) Steps(definitions ...string) []PlannedStep {
	var out []PlannedStep
	for _, sc := range p.Scenarios {
		for _, st := range sc.Steps {
			for _, d := range definitions {
				if st.Definition == d {
					out = append(out, st)
					break
				}
			}
		}
	}
	return out
}

// Initializer is implemented by packs that need suite-scoped setup.
type Initializer interface {
	Init(ctx context.Context, s *Suite) error
}

// Finisher is implemented by packs that check the run as a whole once every
// scenario has finished, such as problems no single scenario could be held
// responsible for. A returned error fails the run and is reported after the
// scenarios; it is not called for dry runs or interrupted runs.
type Finisher interface {
	Finish(ctx context.Context, s *Suite) error
}

// Closer is implemented by packs that hold suite-scoped resources.
type Closer interface {
	Close(ctx context.Context) error
}

// Manifest describes everything a pack contributes.
type Manifest struct {
	// Name identifies the pack, e.g. "rest".
	Name string `json:"name"`
	// Version of the pack (axx's packs report the axx version).
	Version string `json:"version,omitempty"`
	// Namespace identifies the pack's state, e.g. "rest".
	Namespace string `json:"namespace,omitempty"`
	// Doc is a Markdown summary shown in `axx steps` and generated docs.
	Doc string `json:"doc,omitempty"`
	// Requires names the packs axx publishes that this pack builds on
	// (gcp-core for gcp-storage): selecting it loads them too, before it.
	Requires []string    `json:"requires,omitempty"`
	Params   []ParamType `json:"params,omitempty"`
	Steps    []StepDef   `json:"steps"`
	Hooks    []Hook      `json:"hooks,omitempty"`
	// ConfigSchema is a JSON Schema for the pack's section in axx.yaml.
	ConfigSchema json.RawMessage `json:"configSchema,omitempty"`
}

// ArgKind declares which Gherkin step argument a step accepts.
type ArgKind int

const (
	// ArgNone accepts neither a data table nor a doc string.
	ArgNone ArgKind = iota
	// ArgTable requires a data table.
	ArgTable
	// ArgDocString requires a doc string.
	ArgDocString
	// ArgOptional accepts either or neither.
	ArgOptional
)

func (k ArgKind) String() string {
	switch k {
	case ArgTable:
		return "table"
	case ArgDocString:
		return "docstring"
	case ArgOptional:
		return "optional"
	default:
		return "none"
	}
}

// MarshalText implements encoding.TextMarshaler.
func (k ArgKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// StepFunc executes a step. Returning an *AssertionError (see Fail) marks the
// step failed with expected/actual values; any other error marks it errored.
type StepFunc func(sc *Scenario, args Args) error

// StepDef defines one step.
type StepDef struct {
	// ID is stable and unique across all packs, e.g. "rest.response.status".
	ID string `json:"id"`
	// Expr is a Cucumber Expression. Segments wrapped in [[ ]] are optional
	// variants: "the response status code is {int}[[ on {service}]]" registers
	// both the short and long form. Arguments are always numbered by their
	// position in the full expression; absent optional arguments report
	// Present(i) == false.
	Expr string `json:"expr"`
	// Keyword is the conventional Gherkin keyword (Given/When/Then); matching
	// ignores keywords, this only guides docs and snippets.
	Keyword string `json:"keyword,omitempty"`
	// Doc is Markdown documentation.
	Doc string `json:"doc,omitempty"`
	// Examples are complete step lines; tooling checks that each one matches.
	Examples []string `json:"examples,omitempty"`
	Arg      ArgKind  `json:"arg"`
	// TableTypes are the types of a key/value data table's values, by key,
	// for the properties that name files. The step still reads its table
	// itself; editors use them. "filepath" is a file the step reads, like a
	// {filepath} parameter: editors link it, complete its path and warn when
	// it is missing (a property that also takes URLs names a file as a path
	// or a file: URL). "url" is a URL whose file: form names a file that may
	// appear only during the run, such as a log: editors link it when it
	// exists and complete its path.
	TableTypes map[string]string `json:"tableTypes,omitempty"`
	// Since is the axx version that introduced the step.
	Since string `json:"since,omitempty"`
	// DeprecatedBy, if set, explains what to use instead.
	DeprecatedBy string `json:"deprecated,omitempty"`
	// Timeout overrides the default step timeout when positive.
	Timeout time.Duration `json:"-"`
	Source  SourceRef     `json:"source,omitzero"`
	Run     StepFunc      `json:"-"`
}

// SourceRef locates a definition for go-to-definition and error messages.
type SourceRef struct {
	URI  string `json:"uri,omitempty"`
	Line int    `json:"line,omitempty"`
}

// ParamType is a custom Cucumber Expression parameter type.
type ParamType struct {
	Name    string   `json:"name"`
	Regexps []string `json:"regexps"`
	Doc     string   `json:"doc,omitempty"`
	// Snippet marks the type as a candidate when generating snippets for
	// undefined steps. Broad types such as [^\s]+ should leave it false.
	Snippet bool `json:"snippet,omitempty"`
	// Transform converts the matched text. It runs inside the scenario, so it
	// may consult scenario state (e.g. look up a registered service). groups
	// holds the regexp's capture groups (nil entries for groups that did not
	// participate). A nil Transform yields the matched text.
	Transform func(sc *Scenario, match string, groups []*string) (any, error) `json:"-"`
}

// Phase says when a hook runs.
type Phase int

const (
	BeforeScenario Phase = iota
	AfterScenario
	BeforeStep
	AfterStep
)

func (p Phase) String() string {
	switch p {
	case BeforeScenario:
		return "beforeScenario"
	case AfterScenario:
		return "afterScenario"
	case BeforeStep:
		return "beforeStep"
	case AfterStep:
		return "afterStep"
	}
	return "unknown"
}

// MarshalText implements encoding.TextMarshaler.
func (p Phase) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// Hook runs around scenarios or steps.
type Hook struct {
	ID    string `json:"id"`
	Phase Phase  `json:"phase"`
	// Tags is an optional tag expression limiting which scenarios run the hook.
	Tags string `json:"tags,omitempty"`
	// Order sorts hooks of the same phase; lower runs first for Before* and
	// last for After*.
	Order  int                      `json:"order,omitempty"`
	Source SourceRef                `json:"source,omitzero"`
	Run    func(sc *Scenario) error `json:"-"`
}
