package report

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/runner"
)

// cucumberJSON writes the legacy Cucumber JSON format in Cucumber-JVM's
// layout (background elements precede their scenario, hooks are attached to
// the scenario), the layout tools like cucumber-reporting expect.
type cucumberJSON struct {
	out  *writer
	opts Options
}

type cjFeature struct {
	URI         string       `json:"uri"`
	ID          string       `json:"id"`
	Keyword     string       `json:"keyword"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Line        int          `json:"line"`
	Tags        []cjTag      `json:"tags,omitempty"`
	Elements    []*cjElement `json:"elements"`
}

type cjTag struct {
	Name     string      `json:"name"`
	Type     string      `json:"type,omitempty"`
	Location *cjLocation `json:"location,omitempty"`
}

type cjLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type cjElement struct {
	StartTimestamp string    `json:"start_timestamp,omitempty"`
	ID             string    `json:"id,omitempty"`
	Keyword        string    `json:"keyword"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Line           int       `json:"line"`
	Type           string    `json:"type"`
	Tags           []cjTag   `json:"tags,omitempty"`
	Before         []*cjHook `json:"before,omitempty"`
	Steps          []*cjStep `json:"steps"`
	After          []*cjHook `json:"after,omitempty"`
}

type cjStep struct {
	Keyword    string        `json:"keyword"`
	Name       string        `json:"name"`
	Line       int           `json:"line"`
	Rows       []cjRow       `json:"rows,omitempty"`
	DocString  *cjDocString  `json:"doc_string,omitempty"`
	Match      cjMatch       `json:"match"`
	Result     cjResult      `json:"result"`
	Embeddings []cjEmbedding `json:"embeddings,omitempty"`
	Output     []string      `json:"output,omitempty"`
}

type cjHook struct {
	Match      cjMatch       `json:"match"`
	Result     cjResult      `json:"result"`
	Embeddings []cjEmbedding `json:"embeddings,omitempty"`
	Output     []string      `json:"output,omitempty"`
}

type cjRow struct {
	Cells []string `json:"cells"`
}

type cjDocString struct {
	ContentType string `json:"content_type,omitempty"`
	Value       string `json:"value"`
	Line        int    `json:"line"`
}

type cjMatch struct {
	Location  string  `json:"location,omitempty"`
	Arguments []cjArg `json:"arguments,omitempty"`
}

type cjArg struct {
	Val    string `json:"val"`
	Offset int    `json:"offset"`
}

type cjResult struct {
	Status       string `json:"status"`
	Duration     int64  `json:"duration,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type cjEmbedding struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
	Name     string `json:"name,omitempty"`
}

func (c *cucumberJSON) Envelope(*messages.Envelope) {}

func (c *cucumberJSON) ScenarioFinished(*runner.ScenarioResult) {}

func (c *cucumberJSON) Finish(res *runner.RunResult) error {
	features := []*cjFeature{}
	byURI := map[string]*cjFeature{}
	indexes := map[*feature.Document]*astIndex{}
	for _, r := range res.Scenarios {
		var idx *astIndex
		if r.Pickle != nil && r.Pickle.Doc != nil {
			if idx = indexes[r.Pickle.Doc]; idx == nil {
				idx = newASTIndex(r.Pickle.Doc.AST)
				indexes[r.Pickle.Doc] = idx
			}
		} else {
			idx = newASTIndex(nil)
		}
		uri := c.opts.uri(r)
		f := byURI[uri]
		if f == nil {
			f = c.feature(r, uri)
			byURI[uri] = f
			features = append(features, f)
		}
		if bg := c.background(r, idx); bg != nil {
			f.Elements = append(f.Elements, bg)
		}
		f.Elements = append(f.Elements, c.scenario(r, idx, f.ID))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(features); err != nil {
		return err
	}
	c.out.put(buf.String())
	return c.out.err
}

func (c *cucumberJSON) feature(r *runner.ScenarioResult, uri string) *cjFeature {
	f := &cjFeature{URI: uri, Keyword: "Feature", Elements: []*cjElement{}}
	if r.Pickle != nil && r.Pickle.Doc != nil && r.Pickle.Doc.AST != nil && r.Pickle.Doc.AST.Feature != nil {
		ft := r.Pickle.Doc.AST.Feature
		f.Keyword, f.Name, f.Description = ft.Keyword, ft.Name, ft.Description
		f.Line = lineOf(ft.Location)
		for _, t := range ft.Tags {
			tag := cjTag{Name: t.Name, Type: "Tag"}
			if t.Location != nil {
				tag.Location = &cjLocation{Line: int(t.Location.Line), Column: int(t.Location.Column)}
			}
			f.Tags = append(f.Tags, tag)
		}
	}
	f.ID = cucumberID(f.Name)
	return f
}

// background returns the element holding the scenario's Background steps.
func (c *cucumberJSON) background(r *runner.ScenarioResult, idx *astIndex) *cjElement {
	var el *cjElement
	for _, s := range r.Steps {
		if !s.Background {
			continue
		}
		if el == nil {
			el = &cjElement{Keyword: "Background", Type: "background"}
			if bg := idx.backgrounds[c.astStepID(r, s)]; bg != nil {
				el.Keyword, el.Name, el.Description, el.Line = bg.Keyword, bg.Name, bg.Description, lineOf(bg.Location)
			}
		}
		el.Steps = append(el.Steps, c.step(r, s, idx))
	}
	return el
}

func (c *cucumberJSON) scenario(r *runner.ScenarioResult, idx *astIndex, featureID string) *cjElement {
	el := &cjElement{Keyword: scenarioKeyword(r), Name: scenarioName(r), Type: "scenario", Steps: []*cjStep{}}
	if r.Pickle != nil {
		el.Line = r.Pickle.Line
		for _, t := range r.Pickle.TagNames {
			el.Tags = append(el.Tags, cjTag{Name: t})
		}
		el.ID = featureID + ";" + cucumberID(r.Pickle.Name)
		if r.Pickle.Pickle != nil && len(r.Pickle.AstNodeIds) > 0 {
			if sc := idx.scenarios[r.Pickle.AstNodeIds[0]]; sc != nil {
				el.Description = sc.Description
				el.ID = featureID + ";" + cucumberID(sc.Name)
			}
			if len(r.Pickle.AstNodeIds) > 1 {
				if row, ok := idx.rows[r.Pickle.AstNodeIds[1]]; ok {
					el.ID += ";" + cucumberID(row.examples.Name) + ";" + strconv.Itoa(row.index+2)
				}
			}
		}
	}
	if !r.Started.IsZero() {
		el.StartTimestamp = r.Started.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	for _, s := range r.Before {
		el.Before = append(el.Before, c.hook(s))
	}
	for _, s := range r.Steps {
		if !s.Background {
			el.Steps = append(el.Steps, c.step(r, s, idx))
		}
	}
	for _, s := range r.After {
		el.After = append(el.After, c.hook(s))
	}
	return el
}

func (c *cucumberJSON) step(r *runner.ScenarioResult, s *runner.StepResult, idx *astIndex) *cjStep {
	st := &cjStep{Keyword: s.Keyword + " ", Name: s.Text, Line: s.Line, Result: result(s)}
	ast := idx.steps[c.astStepID(r, s)]
	if ast != nil {
		st.Keyword = ast.Keyword
	}
	if s.Table != nil {
		for _, row := range s.Table.Rows {
			st.Rows = append(st.Rows, cjRow{Cells: append([]string{}, row...)})
		}
	}
	if s.DocString != nil {
		st.DocString = &cjDocString{ContentType: s.DocString.MediaType, Value: s.DocString.Content}
		if ast != nil && ast.DocString != nil {
			st.DocString.Line = lineOf(ast.DocString.Location)
		}
	}
	if s.Match != nil {
		st.Match.Location = s.Match.Def().Step.ID
		for _, a := range s.Match.Args {
			if a.Present {
				st.Match.Arguments = append(st.Match.Arguments, cjArg{Val: a.Raw, Offset: a.Start})
			}
		}
	}
	st.Embeddings, st.Output = embeddings(s)
	return st
}

func (c *cucumberJSON) hook(s *runner.StepResult) *cjHook {
	h := &cjHook{Match: cjMatch{Location: s.Text}, Result: result(s)}
	if s.Hook != nil {
		h.Match.Location = s.Hook.ID
	}
	h.Embeddings, h.Output = embeddings(s)
	return h
}

// astStepID returns the Gherkin step ID behind a pickle step result.
func (c *cucumberJSON) astStepID(r *runner.ScenarioResult, s *runner.StepResult) string {
	if r.Pickle == nil || r.Pickle.Pickle == nil {
		return ""
	}
	for _, ps := range r.Pickle.Steps {
		if ps.Id == s.PickleStepID && len(ps.AstNodeIds) > 0 {
			return ps.AstNodeIds[0]
		}
	}
	return ""
}

func result(s *runner.StepResult) cjResult {
	res := cjResult{Status: s.Status.String(), Duration: s.Duration.Nanoseconds()}
	if f := describe(s); f != nil && f.kind != KindUndefined {
		res.ErrorMessage = f.text()
		if f.kind == KindPending && f.message == "pending" {
			res.ErrorMessage = ""
		}
	}
	return res
}

func embeddings(s *runner.StepResult) ([]cjEmbedding, []string) {
	var out []cjEmbedding
	for _, a := range s.Attachments {
		out = append(out, cjEmbedding{Data: base64.StdEncoding.EncodeToString(a.Body), MimeType: a.MediaType, Name: a.Name})
	}
	return out, s.Logs
}

// astIndex locates Gherkin AST nodes by ID.
type astIndex struct {
	steps       map[string]*messages.Step
	backgrounds map[string]*messages.Background // by step ID
	scenarios   map[string]*messages.Scenario
	rows        map[string]rowRef
}

type rowRef struct {
	examples *messages.Examples
	index    int
}

func newASTIndex(doc *messages.GherkinDocument) *astIndex {
	idx := &astIndex{
		steps: map[string]*messages.Step{}, backgrounds: map[string]*messages.Background{},
		scenarios: map[string]*messages.Scenario{}, rows: map[string]rowRef{},
	}
	if doc == nil || doc.Feature == nil {
		return idx
	}
	visit := func(bg *messages.Background, sc *messages.Scenario) {
		if bg != nil {
			for _, s := range bg.Steps {
				idx.steps[s.Id] = s
				idx.backgrounds[s.Id] = bg
			}
		}
		if sc != nil {
			idx.scenarios[sc.Id] = sc
			for _, s := range sc.Steps {
				idx.steps[s.Id] = s
			}
			for _, ex := range sc.Examples {
				for i, row := range ex.TableBody {
					idx.rows[row.Id] = rowRef{examples: ex, index: i}
				}
			}
		}
	}
	for _, c := range doc.Feature.Children {
		visit(c.Background, c.Scenario)
		if c.Rule != nil {
			for _, rc := range c.Rule.Children {
				visit(rc.Background, rc.Scenario)
			}
		}
	}
	return idx
}

var idChars = regexp.MustCompile(`[\s'_,!]`)

// cucumberID converts a name to an ID the way Cucumber-JVM does.
func cucumberID(name string) string {
	return strings.ToLower(idChars.ReplaceAllString(name, "-"))
}

func lineOf(l *messages.Location) int {
	if l == nil {
		return 0
	}
	return int(l.Line)
}
