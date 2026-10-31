package host

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines a hunk shows around a change.
const diffContext = 3

// Diff is a unified diff from the approved definition's text to the
// proposed one's.
func Diff(approved, proposed string) string {
	a, b := splitLines(approved), splitLines(proposed)
	ops := editScript(a, b)
	var out strings.Builder
	out.WriteString("--- approved\n+++ proposed\n")
	for start := 0; start < len(ops); {
		// The next change, and the hunk around it.
		first := start
		for first < len(ops) && ops[first].kind == ' ' {
			first++
		}
		if first == len(ops) {
			break
		}
		from := max(first-diffContext, start)
		end := first
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*diffContext {
				end = min(end+diffContext, run)
				break
			}
			end = run
		}
		aLine, bLine := 1, 1
		for _, op := range ops[:from] {
			if op.kind != '+' {
				aLine++
			}
			if op.kind != '-' {
				bLine++
			}
		}
		var aN, bN int
		for _, op := range ops[from:end] {
			if op.kind != '+' {
				aN++
			}
			if op.kind != '-' {
				bN++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aLine, aN, bLine, bN)
		for _, op := range ops[from:end] {
			out.WriteByte(op.kind)
			out.WriteString(op.text)
			out.WriteByte('\n')
		}
		start = end
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

type diffOp struct {
	kind byte // ' ', '-' or '+'
	text string
}

// maxLCSLines bounds each side of the table of common subsequences
// editScript fills (at most 16 MB): a differing middle of more lines is
// replaced as a whole.
const maxLCSLines = 2000

// editScript is a line edit script from a to b: the lines they start and
// end with alike kept, and between them a shortest one, or, for a middle
// too long to compare line by line, its lines removed and added.
func editScript(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []diffOp
	for _, s := range a[:pre] {
		ops = append(ops, diffOp{' ', s})
	}
	ops = append(ops, middleScript(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, s := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', s})
	}
	return ops
}

// middleScript is a shortest edit script from a to b, from the table of
// their common subsequences, when neither has more than maxLCSLines.
func middleScript(a, b []string) []diffOp {
	if len(a) > maxLCSLines {
		return replaced(a, b)
	}
	if len(b) > maxLCSLines {
		return replaced(a, b)
	}
	n, m := len(a), len(b)
	var ops []diffOp
	w := m + 1
	t := make([]int32, (n+1)*w) // t[i*w+j]: the longest common subsequence of a[i:] and b[j:]
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			} else {
				t[i*w+j] = max(t[(i+1)*w+j], t[i*w+j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case t[(i+1)*w+j] >= t[i*w+j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// replaced is a's lines removed and b's added.
func replaced(a, b []string) []diffOp {
	var ops []diffOp
	for _, s := range a {
		ops = append(ops, diffOp{'-', s})
	}
	for _, s := range b {
		ops = append(ops, diffOp{'+', s})
	}
	return ops
}
