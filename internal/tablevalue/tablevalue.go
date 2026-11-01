// Package tablevalue builds a JSON object from a step's table of paths and
// values, for the steps that send one they make from scratch: an
// operation's variables, a tool's arguments.
package tablevalue

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// Row is a row of the table: a path (`reference`, `address.postcode`,
// `lines[0].reference`) and its value.
type Row struct {
	Path, Value string
	// Null is an empty cell, which is an error asking for the value.
	Null bool
}

// Build is the object the rows make, with the objects and arrays on their
// paths. A value means what it says (core.ParseValue, ADR 0014): `42` is a
// number, `"42"` the string, `null` null, and `undefined` leaves the path
// out; nothing is inferred from a schema.
func Build(rows []Row) (map[string]any, error) {
	return Apply(map[string]any{}, rows)
}

// Apply sets the rows into an object, as Build does: the object a file
// gave, which the rows change.
func Apply(base map[string]any, rows []Row) (map[string]any, error) {
	var root any = base
	for _, r := range rows {
		steps, err := parse(r.Path)
		if err != nil {
			return nil, err
		}
		v, err := core.ParseValue(r.Value, r.Null, true)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Path, err)
		}
		var val any
		if !v.Undefined() && !v.IsNull() {
			if val, err = plain(v.JSON()); err != nil {
				return nil, err
			}
		}
		if root, err = put(root, steps, val, v.Undefined(), r.Path); err != nil {
			return nil, err
		}
	}
	return root.(map[string]any), nil
}

// plain is a value's JSON as a plain Go value, its numbers json.Number.
func plain(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var out any
	err := dec.Decode(&out)
	return out, err
}

// step is a step of a path: an object's key, or an array's index.
type step struct {
	key   string
	index int // -1 for a key
}

func parse(path string) ([]step, error) {
	p := strings.TrimPrefix(strings.TrimPrefix(path, "$"), ".")
	if p == "" {
		return nil, fmt.Errorf("the path %q names nothing", path)
	}
	var out []step
	for _, seg := range strings.Split(p, ".") {
		name, rest, hasIndex := strings.Cut(seg, "[")
		if name == "" && !hasIndex {
			return nil, fmt.Errorf("the path %q has an empty step", path)
		}
		if name != "" {
			out = append(out, step{key: name, index: -1})
		}
		for hasIndex {
			var n string
			n, rest, _ = strings.Cut(rest, "]")
			i, err := strconv.Atoi(n)
			if err != nil || i < 0 {
				return nil, fmt.Errorf("the path %q has an index that is not a number: [%s]", path, n)
			}
			out = append(out, step{index: i})
			_, rest, hasIndex = strings.Cut(rest, "[")
		}
	}
	return out, nil
}

// put sets, or deletes, the value at the steps of node, making the objects
// and arrays they go through, and returns the node.
func put(node any, steps []step, v any, del bool, path string) (any, error) {
	if len(steps) == 0 {
		return v, nil
	}
	s := steps[0]
	if s.index >= 0 {
		arr, ok := node.([]any)
		if node != nil && !ok {
			return nil, fmt.Errorf("the path %q indexes a value that is not an array", path)
		}
		if del && len(steps) == 1 {
			if s.index < len(arr) {
				arr = append(arr[:s.index], arr[s.index+1:]...)
			}
			return arr, nil
		}
		for len(arr) <= s.index {
			arr = append(arr, nil)
		}
		child, err := put(arr[s.index], steps[1:], v, del, path)
		if err != nil {
			return nil, err
		}
		arr[s.index] = child
		return arr, nil
	}
	obj, ok := node.(map[string]any)
	if node != nil && !ok {
		return nil, fmt.Errorf("the path %q goes into a value that is not an object", path)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	if del && len(steps) == 1 {
		delete(obj, s.key)
		return obj, nil
	}
	child, err := put(obj[s.key], steps[1:], v, del, path)
	if err != nil {
		return nil, err
	}
	obj[s.key] = child
	return obj, nil
}
