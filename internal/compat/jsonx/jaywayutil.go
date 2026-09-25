package jsonx

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// unescape ports com.jayway.jsonpath.internal.Utils.unescape, used for
// bracket-notation property names and quoted filter strings.
func unescape(s string) string {
	in := toJ(s)
	out := make([]uint16, 0, len(in))
	var unicode []uint16
	hadSlash, inUnicode := false, false
	for _, ch := range in {
		switch {
		case inUnicode:
			unicode = append(unicode, ch)
			if len(unicode) == 4 {
				v, err := parseJavaHexInt(jstr(unicode).String())
				if err != nil {
					panic(wrapUnicodeError(jstr(unicode).String(), err))
				}
				out = append(out, uint16(v))
				unicode = unicode[:0]
				inUnicode, hadSlash = false, false
			}
		case hadSlash:
			hadSlash = false
			switch ch {
			case '"', '\'', '\\':
				out = append(out, ch)
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				inUnicode = true
			default:
				out = append(out, ch)
			}
		case ch == '\\':
			hadSlash = true
		default:
			out = append(out, ch)
		}
	}
	if hadSlash {
		out = append(out, '\\')
	}
	return string(utf16.Decode(out))
}

func wrapUnicodeError(unicode string, _ error) *Error {
	return newErr(JsonPathError, "Unable to parse unicode value: %s", unicode)
}

// parseJavaHexInt is Integer.parseInt(s, 16) for ASCII input.
func parseJavaHexInt(s string) (int64, error) {
	return strconv.ParseInt(s, 16, 32)
}

// escapeJayway ports Utils.escape(str, escapeSingleQuote).
func escapeJayway(s string, escapeSingleQuote bool) string {
	var sb strings.Builder
	for _, ch := range toJ(s) {
		switch {
		case ch > 4095:
			sb.WriteString(`\u` + hexUpper(ch))
		case ch > 255:
			sb.WriteString(`\u0` + hexUpper(ch))
		case ch > 127:
			sb.WriteString(`\u00` + hexUpper(ch))
		case ch < ' ':
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
			default:
				if ch > 15 {
					sb.WriteString(`\u00` + hexUpper(ch))
				} else {
					sb.WriteString(`\u000` + hexUpper(ch))
				}
			}
		default:
			switch ch {
			case '"':
				sb.WriteString(`\"`)
			case '\'':
				if escapeSingleQuote {
					sb.WriteByte('\\')
				}
				sb.WriteByte('\'')
			case '/':
				sb.WriteString(`\/`)
			case '\\':
				sb.WriteString(`\\`)
			default:
				sb.WriteByte(byte(ch))
			}
		}
	}
	return sb.String()
}

func hexUpper(ch uint16) string {
	return strings.ToUpper(strconv.FormatUint(uint64(ch), 16))
}
