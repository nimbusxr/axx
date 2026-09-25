package jsonx

import (
	"math"
	"math/big"
	"strings"
)

// This file ports Jayway's path functions (length, size, sum, min, max, avg,
// stddev, concat, append, keys, first, last, index) and FunctionPathToken.

type paramKind int

const (
	paramJSON paramKind = iota + 1
	paramPath
)

type parameter struct {
	kind paramKind
	json string
	path *compiledPath
	// late binding: the evaluated value of a path parameter.
	value any
}

// get is Parameter.getValue: a path parameter's evaluated value, or a JSON
// parameter parsed anew on every call.
func (p *parameter) get() any {
	if p.kind == paramJSON {
		v, err := parseJSONPathText(p.json)
		if err != nil {
			panic(err)
		}
		return v
	}
	return p.value
}

type functionToken struct {
	tokenBase
	name     string
	fragment string
	params   []*parameter
}

func newFunctionToken(name string, params []*parameter) *functionToken {
	suffix := "()"
	if len(params) > 0 {
		suffix = "(...)"
	}
	return &functionToken{tokenBase: newBase(), name: name, fragment: name + suffix, params: params}
}

var knownFunctions = map[string]bool{
	"avg": true, "stddev": true, "sum": true, "min": true, "max": true, "concat": true, "length": true,
	"size": true, "append": true, "keys": true, "first": true, "last": true, "index": true,
}

func (f *functionToken) evaluate(currentPath string, parent pathRef, model any, ctx *evalContext) {
	if !knownFunctions[f.name] {
		throw(InvalidPath, "Function with name: %s does not exist.", f.name)
	}
	for _, p := range f.params {
		if p.kind == paramPath {
			p.value = p.path.evaluate(ctx.rootDocument, ctx.rootDocument, false, evalOptions{}).getValue()
		}
	}
	result := invokeFunction(f.name, model, f.params)
	ctx.addResult(currentPath+"."+f.name, parent, result)
	f.cleanWildcardPathToken()
	if !isLeaf(f) {
		nextToken(f).evaluate(currentPath, parent, result, ctx)
	}
}

// cleanWildcardPathToken undoes the wildcard length() appends to its path
// parameter.
func (f *functionToken) cleanWildcardPathToken() {
	if len(f.params) == 0 || f.params[0].path == nil || f.params[0].path.isFunctionPath() {
		return
	}
	for tail := f.params[0].path.root.next; tail != nil && tail.tok().next != nil; tail = tail.tok().next {
		if _, ok := tail.tok().next.(*wildcardToken); ok {
			tail.tok().next = tail.tok().next.tok().next
			break
		}
	}
}

func (f *functionToken) isTokenDefinite() bool { return true }
func (f *functionToken) pathFragment() string  { return "." + f.fragment }

func invokeFunction(name string, model any, params []*parameter) any {
	switch name {
	case "length", "size":
		return fnLength(model, params)
	case "sum", "min", "max", "avg", "stddev":
		return fnAggregate(name, model, params)
	case "concat":
		var sb strings.Builder
		if isList(model) {
			for _, o := range listItems(model) {
				if s, ok := o.(string); ok {
					sb.WriteString(s)
				}
			}
		}
		for _, v := range paramValues(params, true) {
			sb.WriteString(v.(string))
		}
		return sb.String()
	case "append":
		for _, p := range params {
			if isList(model) {
				setArrayIndex(model, jlength(model), p.get())
			}
		}
		return model
	case "keys":
		if obj, ok := model.(*Object); ok {
			return KeySet(obj.Keys())
		}
		return nil
	case "first", "last", "index":
		return fnSequence(name, model, params)
	}
	return nil
}

func fnLength(model any, params []*parameter) any {
	if len(params) > 0 {
		p := params[0]
		if p.path != nil && !p.path.isFunctionPath() {
			tail := p.path.root.next
			for tail != nil && tail.tok().next != nil {
				tail = tail.tok().next
			}
			if tail != nil {
				tail.tok().next = newWildcardToken()
			}
		}
		if p.path == nil {
			// A JSON parameter has no path: Java fails with a NullPointerException.
			throwNoMessage(NullPointer)
		}
		inner := p.path.evaluate(model, model, false, evalOptions{}).getValue()
		if isList(inner) {
			return IntegerNumber(int32(jlength(inner)))
		}
	}
	if isList(model) || isMap(model) {
		return IntegerNumber(int32(jlength(model)))
	}
	return nil
}

// paramValues is Parameter.toList: parameter values (list elements are
// flattened) of the wanted type, numbers or strings.
func paramValues(params []*parameter, wantString bool) []any {
	var out []any
	consume := func(v any) {
		switch x := v.(type) {
		case Number:
			if wantString {
				out = append(out, x.String())
			} else {
				out = append(out, x)
			}
		case nil:
		default:
			if wantString {
				if s, ok := x.(string); ok {
					out = append(out, s)
				} else {
					out = append(out, javaToString(x))
				}
			}
		}
	}
	for _, p := range params {
		v := p.get()
		if isList(v) {
			for _, o := range listItems(v) {
				consume(o)
			}
		} else {
			consume(v)
		}
	}
	return out
}

func javaToString(v any) string {
	switch x := v.(type) {
	case bool:
		return strconvBool(x)
	case *Object:
		return x.JavaString()
	case *Array:
		return x.JavaString()
	}
	return JavaClassName(v)
}

func fnAggregate(name string, model any, params []*parameter) any {
	var values []float64
	if isList(model) {
		for _, o := range listItems(model) {
			if n, ok := o.(Number); ok {
				values = append(values, n.Float64())
			}
		}
	}
	for _, v := range paramValues(params, false) {
		values = append(values, v.(Number).Float64())
	}
	if len(values) == 0 {
		throw(JsonPathError, "Aggregation function attempted to calculate value using empty array")
	}
	switch name {
	case "sum":
		s := 0.0
		for _, v := range values {
			s += v
		}
		return DoubleNumber(s)
	case "min":
		m := math.MaxFloat64
		for _, v := range values {
			if m > v {
				m = v
			}
		}
		return DoubleNumber(m)
	case "max":
		m := math.SmallestNonzeroFloat64
		for _, v := range values {
			if m < v {
				m = v
			}
		}
		return DoubleNumber(m)
	case "avg":
		s, c := 0.0, 0.0
		for _, v := range values {
			c++
			s += v
		}
		return DoubleNumber(s / c)
	}
	sum, sumSq, count := 0.0, 0.0, 0.0
	for _, v := range values {
		sum += v
		sumSq += v * v
		count++
	}
	return DoubleNumber(math.Sqrt(sumSq/count - sum*sum/count/count))
}

func fnSequence(name string, model any, params []*parameter) any {
	if !isList(model) {
		throw(JsonPathError, "Aggregation function attempted to calculate value using empty array")
	}
	items := listItems(model)
	var target int
	switch name {
	case "first":
		target = 0
	case "last":
		target = -1
	default:
		nums := paramValues(params, false)
		if len(nums) == 0 {
			throw(IndexOutOfBounds, "Index 0 out of bounds for length 0")
		}
		target = int(numberIntValue(nums[0].(Number)))
	}
	if target >= 0 {
		return listGet(model, target)
	}
	real := len(items) + target
	if real > 0 {
		return items[real]
	}
	throw(JsonPathError, "Target index:%d larger than object count:%d", target, len(items))
	return nil
}

// numberIntValue is Number.intValue().
func numberIntValue(n Number) int32 {
	switch n.kind {
	case Integer, Long:
		return int32(n.i)
	case BigInteger:
		return int32(uint32(new(big.Int).And(n.bi, big.NewInt(0xFFFFFFFF)).Uint64()))
	case BigDecimal:
		return n.dec.IntValue()
	}
	return doubleToInt(n.f)
}

// doubleToInt is Java's (int) cast of a double.
func doubleToInt(f float64) int32 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	return int32(f)
}
