package desktopcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
	"github.com/nimbusxr/axx/packs/internal/recording"
)

// fakeDriver runs fake apps on this machine's OS.
type fakeDriver struct {
	claims, releases, resets, starts, watches int
	proc                                      *fakeProc
	// system is the system's app a watch reads.
	system *fakeProc
	// wrap is what runs the proc, when not the proc itself.
	wrap Process
}

func (d *fakeDriver) Platform() string { return appcore.Host() }

func (d *fakeDriver) Claim(sc *core.Scenario) (Desktop, error) {
	d.claims++
	return &fakeDesk{d: d, sc: sc}, nil
}

type fakeDesk struct {
	d  *fakeDriver
	sc *core.Scenario
}

func (k *fakeDesk) Home(app *App) string { return HomeOf(k.sc, app) }
func (k *fakeDesk) DataDir() string      { return "Library/Application Support" }
func (k *fakeDesk) Reset(*core.Scenario, *App) error {
	k.d.resets++
	return nil
}

func (k *fakeDesk) Start(*core.Scenario, *App) (Process, error) {
	k.d.starts++
	k.d.proc.stopped = false
	if k.d.wrap != nil {
		return k.d.wrap, nil
	}
	return k.d.proc, nil
}
func (k *fakeDesk) Release() { k.d.releases++ }

// Screen is a small screen with the app's window drawn on it: one that
// changes at each look when the app animates.
func (k *fakeDesk) Screen() (Screen, error) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{40, 60, 90, 255}}, image.Point{}, draw.Src)
	if p := k.d.proc; p != nil && p.changing {
		img.Set(int(p.looks.Add(1)%60), 20, color.RGBA{250, 250, 250, 255})
	}
	return Screen{Image: img, Scale: 1, Pointer: image.Pt(30, 20), HasPointer: true}, nil
}

func (k *fakeDesk) Watch(*core.Scenario, *App) (Process, error) {
	k.d.watches++
	return k.d.system, nil
}

// fakeProc is an app whose controls are a tree of nodes.
type fakeProc struct {
	tree    *Node
	texts   []string
	typed   []string
	clicks  []string
	keys    []string
	stopped bool
	// keepsNot is how many fills the field ignores.
	keepsNot int
	// changing is whether its window differs at each look (looks).
	changing bool
	looks    atomic.Int32
	// shows, when set, is its window at each look, from the first (1).
	shows func(look int32) image.Image
	// size is its controls' size, in points, once set: a place off it fails.
	size [2]float64
	// remakes is how many actions find the control made anew, as a web view
	// makes its controls as it lays out: the one found has lost its place.
	remakes int
	// leaves is how many times the pointer was moved beside it.
	leaves int
}

type fakeControl struct {
	n                  *Node
	enabled, reportsOn bool
	reportsValue       bool
	// gone is whether the app made it anew: it has no place.
	gone bool
}

func (c *fakeControl) Name() string { return c.n.Name }
func (c *fakeControl) Enabled() (bool, bool) {
	return c.enabled, c.reportsOn
}
func (c *fakeControl) Value() (string, bool) { return c.n.Value, c.reportsValue }

func node(role, name string, c *fakeControl, kids ...*Node) *Node {
	n := &Node{Role: role, Name: name, Children: kids}
	if c != nil {
		c.n, n.Control = n, c
	}
	return n
}

func (p *fakeProc) Find(k appcore.Kind, name string, _ bool) ([]Control, error) {
	var out []Control
	walkNodes(p.tree, func(n *Node) {
		if n.Role == k.Noun && n.Name == name && n.Control != nil {
			out = append(out, n.Control)
		}
	})
	return out, nil
}

func (p *fakeProc) Names(k appcore.Kind) []string {
	var out []string
	walkNodes(p.tree, func(n *Node) {
		if n.Role == k.Noun {
			out = append(out, n.Name)
		}
	})
	return out
}

func (p *fakeProc) Shows(text string) (bool, error) {
	for _, t := range p.texts {
		if t == text {
			return true, nil
		}
	}
	return false, nil
}
func (p *fakeProc) Texts() []string { return p.texts }
func (p *fakeProc) Front() error    { return nil }
func (p *fakeProc) Away() error     { return nil }
func (p *fakeProc) Leave() error {
	p.leaves++
	return nil
}

func (p *fakeProc) ScrollTo(k appcore.Kind, name string) (Control, error) {
	found, _ := p.Find(k, name, true)
	if len(found) == 0 {
		return nil, nil
	}
	return found[0], nil
}
func (p *fakeProc) ScrollIntoView(Control) error { return nil }
func (p *fakeProc) Click(c Control) error {
	if err := p.placed(c); err != nil {
		return err
	}
	p.clicks = append(p.clicks, c.Name())
	return nil
}

// placed is a *Lost for a control the app made anew, and makes it anew
// while remakes lasts.
func (p *fakeProc) placed(c Control) error {
	fc := c.(*fakeControl)
	if !fc.gone && p.remakes > 0 {
		p.remakes--
		fresh := *fc
		fc.n.Control, fc.gone = &fresh, true
	}
	if fc.gone {
		return &Lost{Err: fmt.Errorf("the %s %q lost its place on the screen", fc.n.Role, fc.n.Name)}
	}
	return nil
}

func (p *fakeProc) ClickAt(c Control, from Anchor, x, y float64) error {
	if err := p.placed(c); err != nil {
		return err
	}
	if p.size[0] > 0 {
		if err := from.On(p.size[0], p.size[1], x, y); err != nil {
			return err
		}
	}
	p.clicks = append(p.clicks, fmt.Sprintf("%s at %g, %g from %g, %g", c.Name(), x, y, from.X, from.Y))
	return nil
}

func (p *fakeProc) Drag(c Control, from Anchor, x1, y1, x2, y2 float64) error {
	if err := p.placed(c); err != nil {
		return err
	}
	if p.size[0] > 0 {
		if err := from.On(p.size[0], p.size[1], x1, y1); err != nil {
			return err
		}
	}
	p.clicks = append(p.clicks, fmt.Sprintf("%s dragged from %g, %g to %g, %g from %g, %g", c.Name(), x1, y1, x2, y2, from.X, from.Y))
	return nil
}

func (p *fakeProc) Key(spec string) error {
	p.keys = append(p.keys, spec)
	return nil
}

func (p *fakeProc) Type(text string) error {
	p.typed = append(p.typed, text)
	walkNodes(p.tree, func(n *Node) {
		if n.Role == "field" {
			if p.keepsNot > 0 {
				p.keepsNot--
				return
			}
			n.Value = text
		}
	})
	return nil
}

func (p *fakeProc) Window() (image.Image, float64, error) {
	if p.shows != nil {
		return p.shows(p.looks.Add(1)), 2, nil
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	if p.changing { // as an app that animates: each look differs
		p.looks.Add(1)
		img.Set(0, 0, color.RGBA{G: uint8(p.looks.Load()), A: 255})
	}
	return img, 2, nil
}
func (p *fakeProc) Tree() (*Node, error) { return p.tree, nil }
func (p *fakeProc) Stop() error {
	p.stopped = true
	return nil
}
func (p *fakeProc) Exited() bool             { return p.stopped }
func (p *fakeProc) Describe() map[string]any { return map[string]any{"app": "fake"} }

// desk is a fake depot desk: a Reference field, a Register button and two
// Close buttons.
func desk() (*fakeProc, *fakeControl, *fakeControl) {
	field := &fakeControl{reportsValue: true, reportsOn: true, enabled: true}
	register := &fakeControl{reportsOn: true}
	tree := node("window", "Depot desk", nil,
		node("field", "Reference", field),
		node("button", "Register", register),
		node("button", "Close", &fakeControl{}),
		node("button", "Close", &fakeControl{}),
		node("element", "Courier signature", &fakeControl{}),
	)
	tree.Children[0].ID = "reference"
	return &fakeProc{tree: tree, texts: []string{"No parcels registered yet"}}, field, register
}

func harness(t *testing.T, d *fakeDriver) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.New(t, appcore.Pack(), Pack(), files.Pack())
	h.Start(&core.Plan{})
	app := &App{Name: "depot", App: "depot", Driver: d, Dir: h.Dir}
	if err := Register(h.SC, app); err != nil {
		t.Fatal(err)
	}
	return h
}

// The system's app (owner: system) runs already: the scenario watches it
// from its registration on, reads it with the steps, and never resets,
// starts or stops it; its end closes the windows the app showed for it.
func TestSystemApp(t *testing.T) {
	p, _, _ := desk()
	files := &fakeProc{tree: node("window", ".snap", nil), texts: []string{".snap", "snap.log"}}
	d := &fakeDriver{proc: p, system: files}
	h := harness(t, d)
	if err := Register(h.SC, &App{Name: "files", App: "com.apple.finder", System: true, Driver: d, Dir: h.Dir}); err != nil {
		t.Fatal(err)
	}
	if d.watches != 1 || d.resets != 0 || d.starts != 0 {
		t.Fatalf("registering the system's app watches it, and resets and starts nothing: %d watches, %d resets, %d starts", d.watches, d.resets, d.starts)
	}
	h.OK(`within 1s the files app shows "snap.log"`)
	_ = h.Fails("the files app is launched", "is the system's: it runs already")
	_ = h.Fails("the files app is restarted", "is the system's")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if !files.stopped {
		t.Error("the scenario's end has the system's app close the windows it showed")
	}
}

// The system's app is only that: an owner other than system, or settings
// for an app the scenario would start, are refused.
func TestSystemAppParse(t *testing.T) {
	h := cloudtest.New(t, appcore.Pack(), Pack(), files.Pack())
	h.Start(&core.Plan{})
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"app", "com.apple.finder"}, {"owner", "me"}}, `owner "me" is not one`},
		{[][]string{{"app", "com.apple.finder"}, {"owner", "system"}, {"args", "--new-window"}}, "takes no args"},
	} {
		_, err := Parse(h.SC, appcore.Host(), "files", &core.Table{Rows: c.rows})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.rows, err, c.want)
		}
	}
}

// A registration for this machine claims the desktop as it registers; the
// scenario's end stops its apps and gives the desktop back.
func TestClaimAndRelease(t *testing.T) {
	p, _, _ := desk()
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	if d.claims != 1 {
		t.Fatalf("registering claims the desktop: %d claims", d.claims)
	}
	h.OK("the depot app is launched")
	h.OK("the depot app is restarted")
	if d.starts != 2 || d.resets != 1 {
		t.Errorf("a restart starts the app again, without a reset: %d starts, %d resets", d.starts, d.resets)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if !p.stopped || d.releases != 1 {
		t.Errorf("the scenario's end stops the app (%v) and gives the desktop back (%d)", p.stopped, d.releases)
	}
}

// A field holds what was typed, read back; a field that ignores a fill is
// filled again.
func TestFill(t *testing.T) {
	p, field, register := desk()
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	h.OK("the depot app is launched")
	p.keepsNot = 1
	h.OK(`the "Reference" field in the depot app is filled with "PX-DSK-4201"`)
	if len(p.typed) != 2 || strings.Join(p.keys, ",") != "ControlOrMeta+A,ControlOrMeta+A" {
		t.Errorf("filled twice, selecting the text each time: typed %v, keys %v", p.typed, p.keys)
	}
	h.OK(`the "Reference" field in the depot app has the value "PX-DSK-4201"`)
	_ = h.Fails(`within 1s the "Reference" field in the depot app has the value "PX-DSK-4202"`, `holds "PX-DSK-4201"`)

	field.reportsValue = false
	h.OK(`the "Reference" field in the depot app is filled with "PX-DSK-4203"`)
	_ = h.Fails(`the "Reference" field in the depot app has the value "PX-DSK-4203"`, "does not say what it holds")

	_ = h.Fails(`within 1s the "Register" button is enabled in the depot app`, `"Register" button in the depot app is disabled`)
	h.OK(`the "Register" button is disabled in the depot app`)
	register.reportsOn = false
	_ = h.Fails(`the "Register" button is enabled in the depot app`, "does not say whether it is enabled")
}

// A control a step names is the one control of its kind and name: none
// names the controls of the kind there are, two ask for a selector.
func TestControls(t *testing.T) {
	p, _, register := desk()
	register.enabled = true
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	h.OK("the depot app is launched")
	h.OK(`the "Register" button is clicked in the depot app`)
	h.OK(`the "id=reference" field is clicked in the depot app`)
	h.OK(`the "xpath=//button[@name='Register']" button is clicked in the depot app`)
	if strings.Join(p.clicks, ",") != "Register,Reference,Register" {
		t.Errorf("clicked %v", p.clicks)
	}
	_ = h.Fails(`within 1s the "Regster" button is shown in the depot app`, `No button named "Regster" in the depot app; 3 buttons:`)
	_ = h.Fails(`the "Close" button is clicked in the depot app`, `2 buttons in the depot app are named "Close"`)
	_ = h.Fails(`within 1s the depot app shows "Registered"`, `The depot app does not show "Registered"`)
	var shot, outline bool
	for _, a := range h.Sink.Attachments {
		shot = shot || a.Name == "the depot app" && a.MediaType == "image/png"
		outline = outline || a.Name == "the depot app's controls" && strings.Contains(string(a.Body), `field name="Reference" id="reference"`)
	}
	if !shot || !outline {
		t.Errorf("a failure attaches the window (%v) and the app's controls (%v)", shot, outline)
	}
	// Places are from the control's top left, or from the point a step names.
	p.clicks = nil
	h.OK(`the "Courier signature" element in the depot app is clicked at 40, 40`)
	h.OK(`the "Courier signature" element in the depot app is clicked at -40, -20 from its bottom right`)
	h.OK(`the pointer is dragged from 40, 80 to 300, 80 on the "Courier signature" element in the depot app`)
	h.OK(`the pointer is dragged from -120, 0 to 120, 0 from the middle of the "Courier signature" element in the depot app`)
	h.OK(`the pointer is dragged from 0, -10 to -30, -10 from the top right of the "Courier signature" element in the depot app`)
	want := []string{
		"Courier signature at 40, 40 from 0, 0",
		"Courier signature at -40, -20 from 1, 1",
		"Courier signature dragged from 40, 80 to 300, 80 from 0, 0",
		"Courier signature dragged from -120, 0 to 120, 0 from 0.5, 0.5",
		"Courier signature dragged from 0, -10 to -30, -10 from 1, 0",
	}
	if strings.Join(p.clicks, "\n") != strings.Join(want, "\n") {
		t.Errorf("clicked and dragged:\n%s\nwant:\n%s", strings.Join(p.clicks, "\n"), strings.Join(want, "\n"))
	}
	// A place off the control fails as such, where a click would reach
	// something else; a drag may end off it.
	p.size = [2]float64{160, 64}
	_ = h.Fails(`the pointer is dragged from 40, 80 to 300, 80 on the "Courier signature" element in the depot app`,
		`40, 80 from its top left is off the "Courier signature" element in the depot app, which is 160 by 64 points`)
	_ = h.Fails(`the "Courier signature" element in the depot app is clicked at 130, 0 from its middle`,
		`130, 0 from its middle is off the "Courier signature" element`)
	h.OK(`the pointer is dragged from 40, 30 to 300, 30 on the "Courier signature" element in the depot app`)
	h.OK(`the "Courier signature" element in the depot app is clicked at 0, 0 from its bottom right`)
	p.size = [2]float64{}

	// A step may name its app by a property, where the app differs between
	// platforms.
	t.Setenv("AXX_TEST_DESK", "depot")
	p.clicks = nil
	h.OK(`the "Courier signature" element in the ${env:AXX_TEST_DESK} app is clicked at -40, -20 from its bottom right`)
	h.OK(`the pointer is dragged from -120, 0 to 120, 0 from the middle of the "Courier signature" element in the ${env:AXX_TEST_DESK} app`)
	h.OK(`the Enter key is pressed in the ${env:AXX_TEST_DESK} app`)
	_ = h.Fails(`the Enter key is pressed in the ${env:AXX_TEST_NO_DESK:-office} app`, `no app named "office" is registered in this scenario (it registers depot)`)
	if len(p.clicks) != 2 {
		t.Errorf("clicked and dragged in the app a property names: %v", p.clicks)
	}
}

// A control that lost its place as a step took it (the app made it anew)
// is found again, a few times at most.
func TestLostControls(t *testing.T) {
	p, _, register := desk()
	register.enabled = true
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	h.OK("the depot app is launched")
	p.remakes = 2
	h.OK(`the "Register" button is clicked in the depot app`)
	p.remakes = 1
	h.OK(`the "Reference" field in the depot app is filled with "PX-DSK-4201"`)
	p.remakes = 1
	h.OK(`the "Courier signature" element in the depot app is clicked at -40, -20 from its bottom right`)
	p.remakes = 1
	h.OK(`the pointer is dragged from -120, 0 to 120, 0 from the middle of the "Courier signature" element in the depot app`)
	if len(p.clicks) != 4 || p.remakes != 0 {
		t.Errorf("each action took the control found again, once: %v", p.clicks)
	}
	p.remakes = 3
	_ = h.Fails(`the "Register" button is clicked in the depot app`, `the button "Register" lost its place on the screen`)
}

// A place from an anchor is that far from the anchor's point.
func TestAnchorOn(t *testing.T) {
	for _, c := range []struct {
		from Anchor
		x, y float64
		on   bool
	}{
		{TopLeft, 40, 30, true},
		{TopLeft, 40, 80, false},
		{TopLeft, -1, 0, false},
		{anchors["middle"], -60, 10, true},
		{anchors["middle"], -120, 10, false},
		{anchors["bottom right"], 0, 0, true},
		{anchors["bottom right"], -40, -30, true},
		{anchors["top right"], 1, 0, false},
	} {
		err := c.from.On(160, 64, c.x, c.y)
		var off *Off
		if on := err == nil; on != c.on || !on && (!errors.As(err, &off) || off.Width != 160 || off.Height != 64) {
			t.Errorf("%g, %g from its %v on a 160 by 64 control: %v", c.x, c.y, c.from, err)
		}
	}
}

func TestAnchorPlace(t *testing.T) {
	for _, c := range []struct {
		from Anchor
		x, y float64
	}{
		{TopLeft, 110, 220},
		{anchors["middle"], 160, 245},
		{anchors["bottom right"], 210, 270},
		{anchors["top right"], 210, 220},
		{anchors["bottom left"], 110, 270},
	} {
		if x, y := c.from.Place(100, 200, 100, 50, 10, 20); x != c.x || y != c.y {
			t.Errorf("10, 20 from %v of 100,200 100x50 is %g, %g, not %g, %g", c.from, x, y, c.x, c.y)
		}
	}
}

// An app's files are its home's: "./" where the OS keeps an app's data,
// "~/" the home. The scenario's first use empties the home.
func TestFiles(t *testing.T) {
	p, _, _ := desk()
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	home := filepath.Join(h.Dir, ".axx", "desktop", appcore.Host(), "depot")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "left.txt"), []byte("the last scenario's"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.OK("the desk folder with the following properties:", [][]string{{"owner", "app:depot"}, {"path", `"./Depot desk"`}})
	h.OK("the home folder with the following properties:", [][]string{{"owner", "app:depot"}, {"path", "~/"}})
	data := filepath.Join(home, "Library", "Application Support", "Depot desk")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	h.OK("the depot app is launched")
	if _, err := os.Stat(filepath.Join(home, "left.txt")); !os.IsNotExist(err) {
		t.Errorf("the home is emptied as the scenario first uses it: %v", err)
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "arrivals.json"), []byte(`[{"reference":"PX-DSK-4201"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.OK(`the "arrivals.json" file in the desk folder contains "PX-DSK-4201"`)
	h.OK(`the "Library/Application Support/Depot desk/arrivals.json" file in the home folder contains "PX-DSK-4201"`)
	if d.resets != 1 {
		t.Errorf("the app is reset once in a scenario: %d", d.resets)
	}
	for _, bad := range []string{"/etc", "../x", "./../x"} {
		if _, _, err := homePath(bad); err == nil {
			t.Errorf("%s is no path in the app's files", bad)
		}
	}
}

func TestParse(t *testing.T) {
	h := cloudtest.New(t, appcore.Pack(), Pack())
	h.File("build/Depot desk.app/Contents/Info.plist", "<plist/>")
	tbl := &core.Table{Rows: [][]string{
		{"app", "build/Depot desk.app"},
		{"args", `--depot Leipzig "Depot desk.py"`},
		{"env.PARCELS_API", "http://127.0.0.1:8080"},
		{"locale", "de-DE"},
		{"timezone", "Europe/Berlin"},
		{"registry", `Software\Parcels`},
	}}
	host := appcore.Host()
	a, err := Parse(h.SC, host, "depot", tbl, "registry")
	if err != nil {
		t.Fatal(err)
	}
	if a.App != filepath.Join(h.Dir, "build", "Depot desk.app") || strings.Join(a.Args, "|") != "--depot|Leipzig|Depot desk.py" ||
		a.Env["PARCELS_API"] != "http://127.0.0.1:8080" || a.Locale != "de-DE" || a.Timezone != "Europe/Berlin" || a.Extra["registry"] != `Software\Parcels` {
		t.Errorf("%+v", a)
	}
	for row, want := range map[[2]string]string{
		{"app", "build/missing.app"}: "is not there",
		{"locale", "German"}:         "not a language and region",
		{"timezone", "Mars/Olympus"}: "not a time zone",
		{"registry", `Software\X`}:   `unknown ` + host + ` app property "registry" (supported: app, args, env.<name>, locale, timezone, owner)`,
		{"env.1X", "y"}:              "not an environment variable's name",
	} {
		rows := [][]string{{"app", "TextEdit"}, row[:]}
		if row[0] == "app" {
			rows = rows[1:]
		}
		_, err := Parse(h.SC, host, "depot", &core.Table{Rows: rows})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v", row, err)
		}
	}
	// Another platform's app is there on that platform: it is kept as it is.
	other := "windows"
	if host == other {
		other = "linux"
	}
	if a, err := Parse(h.SC, other, "depot", &core.Table{Rows: [][]string{{"app", `C:\Program Files\Depot desk\DepotDesk.exe`}}}); err != nil || a.App != `C:\Program Files\Depot desk\DepotDesk.exe` {
		t.Errorf("another platform's app is not looked for here: %v %v", a, err)
	}
	if _, err := Parse(h.SC, "linux", "depot", &core.Table{Rows: [][]string{{"args", "x"}}}); err == nil || !strings.Contains(err.Error(), "has no app") {
		t.Errorf("an app is required: %v", err)
	}
	if a, err := Parse(h.SC, "macos", "notes", &core.Table{Rows: [][]string{{"app", "com.apple.TextEdit"}}}); err != nil || a.App != "com.apple.TextEdit" {
		t.Errorf("a bundle identifier is kept as it is: %v %v", a, err)
	}
}

func TestScreenshots(t *testing.T) {
	p, _, _ := desk()
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	h.OK("the depot app is launched")
	_ = h.Fails(`the depot app looks like the "arrivals" screenshot`, `There was no "arrivals.`+platform+`@2x" screenshot to compare with`)
	h.OK(`the depot app looks like the "arrivals" screenshot`)
	if got := screenshotFile("/s", "arrivals", 1.75); got != filepath.Join("/s", "arrivals."+platform+"@1.75x.png") {
		t.Errorf("a display's scale names its screenshots: %s", got)
	}
}

// TestScreenshotsShowTheLookCompared: a failure shows the look that was
// compared with the screenshot, not one taken after it that had not settled.
func TestScreenshotsShowTheLookCompared(t *testing.T) {
	p, _, _ := desk()
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	h.OK("the depot app is launched")
	window := func(c color.RGBA, n int32) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
		img.Set(0, 0, color.RGBA{G: uint8(n), A: 255}) //nolint:gosec // a look's number, small
		return img
	}
	red, blue := color.RGBA{R: 200, A: 255}, color.RGBA{B: 200, A: 255}
	p.shows = func(int32) image.Image { return window(red, 0) }
	_ = h.Fails(`the depot app looks like the "late" screenshot`, `There was no "late.`+platform+`@2x" screenshot`)
	// Blue for two looks, settled; then green and blue in turn, until the
	// wait ends. A look's number tells it from the others.
	p.looks.Store(0)
	p.shows = func(n int32) image.Image {
		switch {
		case n <= 2:
			return window(blue, 0)
		case n%2 == 1:
			return window(color.RGBA{G: 200, A: 255}, n)
		default:
			return window(blue, n)
		}
	}
	h.Sink.Attachments = nil
	_ = h.Fails(`within 1s the depot app looks like the "late" screenshot`, `The depot app does not look like its "late" screenshot: 15 pixels of 16 differ`)
	// The check's own, before the window a failure attaches.
	var shown image.Image
	for _, a := range h.Sink.Attachments {
		if a.Name == "the depot app" {
			shown, _ = png.Decode(bytes.NewReader(a.Body))
			break
		}
	}
	if shown == nil || !samePixels(shown, window(blue, 0)) {
		t.Error("the failure shows a look other than the one compared")
	}
}

// TestHideCaret hides a field's cursor: the field looks the same with its
// cursor on and off, its text left as it is.
func TestBlinked(t *testing.T) {
	field := func(on bool, mark image.Point) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 60, 40))
		draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
		for y := 10; y < 28; y++ {
			if on {
				img.Set(20, y, color.Black)
				img.Set(21, y, color.Gray{0x80})
			}
		}
		img.Set(mark.X, mark.Y, color.Black)
		return img
	}
	r, ok := blinked(field(true, image.Pt(50, 5)), field(false, image.Pt(50, 5)))
	if want := image.Rect(19, 8, 23, 30); !ok || r != want {
		t.Errorf("a cursor that blinked: %v, %v; want %v", r, ok, want)
	}
	if hidden := HideCaret(field(true, image.Pt(50, 5)), r); !samePixels(hidden, HideCaret(field(false, image.Pt(50, 5)), r)) {
		t.Error("hidden where it blinked, the cursor still shows")
	}
	if _, ok := blinked(field(true, image.Pt(50, 5)), field(false, image.Pt(40, 5))); ok {
		t.Error("a cursor's blink and another change: taken for a blink")
	}
	if _, ok := blinked(field(false, image.Pt(50, 5)), field(false, image.Pt(40, 5))); ok {
		t.Error("a dot that moved: taken for a blink")
	}
}

func TestHideCaret(t *testing.T) {
	field := func(on bool) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 40, 20))
		for y := range 20 {
			for x := range 40 {
				img.Set(x, y, color.White)
			}
		}
		for y := 4; y < 16; y++ {
			img.Set(30, y, color.Black) // a letter's stroke, after the cursor
			if on {
				img.Set(20, y, color.Black)
				img.Set(21, y, color.Gray{0x80}) // its anti-aliasing
			}
		}
		return img
	}
	at := image.Rect(18, 2, 24, 18)
	on, off := HideCaret(field(true), at), HideCaret(field(false), at)
	if !samePixels(on, off) {
		t.Error("the field with its cursor on does not look as it does with it off")
	}
	if !samePixels(off, field(false)) {
		t.Error("hiding a cursor that is off changed the field")
	}
	if c := color.GrayModel.Convert(on.At(30, 8)).(color.Gray); c.Y != 0 {
		t.Errorf("the text after the cursor was changed: %v", c)
	}
	// Its place rounded a pixel either way, after a letter's smoothed edge:
	// the same screenshot.
	letter := func(caretAt int) *image.RGBA {
		img := field(false)
		for y := 4; y < 16; y++ {
			img.Set(16, y, color.Black)
			img.Set(17, y, color.Gray{0x80})
			img.Set(caretAt, y, color.Black)
		}
		return img
	}
	if !samePixels(HideCaret(letter(20), image.Rect(18, 2, 24, 18)), HideCaret(letter(20), image.Rect(19, 2, 25, 18))) {
		t.Error("a cursor hidden from a pixel later looks different")
	}
	edge := HideCaret(field(true), image.Rect(0, 0, 3, 20))
	if c := color.GrayModel.Convert(edge.At(1, 1)).(color.Gray); c.Y != 0xff {
		t.Errorf("at the screenshot's edge, the cursor takes the color to its right: %v", c)
	}
	if got := HideCaret(field(true), image.Rect(50, 0, 60, 20)); !samePixels(got, field(true)) {
		t.Error("a cursor outside the screenshot changed it")
	}
}

// TestAwaySpot puts the pointer beside the window, on the screen, and at the
// screen's edge when the window fills it.
func TestAwaySpot(t *testing.T) {
	screen := image.Rect(0, 39, 2056, 1329)
	for _, c := range []struct {
		window image.Rectangle
		want   image.Point
	}{
		{image.Rect(708, 209, 1348, 821), image.Pt(1364, 515)},  // to its right
		{image.Rect(1500, 209, 2056, 821), image.Pt(1484, 515)}, // to its left
		{image.Rect(0, 39, 2056, 700), image.Pt(1028, 716)},     // under it
		{image.Rect(0, 39, 2056, 1329), image.Pt(2054, 684)},    // it fills the screen
	} {
		if got := AwaySpot(c.window, screen); got != c.want {
			t.Errorf("AwaySpot(%v) = %v, want %v", c.window, got, c.want)
		}
	}
}

func samePixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if color.RGBAModel.Convert(a.At(x, y)) != color.RGBAModel.Convert(b.At(x, y)) {
				return false
			}
		}
	}
	return true
}

// TestRecordings keeps a trace of the app (its window after each step) and
// a video of the scenario's desktop (H.264 in an MP4 file), as the settings
// say, and attaches them; the run keeps one video of its scenarios.
func TestRecordings(t *testing.T) {
	p, _, _ := desk()
	p.changing = true
	d := &fakeDriver{proc: p}
	h := cloudtest.NewWith(t, map[string]any{"desktop-core": map[string]any{"traces": "always", "videos": "always"}}, appcore.Pack(), Pack(), files.Pack())
	h.Start(&core.Plan{})
	if err := Register(h.SC, &App{Name: "depot", App: "depot", Driver: d, Dir: h.Dir}); err != nil {
		t.Fatal(err)
	}
	h.OK("the depot app is launched")
	time.Sleep(3 * time.Second / recording.FrameRate)
	h.OK(`the "Register" button is shown in the depot app`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "desktop", "traces", "*-depot-*.html"))
	videos, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "desktop", "videos", "test-*.mp4"))
	if len(traces) != 1 || len(videos) != 1 {
		t.Fatalf("traces %v, videos %v", traces, videos)
	}
	page, _ := os.ReadFile(traces[0])
	for _, want := range []string{"the depot app is launched", `the &#34;Register&#34; button is shown in the depot app`, "data:image/png;base64,"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("the trace lacks %q", want)
		}
	}
	movie, _ := os.ReadFile(videos[0])
	if len(movie) < 8 || string(movie[4:8]) != "ftyp" || !bytes.Contains(movie, []byte("avcC")) {
		t.Errorf("the video is not H.264 in an MP4 file: %q", movie[:min(len(movie), 16)])
	}
	// A chapter for each step, in both forms players read.
	for _, want := range []string{"chpl", "chap", "* the depot app is launched"} {
		if !bytes.Contains(movie, []byte(want)) {
			t.Errorf("the video's chapters lack %q", want)
		}
	}
	var names []string
	for _, a := range h.Sink.Attachments {
		names = append(names, a.Name)
	}
	if got := strings.Join(names, ", "); got != "the depot app's trace" {
		t.Errorf("a passed scenario's video is in its folder only, not the report: attachments %s", got)
	}
	if err := h.Suite.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := os.ReadFile(filepath.Join(h.Dir, ".axx", "desktop", "videos", "run.mp4"))
	if err != nil || len(run) <= len(movie) || string(run[4:8]) != "ftyp" {
		t.Errorf("the run's video is the scenario's after cards: %d bytes, the scenario's %d (%v)", len(run), len(movie), err)
	}
	if !bytes.Contains(run, []byte("✓ "+h.SC.Name)) {
		t.Errorf("the run's video has no chapter for its scenario %q", h.SC.Name)
	}
}

// A scenario that passes keeps none by default: traces are kept for the
// scenarios that fail, and videos for none.
func TestRecordingsByDefault(t *testing.T) {
	p, _, _ := desk()
	h := harness(t, &fakeDriver{proc: p})
	h.OK("the depot app is launched")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	kept := func(kind string) []string {
		files, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "desktop", kind, "*"))
		return files
	}
	if kept := append(kept("traces"), kept("videos")...); len(kept) > 0 {
		t.Errorf("a passing scenario kept %v", kept)
	}
	p, _, _ = desk()
	h = harness(t, &fakeDriver{proc: p})
	h.OK("the depot app is launched")
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	// Traces and videos are asked for: they slow every step.
	if kept := append(kept("traces"), kept("videos")...); len(kept) > 0 {
		t.Errorf("a failed scenario kept %v, asked for nothing", kept)
	}
}

// snapProc snapshots its window slowly, as screencapture does, and tells
// whether it was used while a snapshot was being taken.
type snapProc struct {
	*fakeProc
	inFlight, usedMeanwhile atomic.Int32
}

func (p *snapProc) Snapshot() ([]byte, float64, error) {
	p.inFlight.Add(1)
	defer p.inFlight.Add(-1)
	time.Sleep(300 * time.Millisecond)
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	return b.Bytes(), 2, nil
}

func (p *snapProc) Click(c Control) error {
	if p.inFlight.Load() > 0 {
		p.usedMeanwhile.Add(1)
	}
	return p.fakeProc.Click(c)
}

func (p *snapProc) Type(text string) error {
	if p.inFlight.Load() > 0 {
		p.usedMeanwhile.Add(1)
	}
	return p.fakeProc.Type(text)
}

// A trace's capture is taken while the next step looks at the app, and the
// app is used only once it is done: it shows the window as the step before
// left it.
func TestTraceSnapshotsAreTakenWhileTheNextStepLooks(t *testing.T) {
	p, _, _ := desk()
	sp := &snapProc{fakeProc: p}
	h := cloudtest.NewWith(t, map[string]any{"desktop-core": map[string]any{"traces": "always"}}, appcore.Pack(), Pack(), files.Pack())
	h.Start(&core.Plan{})
	if err := Register(h.SC, &App{Name: "depot", App: "depot", Driver: &fakeDriver{proc: p, wrap: sp}, Dir: h.Dir}); err != nil {
		t.Fatal(err)
	}
	h.OK("the depot app is launched")
	start := time.Now()
	h.OK(`the depot app shows "No parcels registered yet"`)
	if looked := time.Since(start); looked >= 300*time.Millisecond {
		t.Errorf("a look waited %s for the capture", looked)
	}
	h.OK(`the "Reference" field in the depot app is filled with "PX-DSK-4201"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if n := sp.usedMeanwhile.Load(); n > 0 {
		t.Errorf("the app was used %d times while a capture was taken", n)
	}
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "desktop", "traces", "*-depot-*.html"))
	if len(traces) != 1 {
		t.Fatalf("traces %v", traces)
	}
	page, _ := os.ReadFile(traces[0])
	if n := strings.Count(string(page), "data:image/png;base64,"); n != 3 {
		t.Errorf("the trace has %d captures, not one for each of 3 steps", n)
	}
}

func TestOutline(t *testing.T) {
	tree := node("AXWindow", "Depot desk", nil, node("push button", "Register", &fakeControl{}))
	tree.Children[0].Attrs = map[string]string{"enabled": "false"}
	want := "AXWindow name=\"Depot desk\"\n  push_button name=\"Register\" enabled=\"false\"\n"
	if got := Outline(tree); got != want {
		t.Errorf("%q", got)
	}
	s, _ := selector("xpath=//push_button[@enabled='false']")
	if found, err := s.find(tree); err != nil || len(found) != 1 {
		t.Errorf("an xpath= selector names a role as an element: %v %v", found, err)
	}
	s, _ = selector("xpath=//AXWindow/push_button/following-sibling::*")
	if found, err := s.find(tree); err != nil || len(found) != 0 {
		t.Errorf("%v %v", found, err)
	}
}

// An argument that names a file of the project is made absolute, an
// option's value too: a .app starts in /, not in the project, and a browser
// takes no relative profile.
func TestArguments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "desk.py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".axx", "desk"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{Dir: dir, Args: []string{
		"desk.py", "--qt5", "missing.py", filepath.Join(dir, "desk.py"),
		"--user-data-dir=.axx/desk", "--config=missing.yaml", "--depot=Leipzig",
	}}
	want := []string{
		filepath.Join(dir, "desk.py"), "--qt5", "missing.py", filepath.Join(dir, "desk.py"),
		"--user-data-dir=" + filepath.Join(dir, ".axx", "desk"), "--config=missing.yaml", "--depot=Leipzig",
	}
	if got := a.Arguments(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%v", got)
	}
}

// TestRemoveAllWaitsForAFileInUse empties a home whose file is held a moment
// longer, as a stopped browser's helper holds its profile on Windows: open
// there, a folder no one may write to elsewhere.
func TestRemoveAllWaitsForAFileInUse(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	held := filepath.Join(home, "AppData", "Local", "Temp")
	if err := os.MkdirAll(held, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(held, "profile.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(held, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	let := make(chan struct{})
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = f.Close()
		_ = os.Chmod(held, 0o755)
		close(let)
	}()
	err = removeAll(home, 5*time.Second)
	<-let
	if err != nil {
		t.Fatalf("a file held a moment longer: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("the home is still there: %v", err)
	}
}

// An app opens with the pointer beside it, wherever the scenario before
// left it, so nothing of it shows hovered.
func TestLaunchedWithThePointerBeside(t *testing.T) {
	p, _, _ := desk()
	d := &fakeDriver{proc: p}
	h := harness(t, d)
	h.OK("the depot app is launched")
	if p.leaves != 1 {
		t.Errorf("the pointer was moved beside the app %d times as it opened", p.leaves)
	}
}

// Beside is beside the window where the screen has room, and nowhere for a
// window that covers the screen.
func TestBeside(t *testing.T) {
	screen := image.Rect(0, 0, 1920, 1080)
	if p, ok := Beside(image.Rect(0, 0, 1280, 880), screen); !ok || p.In(image.Rect(0, 0, 1280, 880)) {
		t.Errorf("beside a window with room right of it: %v, %v", p, ok)
	}
	if p, ok := Beside(screen, screen); ok {
		t.Errorf("a window that covers the screen has nothing beside it: %v", p)
	}
	if p := AwaySpot(screen, screen); p != image.Pt(1918, 540) {
		t.Errorf("away from a window that covers the screen is its edge: %v", p)
	}
}

// A window of one color all over has drawn nothing yet; one that shows
// anything else has.
func TestFlat(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := range 400 {
		for x := range 640 {
			img.Set(x, y, color.Black)
		}
	}
	if !Flat(img) {
		t.Error("a black window shows nothing")
	}
	for y := 180; y < 220; y++ {
		for x := 300; x < 340; x++ {
			img.Set(x, y, color.RGBA{250, 247, 240, 255})
		}
	}
	if Flat(img) {
		t.Error("a window with a picture in it shows something")
	}
	if !Flat(image.NewRGBA(image.Rectangle{})) {
		t.Error("an empty window shows nothing")
	}
}

// The pointer moves beside an app that opens under it, and stays where it
// is over another app or the desktop: Snap's tray window on Windows, a
// square at the screen's corner, opens nowhere near it.
func TestLeaveSpot(t *testing.T) {
	screen := image.Rect(0, 0, 1920, 1080)
	preview := image.Rect(0, 0, 1280, 880)
	if p, ok := LeaveSpot(preview, screen, image.Pt(600, 400)); !ok || p.In(preview) {
		t.Errorf("an app that opened under the pointer: %v, %v", p, ok)
	}
	if p, ok := LeaveSpot(image.Rect(0, 0, 16, 16), screen, image.Pt(960, 540)); ok {
		t.Errorf("a window away from the pointer: moved to %v", p)
	}
	if p, ok := LeaveSpot(screen, screen, image.Pt(960, 540)); ok {
		t.Errorf("a window that covers the screen: moved to %v", p)
	}
}
