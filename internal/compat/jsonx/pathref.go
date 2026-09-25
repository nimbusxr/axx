package jsonx

import (
	"strconv"
	"strings"
)

// This file ports com.jayway.jsonpath.internal.PathRef: the handles a path
// evaluation "for update" collects so set/delete/put can modify the matched
// locations in place.

type pathRef interface {
	set(v any)
	delete()
	put(key string, v any)
	// accessor is getAccessor().toString(); ok is false for NO_OP, whose
	// accessor is null.
	accessor() (string, bool)
}

type noOpRef struct{}

func (noOpRef) set(any)                  {}
func (noOpRef) delete()                  {}
func (noOpRef) put(string, any)          {}
func (noOpRef) accessor() (string, bool) { return "", false }

type rootRef struct{ parent any }

func (r *rootRef) set(any) { throw(InvalidModification, "Invalid set operation") }
func (r *rootRef) delete() { throw(InvalidModification, "Invalid delete operation") }
func (r *rootRef) put(key string, v any) {
	obj, ok := r.parent.(*Object)
	if !ok {
		throw(InvalidModification, "Invalid put operation. $ is not a map")
	}
	obj.Set(key, v)
}
func (r *rootRef) accessor() (string, bool) { return "$", true }

type arrayIndexRef struct {
	parent any
	index  int
}

func (r *arrayIndexRef) set(v any) { setArrayIndex(r.parent, r.index, v) }

func (r *arrayIndexRef) delete() {
	switch l := r.parent.(type) {
	case *Array:
		if r.index < 0 || r.index >= len(l.items) {
			throw(IndexOutOfBounds, "Index %d out of bounds for length %d", r.index, len(l.items))
		}
		l.Remove(r.index)
	default:
		throwNoMessage(UnsupportedOperation)
	}
}

func (r *arrayIndexRef) put(key string, v any) {
	target := listGet(r.parent, r.index)
	if target == nil {
		return
	}
	obj, ok := target.(*Object)
	if !ok {
		throw(InvalidModification, "Can only add properties to a map")
	}
	obj.Set(key, v)
}

func (r *arrayIndexRef) accessor() (string, bool) { return strconv.Itoa(r.index), true }

// setArrayIndex is AbstractJsonProvider.setArrayIndex: set an element, or
// append when the index equals the size.
func setArrayIndex(list any, index int, v any) {
	switch l := list.(type) {
	case *Array:
		if index == len(l.items) {
			l.items = append(l.items, v)
			return
		}
		if index < 0 || index > len(l.items) {
			throw(IndexOutOfBounds, "Index %d out of bounds for length %d", index, len(l.items))
		}
		l.items[index] = v
	case []any:
		if index < 0 || index >= len(l) {
			throw(IndexOutOfBounds, "Index %d out of bounds for length %d", index, len(l))
		}
		l[index] = v
	default:
		throwNoMessage(UnsupportedOperation)
	}
}

type objectPropertyRef struct {
	parent   *Object
	property string
}

func (r *objectPropertyRef) set(v any) { r.parent.Set(r.property, v) }
func (r *objectPropertyRef) delete()   { r.parent.Delete(r.property) }
func (r *objectPropertyRef) put(key string, v any) {
	target, found := r.parent.Get(r.property)
	if !found || target == nil {
		return
	}
	obj, ok := target.(*Object)
	if !ok {
		throw(InvalidModification, "Can only add properties to a map")
	}
	obj.Set(key, v)
}
func (r *objectPropertyRef) accessor() (string, bool) { return r.property, true }

type objectMultiPropertyRef struct {
	parent     *Object
	properties []string
}

func (r *objectMultiPropertyRef) set(v any) {
	for _, p := range r.properties {
		r.parent.Set(p, v)
	}
}

func (r *objectMultiPropertyRef) delete() {
	for _, p := range r.properties {
		r.parent.Delete(p)
	}
}

func (r *objectMultiPropertyRef) put(string, any) {
	throw(InvalidModification, "Put can not be performed to multiple properties")
}

func (r *objectMultiPropertyRef) accessor() (string, bool) {
	return strings.Join(r.properties, "&&"), true
}

// compareRefs is PathRef.compareTo: descending by accessor text, and
// descending by index between two array element references.
func compareRefs(a, b pathRef) int {
	if ai, ok := a.(*arrayIndexRef); ok {
		if bi, ok := b.(*arrayIndexRef); ok {
			switch {
			case bi.index < ai.index:
				return -1
			case bi.index > ai.index:
				return 1
			}
			return 0
		}
	}
	as, aok := a.accessor()
	bs, bok := b.accessor()
	if !aok || !bok {
		throwNoMessage(NullPointer)
	}
	return -compareJava(as, bs)
}
