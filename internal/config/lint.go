package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	ijs "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// CodeLintInclude is reported for a lint include file that is not valid
// YAML or does not match the lint section of the schema.
const CodeLintInclude = "AXX-E0803"

// Lint configures `axx lint`: test-data isolation rules. Each rule extracts
// values (ids, keys, names) from the files it selects and reports values
// that repeat where they must be unique, so scenarios sharing a database or
// broker can run in parallel without colliding.
type Lint struct {
	// Include lists rule files merged into this section (for example the
	// axx-lint.generated.yaml written by `axx fixtures generate`), resolved
	// relative to the including file. Included files contribute rules and
	// further includes only; they must not have a config block.
	Include StringList `json:"include,omitempty"`
	// Config holds the settings shared by every rule.
	Config *LintConfig `json:"config,omitempty"`
	// Rules are the isolation rules, run in order (included rules follow).
	Rules []LintRule `json:"rules,omitempty"`
}

// LintConfig holds the settings shared by every lint rule.
type LintConfig struct {
	// BaseDir is the directory file patterns are resolved against, relative
	// to axx.yaml. Default: the directory of axx.yaml.
	BaseDir string `json:"baseDir,omitempty"`
	// Mode is "error" (violations make `axx lint` exit 3, default) or "warn"
	// (violations are reported but do not fail).
	Mode string `json:"mode,omitempty" jsonschema:"enum=error,enum=warn"`
	// MaxFileSize skips files larger than this many bytes, with a warning.
	// Default: 5242880 (5 MB).
	MaxFileSize int64 `json:"maxFileSize,omitempty" jsonschema:"minimum=1"`
	// MaxReportedValues is how many duplicate values the human output lists
	// per rule before truncating. Default: 3. Machine formats list all.
	MaxReportedValues int `json:"maxReportedValues,omitempty" jsonschema:"minimum=1"`
	// MaxReportedLocations is how many locations the human output lists per
	// duplicate value before truncating. Default: 5. Machine formats list all.
	MaxReportedLocations int `json:"maxReportedLocations,omitempty" jsonschema:"minimum=1"`
}

// LintRule is one test-data isolation rule.
type LintRule struct {
	// Name identifies the rule in reports.
	Name string `json:"name"`
	// Description says why the values must be unique; shown with violations.
	Description string `json:"description,omitempty"`
	// FilePatterns are glob patterns (Java glob syntax: *, **, ?, [abc],
	// {a,b}) selecting the files to scan, relative to baseDir. The part
	// before the first wildcard is a directory (it may start with ../); the
	// rest is matched against the end of each file path below it, so
	// "seeds/*.yaml" also finds YAML files in subdirectories of seeds.
	FilePatterns StringList `json:"filePatterns"`
	// ExcludePatterns are glob patterns removing files from the match set,
	// matched against the whole path relative to baseDir; ** crosses
	// directories, e.g. "**.fixture.yaml".
	ExcludePatterns StringList `json:"excludePatterns,omitempty"`
	// Type is "regex" (default: extract values with regex) or "jsonpath"
	// (parse each file as JSON and extract the values at jsonPath).
	Type string `json:"type,omitempty" jsonschema:"enum=regex,enum=jsonpath"`
	// Regex is a Java regular expression, applied in MULTILINE mode (^ and $
	// match at line boundaries). Each match contributes the text of its
	// first participating capture group.
	Regex string `json:"regex,omitempty"`
	// JSONPath selects the values of a jsonpath rule, e.g. "order.id",
	// "payments[*].id" or "$.items[0].sku" (exact field paths with array
	// indexes or [*]); other JSONPath expressions ("$..id", filters) are
	// evaluated like Jayway JsonPath. Only scalar values count.
	JSONPath string `json:"jsonPath,omitempty"`
	// Validation is "global-unique" (default: a value may occur once),
	// "file-unique" (no repeats within a file; files may share values) or
	// "cross-file-unique" (repeats within one file are fine; no two files
	// may share a value).
	Validation string `json:"validation,omitempty" jsonschema:"enum=global-unique,enum=file-unique,enum=cross-file-unique"`
	// Mode overrides config.mode for this rule: "error" or "warn".
	Mode string `json:"mode,omitempty" jsonschema:"enum=error,enum=warn"`
	// IgnoreValues are values never reported (intentionally shared constants).
	IgnoreValues ScalarList `json:"ignoreValues,omitempty"`
}

// ScalarList is a list of strings that also accepts numbers and booleans,
// kept as their literal text.
type ScalarList []string

// UnmarshalJSON accepts a list of strings, numbers and booleans.
func (l *ScalarList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("must be a list of values")
	}
	out := make(ScalarList, 0, len(raw))
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, s)
			continue
		}
		t := strings.TrimSpace(string(r))
		if t == "" || t == "null" || t[0] == '{' || t[0] == '[' {
			return fmt.Errorf("values must be strings, numbers or booleans")
		}
		out = append(out, t)
	}
	*l = out
	return nil
}

// JSONSchema describes ScalarList.
func (ScalarList) JSONSchema() *ijs.Schema {
	return &ijs.Schema{Type: "array", Items: &ijs.Schema{OneOf: []*ijs.Schema{
		{Type: "string"}, {Type: "number"}, {Type: "boolean"},
	}}}
}

// Position locates a value of the loaded configuration by its path of
// mapping keys and sequence indexes (e.g. "lint", "rules", "2", "regex"). The
// overlay files are searched first; when the full path is not found the
// nearest enclosing node is reported. ok is false when no file has it.
func (c *Config) Position(path ...string) (loc axxerr.Location, ok bool) {
	file, line, col := positionOf(c.sources, path)
	if file == "" {
		return axxerr.Location{}, false
	}
	return axxerr.Location{File: displayFile(file), Line: line, Column: col}, true
}

// YAMLPosition returns the line and column of the node at path (mapping keys
// and sequence indexes) in a YAML document, or of the nearest enclosing
// node; ok is false when the document does not parse.
func YAMLPosition(data []byte, path ...string) (line, col int, ok bool) {
	_, line, col = positionOf([]source{{file: "-", data: data}}, path)
	return line, col, line > 0
}

// LoadLintFile reads a rules file named in lint.include. It must be YAML
// matching the lint section of the axx.yaml schema; interpolation does not
// apply. Errors carry CodeLintInclude and the offending lines.
func LoadLintFile(file string) (*Lint, error) {
	tree, src, err := readYAML(file)
	if err != nil {
		var ae *axxerr.Error
		if errors.As(err, &ae) && ae.Code != CodeNotFound {
			ae.Code = CodeLintInclude
			ae.Hint = "an included lint file holds `rules:` (and optionally `include:`), in the format of the lint section of axx.yaml"
		}
		return nil, err
	}
	doc, err := toJSON(tree)
	if err != nil {
		return nil, err
	}
	if err := validateDef(doc, "Lint", []source{src}); err != nil {
		var inv *InvalidError
		if errors.As(err, &inv) {
			return nil, axxerr.Wrap(inv, CodeLintInclude, exitcode.Usage, "invalid lint include %s (%d problem%s)", displayFile(file), len(inv.Issues), plural(len(inv.Issues))).
				WithHint("an included lint file holds `rules:` (and optionally `include:`), in the format of the lint section of axx.yaml")
		}
		return nil, err
	}
	l := &Lint{}
	if err := strictUnmarshal(doc, l); err != nil {
		return nil, axxerr.Wrap(err, CodeLintInclude, exitcode.Usage, "invalid lint include %s", displayFile(file))
	}
	return l, nil
}

var (
	defSchemas   = map[string]*jsonschema.Schema{}
	defSchemasMu sync.Mutex
)

// validateDef validates doc against one $defs entry of the axx.yaml schema.
func validateDef(doc []byte, def string, sources []source) error {
	if _, err := compiledSchema(); errors.Is(err, errNoSchema) {
		return nil
	}
	defSchemasMu.Lock()
	sch, ok := defSchemas[def]
	defSchemasMu.Unlock()
	if !ok {
		schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(SchemaJSON))
		if err != nil {
			return err
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(SchemaID, schemaDoc); err != nil {
			return err
		}
		if sch, err = c.Compile(SchemaID + "#/$defs/" + def); err != nil {
			return fmt.Errorf("compile config schema: %w", err)
		}
		defSchemasMu.Lock()
		defSchemas[def] = sch
		defSchemasMu.Unlock()
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
	return &InvalidError{Issues: issues}
}
