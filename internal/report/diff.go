package report

import (
	"strconv"
)

// maxLCSCells bounds the LCS table; larger inputs fall back to replacing
// the whole differing middle section.
const maxLCSCells = 4 << 20

type diffOp struct {
	kind byte // ' ', '-' or '+'
	text string
}

// editScript returns a line edit script turning a into b.
func editScript(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	am, bm := a[pre:len(a)-suf], b[pre:len(b)-suf]
	ops := make([]diffOp, 0, len(a)+len(b))
	for _, s := range a[:pre] {
		ops = append(ops, diffOp{' ', s})
	}
	n, m := len(am), len(bm)
	if n*m > maxLCSCells {
		for _, s := range am {
			ops = append(ops, diffOp{'-', s})
		}
		for _, s := range bm {
			ops = append(ops, diffOp{'+', s})
		}
	} else {
		// t[i*(m+1)+j] is the LCS length of am[i:] and bm[j:].
		w := m + 1
		t := make([]int32, (n+1)*w)
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if am[i] == bm[j] {
					t[i*w+j] = t[(i+1)*w+j+1] + 1
				} else {
					t[i*w+j] = max(t[(i+1)*w+j], t[i*w+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case am[i] == bm[j]:
				ops = append(ops, diffOp{' ', am[i]})
				i++
				j++
			case t[(i+1)*w+j] >= t[i*w+j+1]:
				ops = append(ops, diffOp{'-', am[i]})
				i++
			default:
				ops = append(ops, diffOp{'+', bm[j]})
				j++
			}
		}
		for ; i < n; i++ {
			ops = append(ops, diffOp{'-', am[i]})
		}
		for ; j < m; j++ {
			ops = append(ops, diffOp{'+', bm[j]})
		}
	}
	for _, s := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', s})
	}
	return ops
}

// unifiedDiff renders the hunks of a unified diff (without file headers)
// with the given number of context lines. It returns nil when a equals b.
func unifiedDiff(a, b []string, context int) []string {
	ops := editScript(a, b)
	// aPos[i]/bPos[i] count the lines of a/b consumed before ops[i].
	aPos := make([]int, len(ops)+1)
	bPos := make([]int, len(ops)+1)
	for i, op := range ops {
		aPos[i+1], bPos[i+1] = aPos[i], bPos[i]
		if op.kind != '+' {
			aPos[i+1]++
		}
		if op.kind != '-' {
			bPos[i+1]++
		}
	}
	var out []string
	i := 0
	for {
		for i < len(ops) && ops[i].kind == ' ' {
			i++
		}
		if i == len(ops) {
			return out
		}
		start := max(0, i-context)
		end := i
		for {
			for end < len(ops) && ops[end].kind != ' ' {
				end++
			}
			k := end
			for k < len(ops) && ops[k].kind == ' ' {
				k++
			}
			if k < len(ops) && k-end <= 2*context {
				end = k
				continue
			}
			end = min(len(ops), end+context)
			break
		}
		out = append(out, "@@ -"+hunkRange(aPos[start], aPos[end]-aPos[start])+" +"+hunkRange(bPos[start], bPos[end]-bPos[start])+" @@")
		for _, op := range ops[start:end] {
			out = append(out, string(op.kind)+op.text)
		}
		i = end
	}
}

// hunkRange formats "start,len" with 1-based line numbers; an empty range
// names the line before it, as diff(1) does.
func hunkRange(start, n int) string {
	if n == 0 {
		return strconv.Itoa(start) + ",0"
	}
	if n == 1 {
		return strconv.Itoa(start + 1)
	}
	return strconv.Itoa(start+1) + "," + strconv.Itoa(n)
}
