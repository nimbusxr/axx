package video

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func steps(states ...State) []Step {
	out := make([]Step, len(states))
	for i, s := range states {
		out[i] = Step{Keyword: "And", Text: "the depot desk is opened", State: s}
	}
	return out
}

// The panel keeps the step that runs in view; else the one that failed;
// else the last that ran.
func TestPanelFocus(t *testing.T) {
	for _, c := range []struct {
		states []State
		want   int
	}{
		{[]State{StepWaiting, StepWaiting}, 0},
		{[]State{StepPassed, StepRunning, StepWaiting}, 1},
		{[]State{StepPassed, StepFailed, StepSkipped}, 1},
		{[]State{StepPassed, StepPassed, StepPassed}, 2},
	} {
		if got := (Panel{Steps: steps(c.states...)}).focus(); got != c.want {
			t.Errorf("focus of %v: %d, want %d", c.states, got, c.want)
		}
	}
}

// The list shows whole steps: all of them when they fit; else a window
// with the step in focus in it, that leaves no room empty at the end.
func TestPanelShown(t *testing.T) {
	blocks := make([]block, 30)
	for i := range blocks {
		blocks[i] = block{step: i, height: 40}
	}
	all := Panel{Steps: steps(make([]State, 30)...)}
	if first, last := all.shown(blocks, 30*40, 20); first != 0 || last != 30 {
		t.Errorf("all fit: %d to %d, want 0 to 30", first, last)
	}
	ss := steps(make([]State, 30)...)
	for i := range 20 {
		ss[i].State = StepPassed
	}
	ss[20].State = StepRunning
	p := Panel{Steps: ss}
	first, last := p.shown(blocks, 400, 20)
	if first > 20 || last <= 20 {
		t.Errorf("running step 20 not shown: %d to %d", first, last)
	}
	if used := (last-first)*40 + 20 + 20; used > 400 {
		t.Errorf("%d to %d takes %d of 400", first, last, used)
	}
	ss[20].State, ss[29].State = StepPassed, StepRunning
	if first, last := p.shown(blocks, 400, 20); last != 30 || first > 29 || (30-first)*40+20 > 400 || (30-first+1)*40+20 <= 400 && first > 0 {
		t.Errorf("at the end: %d to %d, want the last steps to fill the list", first, last)
	}
}

// A panel draws whatever it is given: no steps, long text, a long error,
// more steps than fit.
func TestPanelDraws(t *testing.T) {
	long := strings.Repeat("the depot desk's arrivals list shows a parcel ", 12)
	for name, p := range map[string]Panel{
		"empty":    {Title: "Nothing yet"},
		"long":     {Title: long, Where: long, Steps: []Step{{Keyword: "Given", Text: long, State: StepRunning, Logs: []string{long, long, long, long}}}},
		"error":    {Steps: []Step{{Keyword: "Then", Text: "a parcel arrives", State: StepFailed, Error: strings.Repeat(long+"\n", 20)}}},
		"hundreds": {Steps: steps(make([]State, 200)...)},
	} {
		for _, h := range []int{240, 1080, 2160} {
			img := image.NewRGBA(image.Rect(0, 0, PanelWidth(h), h))
			p.Draw(img, img.Bounds())
			if got := img.RGBAAt(PanelWidth(h)-2, h-2); got != panelBG {
				t.Errorf("%s at %d: the panel's corner is %v, want %v", name, h, got, panelBG)
			}
		}
	}
}

// Each state's icon says it by its shape as well as its color.
func TestPanelIcons(t *testing.T) {
	// Inside: the middle, or above a passed step's check, which crosses it.
	for _, c := range []struct {
		state        State
		edge, inside color.RGBA
		y            int
	}{
		{StepWaiting, panelDim, panelBG, 20},
		{StepRunning, runningColor, runningColor, 20},
		{StepPassed, Passed, Passed, 11},
		{StepFailed, Failed, color.RGBA{255, 255, 255, 255}, 20},
		{StepSkipped, panelDim, panelDim, 20},
	} {
		img := image.NewRGBA(image.Rect(0, 0, 41, 41))
		for i := range img.Pix {
			img.Pix[i] = []uint8{panelBG.R, panelBG.G, panelBG.B, 255}[i%4]
		}
		icon(img, 20, 20, 16, c.state)
		if got := img.RGBAAt(20, 5); !near(got, c.edge) {
			t.Errorf("state %d: edge %v, want %v", c.state, got, c.edge)
		}
		if got := img.RGBAAt(20, c.y); !near(got, c.inside) {
			t.Errorf("state %d: inside %v, want %v", c.state, got, c.inside)
		}
	}
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int { return max(int(x), int(y)) - min(int(x), int(y)) }
	return d(a.R, b.R) < 12 && d(a.G, b.G) < 12 && d(a.B, b.B) < 12
}
