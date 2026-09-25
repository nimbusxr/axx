package fixtures

import (
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Dotted-path access over value trees (ValuePaths): "payments[0].id" names
// field id of the first element of list payments.

type segment struct {
	name  string
	index int // -1 when the segment has no index
}

func parsePath(path string) ([]segment, error) {
	var out []segment
	for _, raw := range strings.Split(path, ".") {
		bracket := strings.IndexByte(raw, '[')
		if bracket < 0 {
			out = append(out, segment{name: raw, index: -1})
			continue
		}
		indexText := ""
		if len(raw) > bracket+1 {
			indexText = raw[bracket+1 : len(raw)-1]
		}
		n, err := strconv.ParseInt(indexText, 10, 32)
		if err != nil {
			return nil, genError("bad index in path '%s': %s", path, raw)
		}
		out = append(out, segment{name: raw[:bracket], index: int(n)})
	}
	return out, nil
}

// pathGet reads the value at a dotted path; nil when any segment is absent.
func pathGet(tree *jsonx.Object, path string) (any, error) {
	segs, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	var cursor any = tree
	for _, s := range segs {
		cursor, err = s.step(cursor, path, false)
		if err != nil || cursor == nil {
			return nil, err
		}
	}
	return cursor, nil
}

// mustGet is pathGet for paths already known to parse.
func mustGet(tree *jsonx.Object, path string) any {
	v, _ := pathGet(tree, path)
	return v
}

func (s segment) step(cursor any, path string, create bool) (any, error) {
	m, ok := cursor.(*jsonx.Object)
	if !ok {
		if create {
			return nil, genError("path '%s' traverses a non-map value", path)
		}
		return nil, nil
	}
	next := get(m, s.name)
	if next == nil && create && s.index < 0 {
		next = jsonx.NewObject()
		m.Set(s.name, next)
	}
	if s.index >= 0 {
		list, ok := next.([]any)
		if !ok {
			if create {
				return nil, genError("path '%s' indexes '%s' which is not a declared list", path, s.name)
			}
			return nil, nil
		}
		if s.index >= len(list) {
			if create {
				return nil, requireIndex(list, s, path)
			}
			return nil, nil
		}
		return list[s.index], nil
	}
	return next, nil
}

func requireIndex(list []any, s segment, path string) error {
	return genError("identity path '%s' indexes [%d] but '%s' has only %d element(s); declare the element in the fixture",
		path, s.index, s.name, len(list))
}

// pathSet sets the value at a dotted path, creating intermediate maps; list
// indices must already exist.
func pathSet(tree *jsonx.Object, path string, value any) error {
	segs, err := parsePath(path)
	if err != nil {
		return err
	}
	var cursor any = tree
	for _, s := range segs[:len(segs)-1] {
		if cursor, err = s.step(cursor, path, true); err != nil {
			return err
		}
	}
	last := segs[len(segs)-1]
	m, ok := cursor.(*jsonx.Object)
	if last.index >= 0 {
		var list []any
		if ok {
			list, ok = get(m, last.name).([]any)
		}
		if !ok {
			return genError("identity path '%s' indexes '%s' which is not a declared list", path, last.name)
		}
		if last.index >= len(list) {
			return requireIndex(list, last, path)
		}
		list[last.index] = value
		return nil
	}
	if !ok {
		return genError("identity path '%s' traverses a non-map value", path)
	}
	m.Set(last.name, value)
	return nil
}
