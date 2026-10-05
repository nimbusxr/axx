//go:build windows

package desktopwindows

import (
	"fmt"
	"slices"
	"time"

	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/jab"
	"github.com/nimbusxr/axx/packs/desktop/internal/uia"
)

// javaTree is a Java window as the Java Access Bridge has it: UI Automation
// sees only its frame. Places are Java's points, scale screen pixels each.
// Its methods run on the worker's thread.
type javaTree struct {
	p     *proc
	root  *jab.Element
	scale float64
}

// jctl is a control the bridge found. Its methods ask on the worker's
// thread.
type jctl struct {
	e *jab.Element
	w *worker
}

func (c jctl) Name() string {
	var n string
	_ = c.w.do(func() error { n = collapse(c.e.Name()); return nil })
	return n
}

func (c jctl) Enabled() (bool, bool) {
	var on bool
	_ = c.w.do(func() error { on = c.e.Has("enabled"); return nil })
	return on, true
}

func (c jctl) Value() (string, bool) {
	var v string
	var field bool
	_ = c.w.do(func() error {
		v, field = c.e.Text(), slices.Contains(javaRoles["field"], c.e.Role())
		return nil
	})
	return v, field
}

func jvisit(e *jab.Element, fn func(*jab.Element) bool) bool {
	if !fn(e) {
		return false
	}
	if e.Role() == "table" {
		return true // its children are its one cell renderer: see visibleCells
	}
	for _, k := range e.Children() {
		if !jvisit(k, fn) {
			return false
		}
	}
	return true
}

func jshown(e *jab.Element) bool { return e.Has("showing") }

func jholds(e *jab.Element, n string) bool {
	found := false
	jvisit(e, func(x *jab.Element) bool {
		found = found || collapse(x.Name()) == n
		return !found
	})
	return found
}

// javaRoles are the bridge's roles of each kind.
var javaRoles = map[string][]string{
	"button": {"push button"}, "field": {"text", "password text"}, "checkbox": {"check box"},
	"radio button": {"radio button"}, "switch": {"toggle button"}, "tab": {"page tab"}, "list item": {"list item"},
	"row": {"row"}, "link": {"hyperlink"}, "image": {"icon"}, "text": {"label"},
	"menu": {"menu"}, "menu item": {"menu item", "check box menu item", "radio button menu item"},
}

func jis(kind string, e *jab.Element) bool {
	return kind == "element" || slices.Contains(javaRoles[kind], e.Role())
}

func jmatches(kind, n string, e *jab.Element) bool {
	switch {
	case !jis(kind, e):
		return false
	case kind == "row":
		return jholds(e, n) // a JTable's cells are found by visibleCells
	}
	return collapse(e.Name()) == n
}

func (t *javaTree) search(e *jab.Element, kind, n string, onlyShown bool) []*jab.Element {
	var found []*jab.Element
	jvisit(e, func(x *jab.Element) bool {
		if jmatches(kind, n, x) && (!onlyShown || jshown(x)) {
			found = append(found, x)
		}
		return true
	})
	// A table's rows are its cells that show.
	if kind == "row" || kind == "element" {
		jvisit(e, func(x *jab.Element) bool {
			if x.Role() == "table" {
				for _, c := range visibleCells(x) {
					if collapse(c.Name()) == n {
						found = append(found, c)
					}
				}
			}
			return true
		})
	}
	return outermost(found)
}

// outermost leaves out an element in another of the list, and one found
// again (the bridge gives an open menu's items twice).
func outermost(found []*jab.Element) []*jab.Element {
	var out []*jab.Element
	for _, e := range found {
		if slices.ContainsFunc(out, e.Same) {
			continue
		}
		inner := false
		for _, o := range found {
			if o != e && !o.Same(e) && isAbove(o, e) {
				inner = true
				break
			}
		}
		if !inner {
			out = append(out, e)
		}
	}
	return out
}

func (t *javaTree) names(kind string) []string {
	var out []string
	jvisit(t.root, func(e *jab.Element) bool {
		if kind != "element" && jis(kind, e) && jshown(e) {
			if n := collapse(e.Name()); n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
		return true
	})
	return out
}

func (t *javaTree) shows(text string) bool {
	found := false
	jvisit(t.root, func(e *jab.Element) bool {
		found = found || collapse(e.Name()) == text && jshown(e)
		return !found
	})
	return found
}

func (t *javaTree) texts() []string {
	var out []string
	jvisit(t.root, func(e *jab.Element) bool {
		if n := collapse(e.Name()); n != "" && jshown(e) && !slices.Contains(out, n) {
			out = append(out, n)
		}
		return len(out) < 200
	})
	return out
}

// px is a point of Java's on the screen, in pixels.
func (t *javaTree) px(x, y int) (int, int) {
	return int(float64(x) * t.scale), int(float64(y) * t.scale)
}

func jcenter(e *jab.Element) (int, int) {
	x, y, w, h := e.Bounds()
	return x + w/2, y + h/2
}

// inView is whether a click in the element's middle reaches it: the middle
// is in every viewport it is in and in the work area, and the hit test there
// finds it, or stops above it (Java's hit test stops short).
func (t *javaTree) inView(e *jab.Element) bool {
	if !jshown(e) {
		return false
	}
	x, y := jcenter(e)
	if vx, vy, vw, vh := t.visiblePart(e); x < vx || x >= vx+vw || y < vy || y >= vy+vh {
		return false
	}
	hit := t.root.At(x, y)
	for at := hit; at != nil; at = at.Parent() {
		if at.Same(e) {
			return true
		}
	}
	return hit != nil && isAbove(hit, e)
}

// viewports are the viewports the element is in, the innermost first: a
// scroll pane shows what it holds in its viewport.
func viewports(e *jab.Element) []*jab.Element {
	var out []*jab.Element
	for at := e.Parent(); at != nil; at = at.Parent() {
		if at.Role() == "viewport" {
			out = append(out, at)
		}
	}
	return out
}

// visiblePart is the part of the element that shows, in Java's points: in
// each viewport it is in, and in the screen's work area (not under the
// taskbar).
func (t *javaTree) visiblePart(e *jab.Element) (x, y, w, h int) {
	x, y, w, h = e.Bounds()
	x2, y2 := x+w, y+h
	for _, v := range viewports(e) {
		vx, vy, vw, vh := v.Bounds()
		x, y, x2, y2 = max(x, vx), max(y, vy), min(x2, vx+vw), min(y2, vy+vh)
	}
	a := uia.WorkArea()
	x, y = max(x, int(float64(a.Left)/t.scale)), max(y, int(float64(a.Top)/t.scale))
	x2, y2 = min(x2, int(float64(a.Right)/t.scale)), min(y2, int(float64(a.Bottom)/t.scale))
	return x, y, max(x2-x, 0), max(y2-y, 0)
}

// reveal scrolls the element into view as far as it goes: each viewport it
// is in, the outermost first, until the next one in (or the element) shows
// in it.
func (t *javaTree) reveal(e *jab.Element) {
	if t.inView(e) {
		return
	}
	vs := viewports(e)
	for i := len(vs) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = vs[i-1]
		}
		t.bringInto(vs[i], inner)
	}
}

// bringInto turns the wheel over the viewport until what it holds (inner)
// shows in it, or fills it when it is the taller, over a part of it that no
// viewport in it covers, as that one would take the turns.
func (t *javaTree) bringInto(v, inner *jab.Element) {
	var in []*jab.Element
	jvisit(v, func(e *jab.Element) bool {
		if !e.Same(v) && e.Role() == "viewport" {
			in = append(in, e)
		}
		return true
	})
	still := 0
	for range 120 {
		vx, vy, vw, vh := t.visiblePart(v)
		_, fy, _, fh := inner.Bounds()
		if vw <= 0 || vh <= 0 || fh <= 0 || still >= 3 {
			return
		}
		if fy >= vy-1 && fy+fh <= vy+vh+1 || fy <= vy+1 && fy+fh >= vy+vh-1 {
			return
		}
		lines := -1
		if fy < vy {
			lines = 1
		}
		px, py, ok := freeSpot(vx, vy, vw, vh, in)
		if !ok {
			return
		}
		x, y := t.px(px, py)
		_ = uia.Scroll(x, y, lines)
		t.p.w.pump(100 * time.Millisecond)
		if _, gy, _, _ := inner.Bounds(); gy == fy {
			still++
		} else {
			still = 0
		}
	}
}

// freeSpot is a point in the rectangle that none of the elements covers:
// its middle, or near one of its edges.
func freeSpot(x, y, w, h int, covers []*jab.Element) (int, int, bool) {
	mx, my := x+w/2, y+h/2
	for _, p := range [][2]int{{mx, my}, {x + 6, my}, {x + w - 6, my}, {mx, y + 6}, {mx, y + h - 6}, {x + 6, y + 6}, {x + 6, y + h - 6}} {
		free := true
		for _, c := range covers {
			cx, cy, cw, ch := c.Bounds()
			if p[0] >= cx && p[0] < cx+cw && p[1] >= cy && p[1] < cy+ch {
				free = false
				break
			}
		}
		if free {
			return p[0], p[1], true
		}
	}
	return 0, 0, false
}

// visibleCells are the cells of a table that show. The bridge gives a
// JTable's children, and its table API's cells, as the table's one cell
// renderer: one label holding whatever cell it drew last, with no place.
// Its visible children are the cells themselves, each with its text and
// place.
func visibleCells(table *jab.Element) []*jab.Element {
	var cells []*jab.Element
	for _, c := range table.Visible() {
		if table.Same(c.Parent()) {
			cells = append(cells, c)
		}
	}
	return cells
}

// isAbove is whether a is e or one of e's ancestors.
func isAbove(a, e *jab.Element) bool {
	for at := e; at != nil; at = at.Parent() {
		if at.Same(a) {
			return true
		}
	}
	return false
}

// inPart is whether the cell's middle is in the part of its table that
// shows.
func (t *javaTree) inPart(cell, table *jab.Element) bool {
	x, y, w, h := t.visiblePart(table)
	cx, cy := jcenter(cell)
	return cx >= x && cx < x+w && cy >= y && cy < y+h
}

// scrollTo turns the wheel over the window's lists and tables until the
// control shows in view, finding it anew after each turn.
func (t *javaTree) scrollTo(kind, n string) *jab.Element {
	if found := t.search(t.root, kind, n, true); len(found) > 0 && t.inView(found[0]) {
		return found[0]
	}
	var boxes []*jab.Element
	jvisit(t.root, func(e *jab.Element) bool {
		if r := e.Role(); r == "table" || r == "list" {
			boxes = append(boxes, e)
		}
		return true
	})
	for _, box := range boxes {
		// The box first shows in the window (the page scrolls), then the
		// wheel turns over the middle of the part of it that shows.
		if p := box.Parent(); p != nil && p.Role() == "viewport" {
			t.reveal(p)
		}
		find := func() *jab.Element {
			if box.Role() == "table" && (kind == "row" || kind == "element") {
				for _, c := range visibleCells(box) {
					if collapse(c.Name()) == n && t.inPart(c, box) {
						return c
					}
				}
				return nil
			}
			if found := t.search(box, kind, n, false); len(found) > 0 && t.inView(found[0]) {
				return found[0]
			}
			return nil
		}
		for _, lines := range []int{-3, 3} {
			for range 40 {
				if e := find(); e != nil {
					return e
				}
				bx, by, bw, bh := t.visiblePart(box)
				if bw <= 0 || bh <= 0 {
					break
				}
				x, y := t.px(bx+bw/2, by+bh/2)
				_ = uia.Scroll(x, y, lines)
				t.p.w.pump(150 * time.Millisecond)
			}
		}
	}
	return nil
}

func (t *javaTree) click(e *jab.Element) error {
	t.reveal(e)
	x, y := t.px(jcenter(e))
	if err := uia.Click(x, y); err != nil {
		return err
	}
	t.p.w.pump(250 * time.Millisecond)
	return nil
}

// origin is the element's top left, in pixels, once it is in view.
func (t *javaTree) origin(e *jab.Element) (int, int, error) {
	t.reveal(e)
	x, y, w, _ := e.Bounds()
	if w <= 0 {
		return 0, 0, fmt.Errorf("the %s has no place on the screen", e.Role())
	}
	px, py := t.px(x, y)
	return px, py, nil
}

func (t *javaTree) node(e *jab.Element, depth int, count *int) *desktopcore.Node {
	*count++
	n := &desktopcore.Node{Role: e.Role(), Name: collapse(e.Name()), Attrs: map[string]string{"enabled": fmt.Sprint(e.Has("enabled"))}, Control: jctl{e: e, w: t.p.w}}
	if slices.Contains(javaRoles["field"], e.Role()) {
		n.Value = e.Text()
	}
	if depth > 60 || *count > 5000 || e.Role() == "table" {
		return n
	}
	for _, k := range e.Children() {
		n.Children = append(n.Children, t.node(k, depth+1, count))
	}
	return n
}
