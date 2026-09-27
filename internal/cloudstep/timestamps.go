package cloudstep

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// A seed's timestamps (SeedOptions.Timestamps). Seeds are read as Jackson
// reads YAML (internal/compat/jyaml), into JSON's model, which has no date
// type: a YAML timestamp arrives as its text. So the timestamps are found
// again in the YAML's nodes, and put back in place of that text:
//
//   - a plain scalar that YAML 1.1 resolves as a timestamp, as SnakeYAML's
//     resolver does (jyaml's reTimestamp): 2026-09-01,
//     2026-09-01T10:00:00Z, 2026-09-01 10:00:00.5 +02:00;
//   - a scalar tagged !!timestamp, quoted or not.
//
// Quoted and block scalars, scalars with another tag, keys, and every JSON
// string stay strings.

// implicitTimestamp is what a plain scalar must look like to resolve as a
// timestamp: the regexp of SnakeYAML's implicit resolver, as jyaml has it,
// which also stops looking past 50 characters.
var implicitTimestamp = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)

const implicitTimestampLimit = 50

// timestampParts is the timestamp of https://yaml.org/type/timestamp.html
// with its parts as groups: year, month, day, then optionally hour, minute,
// second, fraction, and the zone (Z, or a sign, hours and minutes).
var timestampParts = regexp.MustCompile(`^([0-9]{4})-([0-9]{1,2})-([0-9]{1,2})(?:(?:[Tt]|[ \t]+)([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]*))?(?:[ \t]*(?:Z|([-+])([0-9]{1,2})(?::([0-9]{2}))?))?)?$`)

// stamps are the timestamps of a YAML value: the time of a scalar, or those
// in a mapping's fields or a sequence's items. nil means none.
type stamps struct {
	// text is the scalar as the seed's value has it; at is its time.
	text   string
	at     *time.Time
	fields map[string]*stamps
	items  map[int]*stamps
}

// field is the timestamps of a mapping's field.
func (s *stamps) field(k string) *stamps {
	if s == nil {
		return nil
	}
	return s.fields[k]
}

// apply puts the times in v, a seed's plain value, in place of their text.
func (s *stamps) apply(v any) any {
	if s == nil {
		return v
	}
	switch x := v.(type) {
	case string:
		if s.at != nil && x == s.text {
			return *s.at
		}
	case map[string]any:
		for k, sub := range s.fields {
			if e, ok := x[k]; ok {
				x[k] = sub.apply(e)
			}
		}
	case []any:
		for i, sub := range s.items {
			if i < len(x) {
				x[i] = sub.apply(x[i])
			}
		}
	}
	return v
}

// findStamps finds the timestamps of the first YAML document of data, which
// jyaml has read: it follows the document as jyaml does (a repeated key keeps
// its last value, an alias is its anchor's value, "<<" is an ordinary key).
func findStamps(data []byte) (*stamps, error) {
	f, err := parser.ParseBytes(data, 0, parser.AllowDuplicateMapKey())
	if err != nil {
		return nil, err
	}
	for _, doc := range f.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		fs := &stampFinder{anchors: map[string]*stamps{}}
		return fs.node(doc.Body)
	}
	return nil, nil
}

type stampFinder struct {
	anchors map[string]*stamps
}

func (fs *stampFinder) node(n ast.Node) (*stamps, error) {
	switch x := n.(type) {
	case nil:
		return nil, nil
	case *ast.DocumentNode:
		return fs.node(x.Body)
	case *ast.MappingNode:
		s := &stamps{}
		for _, mv := range x.Values {
			if err := fs.entry(s, mv); err != nil {
				return nil, err
			}
		}
		return s.orNil(), nil
	case *ast.MappingValueNode:
		s := &stamps{}
		if err := fs.entry(s, x); err != nil {
			return nil, err
		}
		return s.orNil(), nil
	case *ast.SequenceNode:
		s := &stamps{}
		for i, v := range x.Values {
			e, err := fs.node(v)
			if err != nil {
				return nil, err
			}
			if e != nil {
				if s.items == nil {
					s.items = map[int]*stamps{}
				}
				s.items[i] = e
			}
		}
		return s.orNil(), nil
	case *ast.AnchorNode:
		v, err := fs.node(x.Value)
		if err != nil {
			return nil, err
		}
		if name := anchorName(x.Name); name != "" {
			fs.anchors[name] = v
		}
		return v, nil
	case *ast.AliasNode:
		return fs.anchors[anchorName(x.Value)], nil
	case *ast.TagNode:
		return fs.tagged(x)
	case *ast.LiteralNode, *ast.CommentGroupNode, *ast.CommentNode:
		return nil, nil
	}
	tk := n.GetToken()
	if tk == nil || tk.Type == token.SingleQuoteType || tk.Type == token.DoubleQuoteType {
		return nil, nil
	}
	text := scalarText(n)
	if len(text) > implicitTimestampLimit || !implicitTimestamp.MatchString(text) {
		return nil, nil
	}
	t, ok := parseTimestamp(text)
	if !ok {
		return nil, fmt.Errorf("%s%s is not a valid date (quote it to keep it a string)", position(tk), text)
	}
	return &stamps{text: text, at: &t}, nil
}

// entry adds the timestamps of a mapping's entry, under the field name jyaml
// gives it.
func (fs *stampFinder) entry(s *stamps, mv *ast.MappingValueNode) error {
	v, err := fs.node(mv.Value)
	if err != nil {
		return err
	}
	k := fieldName(mv.Key)
	if v == nil {
		delete(s.fields, k)
		return nil
	}
	if s.fields == nil {
		s.fields = map[string]*stamps{}
	}
	s.fields[k] = v
	return nil
}

// tagged is a scalar tagged !!timestamp; tags on collections are ignored, as
// jyaml ignores them.
func (fs *stampFinder) tagged(x *ast.TagNode) (*stamps, error) {
	switch x.Value.(type) {
	case *ast.MappingNode, *ast.SequenceNode, *ast.MappingValueNode, nil:
		return fs.node(x.Value)
	}
	if !isTimestampTag(x.Start.Value) {
		return nil, nil
	}
	n := x.Value
	if a, ok := n.(*ast.AnchorNode); ok {
		n = a.Value
	}
	text := scalarText(n)
	t, ok := parseTimestamp(text)
	if !ok {
		return nil, fmt.Errorf("%s!!timestamp %s is not a YAML timestamp, like 2026-09-01 or 2026-09-01T10:00:00Z", position(x.Start), text)
	}
	return &stamps{text: text, at: &t}, nil
}

func (s *stamps) orNil() *stamps {
	if len(s.fields) == 0 && len(s.items) == 0 {
		return nil
	}
	return s
}

func isTimestampTag(tag string) bool {
	return tag == "!!timestamp" || tag == "!<tag:yaml.org,2002:timestamp>"
}

// fieldName is the name jyaml gives the field of a mapping key: the
// scalar's text, never resolved to another type.
func fieldName(k ast.MapKeyNode) string {
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
		case *ast.MergeKeyNode:
			return "<<"
		case nil:
			return ""
		}
		return scalarText(n)
	}
}

// scalarText is the text jyaml gives a scalar: the unescaped content of a
// quoted one, the scanned value of a plain one.
func scalarText(n ast.Node) string {
	switch x := n.(type) {
	case nil:
		return ""
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

func anchorName(n ast.Node) string {
	if n == nil {
		return ""
	}
	if tk := n.GetToken(); tk != nil {
		return tk.Value
	}
	return ""
}

// position is where a token is, for an error: "line 3, column 14: ".
func position(tk *token.Token) string {
	if tk == nil || tk.Position == nil {
		return ""
	}
	return fmt.Sprintf("line %d, column %d: ", tk.Position.Line, tk.Position.Column)
}

// parseTimestamp reads a YAML timestamp, if s is a valid one. A date alone
// is midnight UTC, and a time without a zone is UTC. Fractions of a second
// past nanoseconds are dropped.
func parseTimestamp(s string) (time.Time, bool) {
	m := timestampParts.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	n := func(i int) int {
		v, _ := strconv.Atoi(m[i])
		return v
	}
	year, month, day := n(1), n(2), n(3)
	var hour, minute, second, nsec int
	loc := time.UTC
	if m[4] != "" {
		hour, minute, second = n(4), n(5), n(6)
		frac := m[7]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		nsec, _ = strconv.Atoi(frac + strings.Repeat("0", 9-len(frac)))
		if m[8] != "" {
			oh, om := n(9), n(10)
			if oh > 23 || om > 59 {
				return time.Time{}, false
			}
			offset := (oh*60 + om) * 60
			if m[8] == "-" {
				offset = -offset
			}
			loc = time.FixedZone("", offset)
		}
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, nsec, loc)
	// time.Date normalizes a 30 February or a 25th hour: they are not dates.
	if t.Year() != year || int(t.Month()) != month || t.Day() != day ||
		t.Hour() != hour || t.Minute() != minute || t.Second() != second {
		return time.Time{}, false
	}
	return t, true
}
