// Package schemacompat says whether a JSON Schema still accepts every document an earlier
// version of it accepted: what "frozen" means for the schemas of axx.yaml and the fixture spec
// files (ADR 0004). A schema may grow (a new property, a new enum value, a looser limit); it
// may not take back what a file could say.
//
// It reads the keywords axx's schemas use. A keyword it does not read, changed, is reported: a
// change is taken for breaking until someone has looked.
package schemacompat

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Breaks lists how the schema now refuses, or no longer knows, what the schema before
// accepted: empty when every document valid before is valid now. Each line starts with the place
// in the schema, as a path of its keywords.
func Breaks(before, now []byte) ([]string, error) {
	var b, n map[string]any
	if err := json.Unmarshal(before, &b); err != nil {
		return nil, fmt.Errorf("the schema before: %w", err)
	}
	if err := json.Unmarshal(now, &n); err != nil {
		return nil, fmt.Errorf("the schema now: %w", err)
	}
	c := &checker{before: b, now: n, seen: map[[2]string]bool{}}
	c.schema("", b, n)
	sort.Strings(c.out)
	return slices.Compact(c.out), nil
}

type checker struct {
	before, now map[string]any
	// seen are the pairs of definitions compared, or being compared: a schema may name itself.
	seen map[[2]string]bool
	out  []string
}

func (c *checker) breakf(at, format string, args ...any) {
	if at == "" {
		at = "the schema"
	}
	c.out = append(c.out, at+": "+fmt.Sprintf(format, args...))
}

// annotations say nothing about what a document may hold.
var annotations = map[string]bool{
	"description": true, "title": true, "examples": true, "default": true, "$schema": true, "$id": true,
	"$comment": true, "$defs": true, "deprecated": true, "markdownDescription": true,
}

// atLeast and atMost are the limits that may only loosen: a lower least, a higher most.
var (
	atLeast = map[string]bool{"minimum": true, "exclusiveMinimum": true, "minItems": true, "minLength": true, "minProperties": true}
	atMost  = map[string]bool{"maximum": true, "exclusiveMaximum": true, "maxItems": true, "maxLength": true, "maxProperties": true}
)

func (c *checker) schema(at string, before, now map[string]any) {
	// A reference stands for its definition, on each side by its own name.
	bRef, _ := before["$ref"].(string)
	nRef, _ := now["$ref"].(string)
	if bRef != "" || nRef != "" {
		key := [2]string{bRef, nRef}
		if bRef != "" && nRef != "" {
			if c.seen[key] {
				return
			}
			c.seen[key] = true
		}
		b, ok := resolve(c.before, before)
		if !ok {
			c.breakf(at, "the schema before names %s, which it does not define", bRef)
			return
		}
		n, ok := resolve(c.now, now)
		if !ok {
			c.breakf(at, "it names %s, which the schema does not define", nRef)
			return
		}
		c.schema(at, b, n)
		return
	}

	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range now {
		keys[k] = true
	}
	for k := range keys {
		if annotations[k] {
			continue
		}
		b, hadB := before[k]
		n, hasN := now[k]
		here := join(at, k)
		switch {
		case k == "type":
			if !hasN {
				continue // any type now
			}
			if !hadB {
				c.breakf(at, "it takes only %s now, and took any type", list(types(n)))
				continue
			}
			for _, t := range types(b) {
				if !slices.Contains(types(n), t) && (t != "integer" || !slices.Contains(types(n), "number")) {
					c.breakf(at, "it took %s, and takes %s now", t, list(types(n)))
				}
			}
		case k == "enum":
			if !hasN {
				continue
			}
			nv, _ := n.([]any)
			if !hadB {
				c.breakf(at, "it takes only %s now, and took any value", values(nv))
				continue
			}
			bv, _ := b.([]any)
			for _, v := range bv {
				if !contains(nv, v) {
					c.breakf(at, "it no longer takes %s", value(v))
				}
			}
		case k == "const":
			if hasN && (!hadB || !reflect.DeepEqual(b, n)) {
				c.breakf(at, "it takes only %s now", value(n))
			}
		case k == "required":
			bv, _ := b.([]any)
			nv, _ := n.([]any)
			for _, v := range nv {
				if !contains(bv, v) {
					c.breakf(at, "it requires %s now", value(v))
				}
			}
		case k == "properties":
			c.properties(at, before, now)
		case k == "additionalProperties":
			c.additional(at, b, hadB, n, hasN)
		case k == "items":
			if !hasN {
				continue
			}
			bm, bOK := b.(map[string]any)
			nm, nOK := n.(map[string]any)
			if !hadB || !bOK || !nOK {
				if !reflect.DeepEqual(b, n) {
					c.breakf(here, "its items are held to a schema they were not held to")
				}
				continue
			}
			c.schema(here, bm, nm)
		case k == "oneOf" || k == "anyOf":
			c.alternatives(here, k, b, hadB, n, hasN)
		case atLeast[k]:
			if hasN && (!hadB || number(n) > number(b)) {
				c.breakf(at, "its %s is %s now, and was %s", k, value(n), was(b, hadB))
			}
		case atMost[k]:
			if hasN && (!hadB || number(n) < number(b)) {
				c.breakf(at, "its %s is %s now, and was %s", k, value(n), was(b, hadB))
			}
		case k == "pattern" || k == "format":
			if hasN && (!hadB || !reflect.DeepEqual(b, n)) {
				c.breakf(at, "its %s is %s now, and was %s", k, value(n), was(b, hadB))
			}
		default:
			if !reflect.DeepEqual(b, n) {
				c.breakf(at, "its %s changed, a keyword this check does not read: from %s to %s", k, was(b, hadB), was(n, hasN))
			}
		}
	}
}

// properties: every property a document could give is still one, and holds what it held. A new
// property is growth where the object took no property it did not name; where it took any, the
// new one's schema now holds a value that was free.
func (c *checker) properties(at string, before, now map[string]any) {
	bp, _ := before["properties"].(map[string]any)
	np, _ := now["properties"].(map[string]any)
	for name, b := range bp {
		here := join(at, name)
		n, ok := np[name]
		if !ok {
			c.breakf(here, "the property is gone")
			continue
		}
		bm, bOK := b.(map[string]any)
		nm, nOK := n.(map[string]any)
		if bOK && nOK {
			c.schema(here, bm, nm)
		} else if !reflect.DeepEqual(b, n) {
			c.breakf(here, "it changed from %s to %s", value(b), value(n))
		}
	}
	// An object that took no property it did not name: a new one is growth.
	if open, isBool := before["additionalProperties"].(bool); isBool && !open {
		return
	}
	for name := range np {
		if _, ok := bp[name]; !ok {
			c.breakf(join(at, name), "the property is new in an object that took any property: its value was free, and is held to a schema now")
		}
	}
}

func (c *checker) additional(at string, b any, hadB bool, n any, hasN bool) {
	if !hasN {
		return // any property now
	}
	if nb, ok := n.(bool); ok {
		if nb {
			return
		}
		if bb, ok := b.(bool); !hadB || !ok || bb {
			c.breakf(at, "it takes no property it does not name now, and took others")
		}
		return
	}
	nm, _ := n.(map[string]any)
	switch bv := b.(type) {
	case bool:
		if bv {
			c.breakf(at, "its other properties are held to a schema now, and were free")
		}
	case map[string]any:
		c.schema(join(at, "additionalProperties"), bv, nm)
	default:
		c.breakf(at, "its other properties are held to a schema now, and were free")
	}
}

// alternatives: each form a document could take is still a form.
func (c *checker) alternatives(at, keyword string, b any, hadB bool, n any, hasN bool) {
	if !hasN {
		return
	}
	if !hadB {
		c.breakf(at, "it has to be one of the forms of a %s now, and had none to be", keyword)
		return
	}
	bv, _ := b.([]any)
	nv, _ := n.([]any)
	for i, alt := range bv {
		bm, _ := alt.(map[string]any)
		// Held against each form now; what is reported is how the form in its place differs.
		kept := false
		var inPlace []string
		for j, cand := range nv {
			nm, _ := cand.(map[string]any)
			try := &checker{before: c.before, now: c.now, seen: map[[2]string]bool{}}
			try.schema(fmt.Sprintf("%s[%d]", at, i), bm, nm)
			if len(try.out) == 0 {
				kept = true
				break
			}
			if j == i {
				inPlace = try.out
			}
		}
		if !kept {
			if len(inPlace) > 0 {
				c.out = append(c.out, inPlace...)
			} else {
				c.breakf(fmt.Sprintf("%s[%d]", at, i), "the form is gone")
			}
		}
	}
}

// resolve is the schema a node stands for: its definition, for a reference into the document's
// own $defs.
func resolve(doc, node map[string]any) (map[string]any, bool) {
	for range 32 {
		ref, _ := node["$ref"].(string)
		if ref == "" {
			return node, true
		}
		name, ok := strings.CutPrefix(ref, "#/$defs/")
		if !ok {
			return nil, false
		}
		defs, _ := doc["$defs"].(map[string]any)
		next, ok := defs[name].(map[string]any)
		if !ok {
			return nil, false
		}
		node = next
	}
	return nil, false
}

func join(at, name string) string {
	if at == "" {
		return name
	}
	return at + "." + name
}

func types(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func list(s []string) string { return strings.Join(s, " or ") }

func contains(vs []any, v any) bool {
	for _, e := range vs {
		if reflect.DeepEqual(e, v) {
			return true
		}
	}
	return false
}

func number(v any) float64 {
	f, _ := v.(float64)
	return f
}

func value(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func values(vs []any) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = value(v)
	}
	return strings.Join(parts, ", ")
}

func was(v any, had bool) string {
	if !had {
		return "not set"
	}
	return value(v)
}
