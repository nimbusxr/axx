package jsonx

import (
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/javare"
)

// This file ports Jayway's filter evaluation: ExpressionNode,
// RelationalExpressionNode, LogicalExpressionNode, ValueNode(s) and
// EvaluatorFactory.

type predicate interface {
	apply(ctx *predicateContext) bool
	String() string
}

type predicateContext struct {
	item     any
	root     any
	docCache map[*compiledPath]any
}

// evaluatePath is PredicateContextImpl.evaluate: root paths are cached per
// evaluation.
func (p *predicateContext) evaluatePath(path *compiledPath) any {
	if path.isRootPath {
		if v, ok := p.docCache[path]; ok {
			return v
		}
		v := path.evaluate(p.root, p.root, false, evalOptions{}).getValue()
		p.docCache[path] = v
		return v
	}
	return path.evaluate(p.item, p.root, false, evalOptions{}).getValue()
}

type logicalExpr struct {
	op    string // "&&", "||" or "!"
	chain []predicate
}

func (l *logicalExpr) apply(ctx *predicateContext) bool {
	switch l.op {
	case "||":
		for _, e := range l.chain {
			if e.apply(ctx) {
				return true
			}
		}
		return false
	case "&&":
		for _, e := range l.chain {
			if !e.apply(ctx) {
				return false
			}
		}
		return true
	}
	return !l.chain[0].apply(ctx)
}

func (l *logicalExpr) String() string {
	parts := make([]string, len(l.chain))
	for i, e := range l.chain {
		parts[i] = e.String()
	}
	if l.op == "!" {
		// LogicalExpressionNode(op, NOT, null) joins [op, null].
		parts = append(parts, "null")
	}
	return "(" + strings.Join(parts, " "+l.op+" ") + ")"
}

type relationalExpr struct {
	left  valueNode
	op    string
	right valueNode
}

func (r *relationalExpr) String() string {
	if r.op == "EXISTS" {
		return r.left.String()
	}
	return r.left.String() + " " + r.op + " " + r.right.String()
}

func (r *relationalExpr) apply(ctx *predicateContext) bool {
	l, rt := r.left, r.right
	if p, ok := l.(*pathNode); ok {
		l = p.evaluate(ctx)
	}
	if p, ok := rt.(*pathNode); ok {
		rt = p.evaluate(ctx)
	}
	return evaluateOperator(r.op, l, rt)
}

// ---- value nodes ----

type valueNode interface {
	String() string
	// equals is the node's Java equals(Object).
	equals(o valueNode) bool
}

type (
	nullNode      struct{}
	undefinedNode struct{}
	booleanNode   struct{ value bool }
	stringNode    struct {
		str         string
		singleQuote bool
	}
	numberNode struct {
		number javafmt.BigDecimal
		nan    bool
	}
	jsonNode struct {
		json   any // the literal text while unparsed
		parsed bool
	}
	patternNode struct {
		pattern string
		flags   string
		re      *javare.Regexp
	}
	valueListNode struct{ nodes []valueNode }
	pathNode      struct {
		path        *compiledPath
		existsCheck bool
		shouldExist bool
	}
)

var (
	trueNode  = &booleanNode{value: true}
	falseNode = &booleanNode{value: false}
	nanNode   = &numberNode{nan: true}
)

func (nullNode) String() string             { return "null" }
func (nullNode) equals(o valueNode) bool    { _, ok := o.(nullNode); return ok }
func (undefinedNode) String() string        { return "undefined" }
func (undefinedNode) equals(valueNode) bool { return false }
func (b *booleanNode) String() string       { return strconvBool(b.value) }
func (b *booleanNode) equals(o valueNode) bool {
	ob, ok := o.(*booleanNode)
	return ok && ob.value == b.value
}

func strconvBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// newStringNode is ValueNodes.StringNode(CharSequence, boolean escape).
func newStringNode(s string, escape bool) *stringNode {
	n := &stringNode{singleQuote: true}
	j := toJ(s)
	if escape && len(j) > 1 {
		open, closeCh := j[0], j[len(j)-1]
		switch {
		case open == '\'' && closeCh == '\'':
			s = j[1 : len(j)-1].String()
		case open == '"' && closeCh == '"':
			s = j[1 : len(j)-1].String()
			n.singleQuote = false
		}
		n.str = unescape(s)
		return n
	}
	n.str = s
	return n
}

func (s *stringNode) String() string {
	q := "'"
	if !s.singleQuote {
		q = "\""
	}
	return q + escapeJayway(s.str, true) + q
}

func (s *stringNode) equals(o valueNode) bool {
	var that *stringNode
	switch x := o.(type) {
	case *stringNode:
		that = x
	case *numberNode:
		that = x.asStringNode()
	default:
		return false
	}
	return s.str == that.str
}

func (s *stringNode) asNumberNode() *numberNode {
	d, err := javafmt.ParseBigDecimal(s.str)
	if err != nil {
		return nanNode
	}
	return &numberNode{number: d}
}

func (n *numberNode) String() string {
	if n.nan {
		// NumberNode.NAN holds a null BigDecimal; toString would throw.
		throwNoMessage(NullPointer)
	}
	return n.number.String()
}

func (n *numberNode) asStringNode() *stringNode {
	return &stringNode{str: n.String(), singleQuote: true}
}

func (n *numberNode) equals(o valueNode) bool {
	var that *numberNode
	switch x := o.(type) {
	case *numberNode:
		that = x
	case *stringNode:
		that = x.asNumberNode()
	default:
		return false
	}
	if that.nan {
		return false
	}
	if n.nan {
		throwNoMessage(NullPointer)
	}
	return n.number.Cmp(that.number) == 0
}

func (j *jsonNode) String() string { return javafmt.ValueOf(j.json) }

func (j *jsonNode) equals(o valueNode) bool {
	oj, ok := o.(*jsonNode)
	if !ok {
		return false
	}
	return jsonNodeRawEquals(j.json, oj.json)
}

// jsonNodeRawEquals compares the raw json fields as Java's Object.equals
// would: text against text, parsed values structurally.
func jsonNodeRawEquals(a, b any) bool {
	as, aText := a.(string)
	bs, bText := b.(string)
	if aText || bText {
		return aText && bText && as == bs
	}
	return JavaEquals(a, b)
}

// parse is JsonNode.parse: the literal is parsed with json-smart on demand.
func (j *jsonNode) parse() any {
	if j.parsed {
		return j.json
	}
	v, perr := parseSmart(j.json.(string))
	if perr != nil {
		throw(IllegalArgument, "net.minidev.json.parser.ParseException: %s", perr.message())
	}
	return v
}

func (j *jsonNode) isArray() bool { return isList(j.parse()) }

func (j *jsonNode) length() int {
	if j.isArray() {
		return len(listItems(j.parse()))
	}
	return -1
}

func (j *jsonNode) isEmpty() bool {
	v := j.parse()
	switch {
	case isList(v):
		return len(listItems(v)) == 0
	case isMap(v):
		// Java casts the map to Collection here and fails.
		throw(ClassCast, "class java.util.LinkedHashMap cannot be cast to class java.util.Collection "+
			"(java.util.LinkedHashMap and java.util.Collection are in module java.base of loader 'bootstrap')")
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	return true
}

// asValueListNode is JsonNode.asValueListNode(ctx): undefined unless the
// JSON is an array.
func (j *jsonNode) asValueListNode() valueNode {
	if !j.isArray() {
		return undefinedNode{}
	}
	items := listItems(j.parse())
	nodes := make([]valueNode, len(items))
	for i, it := range items {
		nodes[i] = toValueNode(it)
	}
	return &valueListNode{nodes: nodes}
}

// equalsParsed is JsonNode.equals(JsonNode, ctx): this node's raw json
// against the other node's parsed json.
func (j *jsonNode) equalsParsed(o *jsonNode) bool {
	if j == o {
		return true
	}
	return jsonNodeRawEquals(j.json, o.parse())
}

func newPatternNode(literal string) *patternNode {
	begin := strings.IndexByte(literal, '/')
	end := strings.LastIndexByte(literal, '/')
	pattern := literal[begin+1 : end]
	flags := literal[end+1:]
	code := 0
	for _, c := range toJ(flags) {
		code |= patternFlagCode(c)
	}
	re, err := javare.CompileFlags(pattern, javare.Flag(code))
	if err != nil {
		throw(PatternSyntax, "%s", err.Error())
	}
	return &patternNode{pattern: pattern, flags: flags, re: re}
}

func (p *patternNode) String() string {
	if !strings.HasPrefix(p.pattern, "/") {
		return "/" + p.pattern + "/" + p.flags
	}
	return p.pattern
}

func (p *patternNode) equals(o valueNode) bool { return p == o }

func (p *patternNode) matches(input string) bool {
	ok, err := p.re.FullMatch(input)
	if err != nil {
		throw(Timeout, "%s", err.Error())
	}
	return ok
}

func (v *valueListNode) String() string {
	parts := make([]string, len(v.nodes))
	for i, n := range v.nodes {
		parts[i] = n.String()
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (v *valueListNode) equals(o valueNode) bool {
	ov, ok := o.(*valueListNode)
	if !ok || len(ov.nodes) != len(v.nodes) {
		return false
	}
	for i := range v.nodes {
		if !v.nodes[i].equals(ov.nodes[i]) {
			return false
		}
	}
	return true
}

// contains is List.contains: node.equals(element) for some element.
func (v *valueListNode) contains(node valueNode) bool {
	for _, e := range v.nodes {
		if node.equals(e) {
			return true
		}
	}
	return false
}

func newPathNode(path string, existsCheck, shouldExist bool) *pathNode {
	return &pathNode{path: compilePath(path), existsCheck: existsCheck, shouldExist: shouldExist}
}

func (p *pathNode) String() string {
	if p.existsCheck && !p.shouldExist {
		return "!" + p.path.String()
	}
	return p.path.String()
}

func (p *pathNode) equals(o valueNode) bool { return p == o }

// evaluate is PathNode.evaluate: the path's value as a value node, undefined
// when the path does not exist.
func (p *pathNode) evaluate(ctx *predicateContext) valueNode {
	if p.existsCheck {
		var result valueNode = trueNode
		err := catchKind(PathNotFound, func() {
			p.path.evaluate(ctx.item, ctx.root, false, evalOptions{requireProperties: true}).getValue()
		})
		if err != nil {
			result = falseNode
		}
		return result
	}
	var res any
	if err := catchKind(PathNotFound, func() { res = ctx.evaluatePath(p.path) }); err != nil {
		return undefinedNode{}
	}
	switch x := res.(type) {
	case Number:
		d, err := javafmt.ParseBigDecimal(x.String())
		if err != nil {
			throw(NumberFormat, "%s", err.Error())
		}
		return &numberNode{number: d}
	case string:
		return newStringNode(x, false)
	case bool:
		if x {
			return trueNode
		}
		return falseNode
	case nil:
		return nullNode{}
	case *Array, []any, *Object:
		return &jsonNode{json: x, parsed: true}
	}
	throw(JsonPathError, "Could not convert class %s:%s to a ValueNode", JavaClassName(res), javafmt.ValueOf(res))
	return nil
}

func asPathNode(v valueNode) *pathNode {
	if p, ok := v.(*pathNode); ok {
		return p
	}
	throw(InvalidPath, "Expected path node")
	return nil
}

// toValueNode is ValueNode.toValueNode for elements of a JSON array literal.
func toValueNode(o any) valueNode {
	switch x := o.(type) {
	case nil:
		return nullNode{}
	case string:
		if isPathString(x) {
			return &pathNode{path: compilePath(x)}
		}
		if isJSONString(x) {
			return &jsonNode{json: x}
		}
		return newStringNode(x, true)
	case Number:
		d, err := javafmt.ParseBigDecimal(x.String())
		if err != nil {
			throw(NumberFormat, "%s", err.Error())
		}
		return &numberNode{number: d}
	case bool:
		if x {
			return trueNode
		}
		return falseNode
	}
	throw(JsonPathError, "Could not determine value type")
	return nil
}

func isPathString(s string) bool {
	str := javaTrim(s)
	if str == "" || str[0] != '@' && str[0] != '$' {
		return false
	}
	return catch(func() { compilePath(str) }) == nil
}

func isJSONString(s string) bool {
	str := javaTrim(s)
	if len(toJ(str)) <= 1 {
		return false
	}
	c0, c1 := str[0], str[len(str)-1]
	if (c0 != '[' || c1 != ']') && (c0 != '{' || c1 != '}') {
		return false
	}
	_, perr := parseSmart(str)
	return perr == nil
}

// ---- evaluators ----

func isNumberNode(v valueNode) bool { _, ok := v.(*numberNode); return ok }
func isStringNode(v valueNode) bool { _, ok := v.(*stringNode); return ok }

func asStringNode(v valueNode) *stringNode {
	switch x := v.(type) {
	case *stringNode:
		return x
	case *numberNode:
		return x.asStringNode()
	}
	throw(InvalidPath, "Expected string node")
	return nil
}

func asNumberNode(v valueNode) *numberNode {
	switch x := v.(type) {
	case *numberNode:
		return x
	case *stringNode:
		return x.asNumberNode()
	}
	throw(InvalidPath, "Expected number node")
	return nil
}

func asBooleanNode(v valueNode) *booleanNode {
	if b, ok := v.(*booleanNode); ok {
		return b
	}
	throw(InvalidPath, "Expected boolean node")
	return nil
}

func asValueListNode(v valueNode) *valueListNode {
	if l, ok := v.(*valueListNode); ok {
		return l
	}
	throw(InvalidPath, "Expected value list node")
	return nil
}

// valueList resolves an operand of IN, NIN, SUBSETOF, ANYOF and NONEOF: a
// JSON array operand becomes its elements; nil means "not an array".
func valueList(v valueNode) *valueListNode {
	if j, ok := v.(*jsonNode); ok {
		vl := j.asValueListNode()
		if _, undefined := vl.(undefinedNode); undefined {
			return nil
		}
		return vl.(*valueListNode)
	}
	return asValueListNode(v)
}

func evaluateOperator(op string, left, right valueNode) bool {
	switch op {
	case "EXISTS":
		lb, lok := left.(*booleanNode)
		rb, rok := right.(*booleanNode)
		if !lok && !rok {
			throw(JsonPathError, "Failed to evaluate exists expression")
		}
		if !lok {
			lb = asBooleanNode(left)
		}
		if !rok {
			rb = asBooleanNode(right)
		}
		return lb.value == rb.value
	case "==":
		return evalEquals(left, right)
	case "!=":
		return !evalEquals(left, right)
	case "===":
		return sameNodeClass(left, right) && evalEquals(left, right)
	case "!==":
		return !sameNodeClass(left, right) || !evalEquals(left, right)
	case "<", "<=", ">", ">=":
		return evalCompare(op, left, right)
	case "=~":
		return evalRegex(left, right)
	case "SIZE":
		rn, ok := right.(*numberNode)
		if !ok {
			return false
		}
		expected := int(rn.number.IntValue())
		switch l := left.(type) {
		case *stringNode:
			return utf16Len(l.str) == expected
		case *jsonNode:
			return l.length() == expected
		}
		return false
	case "EMPTY":
		switch l := left.(type) {
		case *stringNode:
			return (l.str == "") == asBooleanNode(right).value
		case *jsonNode:
			return l.isEmpty() == asBooleanNode(right).value
		}
		return false
	case "IN":
		return evalIn(left, right)
	case "NIN":
		return !evalIn(left, right)
	case "ALL":
		required := asValueListNode(right)
		lj, ok := left.(*jsonNode)
		if !ok {
			return false
		}
		if all, ok := lj.asValueListNode().(*valueListNode); ok {
			for _, r := range required.nodes {
				if !all.contains(r) {
					return false
				}
			}
		}
		return true
	case "CONTAINS":
		if ls, ok := left.(*stringNode); ok {
			if rs, ok := right.(*stringNode); ok {
				return strings.Contains(ls.str, rs.str)
			}
		}
		if lj, ok := left.(*jsonNode); ok {
			vl, ok := lj.asValueListNode().(*valueListNode)
			return ok && vl.contains(right)
		}
		return false
	case "MATCHES":
		throw(InvalidPath, "Expected predicate node")
	case "TYPE":
		throw(InvalidPath, "Expected class node")
	case "SUBSETOF", "ANYOF", "NONEOF":
		rl := valueList(right)
		if rl == nil {
			return false
		}
		ll := valueList(left)
		if ll == nil {
			return false
		}
		switch op {
		case "SUBSETOF":
			for _, l := range ll.nodes {
				if !rl.contains(l) {
					return false
				}
			}
			return true
		case "ANYOF":
			for _, l := range ll.nodes {
				for _, r := range rl.nodes {
					if l.equals(r) {
						return true
					}
				}
			}
			return false
		default:
			for _, l := range ll.nodes {
				for _, r := range rl.nodes {
					if l.equals(r) {
						return false
					}
				}
			}
			return true
		}
	}
	return false
}

func sameNodeClass(a, b valueNode) bool {
	switch a.(type) {
	case nullNode:
		_, ok := b.(nullNode)
		return ok
	case undefinedNode:
		_, ok := b.(undefinedNode)
		return ok
	case *booleanNode:
		_, ok := b.(*booleanNode)
		return ok
	case *stringNode:
		_, ok := b.(*stringNode)
		return ok
	case *numberNode:
		_, ok := b.(*numberNode)
		return ok
	case *jsonNode:
		_, ok := b.(*jsonNode)
		return ok
	case *patternNode:
		_, ok := b.(*patternNode)
		return ok
	case *valueListNode:
		_, ok := b.(*valueListNode)
		return ok
	case *pathNode:
		_, ok := b.(*pathNode)
		return ok
	}
	return false
}

func evalEquals(left, right valueNode) bool {
	lj, lok := left.(*jsonNode)
	rj, rok := right.(*jsonNode)
	if lok && rok {
		return lj.equalsParsed(rj)
	}
	return left.equals(right)
}

func evalCompare(op string, left, right valueNode) bool {
	var c int
	switch {
	case isNumberNode(left) && isNumberNode(right):
		c = asNumberNode(left).number.Cmp(asNumberNode(right).number)
	case isStringNode(left) && isStringNode(right):
		c = compareJava(asStringNode(left).str, asStringNode(right).str)
	default:
		return false
	}
	switch op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	}
	return c >= 0
}

func evalIn(left, right valueNode) bool {
	list := valueList(right)
	if list == nil {
		return false
	}
	return list.contains(left)
}

func evalRegex(left, right valueNode) bool {
	lp, lIsPattern := left.(*patternNode)
	rp, rIsPattern := right.(*patternNode)
	if lIsPattern == rIsPattern {
		return false
	}
	pattern, other := lp, right
	if rIsPattern {
		pattern, other = rp, left
	}
	_, isList := other.(*valueListNode)
	oj, isJSON := other.(*jsonNode)
	if !isList && (!isJSON || !oj.isArray()) {
		return pattern.matches(regexInput(other))
	}
	// matchesAny: asJsonNode() on a value list throws.
	if !isJSON {
		throw(InvalidPath, "Expected json node")
	}
	vl, ok := oj.asValueListNode().(*valueListNode)
	if !ok {
		return false
	}
	for _, n := range vl.nodes {
		if pattern.matches(regexInput(n)) {
			return true
		}
	}
	return false
}

func regexInput(v valueNode) string {
	switch x := v.(type) {
	case *stringNode:
		return x.str
	case *numberNode:
		return x.asStringNode().str
	case *booleanNode:
		return x.String()
	}
	return ""
}
