package jyaml

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Unmarshal reads the first YAML document of data the way Jackson's
// ObjectMapper(new YAMLFactory()).readTree reads it and then converts the tree
// to plain Java values: mappings become *jsonx.Object (a repeated key keeps
// its first position and its last value), sequences []any, and scalars are
// typed as Jackson types them (see the package documentation). An empty or
// comment-only document is nil.
//
// Aliases are resolved to a copy of the anchored value, and "<<" is an
// ordinary key (Jackson does not merge).
func Unmarshal(data []byte) (any, error) {
	f, err := parser.ParseBytes(data, 0, parser.AllowDuplicateMapKey())
	if err != nil {
		return nil, err
	}
	for _, doc := range f.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		d := &decoder{anchors: map[string]any{}}
		return d.node(doc.Body)
	}
	return nil, nil
}

type decoder struct {
	anchors map[string]any
}

// Error is a YAML value that Jackson rejects while building its tree.
type Error struct {
	Message string
	Line    int
	Column  int
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
	}
	return e.Message
}

func errAt(tk *token.Token, format string, args ...any) error {
	e := &Error{Message: fmt.Sprintf(format, args...)}
	if tk != nil && tk.Position != nil {
		e.Line, e.Column = tk.Position.Line, tk.Position.Column
	}
	return e
}

func (d *decoder) node(n ast.Node) (any, error) {
	switch x := n.(type) {
	case nil:
		return nil, nil
	case *ast.DocumentNode:
		return d.node(x.Body)
	case *ast.MappingNode:
		obj := jsonx.NewObject()
		for _, mv := range x.Values {
			if err := d.entry(obj, mv); err != nil {
				return nil, err
			}
		}
		return obj, nil
	case *ast.MappingValueNode:
		obj := jsonx.NewObject()
		if err := d.entry(obj, x); err != nil {
			return nil, err
		}
		return obj, nil
	case *ast.SequenceNode:
		list := make([]any, 0, len(x.Values))
		for _, v := range x.Values {
			e, err := d.node(v)
			if err != nil {
				return nil, err
			}
			list = append(list, e)
		}
		return list, nil
	case *ast.AnchorNode:
		v, err := d.node(x.Value)
		if err != nil {
			return nil, err
		}
		if name := anchorName(x.Name); name != "" {
			d.anchors[name] = v
		}
		return v, nil
	case *ast.AliasNode:
		name := anchorName(x.Value)
		v, ok := d.anchors[name]
		if !ok {
			return nil, errAt(x.GetToken(), "unknown alias *%s", name)
		}
		return deepCopy(v), nil
	case *ast.TagNode:
		return d.tagged(x)
	case *ast.LiteralNode:
		if x.Value == nil {
			return "", nil
		}
		return x.Value.Value, nil
	case *ast.CommentGroupNode, *ast.CommentNode:
		return nil, nil
	}
	tk := n.GetToken()
	if tk == nil {
		return nil, nil
	}
	if tk.Type == token.SingleQuoteType || tk.Type == token.DoubleQuoteType {
		return scalarValue(n), nil
	}
	return resolvePlain(scalarValue(n), tk)
}

func anchorName(n ast.Node) string {
	if n == nil {
		return ""
	}
	if tk := n.GetToken(); tk != nil {
		return tk.Value
	}
	return ""
}

// scalarValue is the text of a scalar node: the unescaped content of quoted
// scalars, the scanned value of plain ones ("" for an empty value).
func scalarValue(n ast.Node) string {
	switch x := n.(type) {
	case *ast.StringNode:
		return x.Value
	case *ast.LiteralNode:
		if x.Value != nil {
			return x.Value.Value
		}
		return ""
	}
	tk := n.GetToken()
	if tk == nil || tk.Type == token.ImplicitNullType {
		return ""
	}
	return tk.Value
}

func (d *decoder) entry(obj *jsonx.Object, mv *ast.MappingValueNode) error {
	key, err := d.key(mv.Key)
	if err != nil {
		return err
	}
	v, err := d.node(mv.Value)
	if err != nil {
		return err
	}
	obj.Set(key, v)
	return nil
}

// key is a field name: the scalar's text, never resolved to another type.
func (d *decoder) key(k ast.MapKeyNode) (string, error) {
	var n ast.Node = k
	for {
		switch x := n.(type) {
		case *ast.MappingKeyNode:
			n = x.Value
			continue
		case *ast.TagNode:
			n = x.Value
			continue
		case *ast.AnchorNode:
			n = x.Value
			continue
		}
		break
	}
	switch x := n.(type) {
	case nil:
		return "", nil
	case *ast.MappingNode, *ast.SequenceNode, *ast.MappingValueNode:
		return "", errAt(x.GetToken(), "Expected a field name (Scalar value in YAML), got a collection instead")
	case *ast.MergeKeyNode:
		return "<<", nil
	}
	return scalarValue(n), nil
}

func (d *decoder) tagged(x *ast.TagNode) (any, error) {
	tag := x.Start.Value
	switch {
	case strings.HasPrefix(tag, "!!"):
		tag = tag[2:]
	case strings.HasPrefix(tag, "!<tag:yaml.org,2002:") && strings.HasSuffix(tag, ">"):
		tag = strings.TrimSuffix(strings.TrimPrefix(tag, "!<tag:yaml.org,2002:"), ">")
	}
	if i := strings.IndexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	switch x.Value.(type) {
	case *ast.MappingNode, *ast.SequenceNode, *ast.MappingValueNode, nil:
		return d.node(x.Value) // Jackson ignores tags on collections
	}
	if tag == "!" {
		return d.node(x.Value)
	}
	value := scalarValue(x.Value)
	switch tag {
	case "binary":
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
		if err != nil {
			return nil, errAt(x.Start, "%s", err.Error())
		}
		return b, nil
	case "bool":
		if b, ok := matchYAMLBoolean(value); ok {
			return b, nil
		}
	case "int":
		if value != "" {
			return decodeNumberScalar(value, x.Start)
		}
	case "float":
		if value != "" {
			return decodeFloat(value, x.Start)
		}
	case "null":
		if value != "" {
			return nil, nil
		}
	}
	return value, nil
}

// SnakeYAML's implicit resolvers (org.yaml.snakeyaml.resolver.Resolver).
var (
	reBool      = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	reFloat     = regexp.MustCompile(`^([-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+]?[0-9]+)?|[-+]?(?:[0-9][0-9_]*)(?:[eE][-+]?[0-9]+)|[-+]?\.[0-9_]+(?:[eE][-+]?[0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	reInt       = regexp.MustCompile(`^(?:[-+]?0b_*[0-1][0-1_]*|[-+]?0_*[0-7][0-7_]*|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x_*[0-9a-fA-F][0-9a-fA-F_]*|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	reMerge     = regexp.MustCompile(`^(?:<<)$`)
	reNull      = regexp.MustCompile(`^(?:~|null|Null|NULL| )$`)
	reTimestamp = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
	reYAML      = regexp.MustCompile(`^(?:!|&|\*)$`)
)

type yamlTag int

const (
	tagStr yamlTag = iota
	tagBool
	tagInt
	tagFloat
	tagNull
	tagOther
)

type resolver struct {
	tag   yamlTag
	re    *regexp.Regexp
	first string
	limit int
}

var resolvers = []resolver{
	{tagBool, reBool, "yYnNtTfFoO", 10},
	{tagInt, reInt, "-+0123456789", 1024},
	{tagFloat, reFloat, "-+0123456789.", 1024},
	{tagOther, reMerge, "<", 10},
	{tagNull, reNull, "~nN\x00", 10},
	{tagOther, reTimestamp, "0123456789", 50},
	{tagOther, reYAML, "!&*", 10},
}

// resolve is Resolver.resolve(NodeId.scalar, value, true).
func resolve(value string) yamlTag {
	if value == "" {
		return tagNull // the EMPTY resolver
	}
	first := utf16.Encode([]rune(value))[0]
	length := len(utf16.Encode([]rune(value)))
	for _, r := range resolvers {
		if first >= 0x80 || !strings.ContainsRune(r.first, rune(first)) {
			continue
		}
		if length <= r.limit && r.re.MatchString(value) {
			return r.tag
		}
	}
	return tagStr
}

func resolvePlain(value string, tk *token.Token) (any, error) {
	switch resolve(value) {
	case tagInt:
		return decodeNumberScalar(value, tk)
	case tagFloat:
		return decodeFloat(value, tk)
	case tagBool:
		if b, ok := matchYAMLBoolean(value); ok {
			return b, nil
		}
	case tagNull:
		return nil, nil
	}
	return value, nil
}

// matchYAMLBoolean is YAMLParser._matchYAMLBoolean.
func matchYAMLBoolean(value string) (bool, bool) {
	l := strings.ToLower(value)
	switch len(value) {
	case 1:
		switch value[0] {
		case 'N', 'n':
			return false, true
		case 'Y', 'y':
			return true, true
		}
	case 2:
		if l == "no" {
			return false, true
		}
		if l == "on" {
			return true, true
		}
	case 3:
		if l == "yes" {
			return true, true
		}
		if l == "off" {
			return false, true
		}
	case 4:
		if l == "true" {
			return true, true
		}
	case 5:
		if l == "false" {
			return false, true
		}
	}
	return false, false
}

// decodeNumberScalar is YAMLParser._decodeNumberScalar followed by the
// number typing of _parseNumericValue.
func decodeNumberScalar(value string, tk *token.Token) (any, error) {
	negative := false
	i := 0
	switch value[0] {
	case '-':
		negative = true
		i = 1
	case '+':
		if len(value) == 1 {
			return value, nil
		}
		i = 1
	}
	if len(value) == i {
		return value, nil
	}
	if value[i] == '0' {
		i++
		if i == len(value) {
			return jsonx.IntegerNumber(0), nil
		}
		switch ch := value[i]; {
		case ch >= '0' && ch <= '9' || ch == '_':
			return decodeRadix(value, strings.ReplaceAll(value[i:], "_", ""), 8, 10, 21, false, negative, tk)
		case ch == 'b' || ch == 'B':
			digits := strings.ReplaceAll(value[i+1:], "_", "")
			return decodeRadix(value, digits, 2, 31, 63, len(digits) == 32, negative, tk)
		case ch == 'x' || ch == 'X':
			digits := strings.ReplaceAll(value[i+1:], "_", "")
			return decodeRadix(value, digits, 16, 7, 15, len(digits) == 8, negative, tk)
		default:
			return value, nil
		}
	}
	underscores := false
	for j := i; j < len(value); j++ {
		c := value[j]
		if c < '0' || c > '9' {
			if c != '_' {
				return value, nil
			}
			underscores = true
		}
	}
	cleaned := value
	if underscores {
		start := 0
		if value[0] == '+' {
			start = 1
		}
		cleaned = strings.ReplaceAll(value[start:], "_", "")
		if cleaned == "" || cleaned == "-" {
			return nil, errAt(tk, "Invalid number ('%s')", value)
		}
	}
	return decimalNumber(cleaned, negative, value, tk)
}

// decimalNumber types a base-10 integer the way _parseNumericValue does:
// by the length of its text, not only by its magnitude.
func decimalNumber(text string, negative bool, raw string, tk *token.Token) (any, error) {
	n := len(text)
	if negative {
		n--
	}
	digits := strings.TrimPrefix(text, "+")
	b, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, errAt(tk, "Malformed numeric value '%s'", raw)
	}
	switch {
	case n <= 9:
		return jsonx.IntegerNumber(int32(b.Int64())), nil
	case n <= 18:
		l := b.Int64()
		if n == 10 && l >= -2147483648 && l <= 2147483647 {
			return jsonx.IntegerNumber(int32(l)), nil
		}
		return jsonx.LongNumber(l), nil
	case n == 19 && b.BitLen() <= 63:
		return jsonx.LongNumber(b.Int64()), nil
	}
	return jsonx.BigIntegerNumber(b), nil
}

func decodeRadix(raw, digits string, base, intDigits, longDigits int, checkIfInt, negative bool, tk *token.Token) (any, error) {
	if digits == "" {
		return nil, errAt(tk, "Invalid base-%d number ('%s'), problem: Zero length string", base, digits)
	}
	switch {
	case len(digits) <= intDigits:
		v, err := strconv.ParseInt(digits, base, 32)
		if err != nil {
			return nil, errAt(tk, "Invalid base-%d number ('%s')", base, digits)
		}
		if negative {
			v = -v
		}
		return jsonx.IntegerNumber(int32(v)), nil
	case len(digits) <= longDigits:
		u, err := strconv.ParseInt(digits, base, 64)
		if err != nil {
			return nil, errAt(tk, "Invalid base-%d number ('%s')", base, digits)
		}
		if negative {
			v := -u
			if checkIfInt && v >= -2147483648 {
				return jsonx.IntegerNumber(int32(v)), nil
			}
			return jsonx.LongNumber(v), nil
		}
		if checkIfInt && u < 2147483647 {
			return jsonx.IntegerNumber(int32(u)), nil
		}
		return jsonx.LongNumber(u), nil
	}
	b, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return nil, errAt(tk, "Invalid base-%d number ('%s')", base, digits)
	}
	if negative {
		b.Neg(b)
	}
	_ = raw
	return jsonx.BigIntegerNumber(b), nil
}

// decodeFloat is _cleanYamlFloat plus Double.parseDouble.
func decodeFloat(value string, tk *token.Token) (any, error) {
	cleaned := value
	if strings.Contains(value, "_") {
		start := 0
		if value[0] == '+' {
			start = 1
		}
		cleaned = strings.ReplaceAll(value[start:], "_", "")
	}
	f, err := javafmt.ParseDouble(cleaned)
	if err != nil {
		return nil, errAt(tk, "Malformed numeric value '%s'", value)
	}
	return jsonx.DoubleNumber(f), nil
}

// deepCopy copies maps and lists so an alias never shares structure with
// its anchor.
func deepCopy(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			out.Set(k, deepCopy(e))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopy(e)
		}
		return out
	case []byte:
		return append([]byte(nil), x...)
	}
	return v
}

// ErrNotMapping is returned by UnmarshalMapping for a document whose root is
// not a mapping.
var ErrNotMapping = errors.New("not a YAML mapping")

// UnmarshalMapping reads a document whose root must be a mapping; an empty
// document yields nil.
func UnmarshalMapping(data []byte) (*jsonx.Object, error) {
	v, err := Unmarshal(data)
	if err != nil || v == nil {
		return nil, err
	}
	obj, ok := v.(*jsonx.Object)
	if !ok {
		return nil, ErrNotMapping
	}
	return obj, nil
}
