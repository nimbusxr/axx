package jyaml

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// render is a node tree as text: scalars as Unmarshal's values, each with
// its line:column.
func render(n *Node) string {
	switch n.Kind {
	case MappingNode:
		var parts []string
		for i, k := range n.Keys {
			parts = append(parts, k+": "+render(n.Kids[i]))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
	case SequenceNode:
		var parts []string
		for _, k := range n.Kids {
			parts = append(parts, render(k))
		}
		return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
	}
	v := "null"
	switch x := n.Value.(type) {
	case string:
		v = fmt.Sprintf("%q", x)
	case bool:
		v = fmt.Sprint(x)
	case jsonx.Number:
		v = x.Text()
	}
	return fmt.Sprintf("%s@%d:%d", v, n.Line, n.Column)
}

func TestUnmarshalNodesKeepsPositionsAndUnmarshalsTyping(t *testing.T) {
	docs, err := UnmarshalNodes([]byte(`# a seed
public.closures:
  - id: c-1
    lanes: 0x2
    open: yes
    note: ~
    road: "Grand Ave"
  - &shared {id: c-2}
  - *shared
id: first
id: second
---
order: {id: 7}
`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range docs {
		got = append(got, render(d))
	}
	want := []string{
		`{public.closures: [{id: "c-1"@3:9, lanes: 2@4:12, open: true@5:11, note: null@6:11, road: "Grand Ave"@7:11}, {id: "c-2"@8:18}, {id: "c-2"@9:5}], id: "first"@10:5, id: "second"@11:5}`,
		`{order: {id: 7@13:13}}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("nodes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The values are Unmarshal's: the same document reads the same.
	v, err := Unmarshal([]byte("lanes: 0x2\nopen: yes\n"))
	if err != nil {
		t.Fatal(err)
	}
	o := v.(*jsonx.Object)
	lanes, _ := o.Get("lanes")
	open, _ := o.Get("open")
	if lanes.(jsonx.Number).Text() != "2" || open != true {
		t.Errorf("Unmarshal: lanes %v, open %v", lanes, open)
	}
}

func TestUnmarshalNodesSyntaxErrorHasAPosition(t *testing.T) {
	_, err := UnmarshalNodes([]byte("order:\n  id: [x\n"))
	var e *Error
	if !errors.As(err, &e) || e.Line == 0 || e.Message == "" {
		t.Errorf("err = %#v", err)
	}
}

// UnmarshalPlain gives the plain scalars its reader has a value for, and
// only those: the same word in quotes, tagged or in a block is the string.
func TestUnmarshalPlainReadsPlainWordsOnly(t *testing.T) {
	type absent struct{}
	v, err := UnmarshalPlain([]byte("a: undefined\nb: \"undefined\"\nc: 'undefined'\nd: !!str undefined\ne: |\n  undefined\nf: [undefined, x]\ng: null\nh: undefined-ish\n"),
		func(text string) (any, bool) { return absent{}, text == "undefined" })
	if err != nil {
		t.Fatal(err)
	}
	o := v.(*jsonx.Object)
	get := func(k string) any { x, _ := o.Get(k); return x }
	if _, ok := get("a").(absent); !ok {
		t.Errorf("a: %#v", get("a"))
	}
	for _, k := range []string{"b", "c", "d"} {
		if get(k) != "undefined" {
			t.Errorf("%s: %#v", k, get(k))
		}
	}
	if get("e") != "undefined\n" || get("g") != nil || get("h") != "undefined-ish" {
		t.Errorf("e, g, h: %#v %#v %#v", get("e"), get("g"), get("h"))
	}
	if list := get("f").([]any); len(list) != 2 || list[1] != "x" {
		t.Errorf("f: %#v", list)
	} else if _, ok := list[0].(absent); !ok {
		t.Errorf("f[0]: %#v", list[0])
	}
}

// Quoted keeps the strings a reader gives another meaning in quotes.
func TestMarshalKeepsQuotedStringsQuoted(t *testing.T) {
	o := jsonx.NewObject()
	o.Set("a", "undefined")
	o.Set("b", "defined")
	out, err := Marshal(o, Options{MinimizeQuotes: true, Quoted: func(s string) bool { return s == "undefined" }})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a: \"undefined\"\nb: defined\n" {
		t.Errorf("got %q", out)
	}
}
