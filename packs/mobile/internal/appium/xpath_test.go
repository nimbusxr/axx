package appium

import (
	"testing"

	"github.com/antchfx/xpath"
)

// A text that has quotes of either kind, or both, is still the one text in a locator.
func TestXPathStringQuotesAnyText(t *testing.T) {
	for _, s := range []string{"", "PX-MOB-9401", `the "Downloads" root`, "Hanna's parcel", `Hanna's "fragile" parcel`, `"`, `'"'`} {
		expr, err := xpath.Compile("string(" + XPathString(s) + ")")
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if got := expr.Evaluate(nil); got != s {
			t.Errorf("%s is %q, not %q", XPathString(s), got, s)
		}
	}
}
