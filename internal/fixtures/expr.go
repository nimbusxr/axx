package fixtures

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Function is callable from spec values as a whole-scalar expression:
// token: ${ tokenize(card_number, "CRM") }. Expressions evaluate when
// fixtures expand; the generated files carry plain values.
//
// A pure function (the default) is a deterministic function of its
// arguments and is re-evaluated by check too, so changing it surfaces as
// fixture drift. A recorded function may reach local infrastructure when
// `axx fixtures generate` runs; its results are written to the committed
// pairings lock and check reads only that file, never invoking it. Changing a
// recorded function's behavior requires a new Version, which invalidates its
// recorded pairings until the next generate.
//
// The interface is internal; a future plugin capability (factory.function)
// can back it.
type Function interface {
	// Name is what expressions call, e.g. "tokenize".
	Name() string
	// Version of the function's behavior; bump it when a recorded function's
	// output for the same arguments changes.
	Version() string
	// Recorded reports whether results go to the pairings lock instead of
	// being re-evaluated at check time.
	Recorded() bool
	// Apply computes the value. Arguments arrive as strings: literals as
	// written, references resolved to the referenced field's text. scope is
	// the instance being produced.
	Apply(args []string, scope *InstanceScope) (string, error)
}

// Lifecycle is implemented by functions that start something for a
// generate run. BeforeAll runs once, lazily, before the first Apply of a
// run (never in check mode, and not at all when every pairing is already
// recorded); AfterAll runs once at the end for every function whose
// BeforeAll ran, even when generation fails.
type Lifecycle interface {
	BeforeAll() error
	AfterAll() error
}

// InstanceScope is the instance an expression produces values for: one per
// fixture, stable across runs, so a function whose value must vary per
// instance (a fresh identifier, say) can derive it deterministically.
type InstanceScope struct {
	// Discriminator identifies the instance; derived values key on it.
	Discriminator string
	// Attributes are extra context a producer offers its own functions.
	Attributes map[string]string
}

// Material is keying material for a value named name within the instance.
func (s *InstanceScope) Material(name string) string { return s.Discriminator + "|" + name }

// Mode selects how recorded functions resolve.
type Mode int

const (
	// ModeVerify resolves recorded functions from the committed pairings
	// only and fails on a miss or version mismatch (check, adopt).
	ModeVerify Mode = iota
	// ModeGenerate may invoke recorded functions and records their results.
	ModeGenerate
)

// Scope names for scope-addressed arguments ($data.x, $metadata.x).
const (
	ScopeData     = "data"
	ScopeMetadata = "metadata"
)

var scopeNames = []string{ScopeData, ScopeMetadata}

// expression is ExpressionEvaluator.EXPRESSION.
var expression = regexp.MustCompile(`^\$\{[ \t\n\x0B\f\r]*([A-Za-z][A-Za-z0-9_-]*)\(([\s\S]*)\)[ \t\n\x0B\f\r]*\}$`)

var numericArg = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// evaluator is ExpressionEvaluator.
type evaluator struct {
	functions  map[string]Function
	mode       Mode
	committed  *pairingsLock
	used       *pairingsLock
	started    []string
	references References
	scope      *InstanceScope
}

func newEvaluator(functions map[string]Function, mode Mode, committed *pairingsLock) *evaluator {
	return &evaluator{functions: functions, mode: mode, committed: committed, used: newPairingsLock()}
}

// javaStrip is String.strip().
func javaStrip(s string) string {
	return strings.TrimFunc(s, unicode.IsSpace)
}

func isExpression(v any) bool {
	s, ok := v.(string)
	return ok && expression.MatchString(javaStrip(s))
}

// evaluateTree evaluates every expression scalar in tree, in place, with
// scopes naming the document's resolved trees.
func (e *evaluator) evaluateTree(tree *jsonx.Object, where string, scopes map[string]*jsonx.Object) error {
	if scopes == nil {
		scopes = map[string]*jsonx.Object{ScopeData: tree}
	}
	return e.evaluateMap(tree, tree, where, scopes)
}

func (e *evaluator) evaluateMap(m, tree *jsonx.Object, where string, scopes map[string]*jsonx.Object) error {
	for _, k := range m.Keys() {
		v, err := e.evaluateValue(get(m, k), tree, where, scopes)
		if err != nil {
			return err
		}
		m.Set(k, v)
	}
	return nil
}

func (e *evaluator) evaluateValue(v any, tree *jsonx.Object, where string, scopes map[string]*jsonx.Object) (any, error) {
	switch x := v.(type) {
	case *jsonx.Object:
		return x, e.evaluateMap(x, tree, where, scopes)
	case []any:
		for i := range x {
			r, err := e.evaluateValue(x[i], tree, where, scopes)
			if err != nil {
				return nil, err
			}
			x[i] = r
		}
		return x, nil
	}
	return e.evaluateScalar(v, tree, where, scopes)
}

// evaluateScalar evaluates one scalar; non-expressions come back unchanged.
func (e *evaluator) evaluateScalar(v any, tree *jsonx.Object, where string, scopes map[string]*jsonx.Object) (any, error) {
	s, ok := v.(string)
	if !ok {
		return v, nil
	}
	m := expression.FindStringSubmatch(javaStrip(s))
	if m == nil {
		return v, nil
	}
	args, err := e.resolveArgs(m[2], tree, where, scopes)
	if err != nil {
		return nil, err
	}
	return e.invoke(m[1], args, where)
}

func (e *evaluator) resolveArgs(raw string, tree *jsonx.Object, where string, scopes map[string]*jsonx.Object) ([]string, error) {
	var args []string
	for _, part := range splitArgs(raw) {
		arg := javaStrip(part)
		switch {
		case arg == "":
			continue
		case len(arg) >= 2 && (arg[0] == '"' && arg[len(arg)-1] == '"' || arg[0] == '\'' && arg[len(arg)-1] == '\''):
			args = append(args, arg[1:len(arg)-1])
		case numericArg.MatchString(arg):
			args = append(args, arg)
		case strings.HasPrefix(arg, "$"):
			v, err := e.scoped(arg, where, scopes)
			if err != nil {
				return nil, err
			}
			args = append(args, valueOf(v))
		default:
			v, err := pathGet(tree, arg)
			if err != nil {
				return nil, err
			}
			if v, err = referenced(v, arg, "this fixture", where); err != nil {
				return nil, err
			}
			args = append(args, valueOf(v))
		}
	}
	return args, nil
}

func prefixedScopes(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "$" + n
	}
	return javaListString(out)
}

func (e *evaluator) scoped(arg, where string, scopes map[string]*jsonx.Object) (any, error) {
	scope, path := arg[1:], ""
	if dot := strings.IndexByte(arg, '.'); dot >= 0 {
		scope, path = arg[1:dot], arg[dot+1:]
	}
	known := false
	for _, s := range scopeNames {
		known = known || s == scope
	}
	if !known {
		return nil, genError("%s: expression argument '%s' names no scope; the scopes are %s", where, arg, prefixedScopes(scopeNames))
	}
	if path == "" {
		return nil, genError("%s: expression argument '%s' names a scope but no field in it, as in $%s.someField", where, arg, scope)
	}
	tree, ok := scopes[scope]
	if !ok || tree == nil {
		var offered []string
		for _, s := range scopeNames {
			if scopes[s] != nil {
				offered = append(offered, s)
			}
		}
		return nil, genError("%s: expression argument '%s' reads $%s, which is not resolved where this expression lives; here it may read %s",
			where, arg, scope, prefixedScopes(offered))
	}
	v, err := pathGet(tree, path)
	if err != nil {
		return nil, err
	}
	return referenced(v, arg, "$"+scope, where)
}

// referenced returns a referenced value, or fails when nothing is there or it
// is another expression (no chaining).
func referenced(v any, arg, in, where string) (any, error) {
	if v == nil {
		return nil, genError("%s: expression argument '%s' references no field in %s", where, arg, in)
	}
	if isExpression(v) {
		return nil, genError("%s: expression argument '%s' references another expression field - chaining is not supported", where, arg)
	}
	return v, nil
}

func splitArgs(raw string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, c := range raw {
		switch {
		case quote != 0:
			cur.WriteRune(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			cur.WriteRune(c)
		case c == ',':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (e *evaluator) functionNames() []string {
	names := make([]string, 0, len(e.functions))
	for n := range e.functions {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}

func (e *evaluator) invoke(name string, args []string, where string) (string, error) {
	fn, ok := e.functions[name]
	if !ok {
		return "", genError("%s: unknown expression function '%s' (available: %s)", where, name, javaListString(e.functionNames()))
	}
	if !fn.Recorded() {
		if err := e.ensureStarted(fn, where); err != nil {
			return "", err
		}
		return e.apply(fn, args, where)
	}
	recordedVersion, has := e.committed.recordedVersion(name)
	versionMatches := has && fn.Version() == recordedVersion
	if has && !versionMatches && e.mode == ModeVerify {
		return "", genError("%s: pairings for '%s' were recorded with version %s but the function is now version %s - run `axx fixtures generate` (with your local stack up) to re-record",
			where, name, recordedVersion, fn.Version())
	}
	if versionMatches {
		if recorded, ok := e.committed.lookup(name, args); ok {
			e.used.record(name, fn.Version(), args, recorded)
			return recorded, nil
		}
	}
	if e.mode == ModeVerify {
		return "", genError("%s: no recorded pairing for %s(%s) in %s - run `axx fixtures generate` (with your local stack up) to record it",
			where, name, strings.Join(args, ", "), PairingsFile)
	}
	if err := e.ensureStarted(fn, where); err != nil {
		return "", err
	}
	value, err := e.apply(fn, args, where)
	if err != nil {
		return "", err
	}
	e.used.record(name, fn.Version(), args, value)
	return value, nil
}

func (e *evaluator) apply(fn Function, args []string, where string) (string, error) {
	v, err := fn.Apply(args, e.scope)
	if err != nil {
		return "", genError("%s: expression function '%s' failed: %s", where, fn.Name(), errText(err))
	}
	return v, nil
}

func (e *evaluator) ensureStarted(fn Function, where string) error {
	if e.mode != ModeGenerate {
		return nil
	}
	for _, s := range e.started {
		if s == fn.Name() {
			return nil
		}
	}
	e.started = append(e.started, fn.Name())
	lc, ok := fn.(Lifecycle)
	if !ok {
		return nil
	}
	if err := lc.BeforeAll(); err != nil {
		e.started = e.started[:len(e.started)-1]
		return genError("%s: beforeAll of expression function '%s' failed: %s", where, fn.Name(), errText(err))
	}
	return nil
}

// finish runs AfterAll for every function whose BeforeAll ran.
func (e *evaluator) finish() error {
	var problems []string
	for _, name := range e.started {
		if lc, ok := e.functions[name].(Lifecycle); ok {
			if err := lc.AfterAll(); err != nil {
				problems = append(problems, name+": "+errText(err))
			}
		}
	}
	e.started = nil
	if len(problems) > 0 {
		return genError("afterAll of expression function(s) failed:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// functionMap indexes functions by name, refusing duplicates.
func functionMap(fns []Function) (map[string]Function, error) {
	out := map[string]Function{}
	for _, fn := range fns {
		if prev, ok := out[fn.Name()]; ok {
			return nil, configError("two expression functions claim the name '%s': %T and %T", fn.Name(), prev, fn)
		}
		out[fn.Name()] = fn
	}
	return out, nil
}

var _ = fmt.Sprint
