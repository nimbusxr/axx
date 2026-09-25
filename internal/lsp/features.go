package lsp

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/internal/match"
)

// maxCompletions caps a completion list; the list is marked incomplete, so
// the editor asks again as the user types.
const maxCompletions = 200

// hoverAt documents the step under the cursor: the step it matches with its
// documentation and the values of its parameters, or why it matches none.
func (d *document) hoverAt(reg *match.Registry, pos position) *hover {
	st, ok := d.stepAt(pos.Line)
	if !ok || reg == nil {
		return nil
	}
	ms := reg.Match(d.matchText(st))
	var b strings.Builder
	switch len(ms) {
	case 0:
		b.WriteString("**Undefined step.** No step matches this text.")
		if sugg := reg.Suggest(d.matchText(st), 3); len(sugg) > 0 {
			b.WriteString("\n\nDid you mean:\n")
			for _, s := range sugg {
				fmt.Fprintf(&b, "\n- `%s`", s.Expr)
			}
		}
	case 1:
		writeStepDoc(&b, ms[0], st.outline)
	default:
		b.WriteString("**Ambiguous step.** More than one step matches:\n")
		for _, m := range ms {
			fmt.Fprintf(&b, "\n- `%s`: `%s`", m.Def().Step.ID, m.Variant.Expr)
		}
	}
	r := d.textRange(st)
	return &hover{Contents: markupContent{Kind: "markdown", Value: b.String()}, Range: &r}
}

func writeStepDoc(b *strings.Builder, m match.Match, outline bool) {
	s := m.Def().Step
	fmt.Fprintf(b, "**`%s`** · %s\n\n```gherkin\n%s %s\n```\n", s.ID, m.Def().Pack, keywordOr(s.Keyword), m.Variant.Expr)
	if s.Doc != "" {
		fmt.Fprintf(b, "\n%s\n", strings.TrimSpace(s.Doc))
	}
	var rows []string
	for _, a := range m.Args {
		if a.Present {
			rows = append(rows, fmt.Sprintf("| `{%s}` | `%s` |", a.Param, strings.ReplaceAll(a.Raw, "|", `\|`)))
		}
	}
	if len(rows) > 0 {
		label := "Value"
		if outline {
			label = "Value (first example row)"
		}
		fmt.Fprintf(b, "\n| Parameter | %s |\n|---|---|\n%s\n", label, strings.Join(rows, "\n"))
	}
	if len(s.Examples) > 0 {
		fmt.Fprintf(b, "\n**Example:** `%s`\n", s.Examples[0])
	}
}

func keywordOr(kw string) string {
	if kw == "" {
		return "*"
	}
	return kw
}

// completionsAt offers what can be typed at the cursor: the files and
// directories that complete a value that names a file, and the steps whose
// text continues what is typed after the keyword. A slash completes paths
// only.
func (d *document) completionsAt(pr *project, pos position, slash bool) completionList {
	list := completionList{IsIncomplete: true, Items: []completionItem{}}
	paths, ok := d.pathCompletions(pr, pos)
	if ok {
		list.Items = append(list.Items, paths...)
	}
	if slash {
		return list
	}
	steps := d.stepCompletions(pr.reg, pos)
	list.Items = append(list.Items, steps.Items...)
	return list
}

// stepCompletions offers the steps whose text continues what is typed after
// the keyword on the cursor's line.
func (d *document) stepCompletions(reg *match.Registry, pos position) completionList {
	list := completionList{IsIncomplete: true, Items: []completionItem{}}
	st, ok := d.stepAt(pos.Line)
	if !ok || reg == nil {
		return list
	}
	line := d.lines[pos.Line]
	cursor := byteOffset(line, pos.Character)
	if cursor < st.textStart {
		return list
	}
	typed := line[st.textStart:cursor]
	after := ""
	if end := st.textStart + len(st.text); cursor < end {
		after = line[cursor:end]
	}
	want := d.effectiveKeyword(st)
	// Completions insert at the cursor and keep everything already written.
	edit := span(d.lines, st.line, st.textStart, cursor)
	for i, v := range reg.Variants() {
		ok, literal := continues(v.Expr, typed)
		if !ok {
			continue
		}
		rest := completion(v.Expr, typed, after)
		if rest == "" {
			continue // the step is already written out
		}
		s := v.Def.Step
		rank := 1
		if s.Keyword == "" || s.Keyword == want {
			rank = 0
		}
		item := completionItem{
			Label:  v.Expr,
			Kind:   completionKindSnippet,
			Detail: s.ID + " (" + v.Def.Pack + ")",
			// The server filters; the editor keeps what it sends.
			FilterText: typed,
			// Steps whose words match what is typed come first, then steps of
			// the scenario's current keyword, then the shorter forms.
			SortText:         fmt.Sprintf("%03d%d%02d%05d", 999-literal, rank, strings.Count(v.Expr, "{"), i),
			InsertTextFormat: insertFormatSnippet,
			TextEdit:         textEdit{Range: edit, NewText: escapeSnippet(typed) + rest},
		}
		if s.Doc != "" || len(s.Examples) > 0 {
			doc := strings.TrimSpace(s.Doc)
			if len(s.Examples) > 0 {
				doc += "\n\n**Example:** `" + s.Examples[0] + "`"
			}
			item.Documentation = &markupContent{Kind: "markdown", Value: strings.TrimSpace(doc)}
		}
		list.Items = append(list.Items, item)
		if len(list.Items) == maxCompletions {
			break
		}
	}
	return list
}

// effectiveKeyword is Given, When or Then for a step, following And, But
// and * back to the keyword they continue.
func (d *document) effectiveKeyword(st stepLine) string {
	for i := len(d.steps) - 1; i >= 0; i-- {
		s := d.steps[i]
		if s.line > st.line {
			continue
		}
		switch s.keyword {
		case "Given", "When", "Then":
			return s.keyword
		}
	}
	return ""
}

// continues reports whether a step expression can continue the typed
// text: every typed word matches the expression's word at that position (a
// {parameter} matches any value), and a last, unfinished word may be the
// start of one. It also counts the typed words that matched literal words,
// which ranks the step.
func continues(expr, typed string) (bool, int) {
	words := strings.Fields(expr)
	got := typedWords(typed)
	open := typed != "" && !strings.HasSuffix(typed, " ") && !strings.HasSuffix(typed, "\t")
	if len(got) > len(words) {
		return false, 0
	}
	literal := 0
	for i, g := range got {
		w := words[i]
		if isParam(w) {
			continue
		}
		partial := open && i == len(got)-1
		if !wordMatches(w, g, partial) {
			return false, 0
		}
		literal++
	}
	return true, literal
}

// typedWords splits typed text into words, keeping a quoted value (which
// may contain spaces) as one word.
func typedWords(s string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	for _, r := range s {
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
			cur.WriteRune(r)
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func isParam(w string) bool { return strings.HasPrefix(w, "{") && strings.HasSuffix(w, "}") }

// wordMatches compares a typed word with an expression word, whose optional
// text in parentheses may be left out: time(s) matches time and times.
func wordMatches(w, typed string, partial bool) bool {
	typed = strings.ToLower(typed)
	for _, alt := range optionalForms(strings.ToLower(w)) {
		if typed == alt || partial && strings.HasPrefix(alt, typed) {
			return true
		}
	}
	return false
}

// optionalForms expands Cucumber optional text: "time(s)" is "time" or
// "times", "a(n)" is "a" or "an".
func optionalForms(w string) []string {
	open := strings.IndexByte(w, '(')
	closing := strings.IndexByte(w, ')')
	if open < 0 || closing < open {
		return []string{w}
	}
	without := w[:open] + w[closing+1:]
	with := w[:open] + w[open+1:closing] + w[closing+1:]
	return []string{without, with}
}

// completion is the snippet text that completes typed (which continues
// expr) into the step: the rest of the word being typed, then the step's
// remaining words, with its parameters as numbered placeholders. What is
// typed is kept as it is. With text after the cursor, only the word at the
// cursor is completed, and only when the cursor ends it.
func completion(expr, typed, after string) string {
	words := strings.Fields(expr)
	got := typedWords(typed)
	open := typed != "" && !strings.HasSuffix(typed, " ") && !strings.HasSuffix(typed, "\t")
	var b strings.Builder
	if open && len(got) > 0 && !startsWord(after) {
		if w := words[len(got)-1]; !isParam(w) {
			last := got[len(got)-1]
			for _, form := range preferredForms(w) {
				if len(form) >= len(last) && strings.EqualFold(form[:len(last)], last) {
					b.WriteString(escapeSnippet(form[len(last):]))
					break
				}
			}
		}
	}
	if strings.TrimSpace(after) != "" {
		return b.String()
	}
	next := len(got)
	n := 0
	for i, w := range words[next:] {
		if i > 0 || open {
			b.WriteByte(' ')
		}
		if isParam(w) {
			n++
			fmt.Fprintf(&b, "${%d:%s}", n, strings.Trim(w, "{}"))
			continue
		}
		b.WriteString(escapeSnippet(preferredForms(w)[0]))
	}
	return b.String()
}

// startsWord reports whether s begins with a word character, which means
// the cursor is inside a word.
func startsWord(s string) bool { return s != "" && s[0] != ' ' && s[0] != '\t' }

// preferredForms lists the written forms of an expression word, the usual
// one first: the plural of time(s), the "a" of a(n).
func preferredForms(w string) []string {
	forms := optionalForms(w)
	if len(forms) == 2 && !strings.HasPrefix(w, "a(n)") {
		forms[0], forms[1] = forms[1], forms[0]
	}
	return forms
}

// snippet turns an expression into snippet text: each {parameter} becomes a
// numbered placeholder, and optional text takes its usual form.
func snippet(expr string) string { return completion(expr, "", "") }

func escapeSnippet(s string) string {
	return strings.NewReplacer(`\`, `\\`, `$`, `\$`, `}`, `\}`).Replace(s)
}
