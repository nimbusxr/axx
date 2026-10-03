// Package stepsearch finds steps by what they do, for `axx steps search` and
// the MCP server's steps_search: one ranking for both.
//
// A query is words of intent ("response status", "rows in table"). Each word
// counts once, matched as a whole word (plurals fold: "rows" finds "row")
// against the step's expression, its id and its documentation, in that
// order of weight; words like "the", "with" and "step" carry nothing. A step
// matches strongly when a word is in its expression or its id; one that only
// its documentation mentions is a weak match, and a search with no strong
// match says so.
package stepsearch

import (
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Step is what the search reads of a step.
type Step struct {
	ID   string
	Pack string
	Expr string
	Doc  string
}

// Result is a ranked step: its index in the searched slice.
type Result struct {
	Index  int
	Score  int
	Strong bool
}

// stopwords carry no intent.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true, "to": true, "in": true, "on": true,
	"for": true, "with": true, "by": true, "from": true, "at": true, "as": true, "is": true, "are": true, "be": true,
	"it": true, "its": true, "that": true, "this": true, "into": true, "using": true, "use": true, "via": true,
	"step": true, "steps": true, "given": true, "when": true, "then": true, "how": true, "do": true, "i": true,
}

// synonyms are words agents use for what the steps call otherwise: a
// request's body is its payload, a JSON field a property.
var synonyms = map[string][]string{
	"body": {"payload"}, "payload": {"body"}, "json": {"payload"}, "field": {"property"}, "attribute": {"property"},
	"key": {"property"}, "value": {"property"}, "insert": {"seed"}, "seed": {"insert"}, "fixture": {"seed"},
	"database": {"db"}, "db": {"database"}, "sql": {"database"}, "query": {"selection"}, "select": {"selection"},
	"row": {"selection"}, "message": {"event"}, "event": {"message"}, "publish": {"published"}, "produce": {"published"},
	"consume": {"consumed"}, "mock": {"mocked"}, "stub": {"mocked"}, "call": {"request"}, "code": {"status"},
	"http": {"request", "response"}, "api": {"request", "response"}, "send": {"request"}, "wait": {"within"},
	"eventually": {"within"}, "poll": {"within"}, "contain": {"contains"}, "wiremock": {"mocked"},
	"mongodb": {"mongo"}, "document": {"mongo"}, "collection": {"mongo"}, "topic": {"kafka"},
}

// Words splits text into lower-case words, plurals folded, without
// stopwords.
func Words(text string) []string {
	var out []string
	for _, w := range rawWords(text) {
		out = append(out, fold(w))
	}
	return out
}

// rawWords are text's lower-case words, without stopwords, as written.
func rawWords(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !stopwords[w] {
			out = append(out, w)
		}
	}
	return out
}

// fold makes a plural singular: rows → row, entries → entry, headers →
// header (but not "status" or "class").
func fold(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us"):
		return w[:len(w)-1]
	}
	return w
}

func set(words []string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// exprWords are an expression's words, without its parameter names.
func exprWords(expr string) map[string]bool {
	var b strings.Builder
	depth := 0
	for _, r := range expr {
		switch {
		case r == '{':
			depth++
		case r == '}':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return set(Words(b.String()))
}

// Search ranks steps by how well q matches them, best first, leaving out
// those it does not match at all. strong reports whether any result matches
// strongly.
func Search(steps []Step, q string) (results []Result, strong bool) {
	words := Words(q)
	if len(words) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	var unique []string
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			unique = append(unique, w)
		}
	}
	asked := map[string]bool{}
	for _, w := range unique {
		asked[w] = true
		for _, x := range synonyms[w] {
			asked[x] = true
		}
	}
	marks := markers(steps)
	asWritten := rawWords(q)
	for i, s := range steps {
		expr, id, doc := exprWords(s.Expr), set(Words(s.ID)), set(Words(s.Doc))
		score, matched := 0, false
		// The words as the query writes them, plurals and all, settle a tie
		// ("properties" between the property and the properties steps).
		if raw := set(rawWords(s.Expr)); len(asWritten) > 0 {
			for _, w := range asWritten {
				if fold(w) != w && raw[w] {
					score++
				}
			}
		}
		// A step of a pack the query does not name (a mocked request, a
		// kafka event) ranks after the steps the words fit as well.
		if m := marks[s.Pack]; len(m) > 0 && !slices.ContainsFunc(m, func(w string) bool { return asked[w] }) {
			score -= 5
		}
		for _, w := range unique {
			switch {
			case expr[w]:
				score += 5
				matched = true
			case id[w]:
				score += 4
				matched = true
			case slices.ContainsFunc(synonyms[w], func(x string) bool { return expr[x] || id[x] }):
				score += 4
				matched = true
			case doc[w]:
				score++
			}
			if id[w] && expr[w] {
				score++ // the step is about it
			}
		}
		if score > 0 || matched {
			results = append(results, Result{Index: i, Score: score, Strong: matched})
			strong = strong || matched
		}
	}
	sort.SliceStable(results, func(a, b int) bool {
		ra, rb := results[a], results[b]
		if ra.Strong != rb.Strong {
			return ra.Strong
		}
		if ra.Score != rb.Score {
			return ra.Score > rb.Score
		}
		return len(steps[ra.Index].ID) < len(steps[rb.Index].ID)
	})
	return results, strong
}

// markers are the words that mark a pack's steps: in nearly all of its
// steps' expressions and few of the others' ("mocked", "kafka").
func markers(steps []Step) map[string][]string {
	inPack := map[string]map[string]int{}
	total, size := map[string]int{}, map[string]int{}
	for _, s := range steps {
		if inPack[s.Pack] == nil {
			inPack[s.Pack] = map[string]int{}
		}
		size[s.Pack]++
		for w := range exprWords(s.Expr) {
			inPack[s.Pack][w]++
			total[w]++
		}
	}
	out := map[string][]string{}
	for pack, words := range inPack {
		others := len(steps) - size[pack]
		for w, n := range words {
			if size[pack] >= 3 && n*10 >= size[pack]*9 && (total[w]-n)*10 <= others {
				out[pack] = append(out[pack], w)
			}
		}
		sort.Strings(out[pack])
	}
	return out
}

// TwinSuffix ends the id of a step's named-service twin: rest.request.header
// and rest.request.header.on (the same step "… on {service}").
const TwinSuffix = ".on"

// Twins maps the id of each named-service twin to the id of its step, among
// ids.
func Twins(ids []string) map[string]string {
	has := set(ids)
	out := map[string]string{}
	for _, id := range ids {
		if base, ok := strings.CutSuffix(id, TwinSuffix); ok && has[base] {
			out[id] = base
		}
	}
	return out
}

// Entry is a step as a catalog lists it.
type Entry struct {
	ID, Pack, Expr string
	// Arg is the step's argument: none, table or docstring.
	Arg string
	// Columns name the columns of its data table.
	Columns []string
}

// Tail is what follows an entry's expression: the data table and its
// columns, or the doc string.
func (e Entry) Tail() string {
	switch e.Arg {
	case "table":
		if len(e.Columns) > 0 {
			return "  + table | " + strings.Join(e.Columns, " | ") + " |"
		}
		return "  + table"
	case "docstring":
		return "  + doc string"
	}
	return ""
}

// Fold leaves out the named-service twins of the entries whose step is
// there too, and reports the ids of those steps.
func Fold(entries []Entry) (kept []Entry, twinned map[string]bool) {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	twins := Twins(ids)
	twinned = map[string]bool{}
	for _, e := range entries {
		if base, ok := twins[e.ID]; ok {
			twinned[base] = true
			continue
		}
		kept = append(kept, e)
	}
	return kept, twinned
}

// TwinMark follows a step whose named-service twin is folded into it, and
// TwinLegend says what it means.
const (
	TwinMark   = "  (+ .on)"
	TwinLegend = "(+ .on): the step also has an `<id>.on` form, for a named service (`… on {service}`)"
)

// Catalog renders every step, one line each, by pack: the cheapest way for
// an agent to see all the steps there are.
func Catalog(entries []Entry) string {
	kept, twinned := Fold(entries)
	var b strings.Builder
	var pack string
	for _, e := range kept {
		if e.Pack != pack {
			if pack != "" {
				b.WriteString("\n")
			}
			pack = e.Pack
			b.WriteString(pack + "\n")
		}
		b.WriteString("  " + e.ID + "  " + e.Expr + e.Tail())
		if twinned[e.ID] {
			b.WriteString(TwinMark)
		}
		b.WriteString("\n")
	}
	if len(twinned) > 0 {
		b.WriteString("\n" + TwinLegend + "\n")
	}
	return b.String()
}
