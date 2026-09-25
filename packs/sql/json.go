package sql

import (
	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// jsonProperties asserts JSON properties of a column in one row of a
// selection, with Jayway JSONPath semantics.
func jsonProperties(sc *core.Scenario, a core.Args, regex bool) error {
	svc, err := service(sc, a, 3)
	if err != nil {
		return err
	}
	sel, err := svc.selection(a.IntOr(2, 1) - 1)
	if err != nil {
		return err
	}
	v, err := sel.Value(a.Int(0)-1, a.String(1))
	if err != nil {
		return err
	}
	return jsonassert.Properties(text(v), a.Table, regex)
}
