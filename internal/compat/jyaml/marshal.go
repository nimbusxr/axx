package jyaml

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Options are the YAMLGenerator features axx toggles.
type Options struct {
	// MinimizeQuotes is YAMLGenerator.Feature.MINIMIZE_QUOTES: strings are
	// written plain unless they must be quoted, and multi-line strings as
	// literal blocks.
	MinimizeQuotes bool
	// QuoteNumericStrings is ALWAYS_QUOTE_NUMBERS_AS_STRINGS: with
	// MinimizeQuotes, strings that look like numbers stay quoted.
	QuoteNumericStrings bool
	// DocumentStart is WRITE_DOC_START_MARKER (Jackson's default is on).
	DocumentStart bool
}

// Marshal writes v as Jackson's ObjectMapper(new YAMLFactory()) configured
// with opts writes the corresponding Java value.
//
// v may hold *jsonx.Object, []any, string, bool, nil, jsonx.Number, []byte,
// Go integers and float64 (written as Long and Double).
func Marshal(v any, opts Options) ([]byte, error) {
	g := &generator{opts: opts}
	g.events = append(g.events, event{kind: evStreamStart}, event{kind: evDocumentStart, explicit: opts.DocumentStart})
	if err := g.value(v); err != nil {
		return nil, err
	}
	g.events = append(g.events, event{kind: evDocumentEnd}, event{kind: evStreamEnd})
	return []byte(emit(g.events)), nil
}

type generator struct {
	opts   Options
	events []event
}

func (g *generator) scalar(value string, style scalarStyle) {
	g.events = append(g.events, event{kind: evScalar, value: value, style: style, implicitPlain: true, implicitNonPlain: true})
}

func (g *generator) value(v any) error {
	switch x := v.(type) {
	case nil:
		g.scalar("null", stylePlain)
	case string:
		g.writeString(x)
	case bool:
		g.scalar(strconv.FormatBool(x), stylePlain)
	case jsonx.Number:
		g.scalar(x.String(), stylePlain)
	case int:
		g.scalar(strconv.Itoa(x), stylePlain)
	case int32:
		g.scalar(strconv.FormatInt(int64(x), 10), stylePlain)
	case int64:
		g.scalar(strconv.FormatInt(x, 10), stylePlain)
	case *big.Int:
		g.scalar(x.String(), stylePlain)
	case float64:
		g.scalar(javafmt.Double(x), stylePlain)
	case []byte:
		// Jackson writes binary as a MIME base64 literal tagged !!binary.
		g.events = append(g.events, event{kind: evScalar, value: mimeBase64(x), style: styleLiteral, tag: "tag:yaml.org,2002:binary"})
	case *jsonx.Object:
		g.events = append(g.events, event{kind: evMappingStart})
		for _, k := range x.Keys() {
			g.fieldName(k)
			child, _ := x.Get(k)
			if err := g.value(child); err != nil {
				return err
			}
		}
		g.events = append(g.events, event{kind: evMappingEnd})
	case []any:
		g.events = append(g.events, event{kind: evSequenceStart})
		for _, e := range x {
			if err := g.value(e); err != nil {
				return err
			}
		}
		g.events = append(g.events, event{kind: evSequenceEnd})
	default:
		return fmt.Errorf("jyaml: cannot write %T", v)
	}
	return nil
}

func (g *generator) fieldName(name string) {
	if needToQuoteName(name) {
		g.scalar(name, styleDoubleQuoted)
	} else {
		g.scalar(name, stylePlain)
	}
}

// plainNumber is YAMLGenerator.PLAIN_NUMBER_P.
var plainNumber = regexp.MustCompile(`^[+-]?[0-9]*(\.[0-9]*)?$`)

func (g *generator) writeString(text string) {
	if text == "" {
		g.scalar(text, styleDoubleQuoted)
		return
	}
	style := styleDoubleQuoted
	if g.opts.MinimizeQuotes {
		switch {
		case strings.ContainsRune(text, '\n'):
			style = styleLiteral
		case !needToQuoteValue(text) && (!g.opts.QuoteNumericStrings || !plainNumber.MatchString(text)):
			style = stylePlain
		}
	}
	g.scalar(text, style)
}

// StringQuotingChecker.Default.

var reservedKeywords = map[string]bool{
	"false": true, "False": true, "FALSE": true, "n": true, "N": true, "no": true, "No": true, "NO": true,
	"null": true, "Null": true, "NULL": true, "on": true, "On": true, "ON": true, "off": true, "Off": true, "OFF": true,
	"true": true, "True": true, "TRUE": true, "y": true, "Y": true, "yes": true, "Yes": true, "YES": true,
}

func isReservedKeyword(s string) bool {
	if s == "" {
		return true
	}
	switch s[0] {
	case 'f', 'n', 'o', 't', 'y', 'F', 'N', 'O', 'T', 'Y':
		return reservedKeywords[s]
	case '~':
		return true
	}
	return false
}

func looksLikeYAMLNumber(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '+', '-', '.', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

func valueHasQuotableChar(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '#':
			if i == 0 || isBlank(s[i-1]) {
				return true
			}
		case ',', '[', ']', '{', '}':
			return true
		case ':':
			if i == len(s)-1 || isBlank(s[i+1]) {
				return true
			}
		}
	}
	return false
}

func nameHasQuotableChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			return true
		}
	}
	return false
}

func needToQuoteName(s string) bool {
	return isReservedKeyword(s) || looksLikeYAMLNumber(s) || nameHasQuotableChar(s)
}

func needToQuoteValue(s string) bool {
	return isReservedKeyword(s) || valueHasQuotableChar(s)
}

// mimeBase64 is Jackson's Base64Variants.MIME encoding: 76-character lines
// separated by "\n", no trailing line break.
func mimeBase64(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	var out strings.Builder
	for len(s) > 76 {
		out.WriteString(s[:76])
		out.WriteByte('\n')
		s = s[76:]
	}
	out.WriteString(s)
	return out.String()
}
