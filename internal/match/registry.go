// Package match compiles step definitions into Cucumber expressions and
// matches step text against them.
//
// Matching is pure and static: parameter types are registered with identity
// transforms, and the real (possibly scenario-dependent) transforms run later
// in Resolve. That lets dry runs, lint, `axx explain`, the MCP server and the
// language server match steps without executing anything.
package match

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	cukexpr "github.com/cucumber/cucumber-expressions/go/v20"

	"github.com/nimbusxr/axx/core"
)

// Def is a registered step definition.
type Def struct {
	Pack  string
	Step  core.StepDef
	Names []string // parameter type names of the full expression
}

// Variant is one concrete expression of a Def.
type Variant struct {
	Def    *Def
	Expr   string
	argPos []int
	cexpr  cukexpr.Expression
}

// Match is a successful match of step text against a variant.
type Match struct {
	Variant *Variant
	// Args has one entry per parameter of the full expression.
	Args []core.Arg
}

// Def returns the matched definition.
func (m Match) Def() *Def { return m.Variant.Def }

// Param is a registered parameter type.
type Param struct {
	Pack string
	Type core.ParamType
	// builtin is set for Cucumber's own types (int, string, word...).
	builtin bool
}

// Registry holds parameter types and step definitions.
type Registry struct {
	ctypes   *cukexpr.ParameterTypeRegistry
	params   map[string]*Param
	defs     []*Def
	variants []*Variant
	byID     map[string]*Def

	cache sync.Map // text -> []Match
}

// NewRegistry returns a registry containing Cucumber's built-in parameter
// types.
func NewRegistry() *Registry {
	r := &Registry{
		ctypes: cukexpr.NewParameterTypeRegistry(),
		params: map[string]*Param{},
		byID:   map[string]*Def{},
	}
	for _, b := range builtinParams {
		r.params[b.Name] = &Param{Pack: "cucumber", Type: b, builtin: true}
	}
	return r
}

// AddParams registers a pack's parameter types. Call it for every pack before
// AddSteps, because steps may use parameter types from other packs.
func (r *Registry) AddParams(pack string, params []core.ParamType) error {
	for _, p := range params {
		if existing, ok := r.params[p.Name]; ok {
			return fmt.Errorf("parameter type {%s} from %s is already defined by %s", p.Name, pack, existing.Pack)
		}
		if len(p.Regexps) == 0 {
			return fmt.Errorf("parameter type {%s} from %s has no regexps", p.Name, pack)
		}
		res := make([]*regexp.Regexp, 0, len(p.Regexps))
		for _, s := range p.Regexps {
			re, err := regexp.Compile(s)
			if err != nil {
				return fmt.Errorf("parameter type {%s} from %s: %w", p.Name, pack, err)
			}
			res = append(res, re)
		}
		pt, err := cukexpr.NewParameterType(p.Name, res, p.Name, nil, p.Snippet, false, false)
		if err != nil {
			return fmt.Errorf("parameter type {%s} from %s: %w", p.Name, pack, err)
		}
		if err := r.ctypes.DefineParameterType(pt); err != nil {
			return fmt.Errorf("parameter type {%s} from %s: %w", p.Name, pack, err)
		}
		r.params[p.Name] = &Param{Pack: pack, Type: p}
	}
	return nil
}

// AddSteps registers a pack's steps.
func (r *Registry) AddSteps(pack string, steps []core.StepDef) error {
	for _, s := range steps {
		if s.ID == "" {
			return fmt.Errorf("%s: step %q has no ID", pack, s.Expr)
		}
		if other, ok := r.byID[s.ID]; ok {
			return fmt.Errorf("%s: step ID %q is already used by %s", pack, s.ID, other.Pack)
		}
		vars, names, err := expandVariants(s.Expr)
		if err != nil {
			return fmt.Errorf("%s: step %s: %w", pack, s.ID, err)
		}
		def := &Def{Pack: pack, Step: s, Names: names}
		for _, v := range vars {
			ce, err := cukexpr.NewCucumberExpression(v.expr, r.ctypes)
			if err != nil {
				return fmt.Errorf("%s: step %s: expression %q: %w", pack, s.ID, v.expr, err)
			}
			r.variants = append(r.variants, &Variant{Def: def, Expr: v.expr, argPos: v.argPos, cexpr: ce})
		}
		r.defs = append(r.defs, def)
		r.byID[s.ID] = def
	}
	r.cache = sync.Map{}
	return nil
}

// AddPack registers a pack's parameter types and steps.
func (r *Registry) AddPack(name string, m core.Manifest) error {
	if err := r.AddParams(name, m.Params); err != nil {
		return err
	}
	return r.AddSteps(name, m.Steps)
}

// Defs returns all step definitions in registration order.
func (r *Registry) Defs() []*Def { return r.defs }

// Variants returns every concrete expression in registration order.
func (r *Registry) Variants() []*Variant { return r.variants }

// Params returns all parameter types sorted by name.
func (r *Registry) Params() []*Param {
	out := make([]*Param, 0, len(r.params))
	for _, p := range r.params {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type.Name < out[j].Type.Name })
	return out
}

// Def returns the definition with the given ID.
func (r *Registry) Def(id string) (*Def, bool) {
	d, ok := r.byID[id]
	return d, ok
}

// Match returns every variant matching text. Zero results means the step is
// undefined; more than one means it is ambiguous. Results are memoized.
func (r *Registry) Match(text string) []Match {
	if v, ok := r.cache.Load(text); ok {
		return v.([]Match)
	}
	var out []Match
	for _, v := range r.variants {
		args, err := v.cexpr.Match(text)
		if err != nil || args == nil {
			continue
		}
		full := make([]core.Arg, len(v.Def.Names))
		for i, name := range v.Def.Names {
			full[i] = core.Arg{Param: name}
		}
		for i, a := range args {
			g := a.Group()
			arg := &full[v.argPos[i]]
			arg.Present = true
			if g.Value() != nil {
				arg.Raw = *g.Value()
			}
			arg.Start = g.Start()
			for _, c := range g.Children() {
				arg.Groups = append(arg.Groups, c.Value())
			}
		}
		out = append(out, Match{Variant: v, Args: full})
	}
	r.cache.Store(text, out)
	return out
}

// Resolve transforms a match's arguments within a scenario and attaches the
// step's data table or doc string.
func (r *Registry) Resolve(sc *core.Scenario, m Match, text string, table *core.Table, doc *core.DocString) (core.Args, error) {
	args := core.Args{Text: text, List: make([]core.Arg, len(m.Args)), Table: table, DocString: doc}
	copy(args.List, m.Args)
	for i := range args.List {
		a := &args.List[i]
		if !a.Present {
			continue
		}
		p, ok := r.params[a.Param]
		if !ok {
			return args, fmt.Errorf("undefined parameter type {%s}", a.Param)
		}
		v, err := transform(sc, p, a)
		if err != nil {
			return args, err
		}
		a.Value = v
	}
	return args, nil
}

func transform(sc *core.Scenario, p *Param, a *core.Arg) (any, error) {
	if p.Type.Transform == nil {
		return a.Raw, nil
	}
	v, err := p.Type.Transform(sc, a.Raw, a.Groups)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// builtinParams mirrors Cucumber's built-in parameter types with the value
// conversions Cucumber-JVM applies (so {int} overflows are errors, {string}
// strips quotes and unescapes).
var builtinParams = []core.ParamType{
	{Name: "int", Regexps: []string{`-?\d+`, `\d+`}, Doc: "a 32-bit integer", Transform: intTransform(math.MinInt32, math.MaxInt32, "Integer")},
	{Name: "byte", Regexps: []string{`-?\d+`, `\d+`}, Doc: "an 8-bit integer", Transform: intTransform(math.MinInt8, math.MaxInt8, "Byte")},
	{Name: "short", Regexps: []string{`-?\d+`, `\d+`}, Doc: "a 16-bit integer", Transform: intTransform(math.MinInt16, math.MaxInt16, "Short")},
	{Name: "long", Regexps: []string{`-?\d+`, `\d+`}, Doc: "a 64-bit integer", Transform: func(_ *core.Scenario, s string, _ []*string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, transformError("long", s, "Long")
		}
		return n, nil
	}},
	{Name: "biginteger", Regexps: []string{`-?\d+`, `\d+`}, Doc: "an arbitrary-precision integer", Transform: func(_ *core.Scenario, s string, _ []*string) (any, error) {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, transformError("biginteger", s, "BigInteger")
		}
		return n, nil
	}},
	{Name: "float", Regexps: []string{`[-+]?(?:\d+(?:\.\d+)?|\.\d+)(?:[E][+-]?\d+)?`}, Doc: "a 32-bit float", Transform: floatTransform(32, "float", "Float")},
	{Name: "double", Regexps: []string{`[-+]?(?:\d+(?:\.\d+)?|\.\d+)(?:[E][+-]?\d+)?`}, Doc: "a 64-bit float", Transform: floatTransform(64, "double", "Double")},
	{Name: "bigdecimal", Regexps: []string{`[-+]?(?:\d+(?:\.\d+)?|\.\d+)(?:[E][+-]?\d+)?`}, Doc: "an arbitrary-precision decimal", Transform: func(_ *core.Scenario, s string, _ []*string) (any, error) {
		f, ok := new(big.Float).SetString(s)
		if !ok {
			return nil, transformError("bigdecimal", s, "BigDecimal")
		}
		return f, nil
	}},
	{Name: "word", Regexps: []string{`[^\s]+`}, Doc: "one word, no spaces"},
	{Name: "string", Regexps: []string{`"([^"\\]*(\\.[^"\\]*)*)"|'([^'\\]*(\\.[^'\\]*)*)'`}, Doc: "text in single or double quotes; the quotes are removed", Transform: stringTransform},
	{Name: "", Regexps: []string{`.*`}, Doc: "anonymous: any text"},
}

func intTransform(minV, maxV int64, javaType string) func(*core.Scenario, string, []*string) (any, error) {
	return func(_ *core.Scenario, s string, _ []*string) (any, error) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < minV || n > maxV {
			return nil, transformError(strings.ToLower(javaType), s, javaType)
		}
		return int(n), nil
	}
}

func floatTransform(bits int, name, javaType string) func(*core.Scenario, string, []*string) (any, error) {
	return func(_ *core.Scenario, s string, _ []*string) (any, error) {
		f, err := strconv.ParseFloat(s, bits)
		if err != nil {
			return nil, transformError(name, s, javaType)
		}
		if bits == 32 {
			return float32(f), nil
		}
		return f, nil
	}
}

func transformError(param, value, javaType string) error {
	return fmt.Errorf("parameter type {%s} failed to transform [%s] to %s", param, value, javaType)
}

// stringTransform removes the surrounding quotes and unescapes \" and \'
// exactly as Cucumber-JVM does.
func stringTransform(_ *core.Scenario, raw string, _ []*string) (any, error) {
	if len(raw) < 2 {
		return raw, nil
	}
	quote := raw[0]
	inner := raw[1 : len(raw)-1]
	if quote == '"' {
		return strings.ReplaceAll(inner, `\"`, `"`), nil
	}
	return strings.ReplaceAll(inner, `\'`, `'`), nil
}
