package match

import (
	"regexp"
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
// A text that keeps an expression's notation ("a(n)", "row(s)", "[[...]]")
// gets the line it means first, written out (see WrittenOut).
func (r *Registry) Suggest(text string, n int) []Suggestion {
	line, d, ok := r.WrittenOut(text)
	if !ok || n <= 0 {
		return r.closest(text, n)
	}
	out := []Suggestion{{Def: d, ID: d.Step.ID, Expr: line}}
	for _, s := range r.closest(text, n) {
		if s.Def != d && len(out) < n {
			out = append(out, s)
		}
	}
	return out
}

// optionalLetters is an expression's optional text in a word: a(n), row(s).
var optionalLetters = regexp.MustCompile(`([A-Za-z])\(([a-z]{1,3})\)`)

// WrittenOut is the line a step text that keeps an expression's notation
// means, when it matches exactly one step once written out: "a(n)" as "a"
// or "an" (by the word after it), "row(s)" as "row" or, after a number
// other than 1, "rows", and "[[...]]" as the words inside. Agents copy
// expressions from a list of steps, notation and all.
func (r *Registry) WrittenOut(text string) (string, *Def, bool) {
	if !strings.Contains(text, "(") && !strings.Contains(text, "[[") {
		return "", nil, false
	}
	t := strings.NewReplacer("[[", "", "]]", "").Replace(text)
	t = optionalLetters.ReplaceAllStringFunc(t, func(m string) string {
		return m[:1] + "\x00" + m[2:len(m)-1] + "\x01"
	})
	// Each optional group, kept or dropped as the words around it read.
	var b strings.Builder
	for i := 0; i < len(t); i++ {
		if t[i] != '\x00' {
			b.WriteByte(t[i])
			continue
		}
		end := strings.IndexByte(t[i:], '\x01') + i
		letters := t[i+1 : end]
		prev := lastWord(b.String())
		next := firstWord(t[end+1:])
		keep := false
		switch {
		case strings.EqualFold(prev, "a") && letters == "n":
			keep = startsWithVowel(next)
		case letters == "s" || letters == "es":
			keep = afterNumberOtherThanOne(b.String())
		}
		if keep {
			b.WriteString(letters)
		}
		i = end
	}
	line := strings.Join(strings.Fields(b.String()), " ")
	if line == strings.Join(strings.Fields(text), " ") {
		return "", nil, false
	}
	if ms := r.Match(line); len(ms) == 1 {
		return line, ms[0].Def(), true
	}
	return "", nil, false
}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return strings.Trim(f[0], "'\"")
}

func startsWithVowel(w string) bool {
	return w != "" && strings.ContainsRune("aeiouAEIOU", rune(w[0]))
}

// afterNumberOtherThanOne reports whether the text before a word ends with
// a number other than 1 (2 rows, 0 documents).
func afterNumberOtherThanOne(before string) bool {
	f := strings.Fields(before)
	if len(f) < 2 {
		return false
	}
	n := f[len(f)-2]
	if strings.IndexFunc(n, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return false
	}
	return n != "1"
}

// closest returns up to n expressions closest to text by token distance.
func (r *Registry) closest(text string, n int) []Suggestion {
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
