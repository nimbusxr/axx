package appium

import "strings"

// XPathString is s as an XPath 1.0 string literal, for a locator: in double quotes, in single
// quotes when it has double ones, and as concat()'s parts when it has both, as XPath 1.0 has no
// escapes.
func XPathString(s string) string {
	switch {
	case !strings.Contains(s, `"`):
		return `"` + s + `"`
	case !strings.Contains(s, `'`):
		return `'` + s + `'`
	}
	parts := strings.Split(s, `"`)
	for i, p := range parts {
		parts[i] = `"` + p + `"`
	}
	return "concat(" + strings.Join(parts, `, '"', `) + ")"
}
