package javafmt

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// JavaStringer is implemented by values that model a specific Java class and
// know how that class renders itself with toString().
type JavaStringer interface {
	JavaString() string
}

// ValueOf renders v like Java's String.valueOf(Object) would render the
// corresponding Java value:
//
//	nil                        "null"
//	string                     the string itself
//	bool                       "true" / "false"
//	int, int32, int64, ...     decimal digits (Integer/Long.toString)
//	float64                    Double.toString
//	float32                    Float.toString
//	*big.Int, BigDecimal       BigInteger/BigDecimal.toString
//	JavaStringer               v.JavaString()
//	[]any                      java.util.List: "[a, b]"
//	map[string]any             java.util.Map: "{k=v, k2=v2}" (keys sorted, as Go maps are unordered)
//
// Elements of lists and maps are rendered recursively with ValueOf.
func ValueOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case float64:
		return Double(x)
	case float32:
		return Float(x)
	case *big.Int:
		if x == nil {
			return "null"
		}
		return x.String()
	case BigDecimal:
		return x.String()
	case JavaStringer:
		return x.JavaString()
	case []any:
		return ListString(x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(k)
			sb.WriteByte('=')
			sb.WriteString(ValueOf(x[k]))
		}
		sb.WriteByte('}')
		return sb.String()
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

// ListString renders elements like java.util.AbstractCollection.toString:
// "[a, b, c]".
func ListString(items []any) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, e := range items {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(ValueOf(e))
	}
	sb.WriteByte(']')
	return sb.String()
}

// MapString renders ordered entries like java.util.AbstractMap.toString:
// "{k=v, k2=v2}".
func MapString(keys []string, value func(string) any) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(ValueOf(value(k)))
	}
	sb.WriteByte('}')
	return sb.String()
}
