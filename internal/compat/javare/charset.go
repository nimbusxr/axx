package javare

import (
	"sort"
	"sync"
	"unicode"
)

const maxRune = unicode.MaxRune

// rng is an inclusive code point range.
type rng struct{ lo, hi rune }

// charset is a set of code points as sorted, disjoint, non-adjacent ranges.
type charset []rng

func csRune(r rune) charset       { return charset{{r, r}} }
func csRange(lo, hi rune) charset { return charset{{lo, hi}} }
func csRunes(rs ...rune) charset {
	var out charset
	for _, r := range rs {
		out = out.union(csRune(r))
	}
	return out
}

var csAll = charset{{0, maxRune}}

func normalize(rs []rng) charset {
	if len(rs) == 0 {
		return nil
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].lo < rs[j].lo })
	out := charset{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.lo <= last.hi+1 {
			if r.hi > last.hi {
				last.hi = r.hi
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

func (a charset) union(b charset) charset {
	all := make([]rng, 0, len(a)+len(b))
	all = append(all, a...)
	all = append(all, b...)
	return normalize(all)
}

func (a charset) negate() charset {
	var out charset
	next := rune(0)
	for _, r := range a {
		if r.lo > next {
			out = append(out, rng{next, r.lo - 1})
		}
		next = r.hi + 1
	}
	if next <= maxRune {
		out = append(out, rng{next, maxRune})
	}
	return out
}

func (a charset) intersect(b charset) charset {
	return a.negate().union(b.negate()).negate()
}

func (a charset) contains(r rune) bool {
	i := sort.Search(len(a), func(i int) bool { return a[i].hi >= r })
	return i < len(a) && a[i].lo <= r
}

func (a charset) isEmpty() bool { return len(a) == 0 }

// single reports the code point of a one-element set.
func (a charset) single() (rune, bool) {
	if len(a) == 1 && a[0].lo == a[0].hi {
		return a[0].lo, true
	}
	return 0, false
}

func csFromTable(t *unicode.RangeTable) charset {
	var rs []rng
	for _, r := range t.R16 {
		if r.Stride == 1 {
			rs = append(rs, rng{rune(r.Lo), rune(r.Hi)})
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			rs = append(rs, rng{c, c})
		}
	}
	for _, r := range t.R32 {
		if r.Stride == 1 {
			rs = append(rs, rng{rune(r.Lo), rune(r.Hi)})
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			rs = append(rs, rng{c, c})
		}
	}
	return normalize(rs)
}

func csFromTables(ts ...*unicode.RangeTable) charset {
	var out charset
	for _, t := range ts {
		out = out.union(csFromTable(t))
	}
	return out
}

// ---- case folding (Java semantics) ----

func asciiLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

func asciiUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}

func isASCIILetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

// asciiCaseClose adds the other ASCII case of every ASCII letter in the set:
// Java's CASE_INSENSITIVE without UNICODE_CASE.
func (a charset) asciiCaseClose() charset {
	var extra []rng
	for _, r := range a {
		lo, hi := max(r.lo, 'A'), min(r.hi, 'z')
		for c := lo; c <= hi; c++ {
			if isASCIILetter(c) {
				o := asciiLower(c)
				if o == c {
					o = asciiUpper(c)
				}
				extra = append(extra, rng{o, o})
			}
		}
	}
	return a.union(normalize(extra))
}

type casedRune struct{ r, upper, lower rune }

var casedRunes = sync.OnceValue(func() []casedRune {
	var out []casedRune
	for r := rune(0); r <= maxRune; r++ {
		u, l := unicode.ToUpper(r), unicode.ToLower(r)
		if u != r || l != r {
			out = append(out, casedRune{r, u, l})
		}
	}
	return out
})

// unicodeRangeCaseClose adds every character whose upper or lower case
// mapping is in the set: Java's case-insensitive range test with UNICODE_CASE.
func (a charset) unicodeRangeCaseClose() charset {
	var extra []rng
	for _, c := range casedRunes() {
		if a.contains(c.upper) || a.contains(c.lower) {
			extra = append(extra, rng{c.r, c.r})
		}
	}
	return a.union(normalize(extra))
}

// unicodeSingleCaseSet is the set a single character matches with
// CASE_INSENSITIVE|UNICODE_CASE: ch matches c when
// toLowerCase(toUpperCase(ch)) == toLowerCase(toUpperCase(c)).
func unicodeSingleCaseSet(c rune) charset {
	key := unicode.ToLower(unicode.ToUpper(c))
	rs := []rng{{c, c}, {key, key}}
	for _, cr := range casedRunes() {
		if unicode.ToLower(cr.upper) == key {
			rs = append(rs, rng{cr.r, cr.r})
		}
	}
	return normalize(rs)
}
