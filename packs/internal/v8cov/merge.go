package v8cov

import (
	"fmt"
	"slices"
	"sort"
)

// Merging V8 coverage, as @bcoe/v8-coverage's mergeScriptCovs does (what c8
// does before converting): the takes of one script (several takes of a
// page, pages, scenarios) sum into one coverage. Coverage is summed on V8's
// range trees, not on lines: a take only reports what ran since the one
// before, and converting it on its own would count the lines it does not
// mention as covered.

// Merge merges the takes of one script (the same URL and source) into one
// coverage, normalized. It leaves the takes as they are: a take may be
// given more than once.
func Merge(takes [][]Function) []Function {
	switch len(takes) {
	case 0:
		return nil
	case 1:
		out := make([]Function, len(takes[0]))
		for i, f := range takes[0] {
			out[i] = normalized(f)
		}
		sortFunctions(out)
		return out
	}
	// The functions of the takes, by their own range, in the order they come.
	type group struct{ funcs []Function }
	var order []string
	groups := map[string]*group{}
	for _, take := range takes {
		for _, f := range take {
			if len(f.Ranges) == 0 {
				continue
			}
			key := fmt.Sprintf("%d;%d", f.Ranges[0].StartOffset, f.Ranges[0].EndOffset)
			g, ok := groups[key]
			switch {
			case !ok:
				g = &group{}
				groups[key] = g
				order = append(order, key)
			case !g.funcs[0].IsBlockCoverage && f.IsBlockCoverage:
				// Block granularity wins over function granularity.
				g.funcs = nil
			case g.funcs[0].IsBlockCoverage && !f.IsBlockCoverage:
				continue
			}
			g.funcs = append(g.funcs, f)
		}
	}
	out := make([]Function, 0, len(order))
	for _, k := range order {
		out = append(out, mergeFunctions(groups[k].funcs))
	}
	sortFunctions(out)
	return out
}

// MergeCounted is Merge of takes that came several times, times[i] the
// times of takes[i], without repeating them: a function the same in each
// take merges as one with its counts multiplied, which is what merging its
// copies makes.
func MergeCounted(takes [][]Function, times []int) []Function {
	total := 0
	for _, n := range times {
		total += n
	}
	if total <= 1 {
		for i, n := range times {
			if n == 1 {
				return Merge(takes[i : i+1])
			}
		}
		return nil
	}
	type counted struct {
		f     Function
		times int
	}
	type group struct{ funcs []counted }
	var order []string
	groups := map[string]*group{}
	for i, take := range takes {
		if times[i] == 0 {
			continue
		}
		for _, f := range take {
			if len(f.Ranges) == 0 {
				continue
			}
			key := fmt.Sprintf("%d;%d", f.Ranges[0].StartOffset, f.Ranges[0].EndOffset)
			g, ok := groups[key]
			switch {
			case !ok:
				g = &group{}
				groups[key] = g
				order = append(order, key)
			case !g.funcs[0].f.IsBlockCoverage && f.IsBlockCoverage:
				g.funcs = nil
			case g.funcs[0].f.IsBlockCoverage && !f.IsBlockCoverage:
				continue
			}
			g.funcs = append(g.funcs, counted{f, times[i]})
		}
	}
	out := make([]Function, 0, len(order))
	for _, k := range order {
		g := groups[k].funcs
		if len(g) == 1 && g[0].times == 1 {
			out = append(out, normalized(g[0].f))
			continue
		}
		// Copies of a tree merge into the tree with its counts multiplied.
		trees := make([]*rangeTree, len(g))
		for i, c := range g {
			trees[i] = treeFromSortedRanges(c.f.Ranges)
			trees[i].scale(c.times)
		}
		merged := mergeRangeTrees(trees)
		merged.normalize()
		ranges := merged.toRanges()
		out = append(out, Function{
			FunctionName:    g[0].f.FunctionName,
			Ranges:          ranges,
			IsBlockCoverage: len(ranges) != 1 || ranges[0].Count != 0,
		})
	}
	sortFunctions(out)
	return out
}

// scale multiplies the tree's counts.
func (t *rangeTree) scale(n int) {
	t.delta *= n
	for _, c := range t.children {
		c.scale(n)
	}
}

func compareRanges(a, b Range) int {
	if a.StartOffset != b.StartOffset {
		return a.StartOffset - b.StartOffset
	}
	return b.EndOffset - a.EndOffset
}

func sortFunctions(fs []Function) {
	sort.SliceStable(fs, func(i, j int) bool { return compareRanges(fs[i].Ranges[0], fs[j].Ranges[0]) < 0 })
}

// normalized is a function's coverage normalized, in ranges of its own.
func normalized(f Function) Function {
	f.Ranges = slices.Clone(f.Ranges)
	sort.SliceStable(f.Ranges, func(i, j int) bool { return compareRanges(f.Ranges[i], f.Ranges[j]) < 0 })
	t := treeFromSortedRanges(f.Ranges)
	t.normalize()
	f.Ranges = t.toRanges()
	return f
}

func mergeFunctions(fs []Function) Function {
	if len(fs) == 1 {
		return normalized(fs[0])
	}
	trees := make([]*rangeTree, len(fs))
	for i, f := range fs {
		trees[i] = treeFromSortedRanges(f.Ranges)
	}
	merged := mergeRangeTrees(trees)
	merged.normalize()
	ranges := merged.toRanges()
	return Function{
		FunctionName:    fs[0].FunctionName,
		Ranges:          ranges,
		IsBlockCoverage: len(ranges) != 1 || ranges[0].Count != 0,
	}
}

// rangeTree is a range and the ranges nested in it; delta is its count
// minus its parent's.
type rangeTree struct {
	start, end int
	delta      int
	children   []*rangeTree
}

func treeFromSortedRanges(ranges []Range) *rangeTree {
	type entry struct {
		t     *rangeTree
		count int
	}
	var root *rangeTree
	var stack []entry
	for _, r := range ranges {
		node := &rangeTree{start: r.StartOffset, end: r.EndOffset, delta: r.Count}
		if root == nil {
			root = node
			stack = append(stack, entry{node, r.Count})
			continue
		}
		var parent *rangeTree
		var parentCount int
		for {
			top := stack[len(stack)-1]
			parent, parentCount = top.t, top.count
			if r.StartOffset < parent.end {
				break
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				break
			}
		}
		node.delta -= parentCount
		parent.children = append(parent.children, node)
		stack = append(stack, entry{node, r.Count})
	}
	return root
}

// normalize joins the adjacent children with the same count, and a child
// that spans all of its parent with the parent.
func (t *rangeTree) normalize() {
	var children []*rangeTree
	var curEnd int
	var head *rangeTree
	var tail []*rangeTree
	endChain := func() {
		if len(tail) != 0 {
			head.end = tail[len(tail)-1].end
			for _, tt := range tail {
				for _, sub := range tt.children {
					sub.delta += tt.delta - head.delta
					head.children = append(head.children, sub)
				}
			}
			tail = nil
		}
		head.normalize()
		children = append(children, head)
	}
	for _, child := range t.children {
		switch {
		case head == nil:
			head = child
		case child.delta == head.delta && child.start == curEnd:
			tail = append(tail, child)
		default:
			endChain()
			head = child
		}
		curEnd = child.end
	}
	if head != nil {
		endChain()
	}
	if len(children) == 1 {
		c := children[0]
		if c.start == t.start && c.end == t.end {
			t.delta += c.delta
			t.children = c.children
			return
		}
	}
	t.children = children
}

// split cuts the tree at value (start < value < end) and returns the right
// part.
func (t *rangeTree) split(value int) *rangeTree {
	leftLen := len(t.children)
	var mid *rangeTree
	for i, c := range t.children {
		if c.start < value && value < c.end {
			mid = c.split(value)
			leftLen = i + 1
			break
		} else if c.start >= value {
			leftLen = i
			break
		}
	}
	right := slices.Clone(t.children[leftLen:])
	t.children = slices.Clip(t.children[:leftLen])
	if mid != nil {
		right = append([]*rangeTree{mid}, right...)
	}
	r := &rangeTree{start: value, end: t.end, delta: t.delta, children: right}
	t.end = value
	return r
}

func (t *rangeTree) toRanges() []Range {
	type entry struct {
		t           *rangeTree
		parentCount int
	}
	var out []Range
	stack := []entry{{t, 0}}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count := e.parentCount + e.t.delta
		out = append(out, Range{StartOffset: e.t.start, EndOffset: e.t.end, Count: count})
		for i := len(e.t.children) - 1; i >= 0; i-- {
			stack = append(stack, entry{e.t.children[i], count})
		}
	}
	return out
}

func mergeRangeTrees(trees []*rangeTree) *rangeTree {
	if len(trees) <= 1 {
		return trees[0]
	}
	delta := 0
	for _, t := range trees {
		delta += t.delta
	}
	return &rangeTree{start: trees[0].start, end: trees[0].end, delta: delta, children: mergeRangeTreeChildren(trees)}
}

type treeWithParent struct {
	parent int
	tree   *rangeTree
}

type startEvent struct {
	offset int
	trees  []treeWithParent
}

// startEventQueue gives the children of the trees merged by their start,
// with the parts of those cut at the end of the range open.
type startEventQueue struct {
	queue         []*startEvent
	next          int
	pendingOffset int
	pending       []treeWithParent
	hasPending    bool
}

func newStartEventQueue(parents []*rangeTree) *startEventQueue {
	byStart := map[int][]treeWithParent{}
	var starts []int
	for pi, p := range parents {
		for _, c := range p.children {
			if _, ok := byStart[c.start]; !ok {
				starts = append(starts, c.start)
			}
			byStart[c.start] = append(byStart[c.start], treeWithParent{pi, c})
		}
	}
	sort.Ints(starts)
	q := &startEventQueue{}
	for _, s := range starts {
		q.queue = append(q.queue, &startEvent{s, byStart[s]})
	}
	return q
}

func (q *startEventQueue) pop() *startEvent {
	var ev *startEvent
	if q.next < len(q.queue) {
		ev = q.queue[q.next]
	}
	if !q.hasPending {
		q.next++
		return ev
	}
	pending := q.pending
	if ev == nil || q.pendingOffset < ev.offset {
		q.pending, q.hasPending = nil, false
		return &startEvent{q.pendingOffset, pending}
	}
	if q.pendingOffset == ev.offset {
		q.pending, q.hasPending = nil, false
		ev.trees = append(ev.trees, pending...)
	}
	q.next++
	return ev
}

func mergeRangeTreeChildren(parents []*rangeTree) []*rangeTree {
	var result []*rangeTree
	q := newStartEventQueue(parents)
	// The nested trees by parent, in the order the parents come (a JS Map).
	var nestedOrder []int
	nested := map[int][]*rangeTree{}
	insert := func(parent int, t *rangeTree) {
		if _, ok := nested[parent]; !ok {
			nestedOrder = append(nestedOrder, parent)
		}
		nested[parent] = append(nested[parent], t)
	}
	nextChild := func(start, end int) *rangeTree {
		var matching []*rangeTree
		for _, p := range nestedOrder {
			ns := nested[p]
			if len(ns) == 1 && ns[0].start == start && ns[0].end == end {
				matching = append(matching, ns[0])
			} else {
				matching = append(matching, &rangeTree{start: start, end: end, children: ns})
			}
		}
		nestedOrder, nested = nil, map[int][]*rangeTree{}
		return mergeRangeTrees(matching)
	}
	open := false
	var openStart, openEnd int
	for {
		ev := q.pop()
		if ev == nil {
			break
		}
		if open && openEnd <= ev.offset {
			result = append(result, nextChild(openStart, openEnd))
			open = false
		}
		if !open {
			end := ev.offset + 1
			for _, t := range ev.trees {
				end = max(end, t.tree.end)
				insert(t.parent, t.tree)
			}
			q.pendingOffset = end
			open, openStart, openEnd = true, ev.offset, end
		} else {
			for _, t := range ev.trees {
				if t.tree.end > openEnd {
					right := t.tree.split(openEnd)
					q.pending = append(q.pending, treeWithParent{t.parent, right})
					q.hasPending = true
				}
				insert(t.parent, t.tree)
			}
		}
	}
	if open {
		result = append(result, nextChild(openStart, openEnd))
	}
	return result
}
