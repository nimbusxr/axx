package lsp

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/featurefmt"
)

// formattingParams are textDocument/formatting's and, with a range,
// textDocument/rangeFormatting's. Their options (tab size, spaces) do not
// apply: features have the one layout.
type formattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        *textRange             `json:"range"`
}

// formatRequest answers a formatting request for a document whose text is
// text: the edits that lay it out, those of the lines in its range for
// rangeFormatting.
func formatRequest(p formattingParams, text string) ([]textEdit, *rpcError) {
	edits, err := formatEdits(p.TextDocument.URI, text)
	if err != nil || p.Range == nil {
		return edits, err
	}
	return inRange(edits, *p.Range), nil
}

// lastLine is the last line a range covers: one that ends at the start of
// a line leaves that line out.
func lastLine(r textRange) int {
	if r.End.Character == 0 && r.End.Line > r.Start.Line {
		return r.End.Line - 1
	}
	return r.End.Line
}

// inRange are the edits that change lines of r.
func inRange(edits []textEdit, r textRange) []textEdit {
	out := []textEdit{}
	for _, e := range edits {
		if lastLine(e.Range) >= r.Start.Line && e.Range.Start.Line <= lastLine(r) {
			out = append(out, e)
		}
	}
	return out
}

// formatEdits are the edits that lay out a feature's text as `axx fmt`
// does: none for a feature that does not parse, whose diagnostics say why,
// and an error for a layout axx refuses.
func formatEdits(uri, text string) ([]textEdit, *rpcError) {
	name := uri
	if p := uriToPath(uri); p != "" {
		name = filepath.Base(p)
	}
	out, err := featurefmt.Format(name, []byte(text))
	if err != nil {
		var ae *axxerr.Error
		if errors.As(err, &ae) && ae.Code == featurefmt.CodeParse {
			return []textEdit{}, nil
		}
		return nil, &rpcError{Code: codeRequestFailed, Message: err.Error()}
	}
	return lineEdits(text, string(out)), nil
}

// lineEdits turns text into out, replacing whole lines: only those that
// change, so that an editor keeps its cursor and its marks on the others.
func lineEdits(text, out string) []textEdit {
	if text == out {
		return []textEdit{}
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\r") {
		// A carriage return alone ends a line for LSP, and not for Gherkin.
		return []textEdit{{Range: textRange{End: documentEnd(text)}, NewText: out}}
	}
	old, next := lineItems(text), lineItems(out)
	end := func(i int) position {
		if i < len(old) {
			return position{Line: i}
		}
		return documentEnd(text)
	}
	var edits []textEdit
	for _, h := range hunks(old, next) {
		edits = append(edits, textEdit{
			Range:   textRange{Start: position{Line: h.oldFrom}, End: end(h.oldTo)},
			NewText: strings.Join(next[h.newFrom:h.newTo], ""),
		})
	}
	return edits
}

// lineItems splits s into lines with their line breaks; the last has none
// when s does not end with one.
func lineItems(s string) []string {
	var items []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			items = append(items, s)
			break
		}
		items = append(items, s[:i+1])
		s = s[i+1:]
	}
	return items
}

// documentEnd is the position after the last character of text.
func documentEnd(text string) position {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	i := strings.LastIndexByte(text, '\n')
	return position{Line: strings.Count(text, "\n"), Character: utf16Len(text[i+1:])}
}

type hunk struct{ oldFrom, oldTo, newFrom, newTo int }

// hunks are the runs of lines of a that b replaces: a longest common
// subsequence of lines between the lines both start and end with.
func hunks(a, b []string) []hunk {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if len(ma) == 0 && len(mb) == 0 {
		return nil
	}
	whole := []hunk{{pre, len(a) - suf, pre, len(b) - suf}}
	if len(ma) == 0 || len(mb) == 0 || len(ma)*len(mb) > 1<<20 {
		return whole
	}
	// lcs[i][j] is the length of the longest common subsequence of ma[i:]
	// and mb[j:].
	lcs := make([][]int, len(ma)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(mb)+1)
	}
	for i := len(ma) - 1; i >= 0; i-- {
		for j := len(mb) - 1; j >= 0; j-- {
			if ma[i] == mb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []hunk
	i, j := 0, 0
	open := false
	var h hunk
	flush := func() {
		if open {
			h.oldTo, h.newTo = pre+i, pre+j
			out = append(out, h)
			open = false
		}
	}
	for i < len(ma) || j < len(mb) {
		switch {
		case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
			flush()
			i++
			j++
			continue
		case !open:
			h = hunk{oldFrom: pre + i, newFrom: pre + j}
			open = true
		}
		if j < len(mb) && (i == len(ma) || lcs[i][j+1] >= lcs[i+1][j]) {
			j++
		} else {
			i++
		}
	}
	flush()
	return out
}
