package mobilecore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/hostscenario"
	"github.com/nimbusxr/axx/packs/internal/hostwire"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// Phones through the axx host. A run in an agent's sandbox cannot start a
// simulator or an emulator: the sandbox keeps it from the services and the
// hypervisor they need. When an axx host serves the project, the platform
// pack's side there (core.HostServer) leases the device, installs and
// starts the app and does everything else that is the machine's, with the
// pack's own code, and the run's side is a Platform and a Device that ask it
// to (this file); the run drives the app's screen itself, through the
// device's driver, which listens on this machine's loopback.
//
// What crosses is what a Device does, by name (hosted_serve.go has the
// host's side): the run names no program, and no file but one of the
// project's, which the host's suite checks (core.Suite.Confine). The app
// itself is the agent's code, started past its sandbox: the person who runs
// the host approves it first (HostPlatform.HostApproval).

// Property is a row of an app's registration: a property and its value, as
// the step read it (references to the environment and to secrets filled in).
type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HostPlatform is a platform the axx host can run for a run in a sandbox.
type HostPlatform interface {
	Platform
	// HostApproval says, for the person who runs the host, what starting
	// the app on their machine is and what the app reaches there: they
	// approve it before the host starts it. Its text names everything of
	// the registration and the pack's settings that decides what runs.
	HostApproval(sc *core.Scenario) core.HostApproval
}

// Hosted is the platform an app registers with: local, or, when an axx host
// serves the project, one that has the host run local's work. pack is the
// platform pack, whose side in the host serves it (ServeHost); rows are the
// registration the host registers the app from, as the step did here.
func Hosted(sc *core.Scenario, pack, app string, rows []Property, local Platform) Platform {
	if !sc.Suite().Hosted() || !local.RunsHere() {
		return local
	}
	// A run a person watches or pauses shows its devices where it runs, to
	// the person who started it: it is theirs, outside any sandbox.
	if watching, _ := sc.Suite().Watching(); watching || sc.Suite().Pausing() {
		return local
	}
	// The scenario's place in the host is kept from now, the app's
	// registration: it ends after what the scenario makes later, its
	// running apps among it, which are stopped in the host first.
	hostscenario.Key(sc)
	p := &hostedPlatform{pack: pack, app: app, rows: rows, local: local}
	if _, ok := local.(Camera); ok {
		return &hostedCameraPlatform{hostedPlatform: p}
	}
	return p
}

type hostedPlatform struct {
	pack, app string
	rows      []Property
	local     Platform

	mu    sync.Mutex
	phone *hostedPhone // the phone in the scene the app's camera sees
}

func (p *hostedPlatform) Kind() string   { return p.local.Kind() }
func (p *hostedPlatform) RunsHere() bool { return true }

// hostLink is a run's connection to a platform pack's side in the host: one
// for the run, which its scenarios share.
type hostLink struct {
	*hostscenario.Link

	mu      sync.Mutex
	devices map[string]*hostedDevice // by scenario key and app
}

// link is the suite's connection to pack's side in the host.
func link(sc *core.Scenario, pack string) (*hostLink, error) {
	return core.Cached(sc.Suite(), Name+"/host/"+pack, func() (*hostLink, error) {
		l := &hostLink{devices: map[string]*hostedDevice{}}
		var err error
		l.Link, err = hostscenario.Open(sc, pack, l.told)
		return l, err
	})
}

// What the host tells the run of a device.
type (
	toldTouch struct {
		Scenario string       `json:"scenario"`
		App      string       `json:"app"`
		Touch    appium.Touch `json:"touch"`
	}
	toldArrival struct {
		Scenario string `json:"scenario"`
		App      string `json:"app"`
		Journey  int    `json:"journey"`
	}
)

func (l *hostLink) told(op string, params json.RawMessage) {
	switch op {
	case "touch":
		var m toldTouch
		if hostwire.Read(params, &m) == nil {
			if d := l.device(m.Scenario, m.App); d != nil {
				d.touched(m.Touch)
			}
		}
	case "arrived":
		var m toldArrival
		if hostwire.Read(params, &m) == nil {
			if d := l.device(m.Scenario, m.App); d != nil {
				d.arrived(m.Journey)
			}
		}
	}
}

func (l *hostLink) device(scenario, app string) *hostedDevice {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.devices[scenario+"\x00"+app]
}

// hostedApps are the apps a scenario registered in the host, by the link
// each registered through.
type hostedApps struct {
	// mu is held while an app registers in the host, a call.
	mu   sync.Mutex
	apps map[*hostLink]map[string]bool
}

var hostedScenario = core.NewStateKey(Name+"/hosted", func(*core.Scenario) *hostedApps {
	return &hostedApps{apps: map[*hostLink]map[string]bool{}}
}, nil)

// What every call says: the scenario, as the host knows it, and the app.
type appRef struct {
	Scenario string `json:"scenario"`
	App      string `json:"app"`
}

// registerParams registers an app in the host's scenario, as the step
// registered it in the run's.
type registerParams struct {
	appRef
	Rows []Property `json:"rows"`
}

// in gives the scenario's link to the host with the app registered there.
func (p *hostedPlatform) in(sc *core.Scenario) (*hostLink, appRef, error) {
	l, err := link(sc, p.pack)
	if err != nil {
		return nil, appRef{}, err
	}
	ref := appRef{Scenario: hostscenario.Key(sc), App: p.app}
	st := hostedScenario.Of(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.apps[l][p.app] {
		return l, ref, nil
	}
	if err := l.Call(sc.Context(), sc, "register", registerParams{appRef: ref, Rows: p.rows}, nil); err != nil {
		return nil, appRef{}, err
	}
	if st.apps[l] == nil {
		st.apps[l] = map[string]bool{}
	}
	st.apps[l][p.app] = true
	return l, ref, nil
}

// startAnswer is the device the host started, for the run to drive.
type startAnswer struct {
	Session   sessionInfo `json:"session"`
	Stream    string      `json:"stream"`
	ScreenKey string      `json:"screenKey"`
}

// sessionInfo is the driver's session of the app: the run attaches to it.
type sessionInfo struct {
	URL          string         `json:"url"`
	Selectors    bool           `json:"selectors,omitempty"`
	ID           string         `json:"id"`
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

func (p *hostedPlatform) Start(sc *core.Scenario) (Device, error) {
	l, ref, err := p.in(sc)
	if err != nil {
		return nil, err
	}
	var a startAnswer
	if err := l.Call(sc.Context(), sc, "start", ref, &a); err != nil {
		return nil, err
	}
	d := &hostedDevice{link: l, ref: ref, platform: p, stream: a.Stream, screenKey: a.ScreenKey, journeys: map[int]chan struct{}{}}
	d.attach(a.Session)
	l.mu.Lock()
	l.devices[ref.Scenario+"\x00"+ref.App] = d
	l.mu.Unlock()
	return d, nil
}

// hostedCameraPlatform is a hosted platform whose apps' camera axx plays.
type hostedCameraPlatform struct{ *hostedPlatform }

type seeParams struct {
	appRef
	Path string `json:"path"`
}

func (p *hostedCameraPlatform) See(sc *core.Scenario, path string) error {
	l, ref, err := p.in(sc)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var a struct {
		Scene bool       `json:"scene"`
		Pose  scene.Mat4 `json:"pose"`
	}
	if err := l.Call(sc.Context(), sc, "see", seeParams{appRef: ref, Path: abs}, &a); err != nil {
		return err
	}
	var phone *hostedPhone
	if a.Scene {
		// The scene's cameras and paths are read here, where the steps
		// that move the phone look them up: the host has read the file too.
		s, err := scene.Load(abs)
		if err != nil {
			return err
		}
		phone = &hostedPhone{link: l, ref: ref, scene: s, pose: a.Pose}
	}
	p.mu.Lock()
	p.phone = phone
	p.mu.Unlock()
	return nil
}

func (p *hostedCameraPlatform) ScenePhone() ScenePhone {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phone == nil {
		return nil
	}
	return p.phone
}

// hostedPhone is the phone in the scene an app's camera sees, in the host.
type hostedPhone struct {
	link  *hostLink
	ref   appRef
	scene *scene.Scene

	mu   sync.Mutex
	pose scene.Mat4
}

func (p *hostedPhone) Scene() *scene.Scene { return p.scene }

func (p *hostedPhone) Pose() scene.Mat4 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pose
}

type moveParams struct {
	appRef
	Pose scene.Mat4 `json:"pose"`
}

func (p *hostedPhone) Move(ctx context.Context, pose scene.Mat4) error {
	if err := p.link.Call(ctx, p.link.Scenario(p.ref.Scenario), "phone.move", moveParams{appRef: p.ref, Pose: pose}, nil); err != nil {
		return err
	}
	p.mu.Lock()
	p.pose = pose
	p.mu.Unlock()
	return nil
}

// hostedDevice is an app on a device the host leased and runs.
type hostedDevice struct {
	link     *hostLink
	ref      appRef
	platform *hostedPlatform

	stream, screenKey string

	mu       sync.Mutex
	session  *appium.Session
	journeys map[int]chan struct{}
	journey  int
}

// attach makes the run's session the driver's session of the app: the one
// the host started, which the run drives through the driver's own port.
func (d *hostedDevice) attach(s sessionInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session != nil {
		// The same app, in a session the host started anew (its driver was
		// started again): what holds the session goes on with it.
		d.session.Renew(s.ID)
		return
	}
	c := &appium.Client{URL: s.URL, Selectors: s.Selectors}
	d.session = c.Attach(s.ID, s.Capabilities)
	d.session.Revive = d.revive
}

// revive has the host start the driver again, when it stopped answering
// under the session, and takes the session the host then has.
func (d *hostedDevice) revive(ctx context.Context) error {
	var s sessionInfo
	if err := d.link.Call(ctx, d.link.Scenario(d.ref.Scenario), "revive", d.ref, &s); err != nil {
		return err
	}
	d.attach(s)
	return nil
}

// touched is a touch the host's side made on the device (a scroll of its
// own): what hears the run's own touches hears it too.
func (d *hostedDevice) touched(t appium.Touch) {
	d.mu.Lock()
	s := d.session
	d.mu.Unlock()
	if s != nil && s.Touched != nil {
		s.Touched(t)
	}
}

func (d *hostedDevice) arrived(journey int) {
	d.mu.Lock()
	ch := d.journeys[journey]
	delete(d.journeys, journey)
	d.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (d *hostedDevice) Session() *appium.Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.session
}

// call asks the host for something of the device. Every answer says which
// session the app is in now: a launch or a restart can start a new one.
func (d *hostedDevice) call(ctx context.Context, op string, params, result any) error {
	if params == nil {
		params = d.ref
	}
	// What the scenario's packs noted since goes first (hostscenario): the
	// host's side may act on it in this call.
	var a deviceAnswer
	if err := d.link.Call(ctx, d.link.Scenario(d.ref.Scenario), op, params, &a); err != nil {
		return err
	}
	if a.Session != "" {
		d.mu.Lock()
		s := d.session
		d.mu.Unlock()
		if s != nil && s.Identity() != a.Session {
			s.Renew(a.Session)
		}
	}
	if result != nil && len(a.Result) > 0 {
		return hostwire.Read(a.Result, result)
	}
	return nil
}

// deviceAnswer is an answer about a device: the result, and the session the
// app is in now.
type deviceAnswer struct {
	Session string          `json:"session,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func (d *hostedDevice) Screen(ctx context.Context) (*Screen, error) {
	var w wireScreen
	if err := d.call(ctx, "screen", nil, &w); err != nil {
		return nil, err
	}
	return w.screen(), nil
}

func (d *hostedDevice) Launch(ctx context.Context) error { return d.call(ctx, "launch", nil, nil) }

func (d *hostedDevice) Restart(ctx context.Context) error { return d.call(ctx, "restart", nil, nil) }

func (d *hostedDevice) Background(ctx context.Context) error {
	return d.call(ctx, "background", nil, nil)
}

type textParams struct {
	appRef
	Text string `json:"text"`
}

func (d *hostedDevice) OpenLink(ctx context.Context, url string) error {
	return d.call(ctx, "openLink", textParams{appRef: d.ref, Text: url}, nil)
}

func (d *hostedDevice) HideKeyboard(ctx context.Context) (bool, error) {
	var hid bool
	err := d.call(ctx, "hideKeyboard", nil, &hid)
	return hid, err
}

// nodeParams hand the host a node of a screen the run read: the screen it
// is of, whole, and which node, so that the host has it with its parents
// and its neighbours, as the run has.
type nodeParams struct {
	appRef
	Screen *wireScreen `json:"screen,omitempty"`
	Node   int         `json:"node"`
	Text   string      `json:"text,omitempty"`
}

func (d *hostedDevice) node(n *Node, text string) nodeParams {
	p := nodeParams{appRef: d.ref, Node: -1, Text: text}
	if n != nil {
		w, at := wireScreenOf(n)
		p.Screen, p.Node = w, at
	}
	return p
}

func (d *hostedDevice) ScrollToShow(ctx context.Context, n *Node, name string) (bool, error) {
	var ok bool
	err := d.call(ctx, "scrollToShow", d.node(n, name), &ok)
	return ok, err
}

// OutOfView asks the host's device whether a control a selector found is out of view: the
// device knows its driver, and whether it scrolls to such a control.
func (d *hostedDevice) OutOfView(ctx context.Context, el *appium.Element) bool {
	var out bool
	err := d.call(ctx, "outOfView", textParams{appRef: d.ref, Text: el.ID}, &out)
	return err == nil && out
}

func (d *hostedDevice) Swipe(ctx context.Context, direction string) error {
	return d.call(ctx, "swipe", textParams{appRef: d.ref, Text: direction}, nil)
}

func (d *hostedDevice) Scroll(ctx context.Context, direction string) (bool, error) {
	var moved bool
	err := d.call(ctx, "scroll", textParams{appRef: d.ref, Text: direction}, &moved)
	return moved, err
}

func (d *hostedDevice) ScrollRow(ctx context.Context, row *Node, direction string) (bool, error) {
	var moved bool
	err := d.call(ctx, "scrollRow", d.node(row, direction), &moved)
	return moved, err
}

func (d *hostedDevice) OpenNotifications(ctx context.Context) (Notifications, error) {
	if err := d.call(ctx, "notifications.open", nil, nil); err != nil {
		return nil, err
	}
	return hostedNotifications{d}, nil
}

func (d *hostedDevice) SystemBars(ctx context.Context) ([]Rect, error) {
	var bars []Rect
	err := d.call(ctx, "systemBars", nil, &bars)
	return bars, err
}

func (d *hostedDevice) ScreenKey() string { return d.screenKey }
func (d *hostedDevice) Stream() string    { return d.stream }

type filesParams struct {
	appRef
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	Body []byte `json:"body,omitempty"`
	Max  int    `json:"max,omitempty"`
}

func (d *hostedDevice) Files(ctx context.Context, path string) (core.Files, error) {
	var where string
	if err := d.call(ctx, "files", filesParams{appRef: d.ref, Path: path}, &where); err != nil {
		return nil, err
	}
	return hostedFiles{d: d, path: path, where: where}, nil
}

func (d *hostedDevice) Describe() map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out := map[string]any{}
	if err := d.call(ctx, "describe", nil, &out); err != nil {
		return map[string]any{"device": "one the axx host runs", "error": err.Error()}
	}
	return out
}

type stopParams struct {
	appRef
	Failed bool `json:"failed"`
}

func (d *hostedDevice) Stop(failed bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	err := d.call(ctx, "stop", stopParams{appRef: d.ref, Failed: failed}, nil)
	d.link.mu.Lock()
	delete(d.link.devices, d.ref.Scenario+"\x00"+d.ref.App)
	d.link.mu.Unlock()
	d.mu.Lock()
	for id, ch := range d.journeys {
		close(ch)
		delete(d.journeys, id)
	}
	d.mu.Unlock()
	return err
}

// AppLog is what the device logged of the app, for a failed scenario.
func (d *hostedDevice) AppLog(ctx context.Context) string {
	var log string
	_ = d.call(ctx, "appLog", nil, &log)
	return log
}

func (d *hostedDevice) RunBackgroundTask(ctx context.Context, name string) error {
	return d.call(ctx, "backgroundTask", textParams{appRef: d.ref, Text: name}, nil)
}

type travelParams struct {
	appRef
	Route   Route `json:"route"`
	Journey int   `json:"journey"`
}

func (d *hostedDevice) Travel(ctx context.Context, r Route) (<-chan struct{}, error) {
	d.mu.Lock()
	d.journey++
	id := d.journey
	arrived := make(chan struct{})
	d.journeys[id] = arrived
	d.mu.Unlock()
	if err := d.call(ctx, "travel", travelParams{appRef: d.ref, Route: r, Journey: id}, nil); err != nil {
		d.mu.Lock()
		delete(d.journeys, id)
		d.mu.Unlock()
		return nil, err
	}
	return arrived, nil
}

func (d *hostedDevice) SceneObject(ctx context.Context, name string) (float64, float64, error) {
	var at [2]float64
	err := d.call(ctx, "sceneObject", textParams{appRef: d.ref, Text: name}, &at)
	return at[0], at[1], err
}

// hostedNotifications are a device's notifications, shown over its app.
type hostedNotifications struct{ d *hostedDevice }

type notificationParams struct {
	appRef
	Text   string `json:"text,omitempty"`
	Action string `json:"action,omitempty"`
}

func (n hostedNotifications) Shows(ctx context.Context, text string) (bool, error) {
	var shows bool
	err := n.d.call(ctx, "notifications.shows", notificationParams{appRef: n.d.ref, Text: text}, &shows)
	return shows, err
}

func (n hostedNotifications) Texts(ctx context.Context) []string {
	var texts []string
	_ = n.d.call(ctx, "notifications.texts", nil, &texts)
	return texts
}

func (n hostedNotifications) Close(ctx context.Context) error {
	return n.d.call(ctx, "notifications.close", nil, nil)
}

func (n hostedNotifications) Open(ctx context.Context, text string) error {
	return n.d.call(ctx, "notifications.open-one", notificationParams{appRef: n.d.ref, Text: text}, nil)
}

func (n hostedNotifications) Act(ctx context.Context, text, action string) error {
	return n.d.call(ctx, "notifications.act", notificationParams{appRef: n.d.ref, Text: text, Action: action}, nil)
}

// NotificationLog and NotificationSystem are empty on a device that keeps
// neither: a failed check shows what there is.
func (n hostedNotifications) NotificationLog(ctx context.Context) string {
	var log string
	_ = n.d.call(ctx, "notifications.log", nil, &log)
	return log
}

func (n hostedNotifications) NotificationSystem(ctx context.Context) string {
	var state string
	_ = n.d.call(ctx, "notifications.system", nil, &state)
	return state
}

// hostedFiles are an app's files at a path of its sandbox, on a device the
// host runs.
type hostedFiles struct {
	d     *hostedDevice
	path  string
	where string
}

func (f hostedFiles) Where() string { return f.where }

func (f hostedFiles) Read(ctx context.Context, name string) ([]byte, bool, error) {
	var a struct {
		Body  []byte `json:"body"`
		Found bool   `json:"found"`
	}
	err := f.d.call(ctx, "files.read", filesParams{appRef: f.d.ref, Path: f.path, Name: name}, &a)
	return a.Body, a.Found, err
}

func (f hostedFiles) Write(ctx context.Context, name string, body []byte) error {
	return f.d.call(ctx, "files.write", filesParams{appRef: f.d.ref, Path: f.path, Name: name, Body: body}, nil)
}

func (f hostedFiles) List(ctx context.Context, max int) ([]string, error) {
	var names []string
	err := f.d.call(ctx, "files.list", filesParams{appRef: f.d.ref, Path: f.path, Max: max}, &names)
	return names, err
}

func (f hostedFiles) Empty(ctx context.Context) error {
	return f.d.call(ctx, "files.empty", filesParams{appRef: f.d.ref, Path: f.path}, nil)
}

// wireScreen is a screen as it crosses: its nodes in their order, each
// naming its parent by its place.
type wireScreen struct {
	Nodes []wireNode `json:"nodes"`
	Size  Rect       `json:"size"`
}

type wireNode struct {
	Role      Role   `json:"role,omitempty"`
	Class     string `json:"class,omitempty"`
	Text      string `json:"text,omitempty"`
	Label     string `json:"label,omitempty"`
	Hint      string `json:"hint,omitempty"`
	ID        string `json:"id,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
	Displayed bool   `json:"displayed,omitempty"`
	Clickable bool   `json:"clickable,omitempty"`
	Checkable bool   `json:"checkable,omitempty"`
	Checked   bool   `json:"checked,omitempty"`
	Scrolling bool   `json:"scrolling,omitempty"`
	Selected  bool   `json:"selected,omitempty"`
	Password  bool   `json:"password,omitempty"`
	Bounds    Rect   `json:"bounds"`
	ByPoint   bool   `json:"byPoint,omitempty"`
	Using     string `json:"using,omitempty"`
	Value     string `json:"value,omitempty"`
	// Parent is the place of the node's parent among the screen's nodes,
	// -1 for one that has none.
	Parent int `json:"parent"`
}

func wireOf(s *Screen) *wireScreen {
	if s == nil {
		return nil
	}
	return wireNodes(s.Nodes, s.Size)
}

// wireNodes is nodes, parents before their children, as they cross.
func wireNodes(nodes []*Node, size Rect) *wireScreen {
	w := &wireScreen{Size: size, Nodes: make([]wireNode, len(nodes))}
	at := make(map[*Node]int, len(nodes))
	for i, n := range nodes {
		at[n] = i
	}
	for i, n := range nodes {
		parent := -1
		if p, ok := at[n.Parent]; ok && n.Parent != nil {
			parent = p
		}
		w.Nodes[i] = wireNode{
			Role: n.Role, Class: n.Class, Text: n.Text, Label: n.Label, Hint: n.Hint, ID: n.ID,
			Enabled: n.Enabled, Displayed: n.Displayed, Clickable: n.Clickable, Checkable: n.Checkable,
			Checked: n.Checked, Scrolling: n.Scrolling, Selected: n.Selected, Password: n.Password,
			Bounds: n.Bounds, ByPoint: n.ByPoint, Using: n.Using, Value: n.Value, Parent: parent,
		}
	}
	return w
}

// screen is the screen the nodes make, each with its parent and children.
func (w *wireScreen) screen() *Screen {
	s := &Screen{Size: w.Size, Nodes: make([]*Node, len(w.Nodes))}
	for i, n := range w.Nodes {
		s.Nodes[i] = &Node{
			Role: n.Role, Class: n.Class, Text: n.Text, Label: n.Label, Hint: n.Hint, ID: n.ID,
			Enabled: n.Enabled, Displayed: n.Displayed, Clickable: n.Clickable, Checkable: n.Checkable,
			Checked: n.Checked, Scrolling: n.Scrolling, Selected: n.Selected, Password: n.Password,
			Bounds: n.Bounds, ByPoint: n.ByPoint, Using: n.Using, Value: n.Value,
		}
	}
	for i, n := range w.Nodes {
		if n.Parent >= 0 && n.Parent < len(s.Nodes) && n.Parent != i {
			s.Nodes[i].Parent = s.Nodes[n.Parent]
			s.Nodes[n.Parent].Children = append(s.Nodes[n.Parent].Children, s.Nodes[i])
		}
	}
	return s
}

// wireScreenOf is the screen a node is of, from the node itself: every node its
// root leads to, parents before their children, and the node's place there.
func wireScreenOf(n *Node) (*wireScreen, int) {
	root := n
	for root.Parent != nil {
		root = root.Parent
	}
	var nodes []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		nodes = append(nodes, x)
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(root)
	return wireNodes(nodes, Rect{}), slices.Index(nodes, n)
}

// Table is an app's registration as a step's table has it, from its rows.
func Table(rows []Property) *core.Table {
	t := &core.Table{}
	for _, r := range rows {
		t.Rows = append(t.Rows, []string{r.Name, r.Value})
	}
	return t
}

// Properties are a registration's rows, each value as expand fills it in:
// what the host registers the app from.
func Properties(t *core.Table, expand func(string) string) ([]Property, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	rows := make([]Property, len(pairs))
	for i, p := range pairs {
		rows[i] = Property{Name: p.Key, Value: strings.TrimSpace(expand(p.Value))}
	}
	return rows, nil
}
