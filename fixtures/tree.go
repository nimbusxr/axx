package fixtures

import (
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/fixtures"
)

// Tree is an instance's values as the fixture factory holds them: maps keep
// the order their keys were written in, and numbers their kind (int, long,
// double...), which is how its JSON prints them. A Tree is the caller's:
// Instance never changes one it was given, nor keeps it.
type Tree struct{ o *jsonx.Object }

// JSON is the tree as the factory writes a JSON file: keys in their order,
// two-space indentation, numbers as their kind prints them, one trailing
// newline. The same tree always gives the same bytes.
func (t *Tree) JSON() []byte { return fixtures.CanonicalJSON(t.o) }

// CompactJSON is JSON without whitespace, for a request or record body.
func (t *Tree) CompactJSON() []byte { return fixtures.CompactJSON(t.o) }

// Value is the tree as plain Go values, for a body or record built from
// them: maps are map[string]any and lists []any; an int is an int, a long an
// int64, a double a float64, a float a float32, a big integer a *big.Int
// and a big decimal a json.Number; strings, booleans, nil and []byte are as
// they are. Maps do not keep their order: JSON does.
func (t *Tree) Value() map[string]any {
	v, _ := fixtures.PlainValue(t.o).(map[string]any)
	return v
}

// Get is the plain value at a dotted path (customer.tier, items[0].sku), or
// nil when the tree has none.
func (t *Tree) Get(path string) any {
	v, err := fixtures.ValueAt(t.o, path)
	if err != nil {
		return nil
	}
	return fixtures.PlainValue(v)
}
