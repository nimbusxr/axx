package all

import (
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/packset"
)

// The catalog axx builds from and the packs themselves agree: the same
// names, and the same packs to load with each.
func TestCatalog(t *testing.T) {
	packs := Packs()
	if len(packs) != len(packset.Catalog) {
		t.Errorf("%d packs, %d in the catalog", len(packs), len(packset.Catalog))
	}
	for _, c := range packset.Catalog {
		p, ok := packs[c.Name]
		if !ok {
			t.Errorf("%s is in the catalog but not in all.Packs", c.Name)
			continue
		}
		m := p.Manifest()
		if m.Name != c.Name {
			t.Errorf("%s: the pack calls itself %s", c.Name, m.Name)
		}
		if !slices.Equal(m.Requires, c.Requires) {
			t.Errorf("%s requires %v, the catalog says %v", c.Name, m.Requires, c.Requires)
		}
	}
}
