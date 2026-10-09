package video

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// State is where a step of a video's step panel is.
type State int

const (
	StepWaiting State = iota
	StepRunning
	StepPassed
	StepFailed
	StepSkipped
)

// Step is a step of a video's step panel.
type Step struct {
	Keyword, Text string
	// Background is whether the step is the feature's Background's.
	Background bool
	// Argument names the step's data table or doc string, if it has one.
	Argument string
	State    State
	// Error is a failed step's message; Logs, the lines it logged.
	Error string
	Logs  []string
}

// Panel is a scenario's steps as it runs, beside its desktop in its video:
// its name, where it is, and each step, the one it runs and how each ran.
type Panel struct {
	Title, Where string
	Steps        []Step
	// Top shows the list from its first step, not from the one in focus: a
	// video's opening, the whole scenario at a glance.
	Top bool
}

var (
	panelBG      = color.RGBA{24, 27, 34, 255}
	panelEdge    = color.RGBA{44, 49, 60, 255}
	panelRunning = color.RGBA{38, 45, 60, 255}
	panelDim     = color.RGBA{104, 111, 126, 255}
	runningColor = color.RGBA{255, 183, 77, 255}
)

// PanelWidth is the width of the step panel beside a desktop of the height:
// even, as a video's frames are.
func PanelWidth(height int) int { return (height * 6 / 10) &^ 1 }

// panelLayout is the panel's sizes, for its height.
type panelLayout struct {
	unit                    float64
	pad, gap, iconCol, line int
	small, note             int
	title, where, kw, body  font.Face
	label, log              font.Face
}

func layoutFor(height int) panelLayout {
	fontsOnce.Do(loadFonts)
	u := float64(height) / 1080
	return panelLayout{
		unit: u, pad: int(28 * u), gap: int(14 * u), iconCol: int(38 * u),
		line: int(31 * u), small: int(24 * u), note: int(22 * u),
		title: face(bold, 28*u), where: face(regular, 19*u),
		kw: face(bold, 22*u), body: face(regular, 22*u),
		label: face(bold, 16*u), log: face(regular, 17*u),
	}
}

// block is a step as the panel lays it out: its lines and their colors.
type block struct {
	step   int
	label  string // a section's name before the step: Background, Scenario
	lines  [][]word
	notes  []note // its argument, logs and error, under it
	height int
}

type word struct {
	s    string
	bold bool
}

type note struct {
	s string
	c color.RGBA
}

// Draw draws the panel into r of img.
func (p Panel) Draw(img *image.RGBA, r image.Rectangle) {
	l := layoutFor(r.Dy())
	draw.Draw(img, r, &image.Uniform{panelBG}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+max(1, int(2*l.unit)), r.Max.Y), &image.Uniform{panelEdge}, image.Point{}, draw.Src)
	x, y := r.Min.X+l.pad, r.Min.Y+l.pad
	width := r.Dx() - 2*l.pad
	for _, s := range wrap(p.Title, l.title, width) {
		y += int(34 * l.unit)
		left(img, s, l.title, x, y, cardText)
	}
	if p.Where != "" {
		y += int(28 * l.unit)
		left(img, p.Where, l.where, x, y, cardMuted)
	}
	y += l.pad
	list := image.Rect(r.Min.X, y, r.Max.X, r.Max.Y-l.gap)
	blocks := p.layout(l, width-l.iconCol)
	first, last := p.shown(blocks, list.Dy(), l.small)
	y = list.Min.Y
	if first > 0 {
		y += l.small
		left(img, fmt.Sprintf("↑ %d earlier", first), l.log, x+l.iconCol, y-int(8*l.unit), cardMuted)
	}
	for i := first; i < last; i++ {
		p.drawBlock(img, l, blocks[i], x, y, width)
		y += blocks[i].height
	}
	if last < len(blocks) {
		left(img, fmt.Sprintf("↓ %d more", len(blocks)-last), l.log, x+l.iconCol, list.Max.Y-int(4*l.unit), cardMuted)
	}
}

// shown is the steps the list shows, from first to before last: whole
// steps only, the one in focus a third of the way down when they do not
// all fit, with a line (label high) above or below for the steps it leaves
// out.
func (p Panel) shown(blocks []block, height, label int) (first, last int) {
	fits := func(first int) int { // the end of the steps that fit from first
		room := height
		if first > 0 {
			room -= label
		}
		end := first
		for end < len(blocks) {
			need := blocks[end].height
			if end < len(blocks)-1 {
				need += label // the line below, unless this is the last step
			}
			if need > room && end > first {
				break
			}
			room -= blocks[end].height
			end++
		}
		return end
	}
	if p.Top || fits(0) == len(blocks) {
		return 0, fits(0)
	}
	focus := p.focus()
	above := 0
	for first = focus; first > 0 && above+blocks[first-1].height <= height/3; first-- {
		above += blocks[first-1].height
	}
	// No empty room at the end: earlier steps fill it.
	for first > 0 && fits(first-1) == len(blocks) {
		first--
	}
	return first, fits(first)
}

// focus is the step the panel keeps in view: the one that runs, else the
// one that failed, else the last that ran.
func (p Panel) focus() int {
	last := 0
	for i, s := range p.Steps {
		switch s.State {
		case StepRunning, StepFailed:
			return i
		case StepPassed, StepSkipped:
			last = i
		}
	}
	return last
}

func (p Panel) layout(l panelLayout, width int) []block {
	hasBackground := false
	for _, s := range p.Steps {
		hasBackground = hasBackground || s.Background
	}
	var out []block
	for i, s := range p.Steps {
		b := block{step: i}
		if hasBackground && (i == 0 || s.Background != p.Steps[i-1].Background) {
			b.label = "Scenario"
			if s.Background {
				b.label = "Background"
			}
		}
		words := []word{}
		for _, w := range splitWords(s.Keyword) {
			words = append(words, word{w, true})
		}
		for _, w := range splitWords(s.Text) {
			words = append(words, word{w, false})
		}
		b.lines = wrapWords(words, l, width)
		if s.Argument != "" {
			b.notes = append(b.notes, note{"+ " + s.Argument, panelDim})
		}
		if s.State == StepRunning || s.State == StepFailed {
			logs := s.Logs
			if len(logs) > 3 {
				logs = logs[len(logs)-3:]
			}
			for _, lg := range logs {
				b.notes = append(b.notes, note{clip(lg, l.log, width), cardMuted})
			}
		}
		if s.State == StepFailed && s.Error != "" {
			n := 0
			for _, e := range strings.Split(strings.TrimSpace(s.Error), "\n") {
				for _, w := range wrap(strings.TrimSpace(e), l.log, width) {
					if n < 5 && w != "" {
						b.notes = append(b.notes, note{w, Failed})
						n++
					}
				}
			}
		}
		b.height = len(b.lines)*l.line + len(b.notes)*l.note + l.gap
		if b.label != "" {
			b.height += l.small
		}
		out = append(out, b)
	}
	return out
}

func (p Panel) drawBlock(img *image.RGBA, l panelLayout, b block, x, top, width int) {
	s := p.Steps[b.step]
	if b.label != "" {
		top += l.small
		left(img, strings.ToUpper(b.label), l.label, x, top-int(6*l.unit), panelDim)
	}
	if s.State == StepRunning {
		h := len(b.lines)*l.line + len(b.notes)*l.note + l.gap/2
		draw.Draw(img, image.Rect(x-l.pad/2, top, x+width+l.pad/2, top+h), &image.Uniform{panelRunning}, image.Point{}, draw.Src)
	}
	text, kw := cardText, cardText
	switch {
	case s.State == StepWaiting:
		text, kw = cardMuted, cardMuted
	case s.State == StepSkipped:
		text, kw = panelDim, panelDim
	case s.Background && s.State == StepPassed:
		text = cardMuted
	}
	r := float64(l.line) * 0.3
	icon(img, float64(x)+r, float64(top)+float64(l.line)*0.55, r, s.State)
	y := top
	for _, line := range b.lines {
		y += l.line
		dx := x + l.iconCol
		for i, w := range line {
			f, c := l.body, text
			if w.bold {
				f, c = l.kw, kw
			}
			if i > 0 {
				dx += font.MeasureString(l.body, " ").Round()
			}
			left(img, w.s, f, dx, y-int(8*l.unit), c)
			dx += font.MeasureString(f, w.s).Round()
		}
	}
	for _, n := range b.notes {
		y += l.note
		left(img, n.s, l.log, x+l.iconCol, y-int(6*l.unit), n.c)
	}
}

// wrapWords splits words into lines no wider than width, each word in its
// face (a step's keyword bold).
func wrapWords(words []word, l panelLayout, width int) [][]word {
	space := font.MeasureString(l.body, " ").Round()
	var lines [][]word
	var line []word
	w := 0
	for _, wd := range words {
		f := l.body
		if wd.bold {
			f = l.kw
		}
		ww := font.MeasureString(f, wd.s).Round()
		if len(line) > 0 && w+space+ww > width {
			lines = append(lines, line)
			line, w = nil, 0
		}
		if len(line) > 0 {
			w += space
		}
		line = append(line, wd)
		w += ww
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = [][]word{{}}
	}
	return lines
}

// clip shortens s to width, with an ellipsis.
func clip(s string, f font.Face, width int) string {
	if font.MeasureString(f, s).Round() <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && font.MeasureString(f, string(r)+"…").Round() > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// left draws s from x, its baseline at y.
func left(img *image.RGBA, s string, f font.Face, x, y int, c color.RGBA) {
	if f == nil {
		return
	}
	d := &font.Drawer{Dst: img, Src: &image.Uniform{c}, Face: f, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// icon draws a step's state as a circle of radius r at (cx, cy): a ring
// while it waits, a ring with a dot as it runs, a check on green when it
// passed, a cross on red when it failed, a dash when it was skipped. The
// shape says it as well as the color does.
func icon(img *image.RGBA, cx, cy, r float64, s State) {
	b := image.Rect(int(cx-r)-1, int(cy-r)-1, int(math.Ceil(cx+r))+1, int(math.Ceil(cy+r))+1)
	ox, oy := float32(b.Min.X), float32(b.Min.Y)
	// Each shape is drawn through a mask of its own, clipped to img: an
	// icon at the list's edge is cut there.
	shape := func(c color.RGBA, paint func(z *vector.Rasterizer)) {
		mask := image.NewAlpha(image.Rect(0, 0, b.Dx(), b.Dy()))
		z := vector.NewRasterizer(b.Dx(), b.Dy())
		paint(z)
		z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
		draw.DrawMask(img, b, &image.Uniform{c}, image.Point{}, mask, image.Point{}, draw.Over)
	}
	x, y, rr := float32(cx)-ox, float32(cy)-oy, float32(r)
	stroke := max(1.5, rr/4)
	ring := func(z *vector.Rasterizer) {
		circle(z, x, y, rr, false)
		circle(z, x, y, rr-stroke, true)
	}
	white := color.RGBA{255, 255, 255, 255}
	switch s {
	case StepWaiting:
		shape(panelDim, ring)
	case StepRunning:
		shape(runningColor, ring)
		shape(runningColor, func(z *vector.Rasterizer) { circle(z, x, y, rr*0.4, false) })
	case StepPassed:
		shape(Passed, func(z *vector.Rasterizer) { circle(z, x, y, rr, false) })
		shape(white, func(z *vector.Rasterizer) {
			line(z, x-rr*0.45, y+rr*0.02, x-rr*0.1, y+rr*0.38, stroke)
			line(z, x-rr*0.1, y+rr*0.38, x+rr*0.5, y-rr*0.35, stroke)
		})
	case StepFailed:
		shape(Failed, func(z *vector.Rasterizer) { circle(z, x, y, rr, false) })
		shape(white, func(z *vector.Rasterizer) {
			line(z, x-rr*0.38, y-rr*0.38, x+rr*0.38, y+rr*0.38, stroke)
			line(z, x+rr*0.38, y-rr*0.38, x-rr*0.38, y+rr*0.38, stroke)
		})
	case StepSkipped:
		shape(panelDim, ring)
		shape(panelDim, func(z *vector.Rasterizer) { line(z, x-rr*0.45, y, x+rr*0.45, y, stroke) })
	}
}

// circle adds a circle to z, clockwise or (reverse, a hole) counter.
func circle(z *vector.Rasterizer, x, y, r float32, reverse bool) {
	const k = 0.5523
	if !reverse {
		z.MoveTo(x+r, y)
		z.CubeTo(x+r, y+k*r, x+k*r, y+r, x, y+r)
		z.CubeTo(x-k*r, y+r, x-r, y+k*r, x-r, y)
		z.CubeTo(x-r, y-k*r, x-k*r, y-r, x, y-r)
		z.CubeTo(x+k*r, y-r, x+r, y-k*r, x+r, y)
	} else {
		z.MoveTo(x+r, y)
		z.CubeTo(x+r, y-k*r, x+k*r, y-r, x, y-r)
		z.CubeTo(x-k*r, y-r, x-r, y-k*r, x-r, y)
		z.CubeTo(x-r, y+k*r, x-k*r, y+r, x, y+r)
		z.CubeTo(x+k*r, y+r, x+r, y+k*r, x+r, y)
	}
	z.ClosePath()
}

// line adds a stroke from (x1, y1) to (x2, y2), w wide, with square ends.
func line(z *vector.Rasterizer, x1, y1, x2, y2, w float32) {
	dx, dy := x2-x1, y2-y1
	n := float32(math.Hypot(float64(dx), float64(dy)))
	if n == 0 {
		return
	}
	px, py := -dy/n*w/2, dx/n*w/2
	ex, ey := dx/n*w/2, dy/n*w/2
	z.MoveTo(x1+px-ex, y1+py-ey)
	z.LineTo(x2+px+ex, y2+py+ey)
	z.LineTo(x2-px+ex, y2-py+ey)
	z.LineTo(x1-px-ex, y1-py-ey)
	z.ClosePath()
}
