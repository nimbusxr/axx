package desktopcore

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
)

// fakeDriver runs fake apps on this machine's OS.
type fakeDriver struct {
	claims, releases, resets, starts int
	proc                             *fakeProc
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
	return k.d.proc, nil
}
func (k *fakeDesk) Release() { k.d.releases++ }

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
}

type fakeControl struct {
	n                  *Node
	enabled, reportsOn bool
	reportsValue       bool
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
func (p *fakeProc) ScrollTo(k appcore.Kind, name string) (Control, error) {
	found, _ := p.Find(k, name, true)
	if len(found) == 0 {
		return nil, nil
	}
	return found[0], nil
}
func (p *fakeProc) ScrollIntoView(Control) error { return nil }
func (p *fakeProc) Click(c Control) error {
	p.clicks = append(p.clicks, c.Name())
	return nil
}

func (p *fakeProc) ClickAt(c Control, x, y float64) error {
	p.clicks = append(p.clicks, c.Name()+" at")
	return nil
}

func (p *fakeProc) Drag(c Control, x1, y1, x2, y2 float64) error {
	p.clicks = append(p.clicks, c.Name()+" dragged")
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
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
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
	h.OK(`the "Courier signature" element in the depot app is clicked at 40, 40`)
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
	a, err := Parse(h.SC, "windows", "depot", tbl, "registry")
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
		{"registry", `Software\X`}:   `unknown macos app property "registry" (supported: app, args, env.<name>, locale, timezone)`,
		{"env.1X", "y"}:              "not an environment variable's name",
	} {
		rows := [][]string{{"app", "TextEdit"}, row[:]}
		if row[0] == "app" {
			rows = rows[1:]
		}
		_, err := Parse(h.SC, "macos", "depot", &core.Table{Rows: rows})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v", row, err)
		}
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

// An argument that names a file of the project is made absolute: a .app
// starts in /, not in the project.
func TestArguments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "desk.py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{Dir: dir, Args: []string{"desk.py", "--qt5", "missing.py", filepath.Join(dir, "desk.py")}}
	want := []string{filepath.Join(dir, "desk.py"), "--qt5", "missing.py", filepath.Join(dir, "desk.py")}
	if got := a.Arguments(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%v", got)
	}
}
