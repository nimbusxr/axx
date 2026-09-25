package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	ijs "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

//go:generate go run ./internal/genschema -out axx.schema.json

// SchemaID is the published location of the axx.yaml schema.
const SchemaID = "https://axx.nimbusxr.us/schemas/v0/axx.schema.json"

// SchemaJSON is the generated JSON Schema for axx.yaml.
//
//go:embed axx.schema.json
var SchemaJSON []byte

// JSONSchema describes Duration.
func (Duration) JSONSchema() *ijs.Schema {
	return &ijs.Schema{OneOf: []*ijs.Schema{
		{Type: "string", Pattern: `^\s*([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+\s*$`, Examples: []any{"30s", "1m30s", "250ms"}},
		{Type: "number", Minimum: "0", Description: "seconds"},
	}}
}

// JSONSchema describes Command.
func (Command) JSONSchema() *ijs.Schema {
	return &ijs.Schema{OneOf: []*ijs.Schema{
		{Type: "string", Description: "command line, split into words without a shell unless shell: true"},
		{Type: "array", Items: &ijs.Schema{Type: "string"}, MinItems: ptr(uint64(1)), Description: "argv"},
	}}
}

// JSONSchema describes StringList.
func (StringList) JSONSchema() *ijs.Schema {
	return &ijs.Schema{OneOf: []*ijs.Schema{
		{Type: "string"},
		{Type: "array", Items: &ijs.Schema{Type: "string"}},
	}}
}

// JSONSchema describes Workers.
func (Workers) JSONSchema() *ijs.Schema {
	return &ijs.Schema{OneOf: []*ijs.Schema{
		{Type: "integer", Minimum: "1"},
		{Type: "string", Enum: []any{"auto"}},
	}}
}

// JSONSchema describes Reporter.
func (Reporter) JSONSchema() *ijs.Schema {
	names := []any{"pretty", "progress", "junit", "messages", "cucumber-json", "html", "agent"}
	props := ijs.NewProperties()
	for _, n := range names {
		props.Set(n.(string), &ijs.Schema{Type: "string", Description: "output file"})
	}
	return &ijs.Schema{OneOf: []*ijs.Schema{
		{Type: "string", Enum: names},
		{Type: "object", Properties: props, MinProperties: ptr(uint64(1)), MaxProperties: ptr(uint64(1)), AdditionalProperties: ijs.FalseSchema},
	}}
}

// JSONSchema describes Apps as a mapping of name to App.
func (Apps) JSONSchema() *ijs.Schema {
	return &ijs.Schema{Type: "object", AdditionalProperties: inline(&App{})}
}

func inline(v any) *ijs.Schema {
	r := &ijs.Reflector{ExpandedStruct: true, DoNotReference: true}
	_ = addGoComments(r)
	s := r.Reflect(v)
	s.Version = ""
	s.ID = ""
	return s
}

func ptr[T any](v T) *T { return &v }

// addGoComments loads the descriptions from the doc comments of this package. The library
// keys them by a package path it joins with the OS separator, so on Windows the keys get
// backslashes (internal\config) and match no type; they are normalized to slashes.
func addGoComments(r *ijs.Reflector) error {
	if err := r.AddGoComments("github.com/nimbusxr/axx", "./internal/config"); err != nil {
		return err
	}
	normalized := make(map[string]string, len(r.CommentMap))
	for k, v := range r.CommentMap {
		normalized[strings.ReplaceAll(k, `\`, "/")] = v
	}
	r.CommentMap = normalized
	return nil
}

// Generate builds the JSON Schema from the Go types. goComments enables
// descriptions from doc comments (requires running from the module root).
func Generate(goComments bool) ([]byte, error) {
	r := &ijs.Reflector{ExpandedStruct: true, DoNotReference: false}
	if goComments {
		if err := addGoComments(r); err != nil {
			return nil, err
		}
	}
	s := r.Reflect(&Config{})
	s.ID = ijs.ID(SchemaID)
	s.Title = "axx.yaml"
	s.Description = "Configuration for axx, the human-readable acceptance testing framework (https://axx.nimbusxr.us)."
	out, err := s.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := indentJSON(&buf, out); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

var (
	compiled    *jsonschema.Schema
	compileErr  error
	compileOnce sync.Once
)

func compiledSchema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		if len(bytes.TrimSpace(SchemaJSON)) <= 2 {
			compileErr = errNoSchema
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(SchemaJSON))
		if err != nil {
			compileErr = err
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(SchemaID, doc); err != nil {
			compileErr = err
			return
		}
		compiled, compileErr = c.Compile(SchemaID)
	})
	return compiled, compileErr
}

// Issue is one schema violation.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

// InvalidError lists every schema violation.
type InvalidError struct{ Issues []Issue }

func (e *InvalidError) Error() string {
	var b strings.Builder
	for _, is := range e.Issues {
		b.WriteString("\n  ")
		if is.File != "" {
			fmt.Fprintf(&b, "%s:%d:%d: ", displayFile(is.File), is.Line, is.Column)
		}
		if is.Path != "" {
			fmt.Fprintf(&b, "%s: ", is.Path)
		}
		b.WriteString(is.Message)
	}
	return b.String()
}

func validate(doc []byte, sources []source) error {
	sch, err := compiledSchema()
	if errors.Is(err, errNoSchema) {
		return nil // bootstrap before `go generate`; struct decoding still validates
	}
	if err != nil {
		return fmt.Errorf("compile config schema: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return err
	}
	err = sch.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	var issues []Issue
	collect(ve, &issues)
	for i := range issues {
		var path []string
		if issues[i].Path != "" {
			path = strings.Split(issues[i].Path, ".")
		}
		issues[i].File, issues[i].Line, issues[i].Column = positionOf(sources, path)
	}
	sort.SliceStable(issues, func(a, b int) bool { return issues[a].Line < issues[b].Line })
	inv := &InvalidError{Issues: issues}
	return axxerr.Wrap(inv, CodeInvalid, exitcode.Usage, "invalid configuration (%d problem%s)", len(issues), plural(len(issues))).
		WithHint("see %s or run `axx schema` for the full schema", "https://axx.nimbusxr.us/references/config/")
}

var printer = message.NewPrinter(language.English)

// friendlyPatterns replaces raw regular expressions in messages.
var friendlyPatterns = map[string]string{
	`^\s*([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+\s*$`: "a duration such as 500ms, 30s or 1m30s",
}

// collect flattens leaf validation errors. For oneOf/anyOf failures it keeps
// only the branches whose type matched the value, so `workers: many` reports
// "must be 'auto'" instead of also "got string, want integer".
func collect(ve *jsonschema.ValidationError, out *[]Issue) {
	if len(ve.Causes) == 0 {
		msg := ve.ErrorKind.LocalizedString(printer)
		if p, ok := ve.ErrorKind.(*kind.Pattern); ok {
			if friendly, ok := friendlyPatterns[p.Want]; ok {
				msg = fmt.Sprintf("%q is not %s", p.Got, friendly)
			}
		}
		*out = append(*out, Issue{Path: strings.Join(ve.InstanceLocation, "."), Message: msg})
		return
	}
	causes := ve.Causes
	switch ve.ErrorKind.(type) {
	case *kind.OneOf, *kind.AnyOf:
		var typed []*jsonschema.ValidationError
		for _, c := range causes {
			if !isTypeMismatch(c) {
				typed = append(typed, c)
			}
		}
		if len(typed) > 0 {
			causes = typed
		}
	}
	for _, c := range causes {
		collect(c, out)
	}
}

func isTypeMismatch(ve *jsonschema.ValidationError) bool {
	for len(ve.Causes) == 1 {
		ve = ve.Causes[0]
	}
	_, ok := ve.ErrorKind.(*kind.Type)
	return ok && len(ve.Causes) == 0
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
