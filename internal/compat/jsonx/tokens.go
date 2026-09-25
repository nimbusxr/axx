package jsonx

import (
	"sort"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// This file ports com.jayway.jsonpath.internal.path: the token chain a
// compiled path is made of, and its evaluation. Tokens keep Jayway's mutable
// per-token state (cached definiteness, the upstream array index used by
// deep scans), so a compiled path must not be shared between goroutines.

const providerClass = "com.jayway.jsonpath.spi.json.JsonSmartJsonProvider"

type pathToken interface {
	tok() *tokenBase
	evaluate(currentPath string, parent pathRef, model any, ctx *evalContext)
	isTokenDefinite() bool
	pathFragment() string
}

type tokenBase struct {
	prev, next         pathToken
	definite           *bool
	upstreamDefinite   *bool
	upstreamArrayIndex int
}

func newBase() tokenBase { return tokenBase{upstreamArrayIndex: -1} }

func (b *tokenBase) tok() *tokenBase { return b }

func isLeaf(t pathToken) bool { return t.tok().next == nil }

func isRootToken(t pathToken) bool { return t.tok().prev == nil }

func nextToken(t pathToken) pathToken {
	if isLeaf(t) {
		throw(IllegalState, "Current path token is a leaf")
	}
	return t.tok().next
}

func appendTailToken(t, next pathToken) pathToken {
	t.tok().next = next
	next.tok().prev = t
	return next
}

func isUpstreamDefinite(t pathToken) bool {
	b := t.tok()
	if b.upstreamDefinite == nil {
		v := isRootToken(t) || b.prev.isTokenDefinite() && isUpstreamDefinite(b.prev)
		b.upstreamDefinite = &v
	}
	return *b.upstreamDefinite
}

func isPathDefinite(t pathToken) bool {
	b := t.tok()
	if b.definite != nil {
		return *b.definite
	}
	v := t.isTokenDefinite()
	if v && !isLeaf(t) {
		v = isPathDefinite(b.next)
	}
	b.definite = &v
	return v
}

func tokenString(t pathToken) string {
	if isLeaf(t) {
		return t.pathFragment()
	}
	return t.pathFragment() + tokenString(nextToken(t))
}

func handleObjectProperty(t pathToken, currentPath string, model any, ctx *evalContext, properties []string) {
	obj := model.(*Object)
	if len(properties) == 1 {
		property := properties[0]
		evalPath := currentPath + "['" + property + "']"
		propertyVal, found := obj.Get(property)
		if !found {
			if !isLeaf(t) {
				if isUpstreamDefinite(t) && t.isTokenDefinite() || ctx.requireProperties {
					throw(PathNotFound, "Missing property in path %s", evalPath)
				}
				return
			}
			if ctx.requireProperties {
				throw(PathNotFound, "No results for path: %s", evalPath)
			}
			return
		}
		ref := pathRef(noOpRef{})
		if ctx.forUpdate {
			ref = &objectPropertyRef{parent: obj, property: property}
		}
		if isLeaf(t) {
			idx := "[" + strconv.Itoa(t.tok().upstreamArrayIndex) + "]"
			if idx == "[-1]" || ctx.path.root.tail.tok().prev.pathFragment() == idx {
				ctx.addResult(evalPath, ref, propertyVal)
			}
		} else {
			nextToken(t).evaluate(evalPath, ref, propertyVal, ctx)
		}
		return
	}
	quoted := make([]string, len(properties))
	for i, p := range properties {
		quoted[i] = "'" + p + "'"
	}
	evalPath := currentPath + "[" + strings.Join(quoted, ", ") + "]"
	merged := NewObject()
	for _, property := range properties {
		v, ok := obj.Get(property)
		if !ok {
			if ctx.requireProperties {
				throw(PathNotFound, "Missing property in path %s", evalPath)
			}
			continue
		}
		merged.Set(property, v)
	}
	ref := pathRef(noOpRef{})
	if ctx.forUpdate {
		ref = &objectMultiPropertyRef{parent: obj, properties: properties}
	}
	ctx.addResult(evalPath, ref, merged)
}

func handleArrayIndex(t pathToken, index int, currentPath string, model any, ctx *evalContext) {
	evalPath := currentPath + "[" + strconv.Itoa(index) + "]"
	ref := pathRef(noOpRef{})
	if ctx.forUpdate {
		ref = &arrayIndexRef{parent: model, index: index}
	}
	effective := index
	if index < 0 {
		effective = jlength(model) + index
	}
	// Java wraps the element access and the rest of the evaluation in
	// catch (IndexOutOfBoundsException), so such errors raised further down
	// the chain are swallowed too.
	_ = catchKind(IndexOutOfBounds, func() {
		hit := listGet(model, effective)
		if isLeaf(t) {
			ctx.addResult(evalPath, ref, hit)
		} else {
			nextToken(t).evaluate(evalPath, ref, hit, ctx)
		}
	})
}

// listGet is List.get with Java's bounds check.
func listGet(list any, i int) any {
	items := listItems(list)
	if i < 0 || i >= len(items) {
		throw(IndexOutOfBounds, "Index %d out of bounds for length %d", i, len(items))
	}
	return items[i]
}

// jlength is AbstractJsonProvider.length.
func jlength(v any) int {
	switch x := v.(type) {
	case *Array, []any:
		return len(listItems(x))
	case *Object:
		return x.Len()
	case KeySet:
		return len(x)
	case string:
		return utf16Len(x)
	}
	cls := "null"
	if v != nil {
		cls = JavaClassName(v)
	}
	throw(JsonPathError, "length operation cannot be applied to %s", cls)
	return 0
}

// ---- RootPathToken ----

type rootToken struct {
	tokenBase
	tail       pathToken
	tokenCount int
	rootChar   string
}

func newRootToken(c uint16) *rootToken {
	r := &rootToken{tokenBase: newBase(), rootChar: charObj(c), tokenCount: 1}
	r.tail = r
	return r
}

func (r *rootToken) appendToken(next pathToken) {
	r.tail = appendTailToken(r.tail, next)
	r.tokenCount++
}

func (r *rootToken) evaluate(_ string, parent pathRef, model any, ctx *evalContext) {
	if isLeaf(r) {
		ref := pathRef(noOpRef{})
		if ctx.forUpdate {
			ref = parent
		}
		ctx.addResult(r.rootChar, ref, model)
		return
	}
	nextToken(r).evaluate(r.rootChar, parent, model, ctx)
}

func (r *rootToken) isTokenDefinite() bool { return true }
func (r *rootToken) pathFragment() string  { return r.rootChar }

func (r *rootToken) isFunctionPath() bool {
	_, ok := r.tail.(*functionToken)
	return ok
}

// ---- PropertyPathToken ----

type propertyToken struct {
	tokenBase
	properties []string
	delimiter  string
}

func newPropertyToken(properties []string, delimiter uint16) *propertyToken {
	if len(properties) == 0 {
		throw(InvalidPath, "Empty properties")
	}
	return &propertyToken{tokenBase: newBase(), properties: properties, delimiter: charObj(delimiter)}
}

func (p *propertyToken) singlePropertyCase() bool { return len(p.properties) == 1 }
func (p *propertyToken) multiPropertyMergeCase() bool {
	return isLeaf(p) && len(p.properties) > 1
}

func (p *propertyToken) evaluate(currentPath string, _ pathRef, model any, ctx *evalContext) {
	if !isMap(model) {
		if isUpstreamDefinite(p) {
			cls := "null"
			if model != nil {
				cls = JavaClassName(model)
			}
			throw(PathNotFound, "Expected to find an object with property %s in path %s but found '%s'. "+
				"This is not a json object according to the JsonProvider: '%s'.",
				p.pathFragment(), currentPath, cls, providerClass)
		}
		return
	}
	if p.singlePropertyCase() || p.multiPropertyMergeCase() {
		handleObjectProperty(p, currentPath, model, ctx, p.properties)
		return
	}
	for _, property := range p.properties {
		handleObjectProperty(p, currentPath, model, ctx, []string{property})
	}
}

func (p *propertyToken) isTokenDefinite() bool {
	return p.singlePropertyCase() || p.multiPropertyMergeCase()
}

func (p *propertyToken) pathFragment() string {
	quoted := make([]string, len(p.properties))
	for i, prop := range p.properties {
		quoted[i] = p.delimiter + prop + p.delimiter
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// ---- ArrayPathToken (index and slice) ----

func checkArrayModel(t pathToken, currentPath string, model any) bool {
	if model == nil {
		if isUpstreamDefinite(t) {
			throw(PathNotFound, "The path %s is null", currentPath)
		}
		return false
	}
	if !isList(model) {
		if isUpstreamDefinite(t) {
			throw(PathNotFound, "Filter: %s can only be applied to arrays. Current context is: %s",
				tokenString(t), javafmt.ValueOf(model))
		}
		return false
	}
	return true
}

type arrayIndexToken struct {
	tokenBase
	indexes []int
}

func (a *arrayIndexToken) evaluate(currentPath string, _ pathRef, model any, ctx *evalContext) {
	if !checkArrayModel(a, currentPath, model) {
		return
	}
	for _, i := range a.indexes {
		handleArrayIndex(a, i, currentPath, model, ctx)
	}
}

func (a *arrayIndexToken) isTokenDefinite() bool { return len(a.indexes) == 1 }

func (a *arrayIndexToken) pathFragment() string {
	parts := make([]string, len(a.indexes))
	for i, v := range a.indexes {
		parts[i] = strconv.Itoa(v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

type sliceKind int

const (
	sliceFrom sliceKind = iota
	sliceTo
	sliceBetween
)

type arraySliceToken struct {
	tokenBase
	from, to *int
	kind     sliceKind
}

func (a *arraySliceToken) evaluate(currentPath string, _ pathRef, model any, ctx *evalContext) {
	if !checkArrayModel(a, currentPath, model) {
		return
	}
	length := jlength(model)
	switch a.kind {
	case sliceFrom:
		from := *a.from
		if from < 0 {
			from += length
		}
		from = max(0, from)
		if length != 0 && from < length {
			for i := from; i < length; i++ {
				handleArrayIndex(a, i, currentPath, model, ctx)
			}
		}
	case sliceBetween:
		from, to := *a.from, min(length, *a.to)
		if from < to && length != 0 {
			for i := from; i < to; i++ {
				handleArrayIndex(a, i, currentPath, model, ctx)
			}
		}
	case sliceTo:
		if length != 0 {
			to := *a.to
			if to < 0 {
				to += length
			}
			to = min(length, to)
			for i := 0; i < to; i++ {
				handleArrayIndex(a, i, currentPath, model, ctx)
			}
		}
	}
}

func (a *arraySliceToken) isTokenDefinite() bool { return false }

func (a *arraySliceToken) pathFragment() string {
	var sb strings.Builder
	sb.WriteByte('[')
	if a.from != nil {
		sb.WriteString(strconv.Itoa(*a.from))
	}
	sb.WriteByte(':')
	if a.to != nil {
		sb.WriteString(strconv.Itoa(*a.to))
	}
	sb.WriteByte(']')
	return sb.String()
}

// ---- WildcardPathToken ----

type wildcardToken struct{ tokenBase }

func newWildcardToken() *wildcardToken { return &wildcardToken{tokenBase: newBase()} }

func (w *wildcardToken) evaluate(currentPath string, _ pathRef, model any, ctx *evalContext) {
	switch m := model.(type) {
	case *Object:
		for _, property := range m.Keys() {
			handleObjectProperty(w, currentPath, model, ctx, []string{property})
		}
	case *Array, []any:
		for idx := 0; idx < jlength(m); idx++ {
			err := catchKind(PathNotFound, func() { handleArrayIndex(w, idx, currentPath, model, ctx) })
			if err != nil && ctx.requireProperties {
				panic(err)
			}
		}
	}
}

func (w *wildcardToken) isTokenDefinite() bool { return false }
func (w *wildcardToken) pathFragment() string  { return "[*]" }

// ---- ScanPathToken ----

type scanToken struct{ tokenBase }

func (s *scanToken) evaluate(currentPath string, parent pathRef, model any, ctx *evalContext) {
	pt := nextToken(s)
	walk(pt, currentPath, parent, model, ctx, scanPredicate(pt, ctx))
}

func (s *scanToken) isTokenDefinite() bool { return false }
func (s *scanToken) pathFragment() string  { return ".." }

func walk(pt pathToken, currentPath string, parent pathRef, model any, ctx *evalContext, pred func(any) bool) {
	switch {
	case isMap(model):
		walkObject(pt, currentPath, parent, model, ctx, pred)
	case isList(model):
		walkArray(pt, currentPath, parent, model, ctx, pred)
	}
}

func walkArray(pt pathToken, currentPath string, parent pathRef, model any, ctx *evalContext, pred func(any) bool) {
	if pred(model) {
		if isLeaf(pt) {
			pt.evaluate(currentPath, parent, model, ctx)
		} else {
			next := nextToken(pt)
			for idx, evalModel := range append([]any(nil), listItems(model)...) {
				next.tok().upstreamArrayIndex = idx
				next.evaluate(currentPath+"["+strconv.Itoa(idx)+"]", parent, evalModel, ctx)
			}
		}
	}
	for idx, evalModel := range append([]any(nil), listItems(model)...) {
		walk(pt, currentPath+"["+strconv.Itoa(idx)+"]", &arrayIndexRef{parent: model, index: idx}, evalModel, ctx, pred)
	}
}

func walkObject(pt pathToken, currentPath string, parent pathRef, model any, ctx *evalContext, pred func(any) bool) {
	if pred(model) {
		pt.evaluate(currentPath, parent, model, ctx)
	}
	obj := model.(*Object)
	for _, property := range obj.Keys() {
		v, ok := obj.Get(property)
		if ok {
			walk(pt, currentPath+"['"+property+"']", &objectPropertyRef{parent: obj, property: property}, v, ctx, pred)
		}
	}
}

func scanPredicate(target pathToken, ctx *evalContext) func(any) bool {
	switch t := target.(type) {
	case *propertyToken:
		return func(model any) bool {
			obj, ok := model.(*Object)
			if !ok {
				return false
			}
			if !t.isTokenDefinite() {
				return true
			}
			for _, p := range t.properties {
				if !obj.Has(p) {
					return false
				}
			}
			return true
		}
	case *arrayIndexToken, *arraySliceToken:
		return isList
	case *wildcardToken:
		return func(any) bool { return true }
	case *predicateToken:
		return func(model any) bool { return t.accept(model, ctx.rootDocument, ctx) }
	}
	return func(any) bool { return false }
}

// ---- PredicatePathToken ----

type predicateToken struct {
	tokenBase
	predicates []predicate
}

func (p *predicateToken) evaluate(currentPath string, ref pathRef, model any, ctx *evalContext) {
	switch {
	case isMap(model):
		if p.accept(model, ctx.rootDocument, ctx) {
			op := pathRef(noOpRef{})
			if ctx.forUpdate {
				op = ref
			}
			if isLeaf(p) {
				ctx.addResult(currentPath, op, model)
			} else {
				nextToken(p).evaluate(currentPath, op, model, ctx)
			}
		}
	case isList(model):
		for idx, item := range append([]any(nil), listItems(model)...) {
			if p.accept(item, ctx.rootDocument, ctx) {
				handleArrayIndex(p, idx, currentPath, model, ctx)
			}
		}
	default:
		if isUpstreamDefinite(p) {
			throw(InvalidPath, "Filter: %s can not be applied to primitives. Current context is: %s",
				tokenString(p), javafmt.ValueOf(model))
		}
	}
}

func (p *predicateToken) accept(obj, root any, ctx *evalContext) bool {
	pctx := &predicateContext{item: obj, root: root, docCache: ctx.docCache}
	for _, pred := range p.predicates {
		ok := false
		if err := catchKind(InvalidPath, func() { ok = pred.apply(pctx) }); err != nil {
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}

func (p *predicateToken) isTokenDefinite() bool { return false }

func (p *predicateToken) pathFragment() string {
	marks := make([]string, len(p.predicates))
	for i := range marks {
		marks[i] = "?"
	}
	return "[" + strings.Join(marks, ",") + "]"
}

// ---- CompiledPath and the evaluation context ----

type compiledPath struct {
	root       *rootToken
	isRootPath bool
}

func newCompiledPath(root *rootToken, isRootPath bool) *compiledPath {
	return &compiledPath{root: invertScannerFunctionRelationship(root), isRootPath: isRootPath}
}

// invertScannerFunctionRelationship turns "$..x.fn()" into fn applied to the
// path "$..x", as Jayway does.
func invertScannerFunctionRelationship(path *rootToken) *rootToken {
	if !path.isFunctionPath() {
		return path
	}
	if _, ok := nextToken(path).(*scanToken); !ok {
		return path
	}
	var token pathToken = path
	var prior pathToken
	for {
		token = token.tok().next
		if token == nil {
			break
		}
		if _, ok := token.(*functionToken); ok {
			break
		}
		prior = token
	}
	fn, ok := token.(*functionToken)
	if !ok {
		return path
	}
	prior.tok().next = nil
	path.tail = prior
	param := &parameter{kind: paramPath, path: newCompiledPath(path, true)}
	fn.params = []*parameter{param}
	functionRoot := newRootToken('$')
	functionRoot.tail = fn
	functionRoot.next = fn
	return functionRoot
}

func (c *compiledPath) String() string { return tokenString(c.root) }

func (c *compiledPath) isDefinite() bool { return isPathDefinite(c.root) }

func (c *compiledPath) isFunctionPath() bool { return c.root.isFunctionPath() }

type evalOptions struct {
	requireProperties bool
}

func (c *compiledPath) evaluate(document, rootDocument any, forUpdate bool, opts evalOptions) *evalContext {
	ctx := &evalContext{
		path:              c,
		rootDocument:      rootDocument,
		forUpdate:         forUpdate,
		requireProperties: opts.requireProperties,
		docCache:          map[*compiledPath]any{},
	}
	ref := pathRef(noOpRef{})
	if forUpdate {
		ref = &rootRef{parent: rootDocument}
	}
	c.root.evaluate("", ref, document, ctx)
	return ctx
}

type evalContext struct {
	path              *compiledPath
	rootDocument      any
	forUpdate         bool
	requireProperties bool
	values            []any
	paths             []string
	updates           []pathRef
	docCache          map[*compiledPath]any
	resultArray       *Array
}

func (ctx *evalContext) addResult(path string, op pathRef, model any) {
	if ctx.forUpdate {
		ctx.updates = append(ctx.updates, op)
	}
	ctx.values = append(ctx.values, model)
	ctx.paths = append(ctx.paths, path)
}

// getValue is EvaluationContextImpl.getValue: the last result of a definite
// path (PathNotFound when there is none) or the list of all results.
func (ctx *evalContext) getValue() any {
	if ctx.path.isDefinite() {
		if len(ctx.values) == 0 {
			throw(PathNotFound, "No results for path: %s", ctx.path.String())
		}
		return ctx.values[len(ctx.values)-1]
	}
	if ctx.resultArray == nil {
		ctx.resultArray = &Array{items: ctx.values}
	}
	return ctx.resultArray
}

// updateOperations returns the update references sorted as Jayway sorts them
// (descending by accessor, so array elements are removed back to front).
func (ctx *evalContext) updateOperations() []pathRef {
	ops := append([]pathRef(nil), ctx.updates...)
	sort.SliceStable(ops, func(i, j int) bool { return compareRefs(ops[i], ops[j]) < 0 })
	return ops
}
