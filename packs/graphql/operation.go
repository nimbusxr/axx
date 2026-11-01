package graphql

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/vektah/gqlparser/v2/validator"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/schemadoc"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/tablevalue"
)

// operation is a query, a mutation or a subscription a scenario sends.
type operation struct {
	text      string // the document
	doc       *ast.QueryDocument
	def       *ast.OperationDefinition
	variables json.RawMessage // nil without variables
}

// kind is "query", "mutation" or "subscription".
func (o *operation) kind() string { return string(o.def.Operation) }

// label names the operation in messages: its name, or its kind.
func (o *operation) label() string {
	if o.def.Name != "" {
		return o.def.Name
	}
	return "the " + o.kind()
}

// readOperation reads an operation from a file of the project, or a doc string.
func readOperation(sc *core.Scenario, file string, doc *core.DocString) (*operation, error) {
	var text string
	switch {
	case file != "":
		p, err := sc.ResolvePath(file)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		text = string(raw)
	case doc != nil:
		text = doc.Content
	}
	text, err := secrets.Resolve(sc, text)
	if err != nil {
		return nil, err
	}
	d, err := parser.ParseQuery(&ast.Source{Name: file, Input: text})
	if err != nil {
		return nil, fmt.Errorf("the operation is not GraphQL: %w", err)
	}
	if len(d.Operations) != 1 {
		return nil, fmt.Errorf("the operation document has %d operations, not one", len(d.Operations))
	}
	return &operation{text: text, doc: d, def: d.Operations[0]}, nil
}

// setVariables sets the operation's variables from a table: each row a
// variable, or a path into one, and its value, which means what it says
// (ADR 0014). Nothing takes the type the operation declares: a value of
// another type is sent as written, for the schema's check to find, or the
// service to refuse.
func (o *operation) setVariables(sc *core.Scenario, t *core.Table) error {
	if t == nil {
		return nil
	}
	pairs, err := t.Pairs()
	if err != nil {
		return err
	}
	rows := make([]tablevalue.Row, len(pairs))
	for i, p := range pairs {
		rows[i] = tablevalue.Row{Path: p.Key, Value: p.Value, Null: p.Null}
	}
	vars, err := tablevalue.Build(rows, func(v string) (string, error) { return secrets.Resolve(sc, v) })
	if err != nil {
		return err
	}
	o.variables, err = json.Marshal(vars)
	return err
}

type finding = schemadoc.Finding

// check checks the operation and its variables against the schema.
func (o *operation) check(schema *ast.Schema) []finding {
	var out []finding
	for _, e := range validator.ValidateWithRules(schema, o.doc, nil) {
		rule := e.Rule
		if rule == "" {
			rule = "unknown"
		}
		out = append(out, finding{Key: "validation.operation." + rule, Message: e.Message})
	}
	if len(out) > 0 {
		return out
	}
	var vars, typed map[string]any
	if o.variables != nil {
		_ = json.Unmarshal(o.variables, &vars)
		dec := json.NewDecoder(strings.NewReader(string(o.variables)))
		dec.UseNumber()
		_ = dec.Decode(&typed)
	}
	if vars == nil {
		vars = map[string]any{}
	}
	types := o.variableTypes(schema, typed)
	if _, err := validator.VariableValues(schema, o.def, vars); err != nil {
		key := "validation.variables.type"
		if strings.Contains(err.Error(), "must be defined") {
			key = "validation.variables.missing"
		}
		// A scalar of another type is one of types, which says it better.
		if len(types) == 0 || !strings.Contains(err.Error(), "cannot use") {
			out = append(out, finding{Key: key, Message: strings.TrimPrefix(err.Error(), "input: ")})
		}
	}
	return append(out, types...)
}

// variableTypes are the variables' values that are not of their types: a
// String's number, an Int's text. Nothing converts them, so each finding
// says what to write: `$postcode is a String!, not the number 10115: write
// "10115"`.
func (o *operation) variableTypes(schema *ast.Schema, vars map[string]any) []finding {
	var out []finding
	for _, vd := range o.def.VariableDefinitions {
		if v, ok := vars[vd.Variable]; ok {
			out = append(out, typeFindings(schema, vd.Type, v, "$"+vd.Variable)...)
		}
	}
	return out
}

func typeFindings(schema *ast.Schema, t *ast.Type, v any, path string) []finding {
	if v == nil {
		return nil // whether it may be null is VariableValues's to say
	}
	if t.Elem != nil {
		items, ok := v.([]any)
		if !ok {
			return typeFindings(schema, t.Elem, v, path) // a value for a list is a list of one
		}
		var out []finding
		for i, e := range items {
			out = append(out, typeFindings(schema, t.Elem, e, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	}
	def := schema.Types[t.NamedType]
	if def == nil {
		return nil
	}
	switch def.Kind {
	case ast.InputObject:
		obj, ok := v.(map[string]any)
		if !ok {
			return nil // VariableValues says it is not an object
		}
		var out []finding
		for _, k := range slices.Sorted(maps.Keys(obj)) {
			if f := def.Fields.ForName(k); f != nil {
				out = append(out, typeFindings(schema, f.Type, obj[k], path+"."+k)...)
			}
		}
		return out
	case ast.Enum:
		if _, ok := v.(string); ok {
			return nil // VariableValues knows its values
		}
	case ast.Scalar:
		write, ok := scalarFits(def.Name, v)
		if ok {
			return nil
		}
		msg := fmt.Sprintf("%s is %s, not %s", path, withArticle(t.String()), describeVariable(v))
		if write != "" {
			msg += ": write " + write
		}
		return []finding{{Key: "validation.variables.type", Message: msg}}
	default:
		return nil
	}
	return []finding{{Key: "validation.variables.type", Message: fmt.Sprintf("%s is %s, not %s: write one of its values", path, withArticle(t.String()), describeVariable(v))}}
}

// integerText and numberText are text that could have been written as a
// number: what to write instead.
var (
	integerText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	numberText  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
)

// scalarFits says whether a value fits a scalar as GraphQL coerces its
// input, and, when it does not, what to write: a String takes text, an ID
// text or an integer, an Int an integer, a Float a number, a Boolean true or
// false; a custom scalar takes anything.
func scalarFits(name string, v any) (write string, ok bool) {
	switch name {
	case "String", "ID":
		switch x := v.(type) {
		case string:
			return "", true
		case json.Number:
			if name == "ID" && !strings.ContainsAny(x.String(), ".eE") {
				return "", true
			}
			return `"` + x.String() + `"`, false
		case bool:
			return `"` + strconv.FormatBool(x) + `"`, false
		}
		return "", false
	case "Int":
		switch x := v.(type) {
		case json.Number:
			if !strings.ContainsAny(x.String(), ".eE") {
				return "", true // VariableValues says whether it fits 32 bits
			}
		case string:
			if integerText.MatchString(x) {
				return x, false
			}
		}
		return "", false
	case "Float":
		switch x := v.(type) {
		case json.Number:
			return "", true
		case string:
			if numberText.MatchString(x) {
				return x, false
			}
		}
		return "", false
	case "Boolean":
		switch x := v.(type) {
		case bool:
			return "", true
		case string:
			if x == "true" || x == "false" {
				return x, false
			}
		}
		return "true or false", false
	}
	return "", true
}

// describeVariable names a variable's value in a finding: the number 10115,
// the text "800", the boolean true, a list, an object.
func describeVariable(v any) string {
	switch x := v.(type) {
	case string:
		return "the text " + strconv.Quote(x)
	case json.Number:
		return "the number " + x.String()
	case bool:
		return "the boolean " + strconv.FormatBool(x)
	}
	return describeValue(v)
}

// withArticle is a type's name after "a" or "an": "a String!", "an Int".
func withArticle(name string) string {
	if strings.ContainsRune("AEIOU", rune(name[0])) {
		return "an " + name
	}
	return "a " + name
}

// body is the operation as the service is sent it.
func (o *operation) body() map[string]any {
	b := map[string]any{"query": o.text}
	if o.def.Name != "" {
		b["operationName"] = o.def.Name
	}
	if o.variables != nil {
		b["variables"] = o.variables
	}
	return b
}

// checkData checks an answer's data against the operation's types. It
// needs the operation checked against the schema first, which gives its
// fields their definitions.
func (o *operation) checkData(schema *ast.Schema, data json.RawMessage, errs []json.RawMessage) []finding {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil // no data: the errors say why
	}
	var typ *ast.Definition
	switch o.def.Operation {
	case ast.Mutation:
		typ = schema.Mutation
	case ast.Subscription:
		typ = schema.Subscription
	default:
		typ = schema.Query
	}
	if typ == nil {
		return nil
	}
	w := &walker{schema: schema, errPaths: errorPaths(errs)}
	w.object(o.def.SelectionSet, typ, root, "")
	sort.SliceStable(w.found, func(i, j int) bool { return w.found[i].Message < w.found[j].Message })
	return w.found
}

// errorPaths are the paths of an answer's errors, as dotted text.
func errorPaths(errs []json.RawMessage) []string {
	var out []string
	for _, e := range errs {
		var x struct {
			Path []any `json:"path"`
		}
		if json.Unmarshal(e, &x) == nil && len(x.Path) > 0 {
			out = append(out, dotted(x.Path))
		}
	}
	return out
}

func dotted(path []any) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = fmt.Sprint(p)
	}
	return strings.Join(parts, ".")
}

type walker struct {
	schema   *ast.Schema
	errPaths []string
	found    []finding
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (w *walker) add(key, path, format string, args ...any) {
	w.found = append(w.found, finding{Key: key, Message: "data." + path + ": " + fmt.Sprintf(format, args...)})
}

// explained reports whether an error of the answer is at path or below it,
// which explains a null there.
func (w *walker) explained(path string) bool {
	for _, e := range w.errPaths {
		if e == path || strings.HasPrefix(e, path+".") {
			return true
		}
	}
	return false
}

func (w *walker) object(set ast.SelectionSet, typ *ast.Definition, data map[string]any, path string) {
	selected := map[string]bool{}
	for _, f := range w.fields(set, typ, data) {
		key := f.Alias
		if key == "" {
			key = f.Name
		}
		selected[key] = true
		v, ok := data[key]
		if !ok {
			if len(f.Directives) == 0 { // @skip and @include may leave it out
				w.add("validation.response.missingField", join(path, key), "missing, and the operation selects it")
			}
			continue
		}
		if f.Definition == nil || f.Name == "__typename" {
			continue
		}
		w.value(f.Definition.Type, v, f.SelectionSet, join(path, key))
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !selected[k] {
			w.add("validation.response.unknownField", join(path, k), "the operation does not select it")
		}
	}
}

// fields are a selection's fields that apply to a value of typ.
func (w *walker) fields(set ast.SelectionSet, typ *ast.Definition, data map[string]any) []*ast.Field {
	var out []*ast.Field
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			out = append(out, s)
		case *ast.InlineFragment:
			if s.TypeCondition == "" || w.applies(s.TypeCondition, typ, data) {
				out = append(out, w.fields(s.SelectionSet, typ, data)...)
			}
		case *ast.FragmentSpread:
			if s.Definition != nil && w.applies(s.Definition.TypeCondition, typ, data) {
				out = append(out, w.fields(s.Definition.SelectionSet, typ, data)...)
			}
		}
	}
	return out
}

// applies reports whether a fragment on cond applies to a value of typ.
func (w *walker) applies(cond string, typ *ast.Definition, data map[string]any) bool {
	concrete := typ.Name
	if tn, ok := data["__typename"].(string); ok {
		concrete = tn
	} else if typ.IsAbstractType() {
		return true // it cannot say which type it is
	}
	if cond == concrete {
		return true
	}
	if def := w.schema.Types[cond]; def != nil && def.IsAbstractType() {
		for _, p := range w.schema.GetPossibleTypes(def) {
			if p.Name == concrete {
				return true
			}
		}
	}
	return false
}

func (w *walker) value(t *ast.Type, v any, set ast.SelectionSet, path string) {
	if v == nil {
		if t.NonNull && !w.explained(path) {
			w.add("validation.response.nonNull", path, "null, for a non-null %s", t.String())
		}
		return
	}
	if t.Elem != nil {
		arr, ok := v.([]any)
		if !ok {
			w.add("validation.response.type", path, "%s, not a list (%s)", describeValue(v), t.String())
			return
		}
		for i, e := range arr {
			w.value(t.Elem, e, set, join(path, strconv.Itoa(i)))
		}
		return
	}
	def := w.schema.Types[t.NamedType]
	if def == nil {
		return
	}
	switch def.Kind {
	case ast.Scalar:
		if !scalarOK(def.Name, v) {
			w.add("validation.response.type", path, "%s, not a %s", describeValue(v), def.Name)
		}
	case ast.Enum:
		s, ok := v.(string)
		if !ok || def.EnumValues.ForName(s) == nil {
			w.add("validation.response.enum", path, "%s is not a %s", describeValue(v), def.Name)
		}
	case ast.Object, ast.Interface, ast.Union:
		m, ok := v.(map[string]any)
		if !ok {
			w.add("validation.response.type", path, "%s, not a %s object", describeValue(v), def.Name)
			return
		}
		concrete := def
		if tn, ok := m["__typename"].(string); ok && w.schema.Types[tn] != nil {
			concrete = w.schema.Types[tn]
		}
		w.object(set, concrete, m, path)
	}
}

func scalarOK(name string, v any) bool {
	switch name {
	case "Int":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := strconv.ParseInt(n.String(), 10, 32)
		return err == nil
	case "Float":
		_, ok := v.(json.Number)
		return ok
	case "String":
		_, ok := v.(string)
		return ok
	case "Boolean":
		_, ok := v.(bool)
		return ok
	case "ID":
		switch v.(type) {
		case string, json.Number:
			return true
		}
		return false
	}
	return true // a custom scalar: any value
}

func describeValue(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprint(v)
}

// levelKeys are the keys GraphQL levels can be set on.
var levelKeys = oaslevel.NewKeys([]string{
	"validation.operation.{rule}",
	"validation.variables.missing",
	"validation.variables.type",
	"validation.response.nonNull",
	"validation.response.type",
	"validation.response.enum",
	"validation.response.missingField",
	"validation.response.unknownField",
}, map[string][]string{"{rule}": ruleNames}, nil).Named("GraphQL", "validation.operation")

// ruleNames are the names of the GraphQL spec's validation rules, which
// operation findings are keyed by.
var ruleNames = []string{
	"FieldsOnCorrectType", "FragmentsOnCompositeTypes", "KnownArgumentNames", "KnownDirectives",
	"KnownFragmentNames", "KnownRootType", "KnownTypeNames", "LoneAnonymousOperation", "MaxIntrospectionDepth",
	"NoFragmentCycles", "NoUndefinedVariables", "NoUnusedFragments", "NoUnusedVariables",
	"OverlappingFieldsCanBeMerged", "PossibleFragmentSpreads", "ProvidedRequiredArguments", "ScalarLeafs",
	"SingleFieldSubscriptions", "UniqueArgumentNames", "UniqueDirectivesPerLocation", "UniqueFragmentNames",
	"UniqueInputFieldNames", "UniqueOperationNames", "UniqueVariableNames", "ValuesOfCorrectType",
	"VariablesAreInputTypes", "VariablesInAllowedPosition", "unknown",
}

type levelsState struct {
	mu     sync.Mutex
	levels oaslevel.Levels
}

var scenarioLevels = core.NewStateKey(Name+".levels", func(*core.Scenario) *levelsState {
	return &levelsState{levels: oaslevel.Levels{}}
}, nil)

// report fails on the findings at the ERROR level, and logs the others.
func report(sc *core.Scenario, st *settings, s *service, what string, found []finding) error {
	if len(found) == 0 {
		return nil
	}
	levels := st.levels
	if ls, ok := scenarioLevels.Peek(sc); ok {
		ls.mu.Lock()
		levels = levels.Merge(ls.levels)
		ls.mu.Unlock()
	}
	var errs []string
	for _, f := range found {
		switch lv := levels.Resolve(f.Key); lv {
		case oaslevel.Error:
			errs = append(errs, "- "+f.Key+": "+f.Message)
		case oaslevel.Warn, oaslevel.Info:
			sc.Log("GraphQL %s %s: %s", lv, f.Key, f.Message)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return secrets.Hide(sc, core.Failf("%s breaks the %s graphql service's schema:\n%s\nTo relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with "+
		"\"Given the GraphQL validation levels are:\" or packs.graphql.levels in axx.yaml.", what, s.name, strings.Join(errs, "\n")))
}
