package jvalue

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// FormURLEncode is the REST execute step's convertJsonToFormUrlEncoded: the
// top-level members of a JSON object payload rendered as
// application/x-www-form-urlencoded and encoded with Java's URLEncoder
// (UTF-8, space as '+', only A-Z a-z 0-9 . - * _ left as is). Scalars render
// as Java's String.valueOf renders them; nested objects and arrays render as
// their json-smart JSON text. (Java rendered a nested object in its map
// notation, "{a=1, b=[1,2]}", which no server can parse; axx sends JSON.)
func FormURLEncode(json string) (string, error) {
	doc, err := jsonx.Parse(json)
	if err != nil {
		return "", err
	}
	obj, ok := doc.(*jsonx.Object)
	if !ok {
		if _, isArray := doc.(*jsonx.Array); isArray {
			// json-smart's mapper yields null for an array root.
			return "", &jsonx.Error{
				Kind:    jsonx.NullPointer,
				Message: `Cannot invoke "java.util.Map.entrySet()" because "map" is null`,
			}
		}
		cls := jsonx.JavaClassName(doc)
		return "", &jsonx.Error{Kind: jsonx.ClassCast, Message: fmt.Sprintf(
			"class %s cannot be cast to class java.util.Map (%s and java.util.Map are in module java.base of loader 'bootstrap')",
			cls, cls)}
	}
	parts := make([]string, 0, obj.Len())
	for _, k := range obj.Keys() {
		v, _ := obj.Get(k)
		text := javafmt.ValueOf(v)
		switch v.(type) {
		case *jsonx.Object, *jsonx.Array, []any:
			text = jsonx.MarshalValue(v)
		}
		parts = append(parts, URLEncode(k)+"="+URLEncode(text))
	}
	return strings.Join(parts, "&"), nil
}

// URLEncode is java.net.URLEncoder.encode(s, UTF_8).
func URLEncode(s string) string {
	var sb strings.Builder
	for _, b := range []byte(s) {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9',
			b == '.', b == '-', b == '*', b == '_':
			sb.WriteByte(b)
		case b == ' ':
			sb.WriteByte('+')
		default:
			fmt.Fprintf(&sb, "%%%02X", b)
		}
	}
	return sb.String()
}
