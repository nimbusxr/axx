// Package tablevalue builds a JSON object from a step's table of paths and
// values, for the steps that send one they make from scratch: an
// operation's variables, a tool's arguments.
package tablevalue

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// Row is a row of the table: a path (`reference`, `address.postcode`,
// `lines[0].reference`) and its value.
type Row struct {
	Path, Value string
	// Null is an empty cell, which is null.
	Null bool
}

// Build is the object the rows make, with the objects and arrays on their
// paths. `null` is null, `undefined` leaves the path out, and double quotes
// make a value text. Where text says a path is text, its value is text as
// written (01067 keeps its zero); other values are read as JSON values:
// numbers, booleans, objects and arrays, or else text.
func Build(rows []Row, text func(path string) bool) (map[string]any, error) {
	return Apply(map[string]any{}, rows, text)
}

// Apply sets the rows into an object, as Build does: the object a file
// gave, which the rows change.
func Apply(base map[string]any, rows []Row, text func(path string) bool) (map[string]any, error) {
	var root any = base
	for _, r := range rows {
		steps, err := parse(r.Path)
		if err != nil {
			return nil, err
		}
		var v any
		del := false
		switch {
		case r.Null || strings.EqualFold(r.Value, "null"):
		case strings.EqualFold(r.Value, "undefined"):
			del = true
		case quoted(r.Value):
			v = r.Value[1 : len(r.Value)-1]
		case text != nil && text(r.Path):
			v = r.Value
		default:
			if v, err = plain(jvalue.InferRequestValue(r.Value)); err != nil {
				return nil, err
			}
		}
		if root, err = put(root, steps, v, del, r.Path); err != nil {
			return nil, err
		}
	}
	return root.(map[string]any), nil
}

// plain is a value of the jsonx package as a plain Go value, its numbers
// json.Number.
func plain(v any) (any, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	text, err := jsonx.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var out any
	err = dec.Decode(&out)
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

func quoted(v string) bool {
	return len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"'
}
