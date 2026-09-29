package oaslevel

import (
	"fmt"
	"sort"
	"strings"
)

// Keys are the validation keys a validator reports. A level is set on one of
// them or on a dotted prefix of them, which covers the keys below it
// (validation.request.body covers validation.request.body.schema.required);
// any other key would relax nothing.
type Keys struct {
	valid map[string]bool // the keys and each of their dotted prefixes
	// aliases map a key segment, in lower case, to the one the keys use
	// instead (const -> enum), for suggestions.
	aliases map[string]string
	// name is the contract's, and example a key prefix, for errors.
	name, example string
}

// NewKeys returns the keys patterns stand for: each {name} in a pattern
// stands for every value of vars["{name}"]. aliases map a key segment to the
// one the keys use instead, for the suggestions of Check; it may be nil.
func NewKeys(patterns []string, vars map[string][]string, aliases map[string]string) *Keys {
	k := &Keys{valid: map[string]bool{}, aliases: map[string]string{}}
	for from, to := range aliases {
		k.aliases[strings.ToLower(from)] = strings.ToLower(to)
	}
	var add func(string)
	add = func(p string) {
		for name, values := range vars {
			if strings.Contains(p, name) {
				for _, v := range values {
					add(strings.Replace(p, name, v, 1))
				}
				return
			}
		}
		for key := p; ; {
			k.valid[key] = true
			i := strings.LastIndexByte(key, '.')
			if i < 0 {
				break
			}
			key = key[:i]
		}
	}
	for _, p := range patterns {
		add(p)
	}
	return k
}

// Named returns the keys, whose errors name their contract (AsyncAPI) and
// give example as a key prefix (validation.message.payload). Keys are
// OpenAPI's unless they are named.
func (k *Keys) Named(name, example string) *Keys {
	c := *k
	c.name, c.example = name, example
	return &c
}

// Has reports whether levels can be set on key: it is one of the keys or a
// dotted prefix of them.
func (k *Keys) Has(key string) bool { return k.valid[key] }

// Check returns an *UnknownKeyError, which names the closest keys, when key
// is none of the keys nor a dotted prefix of them.
func (k *Keys) Check(key string) error {
	if k.Has(key) {
		return nil
	}
	return &UnknownKeyError{Key: key, Closest: k.closest(key), Name: k.name, Example: k.example}
}

// UnknownKeyError is a key levels cannot be set on: none of the keys a
// validator reports, nor a prefix of them.
type UnknownKeyError struct {
	Key string
	// Closest are the keys closest to it, the closest first.
	Closest []string
	// Name is the contract's, and Example a key prefix: OpenAPI's and
	// validation.request.body when empty.
	Name, Example string
}

func (e *UnknownKeyError) Error() string {
	name, example := e.Name, e.Example
	if name == "" {
		name, example = "OpenAPI", "validation.request.body"
	}
	msg := fmt.Sprintf("unknown %s validation key %q", name, e.Key)
	switch n := len(e.Closest); n {
	case 0:
		msg += "."
	case 1:
		msg += "; did you mean " + e.Closest[0] + "?"
	default:
		msg += "; did you mean " + strings.Join(e.Closest[:n-1], ", ") + " or " + e.Closest[n-1] + "?"
	}
	return msg + " A key is one the validator reports, or a prefix of such keys, like " + example
}

// closest returns up to three keys (or prefixes) closest to key: those
// within a small margin of the closest one.
func (k *Keys) closest(key string) []string {
	type cand struct {
		key  string
		cost float64
	}
	segs := strings.Split(key, ".")
	cands := make([]cand, 0, len(k.valid))
	for v := range k.valid {
		cands = append(cands, cand{v, k.distance(segs, strings.Split(v, "."))})
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.cost != b.cost {
			return a.cost < b.cost
		}
		if len(a.key) != len(b.key) {
			return len(a.key) < len(b.key)
		}
		return a.key < b.key
	})
	var out []string
	for _, c := range cands {
		if len(out) == 3 || (len(out) > 0 && c.cost > max(2*cands[0].cost, cands[0].cost+0.2)) {
			break
		}
		out = append(out, c.key)
	}
	return out
}

// The costs of a segment one key lacks, less than replacing a segment by an
// unrelated one, which costs 1: validation.request.header.missing is close
// to validation.request.parameter.header.missing. Dropping a segment of the
// key asked for costs a little more than adding one, so that
// validation.request.body.required comes closer to
// validation.request.body.schema.required than to validation.request.body.
const (
	addSegment  = 0.5
	dropSegment = 0.6
)

// distance is the edit distance from key a to key b, by segment: a segment
// added or dropped costs addSegment or dropSegment, a different one its
// share of changed letters (case aside), and one that differs only in case,
// or is an alias of the other, almost nothing.
func (k *Keys) distance(a, b []string) float64 {
	prev := make([]float64, len(b)+1)
	cur := make([]float64, len(b)+1)
	for j := range prev {
		prev[j] = float64(j) * addSegment
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = float64(i) * dropSegment
		for j := 1; j <= len(b); j++ {
			cur[j] = min(prev[j]+dropSegment, cur[j-1]+addSegment, prev[j-1]+k.segmentCost(a[i-1], b[j-1]))
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func (k *Keys) segmentCost(a, b string) float64 {
	if a == b {
		return 0
	}
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if to, ok := k.aliases[la]; la == lb || ok && to == lb {
		return 0.01
	}
	return float64(levenshtein(la, lb)) / float64(max(len(la), len(lb)))
}

// levenshtein is the number of single-byte edits that turn a into b.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			sub := prev[j-1]
			if a[i-1] != b[j-1] {
				sub++
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, sub)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
