package desktopcore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"runtime"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/internal/hostscenario"
)

// Desktop apps through the axx host. A run in an agent's sandbox cannot
// start an app on the desktop, nor read it: the sandbox keeps it from the
// window server and the accessibility API. When an axx host serves the
// project, the OS pack's side there (core.HostServer) claims the desktop,
// starts the app and does everything a Process does, with the pack's own
// code, and the run's side is a Driver, a Desktop and a Process that ask it
// to (this file). The app's home is a folder of the project's, which the
// run empties and fills itself.
//
// What crosses is what a Desktop and a Process do, by name (hosted_serve.go
// has the host's side): the run names no program, and no file. The host
// registers the app from the rows the run's step read, as the step did, in
// a suite confined to the project. The app is the agent's code, started
// past its sandbox: the person who runs the host approves it first.

// Hosted is the app as it registers: itself, or, when an axx host serves
// the project and the app is for this machine's OS, with a driver that has
// the host do the driver's work. pack is the OS pack, whose side in the
// host serves it (ServeHost); rows are the registration the host registers
// the app from, as the step read them.
func Hosted(sc *core.Scenario, pack string, app *App, rows []Property) *App {
	if !sc.Suite().Hosted() || app.Driver.Platform() != appcore.Host() {
		return app
	}
	// The scenario's place in the host is kept from now, the app's
	// registration: it ends after what the scenario makes later, its
	// running apps among it, which are stopped in the host first.
	hostscenario.Key(sc)
	hosted := *app
	hosted.Driver = &hostedDriver{pack: pack, local: app.Driver, rows: rows}
	return &hosted
}

// hostedDriver is an app's driver in a run an axx host serves.
type hostedDriver struct {
	pack  string
	local Driver
	// rows are the app's registration, for the host.
	rows []Property
}

func (d *hostedDriver) Platform() string { return d.local.Platform() }

// What every call says: the scenario, as the host knows it, and the app.
type (
	scenarioRef struct {
		Scenario string `json:"scenario"`
	}
	appRef struct {
		Scenario string `json:"scenario"`
		App      string `json:"app"`
	}
	registerParams struct {
		appRef
		Rows []Property `json:"rows"`
	}
	claimAnswer struct {
		DataDir string `json:"dataDir"`
	}
	registerAnswer struct {
		// Home is the app's home on the scenario's desktop.
		Home string `json:"home"`
	}
	startAnswer struct {
		// Snapshots says the app's process captures its window as a PNG
		// for a trace (snapshotter).
		Snapshots bool `json:"snapshots,omitempty"`
	}
	screenAnswer struct {
		PNG        []byte      `json:"png"`
		Scale      float64     `json:"scale"`
		Pointer    image.Point `json:"pointer"`
		HasPointer bool        `json:"hasPointer,omitempty"`
	}
)

func (d *hostedDriver) Claim(sc *core.Scenario) (Desktop, error) {
	l, err := hostscenario.Open(sc, d.pack, nil)
	if err != nil {
		return nil, err
	}
	desk := &hostedDesktop{link: l, sc: sc, key: hostscenario.Key(sc), homes: map[string]string{}}
	var a claimAnswer
	if err := l.Call(sc.Context(), sc, "claim", scenarioRef{Scenario: desk.key}, &a); err != nil {
		return nil, err
	}
	desk.dataDir = a.DataDir
	return desk, nil
}

// hostedDesktop is a scenario's desktop, which the host claimed for it.
type hostedDesktop struct {
	link    *hostscenario.Link
	sc      *core.Scenario
	key     string
	dataDir string

	mu sync.Mutex
	// homes are the homes of the apps registered in the host, by app.
	homes map[string]string
}

// register registers the app in the host's scenario, the first time, and
// gives its home.
func (d *hostedDesktop) register(app *App) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if h, ok := d.homes[app.Name]; ok {
		return h, nil
	}
	hd, ok := app.Driver.(*hostedDriver)
	if !ok {
		return "", errors.New("the " + app.Name + " app did not register for the axx host")
	}
	var a registerAnswer
	if err := d.link.Call(d.sc.Context(), d.sc, "register", registerParams{appRef: d.ref(app), Rows: hd.rows}, &a); err != nil {
		return "", err
	}
	d.homes[app.Name] = a.Home
	return a.Home, nil
}

func (d *hostedDesktop) ref(app *App) appRef { return appRef{Scenario: d.key, App: app.Name} }

// Home is the app's home, as the host's desktop has it. A host that cannot
// register the app says so as the app is reset or started.
func (d *hostedDesktop) Home(app *App) string {
	if h, err := d.register(app); err == nil {
		return h
	}
	return HomeOf(d.sc, app)
}

func (d *hostedDesktop) DataDir() string { return d.dataDir }

func (d *hostedDesktop) Reset(sc *core.Scenario, app *App) error {
	if _, err := d.register(app); err != nil {
		return err
	}
	return d.link.Call(sc.Context(), sc, "reset", d.ref(app), nil)
}

// Start has the host start the app: with what the packs that build on this
// one do as an app launches, which they do there (OnLaunch in the host's
// scenario).
func (d *hostedDesktop) Start(sc *core.Scenario, app *App) (Process, error) {
	return d.process(sc, app, "start")
}

func (d *hostedDesktop) Watch(sc *core.Scenario, app *App) (Process, error) {
	return d.process(sc, app, "watch")
}

func (d *hostedDesktop) process(sc *core.Scenario, app *App, op string) (Process, error) {
	if _, err := d.register(app); err != nil {
		return nil, err
	}
	var a startAnswer
	if err := d.link.Call(sc.Context(), sc, op, d.ref(app), &a); err != nil {
		return nil, err
	}
	p := &hostedProcess{desk: d, ref: d.ref(app)}
	if a.Snapshots {
		return hostedSnapshots{p}, nil
	}
	return p, nil
}

// Screen is what the host's desktop shows of the scenario, for its video:
// asked beside the scenario's steps, not in their turn.
func (d *hostedDesktop) Screen() (Screen, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(d.sc.Context()), 10*time.Second)
	defer cancel()
	var a screenAnswer
	if err := d.link.Call(ctx, nil, "screen", scenarioRef{Scenario: d.key}, &a); err != nil {
		return Screen{}, err
	}
	if len(a.PNG) == 0 {
		return Screen{}, nil
	}
	img, err := png.Decode(bytes.NewReader(a.PNG))
	if err != nil {
		return Screen{}, err
	}
	return Screen{Image: img, Scale: a.Scale, Pointer: a.Pointer, HasPointer: a.HasPointer}, nil
}

func (d *hostedDesktop) Release() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(d.sc.Context()), 2*time.Minute)
	defer cancel()
	_ = d.link.Call(ctx, nil, "release", scenarioRef{Scenario: d.key}, nil)
}

// procParams ask for what a Process does. Drop are the controls the run no
// longer has, which the host forgets.
type procParams struct {
	appRef
	Drop    []uint64 `json:"drop,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Name    string   `json:"name,omitempty"`
	Shown   bool     `json:"shown,omitempty"`
	Text    string   `json:"text,omitempty"`
	Control uint64   `json:"control,omitempty"`
	Anchor  Anchor   `json:"anchor,omitzero"`
	X       float64  `json:"x,omitempty"`
	Y       float64  `json:"y,omitempty"`
	X2      float64  `json:"x2,omitempty"`
	Y2      float64  `json:"y2,omitempty"`
}

// procAnswer is the answer: what the operation gives, or the failure the
// layer above acts on by its kind (a place off its control, a control that
// lost its place, a check that failed), which crosses as what it is.
type procAnswer struct {
	Controls []wireControl `json:"controls,omitempty"`
	Control  *wireControl  `json:"control,omitempty"`
	Names    []string      `json:"names,omitempty"`
	OK       bool          `json:"ok,omitempty"`
	Reported bool          `json:"reported,omitempty"`
	Text     string        `json:"text,omitempty"`
	PNG      []byte        `json:"png,omitempty"`
	Scale    float64       `json:"scale,omitempty"`
	// CaretField is the window's focused text field whose cursor has no place (CaretFielder).
	CaretField *image.Rectangle `json:"caretField,omitempty"`
	Tree       *wireNode        `json:"tree,omitempty"`
	Describe   map[string]any   `json:"describe,omitempty"`

	Off    *Off                 `json:"off,omitempty"`
	Lost   string               `json:"lost,omitempty"`
	Failed *core.AssertionError `json:"failed,omitempty"`
}

// wireControl is a control as it crosses: what the host calls it, and its
// name.
type wireControl struct {
	ID   uint64 `json:"id"`
	Name string `json:"name,omitempty"`
}

// wireNode is an element of an app's tree as it crosses.
type wireNode struct {
	Role     string            `json:"role,omitempty"`
	Name     string            `json:"name,omitempty"`
	ID       string            `json:"id,omitempty"`
	Value    string            `json:"value,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Children []*wireNode       `json:"children,omitempty"`
	Control  *wireControl      `json:"control,omitempty"`
}

// hostedProcess is an app that runs on the host's desktop.
type hostedProcess struct {
	// caretField is the focused text field the host named with the window it last gave.
	caretField *image.Rectangle
	desk       *hostedDesktop
	ref        appRef

	mu      sync.Mutex
	dropped []uint64
}

// hostedSnapshots is a hosted app whose process captures its window for a
// trace (snapshotter).
type hostedSnapshots struct{ *hostedProcess }

func (p hostedSnapshots) Snapshot() ([]byte, float64, error) {
	// Taken beside the scenario's next step, as the capture is in a run.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(p.desk.sc.Context()), time.Minute)
	defer cancel()
	var a procAnswer
	if err := p.desk.link.Call(ctx, nil, "proc.snapshot", procParams{appRef: p.ref}, &a); err != nil {
		return nil, 0, err
	}
	return a.PNG, a.Scale, nil
}

// hostedControl is a control the host found, by what the host calls it. The
// host keeps it until the run has it no longer.
type hostedControl struct {
	p    *hostedProcess
	id   uint64
	name string
}

func (c *hostedControl) Name() string { return c.name }

func (c *hostedControl) Enabled() (enabled, reported bool) {
	a, err := c.p.call("control.enabled", procParams{Control: c.id})
	if err != nil {
		return false, false
	}
	return a.OK, a.Reported
}

func (c *hostedControl) Value() (value string, reported bool) {
	a, err := c.p.call("control.value", procParams{Control: c.id})
	if err != nil {
		return "", false
	}
	return a.Text, a.Reported
}

// control is the run's control for one the host found.
func (p *hostedProcess) control(w *wireControl) Control {
	if w == nil {
		return nil
	}
	c := &hostedControl{p: p, id: w.ID, name: w.Name}
	// The host forgets the control once the run has it no longer: with the
	// run's next call.
	runtime.AddCleanup(c, func(id uint64) {
		p.mu.Lock()
		p.dropped = append(p.dropped, id)
		p.mu.Unlock()
	}, w.ID)
	return c
}

func (p *hostedProcess) controls(ws []wireControl) []Control {
	out := make([]Control, 0, len(ws))
	for i := range ws {
		out = append(out, p.control(&ws[i]))
	}
	return out
}

// id is what the host calls a control of this app's.
func (p *hostedProcess) id(c Control) (uint64, error) {
	hc, ok := c.(*hostedControl)
	if !ok || hc.p != p {
		return 0, errors.New("the control is not one of the " + p.ref.App + " app's on the axx host's desktop")
	}
	return hc.id, nil
}

// call asks the host for something of the app, in the scenario's turn.
func (p *hostedProcess) call(op string, params procParams) (*procAnswer, error) {
	params.appRef = p.ref
	p.mu.Lock()
	params.Drop, p.dropped = p.dropped, nil
	p.mu.Unlock()
	sc := p.desk.sc
	var a procAnswer
	if err := p.desk.link.Call(sc.Context(), sc, "proc."+op, params, &a); err != nil {
		return nil, err
	}
	switch {
	case a.Off != nil:
		return nil, a.Off
	case a.Lost != "":
		return nil, &Lost{Err: errors.New(a.Lost)}
	case a.Failed != nil:
		return nil, a.Failed
	}
	return &a, nil
}

func (p *hostedProcess) act(op string, params procParams) error {
	_, err := p.call(op, params)
	return err
}

func (p *hostedProcess) on(op string, c Control, params procParams) error {
	id, err := p.id(c)
	if err != nil {
		return err
	}
	params.Control = id
	return p.act(op, params)
}

func (p *hostedProcess) Find(k appcore.Kind, name string, shown bool) ([]Control, error) {
	a, err := p.call("find", procParams{Kind: k.Noun, Name: name, Shown: shown})
	if err != nil {
		return nil, err
	}
	return p.controls(a.Controls), nil
}

func (p *hostedProcess) Names(k appcore.Kind) []string {
	a, err := p.call("names", procParams{Kind: k.Noun})
	if err != nil {
		return nil
	}
	return a.Names
}

func (p *hostedProcess) Shows(text string) (bool, error) {
	a, err := p.call("shows", procParams{Text: text})
	if err != nil {
		return false, err
	}
	return a.OK, nil
}

func (p *hostedProcess) Texts() []string {
	a, err := p.call("texts", procParams{})
	if err != nil {
		return nil
	}
	return a.Names
}

func (p *hostedProcess) Front() error { return p.act("front", procParams{}) }
func (p *hostedProcess) Away() error  { return p.act("away", procParams{}) }
func (p *hostedProcess) Leave() error { return p.act("leave", procParams{}) }

func (p *hostedProcess) ScrollTo(k appcore.Kind, name string) (Control, error) {
	a, err := p.call("scrollTo", procParams{Kind: k.Noun, Name: name})
	if err != nil {
		return nil, err
	}
	return p.control(a.Control), nil
}

func (p *hostedProcess) ScrollIntoView(c Control) error {
	return p.on("scrollIntoView", c, procParams{})
}

func (p *hostedProcess) Click(c Control) error { return p.on("click", c, procParams{}) }

func (p *hostedProcess) ClickAt(c Control, from Anchor, x, y float64) error {
	return p.on("clickAt", c, procParams{Anchor: from, X: x, Y: y})
}

func (p *hostedProcess) Drag(c Control, from Anchor, x1, y1, x2, y2 float64) error {
	return p.on("drag", c, procParams{Anchor: from, X: x1, Y: y1, X2: x2, Y2: y2})
}

func (p *hostedProcess) Key(spec string) error  { return p.act("key", procParams{Text: spec}) }
func (p *hostedProcess) Type(text string) error { return p.act("type", procParams{Text: text}) }

func (p *hostedProcess) Window() (image.Image, float64, error) {
	a, err := p.call("window", procParams{})
	if err != nil {
		return nil, 0, err
	}
	img, err := png.Decode(bytes.NewReader(a.PNG))
	if err != nil {
		return nil, 0, err
	}
	p.caretField = a.CaretField
	return img, a.Scale, nil
}

// CaretField is what the host said of the window it last gave (CaretFielder).
func (p *hostedProcess) CaretField() (image.Rectangle, bool) {
	if p.caretField == nil {
		return image.Rectangle{}, false
	}
	return *p.caretField, true
}

func (p *hostedProcess) Tree() (*Node, error) {
	a, err := p.call("tree", procParams{})
	if err != nil {
		return nil, err
	}
	return p.node(a.Tree), nil
}

func (p *hostedProcess) node(w *wireNode) *Node {
	if w == nil {
		return nil
	}
	n := &Node{Role: w.Role, Name: w.Name, ID: w.ID, Value: w.Value, Attrs: w.Attrs, Control: p.control(w.Control)}
	for _, c := range w.Children {
		n.Children = append(n.Children, p.node(c))
	}
	return n
}

func (p *hostedProcess) Stop() error { return p.act("stop", procParams{}) }

// Exited reports whether the app has stopped on its own: one the host no
// longer answers for has.
func (p *hostedProcess) Exited() bool {
	a, err := p.call("exited", procParams{})
	return err != nil || a.OK
}

func (p *hostedProcess) Describe() map[string]any {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(p.desk.sc.Context()), 30*time.Second)
	defer cancel()
	var a procAnswer
	if err := p.desk.link.Call(ctx, nil, "proc.describe", procParams{appRef: p.ref}, &a); err != nil {
		return map[string]any{"app": p.ref.App, "axx host": err.Error()}
	}
	return a.Describe
}
