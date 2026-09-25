package match

import (
	"sort"
	"strings"
	"unicode"
)

// Suggestion is a close-but-not-matching step expression.
type Suggestion struct {
	Def      *Def   `json:"-"`
	ID       string `json:"id"`
	Expr     string `json:"expr"`
	Distance int    `json:"distance"`
}

// Suggest returns up to n expressions closest to an undefined step text,
// using token-level edit distance where each {param} matches any one token.
func (r *Registry) Suggest(text string, n int) []Suggestion {
	tt := tokenize(text)
	best := map[*Def]Suggestion{}
	for _, v := range r.variants {
		et := exprTokens(v.Expr)
		d := tokenDistance(tt, et)
		// Beyond ~40% of the longer token sequence a suggestion is noise.
		if d > max(2, (max(len(tt), len(et))*2+4)/5) {
			continue
		}
		if cur, ok := best[v.Def]; !ok || d < cur.Distance {
			best[v.Def] = Suggestion{Def: v.Def, ID: v.Def.Step.ID, Expr: v.Expr, Distance: d}
		}
	}
	out := make([]Suggestion, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		return out[i].Expr < out[j].Expr
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

const wildcard = "\x00param"

func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	inQuote := rune(0)
	flush := func() {
		if b.Len() > 0 {
			out = append(out, strings.ToLower(b.String()))
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case inQuote != 0:
			b.WriteRune(r)
			if r == inQuote {
				inQuote = 0
				flush()
			}
		case r == '"' || r == '\'':
			flush()
			inQuote = r
			b.WriteRune(r)
		case unicode.IsSpace(r):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// exprTokens tokenizes an expression, turning {param} into wildcards and
// dropping optional-text markers like "(s)".
func exprTokens(expr string) []string {
	var out []string
	for _, tok := range strings.Fields(expr) {
		switch {
		case strings.HasPrefix(tok, "{") && strings.HasSuffix(tok, "}"):
			out = append(out, wildcard)
		default:
			tok = stripOptional(tok)
			if strings.Contains(tok, "/") && !strings.HasPrefix(tok, "/") {
				tok = strings.SplitN(tok, "/", 2)[0]
			}
			if tok != "" {
				out = append(out, strings.ToLower(tok))
			}
		}
	}
	return out
}

func stripOptional(tok string) string {
	var b strings.Builder
	depth := 0
	for _, r := range tok {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func tokenDistance(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if b[j-1] == wildcard || a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
