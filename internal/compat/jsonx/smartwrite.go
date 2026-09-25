package jsonx

import (
	"math"
	"strings"
	"unicode/utf16"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// This file ports json-smart's JSONValue/JSONStyle writers. Jayway's
// DocumentContext.jsonString() uses JSONStyle.LT_COMPRESS; toString() of a
// JSONArray uses JSONStyle.NO_COMPRESS, which additionally escapes '/'.

type writeStyle int

const (
	styleLTCompress writeStyle = iota // JSONStyle.LT_COMPRESS
	styleNoCompress                   // JSONStyle.NO_COMPRESS
)

const hexDigits = "0123456789ABCDEF"

func writeString(sb *strings.Builder, s string, style writeStyle) {
	sb.WriteByte('"')
	// Unescaped code units are buffered so surrogate pairs decode together.
	var raw []uint16
	flush := func() {
		if len(raw) > 0 {
			sb.WriteString(string(utf16.Decode(raw)))
			raw = raw[:0]
		}
	}
	for _, ch := range utf16.Encode([]rune(s)) {
		if !needsEscape(ch, style) {
			raw = append(raw, ch)
			continue
		}
		flush()
		switch ch {
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '/':
			sb.WriteString(`\/`)
		default:
			sb.WriteString(`\u`)
			sb.WriteByte(hexDigits[ch>>12&15])
			sb.WriteByte(hexDigits[ch>>8&15])
			sb.WriteByte(hexDigits[ch>>4&15])
			sb.WriteByte(hexDigits[ch&15])
		}
	}
	flush()
	sb.WriteByte('"')
}

func needsEscape(ch uint16, style writeStyle) bool {
	switch ch {
	case '\b', '\t', '\n', '\f', '\r', '"', '\\':
		return true
	case '/':
		return style == styleNoCompress
	}
	return ch <= 31 || ch >= 127 && ch <= 159 || ch >= 8192 && ch <= 8447
}

func writeValue(sb *strings.Builder, v any, style writeStyle) {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case string:
		writeString(sb, x, style)
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case Number:
		if (x.kind == Double || x.kind == Float) && math.IsInf(x.f, 0) {
			sb.WriteString("null")
		} else {
			sb.WriteString(x.String())
		}
	case *Object:
		sb.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeString(sb, k, style)
			sb.WriteByte(':')
			writeValue(sb, x.vals[k], style)
		}
		sb.WriteByte('}')
	case *Array, []any, KeySet:
		sb.WriteByte('[')
		for i, e := range iterItems(x) {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeValue(sb, e, style)
		}
		sb.WriteByte(']')
	default:
		writeString(sb, javafmt.ValueOf(x), style)
	}
}

func iterItems(v any) []any {
	if ks, ok := v.(KeySet); ok {
		out := make([]any, len(ks))
		for i, k := range ks {
			out[i] = k
		}
		return out
	}
	return listItems(v)
}
