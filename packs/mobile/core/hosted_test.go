package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// testPhones is a platform of the test's own: its devices are in memory, and
// say what was done to them. The driver its apps' sessions are on is an
// HTTP server that answers a tap.
type testPhones struct {
	app    string
	path   string // the app's build, a file of the project
	device string
	log    *deviceLog
	driver string
}

type deviceLog struct {
	mu   sync.Mutex
	said []string
}

func (l *deviceLog) say(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.said = append(l.said, fmt.Sprintf(format, args...))
}

func (l *deviceLog) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.said, "; ")
}

func (p *testPhones) Kind() string   { return "android" }
func (p *testPhones) RunsHere() bool { return true }

func (p *testPhones) HostApproval(*core.Scenario) core.HostApproval {
	return core.HostApproval{
		Subject:  "the " + p.app + " app",
		Text:     "app: " + p.path + "\ndevice: " + p.device,
		Warnings: []string{"the app runs in an emulator on this machine"},
	}
}

func (p *testPhones) Start(sc *core.Scenario) (Device, error) {
	p.log.say("start %s on %s", p.app, p.device)
	release := sc.Hold() // as a pack waits for a device another scenario has
	release()
	sc.Log("the %s app's device is ready", p.app)
	c := &appium.Client{URL: p.driver}
	return &phone{p: p, sc: sc, session: c.Attach("session-1", map[string]any{"platformName": "Android"}), files: map[string][]byte{}}, nil
}

// cameraPhones are test phones whose apps' camera axx plays: it says which
// file the camera was given.
type cameraPhones struct{ *testPhones }

func (p cameraPhones) See(_ *core.Scenario, path string) error {
	p.log.say("the camera sees %s", filepath.Base(path))
	return nil
}
func (p cameraPhones) ScenePhone() ScenePhone { return nil }

type phone struct {
	p       *testPhones
	sc      *core.Scenario
	session *appium.Session
	files   map[string][]byte
	stopped bool
}

// beforeLaunch are what the packs that build on the test's phones do as an
// app launches, in the scenario the app's device is in: a platform pack's
// OnLaunch.
var beforeLaunch = core.NewStateKey("mobile-phones/launch", func(*core.Scenario) *[]func(app string) error {
	return &[]func(app string) error{}
}, nil)

func (d *phone) Session() *appium.Session { return d.session }
func (d *phone) Screen(context.Context) (*Screen, error) {
	list := &Node{Class: "List", Scrolling: true, Bounds: Rect{0, 100, 400, 600}}
	row := &Node{Class: "Row", Text: "PX-4101", Using: "id", Value: "row-1", Displayed: true, Parent: list, Bounds: Rect{0, 100, 400, 80}}
	far := &Node{Class: "Row", Text: "PX-4199", Using: "id", Value: "row-99", Parent: list, Bounds: Rect{0, 9000, 400, 80}}
	list.Children = []*Node{row, far}
	return &Screen{Nodes: []*Node{list, row, far}, Size: Rect{0, 0, 400, 800}}, nil
}

func (d *phone) Launch(context.Context) error {
	for _, fn := range *beforeLaunch.Of(d.sc) {
		if err := fn(d.p.app); err != nil {
			return err
		}
	}
	d.p.log.say("launch")
	return nil
}

func (d *phone) Restart(context.Context) error {
	d.p.log.say("restart")
	d.session.Renew("session-2") // the app is in a new session of its driver
	return nil
}
func (d *phone) Background(context.Context) error { d.p.log.say("background"); return nil }
func (d *phone) OpenLink(_ context.Context, url string) error {
	if !strings.HasPrefix(url, "parcels://") {
		return fmt.Errorf("the %s app opens no %s", d.p.app, url)
	}
	d.p.log.say("open %s", url)
	return nil
}
func (d *phone) HideKeyboard(context.Context) (bool, error) { return true, nil }

// OutOfView is whether a control a selector found is out of view: this phone's driver finds
// such controls, and the element "far" is one.
func (d *phone) OutOfView(_ context.Context, el *appium.Element) bool { return el.ID == "far" }

func (d *phone) ScrollToShow(_ context.Context, n *Node, name string) (bool, error) {
	if n == nil || n.Parent == nil || !n.Parent.Scrolling {
		return false, errors.New("the node came without the list it is in")
	}
	d.p.log.say("scroll %s (%s) into the list of %d", name, n.Value, len(n.Parent.Children))
	return true, nil
}

func (d *phone) Swipe(_ context.Context, direction string) error {
	d.p.log.say("swipe %s", direction)
	return nil
}

func (d *phone) Scroll(_ context.Context, direction string) (bool, error) {
	return direction == "down", nil
}

func (d *phone) ScrollRow(_ context.Context, row *Node, direction string) (bool, error) {
	d.p.log.say("scroll the row at %v %s", row.Bounds.Y, direction)
	return true, nil
}

func (d *phone) OpenNotifications(context.Context) (Notifications, error) {
	return shade{d}, nil
}
func (d *phone) SystemBars(context.Context) ([]Rect, error) { return []Rect{{0, 0, 400, 24}}, nil }
func (d *phone) ScreenKey() string                          { return "android-" + d.p.device }
func (d *phone) Stream() string                             { return "http://127.0.0.1:1/stream" }
func (d *phone) Files(_ context.Context, path string) (core.Files, error) {
	return memFiles{d: d, path: path}, nil
}

func (d *phone) Describe() map[string]any {
	return map[string]any{"device": d.p.device, "app": d.p.app}
}

func (d *phone) Stop(failed bool) error {
	d.stopped = true
	d.p.log.say("stop (failed: %v)", failed)
	return nil
}
func (d *phone) AppLog(context.Context) string { return "the app logged a delivery" }
func (d *phone) RunBackgroundTask(_ context.Context, name string) error {
	if name != "round-reminder" {
		return UnknownBackgroundTask(d.p.app, name, []string{"round-reminder"})
	}
	d.p.log.say("task %s", name)
	return nil
}

func (d *phone) Travel(_ context.Context, r Route) (<-chan struct{}, error) {
	arrived := make(chan struct{})
	go func() { time.Sleep(50 * time.Millisecond); close(arrived) }()
	d.p.log.say("travel %d legs", len(r.Legs))
	return arrived, nil
}

type shade struct{ d *phone }

func (s shade) Shows(_ context.Context, text string) (bool, error) {
	return text == "Parcel delivered", nil
}

func (s shade) Texts(context.Context) []string { return []string{"Parcel delivered"} }

func (s shade) Close(context.Context) error { s.d.p.log.say("close the notifications"); return nil }

func (s shade) Open(_ context.Context, text string) error { s.d.p.log.say("open %q", text); return nil }

func (s shade) Act(_ context.Context, text, action string) error {
	s.d.p.log.say("%s on %q", action, text)
	return nil
}

type memFiles struct {
	d    *phone
	path string
}

func (f memFiles) Where() string { return "the app's files, at ./" + f.path }
func (f memFiles) Read(_ context.Context, name string) ([]byte, bool, error) {
	b, ok := f.d.files[f.path+"/"+name]
	return b, ok, nil
}

func (f memFiles) Write(_ context.Context, name string, body []byte) error {
	f.d.files[f.path+"/"+name] = body
	return nil
}

func (f memFiles) List(context.Context, int) ([]string, error) {
	var out []string
	for name := range f.d.files {
		if rest, ok := strings.CutPrefix(name, f.path+"/"); ok {
			out = append(out, rest)
		}
	}
	return out, nil
}

func (f memFiles) Empty(context.Context) error {
	for name := range f.d.files {
		if strings.HasPrefix(name, f.path+"/") {
			delete(f.d.files, name)
		}
	}
	return nil
}

// hosted is a run's suite and an axx host that serves it, in this process:
// the phones pack's side in the host registers apps from their rows, as its
// step does in the run.
type hostedFixture struct {
	project string
	log     *deviceLog
	driver  *httptest.Server
	taps    chan string
	// approve answers the host's person; asked keeps what they were asked.
	approve func(core.HostApproval) error
	asked   []core.HostApproval
	told    []string
	mu      sync.Mutex

	run      *core.Suite
	hostSide *core.Suite
	served   chan error
	// hostScenario is the packs' hooks for a scenario the host makes.
	hostScenario func(sc *core.Scenario) error
}

func newHosted(t *testing.T) *hostedFixture {
	t.Helper()
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "build", "courier.apk"), []byte("apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &hostedFixture{project: project, log: &deviceLog{}, taps: make(chan string, 8), served: make(chan error, 1)}
	f.driver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.taps <- r.Method + " " + r.URL.Path
		_, _ = w.Write([]byte(`{"value": null}`))
	}))
	t.Cleanup(f.driver.Close)
	resolve := func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return p, nil
		}
		return filepath.Join(project, filepath.FromSlash(p)), nil
	}
	f.hostSide = core.NewSuite(core.SuiteOptions{ProjectDir: project, ResolvePath: resolve, HostScenario: func(sc *core.Scenario) error {
		if f.hostScenario == nil {
			return nil
		}
		return f.hostScenario(sc)
	}})
	f.hostSide.Confine(project)
	f.run = core.NewSuite(core.SuiteOptions{ProjectDir: project, ResolvePath: resolve})
	f.run.UseHost(func(ctx context.Context, pack string) (net.Conn, error) {
		if pack != "mobile-phones" {
			return nil, fmt.Errorf("no %s pack in the host", pack)
		}
		runSide, hostSide := net.Pipe()
		go func() {
			f.served <- ServeHost(context.Background(), core.HostRun{
				ProjectDir: project, Command: "axx run",
				Tell: func(text string) {
					f.mu.Lock()
					f.told = append(f.told, text)
					f.mu.Unlock()
				},
				Approve: func(_ context.Context, a core.HostApproval) error {
					f.mu.Lock()
					f.asked = append(f.asked, a)
					approve := f.approve
					f.mu.Unlock()
					if approve == nil {
						return nil
					}
					return approve(a)
				},
				Suite: func() (*core.Suite, error) { return f.hostSide, nil },
			}, hostSide, f.register)
		}()
		return runSide, nil
	})
	t.Cleanup(func() {
		_ = f.run.Close(context.Background())
		_ = f.hostSide.Close(context.Background())
	})
	return f
}

// register is the phones pack's registration, in a run and in the host.
func (f *hostedFixture) register(sc *core.Scenario, name string, t *core.Table) error {
	rows, err := Properties(t, func(v string) string { return v })
	if err != nil {
		return err
	}
	p := &testPhones{app: name, log: f.log, driver: f.driver.URL}
	camera := false
	for _, r := range rows {
		switch r.Name {
		case "app":
			if p.path, err = sc.ResolvePath(r.Value); err != nil {
				return fmt.Errorf("the %s app's app: %w", name, err)
			}
		case "device":
			p.device = r.Value
		case "camera":
			camera = true
		}
	}
	if camera {
		return Register(sc, &App{Name: name, Platform: Hosted(sc, "mobile-phones", name, rows, cameraPhones{p})})
	}
	return Register(sc, &App{Name: name, Platform: Hosted(sc, "mobile-phones", name, rows, p)})
}

func (f *hostedFixture) scenario(t *testing.T, name string) (*core.Scenario, *sinkLog) {
	t.Helper()
	s := &sinkLog{}
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{ID: name, Name: name, URI: "features/round.feature"}, f.run, s)
	return sc, s
}

type sinkLog struct {
	mu       sync.Mutex
	logs     []string
	attached []string
}

func (s *sinkLog) Log(_ *core.Scenario, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, msg)
}

func (s *sinkLog) Attach(_ *core.Scenario, _ string, body []byte, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached = append(s.attached, name+": "+string(body))
}

func courierRows() *core.Table {
	return &core.Table{Rows: [][]string{{"app", "build/courier.apk"}, {"device", "parcels-pixel"}}}
}

// A run that an axx host serves has its testPhones from the host: the app is
// registered there as it is in the run, the person approves it, the host's
// pack starts the device, and everything a step does with the device is done
// there; the run drives the app's screen through the driver itself.
func TestARunsPhoneIsStartedAndDrivenInTheHost(t *testing.T) {
	f := newHosted(t)
	sc, logs := f.scenario(t, "a courier delivers a parcel")
	if err := f.register(sc, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	app, err := mobileApp(sc, "courier")
	if err != nil {
		t.Fatal(err)
	}
	if _, local := app.Platform.(*testPhones); local {
		t.Fatal("the run's platform is the pack's own: nothing would go through the host")
	}
	d, err := app.Platform.Start(sc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	// The person was asked, about the app's file as the host found it.
	if len(f.asked) != 1 || f.asked[0].Subject != "the courier app" ||
		f.asked[0].Text != "app: "+filepath.Join(f.project, "build", "courier.apk")+"\ndevice: parcels-pixel" {
		t.Errorf("the approval: %+v", f.asked)
	}
	// The run drives the screen itself, in the session the host started.
	if id := d.Session().Identity(); id != "session-1" {
		t.Errorf("the run's session: %s", id)
	}
	if err := d.Session().Tap(ctx, 10, 20); err != nil {
		t.Fatal(err)
	}
	if tap := <-f.taps; !strings.Contains(tap, "/session/session-1/") {
		t.Errorf("the tap went to %s", tap)
	}
	if d.ScreenKey() != "android-parcels-pixel" || d.Stream() != "http://127.0.0.1:1/stream" {
		t.Errorf("the device: %s %s", d.ScreenKey(), d.Stream())
	}

	// What the device does, it does in the host.
	s, err := d.Screen(ctx)
	if err != nil || len(s.Nodes) != 3 || s.Nodes[1].Parent != s.Nodes[0] || len(s.Nodes[0].Children) != 2 || s.Nodes[1].Text != "PX-4101" || s.Size.Height != 800 {
		t.Fatalf("the screen: %+v %v", s, err)
	}
	if ok, err := d.ScrollToShow(ctx, s.Nodes[2], "PX-4199"); err != nil || !ok {
		t.Errorf("scroll to show: %v %v", ok, err)
	}
	if ok, err := d.ScrollRow(ctx, s.Nodes[1], "left"); err != nil || !ok {
		t.Errorf("scroll a row: %v %v", ok, err)
	}
	// Whether a control a selector found is out of view is the host's device's to say: the
	// step scrolls to one that is.
	o, ok := d.(outOfViewer)
	if !ok {
		t.Fatal("the run's device does not say whether a control is out of view")
	}
	if !o.OutOfView(ctx, d.Session().Element("far")) || o.OutOfView(ctx, d.Session().Element("row")) {
		t.Error("out of view: the far element is, the row is not")
	}
	if err := d.OpenLink(ctx, "parcels://delivery/PX-4101"); err != nil {
		t.Error(err)
	}
	if err := d.OpenLink(ctx, "https://example.com"); err == nil || err.Error() != "the courier app opens no https://example.com" {
		t.Errorf("an error of the device's: %v", err)
	}
	if moved, err := d.Scroll(ctx, "down"); err != nil || !moved {
		t.Errorf("scroll: %v %v", moved, err)
	}
	if bars, err := d.SystemBars(ctx); err != nil || len(bars) != 1 || bars[0].Height != 24 {
		t.Errorf("system bars: %v %v", bars, err)
	}
	if got := d.Describe(); got["device"] != "parcels-pixel" {
		t.Errorf("describe: %v", got)
	}
	if err := d.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if id := d.Session().Identity(); id != "session-2" {
		t.Errorf("after the restart the run's session is %s", id)
	}

	// Its notifications, its files, its log, its background tasks, its routes.
	n, err := d.OpenNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shows, err := n.Shows(ctx, "Parcel delivered"); err != nil || !shows {
		t.Errorf("notifications: %v %v", shows, err)
	}
	if err := n.Act(ctx, "Parcel delivered", "Rate"); err != nil {
		t.Error(err)
	}
	if err := n.Close(ctx); err != nil {
		t.Error(err)
	}
	files, err := d.Files(ctx, "Documents")
	if err != nil || files.Where() != "the app's files, at ./Documents" {
		t.Fatalf("files: %v %v", files, err)
	}
	if err := files.Write(ctx, "round.json", []byte(`{"stops": 3}`)); err != nil {
		t.Fatal(err)
	}
	if body, ok, err := files.Read(ctx, "round.json"); err != nil || !ok || string(body) != `{"stops": 3}` {
		t.Errorf("a file read back: %q %v %v", body, ok, err)
	}
	if _, ok, err := files.Read(ctx, "missing.json"); err != nil || ok {
		t.Errorf("a file that is not there: %v %v", ok, err)
	}
	if names, err := files.List(ctx, 10); err != nil || len(names) != 1 || names[0] != "round.json" {
		t.Errorf("list: %v %v", names, err)
	}
	if _, err := d.Files(ctx, "../../etc"); err == nil || !strings.Contains(err.Error(), "leaves the app's files") {
		t.Errorf("a path out of the app's files: %v", err)
	}
	if log := d.(appLog).AppLog(ctx); log != "the app logged a delivery" {
		t.Errorf("the app's log: %q", log)
	}
	if err := d.(BackgroundTasks).RunBackgroundTask(ctx, "depot-sync"); err == nil || !strings.Contains(err.Error(), `its background tasks are "round-reminder"`) {
		t.Errorf("an unknown task: %v", err)
	}
	arrived, err := d.(Traveler).Travel(ctx, Route{Legs: []Leg{{Speed: 5, Time: time.Second}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the phone never arrived")
	}

	if err := d.Stop(true); err != nil {
		t.Fatal(err)
	}
	want := "start courier on parcels-pixel; scroll PX-4199 (row-99) into the list of 2; scroll the row at 100 left; " +
		"open parcels://delivery/PX-4101; restart; Rate on \"Parcel delivered\"; close the notifications; travel 1 legs; stop (failed: true)"
	if got := f.log.all(); got != want {
		t.Errorf("what the host's device did:\n%s\nwant\n%s", got, want)
	}
	// What the host's scenario logged is in the run's.
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if strings.Join(logs.logs, "; ") != "the courier app's device is ready" {
		t.Errorf("the run's log: %v", logs.logs)
	}
	if err := sc.Close(); err != nil {
		t.Errorf("the scenario's end: %v", err)
	}
}

// What the person refuses does not start, and the run says why; a file of
// the registration that is not the project's never reaches the pack.
func TestAHostedPhoneThatMayNotStart(t *testing.T) {
	f := newHosted(t)
	f.approve = func(core.HostApproval) error {
		return errors.New("the person running the axx host refused to start the courier app for runs")
	}
	sc, _ := f.scenario(t, "a courier delivers a parcel")
	if err := f.register(sc, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	app, _ := mobileApp(sc, "courier")
	if _, err := app.Platform.Start(sc); err == nil || !strings.Contains(err.Error(), "refused to start the courier app") {
		t.Errorf("refused: %v", err)
	}
	if got := f.log.all(); got != "" {
		t.Errorf("a refused app's device did: %s", got)
	}
	_ = sc.Close()

	outside := filepath.Join(t.TempDir(), "other.apk")
	if err := os.WriteFile(outside, []byte("apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, _ = f.scenario(t, "another")
	err := f.register(sc, "depot", &core.Table{Rows: [][]string{{"app", outside}, {"device", "parcels-pixel"}}})
	if err != nil {
		t.Fatalf("the run reads what its sandbox lets it: %v", err)
	}
	app, _ = mobileApp(sc, "depot")
	if _, err := app.Platform.Start(sc); err == nil || !strings.Contains(err.Error(), "is not in the project") {
		t.Errorf("an app outside the project: %v", err)
	}
	_ = sc.Close()
}

// A scenario that ends leaves nothing running in the host, and a run whose
// connection ends leaves no scenario there.
func TestTheHostStopsWhatAScenarioAndARunLeave(t *testing.T) {
	f := newHosted(t)
	sc, _ := f.scenario(t, "one")
	if err := f.register(sc, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	app, _ := mobileApp(sc, "courier")
	if _, err := app.Platform.Start(sc); err != nil {
		t.Fatal(err)
	}
	// The scenario ends without stopping its app (a step failed before).
	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.log.all(); !strings.HasSuffix(got, "stop (failed: false)") {
		t.Errorf("the scenario's end: %s", got)
	}
	sc2, _ := f.scenario(t, "two")
	if err := f.register(sc2, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	app, _ = mobileApp(sc2, "courier")
	if _, err := app.Platform.Start(sc2); err != nil {
		t.Fatal(err)
	}
	// The run ends: its suite closes the connection.
	if err := f.run.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-f.served:
		_ = err // the connection's end, or none
	case <-time.After(10 * time.Second):
		t.Fatal("the host's side did not end with the run's connection")
	}
	if got := f.log.all(); strings.Count(got, "stop (failed: false)") != 2 {
		t.Errorf("the run's end: %s", got)
	}
}

// A scenario ends as the pack ends it: its running apps are stopped, in the
// host, and only then does the scenario end there.
func TestAScenariosEndStopsItsAppsBeforeItEndsInTheHost(t *testing.T) {
	f := newHosted(t)
	sc, _ := f.scenario(t, "a courier delivers a parcel")
	if err := f.register(sc, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	if _, err := device(sc, "courier", true); err != nil {
		t.Fatal(err)
	}
	sc.SetStatus("failed")
	if err := sc.Close(); err != nil {
		t.Fatalf("the scenario's end: %v", err)
	}
	if got := f.log.all(); got != "start courier on parcels-pixel; stop (failed: true)" {
		t.Errorf("what the host's device did: %s", got)
	}
}

// A pack that builds on the phones (the model stubs) does its work as an app
// launches, where the app's device is: in the host. Its hook is set up in
// the scenario the host makes for the run's; there it finds the app's files
// with what the run's steps wrote, and its device; and what its two sides
// note of the scenario reaches the other.
func TestAPackThatBuildsOnPhonesWorksInTheHostsScenario(t *testing.T) {
	f := newHosted(t)
	var hostScenarios []string
	f.hostScenario = func(sc *core.Scenario) error {
		hostScenarios = append(hostScenarios, sc.Name)
		hooks := beforeLaunch.Of(sc)
		*hooks = append(*hooks, func(app string) error {
			files, err := appcore.Files(sc, app, "./axx-models")
			if err != nil {
				return err
			}
			wrote := files.(interface{ Written() []string }).Written()
			body, _, err := files.Read(sc.Context(), "labels.axxmodel")
			if err != nil {
				return err
			}
			microphone, _ := sc.Noted("stubs/microphone")
			late, _ := sc.Noted("stubs/late")
			var screenKey string
			if err := OnDevice(sc, app, func(_ context.Context, d Device) error {
				screenKey = d.ScreenKey()
				return nil
			}); err != nil {
				return err
			}
			f.log.say("stubs %v (%s) microphone %s late %s on %s", wrote, body, microphone, late, screenKey)
			sc.Note("stubs/fed/"+app, "true")
			return nil
		})
		return nil
	}
	sc, _ := f.scenario(t, "a courier reads a torn label")
	sc.Note("stubs/microphone", "true")
	if err := f.register(sc, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	// The run's steps write the app's stubs before the app starts.
	files, err := appcore.Files(sc, "courier", "./axx-models")
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Write(t.Context(), "labels.axxmodel", []byte("a stub")); err != nil {
		t.Fatal(err)
	}
	if _, fed := sc.Noted("stubs/fed/courier"); fed {
		t.Error("the app is fed before it launched")
	}
	sc.Note("stubs/late", "noted after the app registered")
	d, err := device(sc, "courier", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Launch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if want := "stubs [labels.axxmodel] (a stub) microphone true late noted after the app registered on android-parcels-pixel; launch"; !strings.Contains(f.log.all(), want) {
		t.Errorf("the pack's work as the app launched in the host:\n got %s\nwant %s", f.log.all(), want)
	}
	if v, _ := sc.Noted("stubs/fed/courier"); v != "true" {
		t.Errorf("what the pack noted in the host did not reach the run's scenario: %q", v)
	}
	if len(hostScenarios) != 1 || hostScenarios[0] != "a courier reads a torn label" {
		t.Errorf("the host's scenarios the packs were set up in: %v", hostScenarios)
	}
	// A second app of the scenario registers in the same scenario of the host's.
	if err := f.register(sc, "depot", courierRows()); err != nil {
		t.Fatal(err)
	}
	if _, err := device(sc, "depot", true); err != nil {
		t.Fatal(err)
	}
	if len(hostScenarios) != 1 {
		t.Errorf("the packs were set up again for the scenario's second app: %v", hostScenarios)
	}
	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}

	// A pack that cannot set up fails the registration, and the scenario
	// has nothing in the host.
	f.hostScenario = func(*core.Scenario) error { return errors.New("the stubs' library does not build") }
	sc, _ = f.scenario(t, "another")
	if err := f.register(sc, "courier", courierRows()); err != nil {
		t.Fatal(err)
	}
	if _, err := device(sc, "courier", true); err == nil || !strings.Contains(err.Error(), "the stubs' library does not build") {
		t.Errorf("a pack that cannot set up in the host's scenario: %v", err)
	}
	_ = sc.Close()
}

// A run a person watches, or pauses, starts its phones itself: their
// windows are for the person who started the run, where they started it.
func TestAWatchedRunStartsItsPhonesItself(t *testing.T) {
	served := func(context.Context, string) (net.Conn, error) { return nil, errors.New("the host is not asked") }
	local := &testPhones{app: "courier", log: &deviceLog{}}
	watched := core.NewSuite(core.SuiteOptions{Watch: true})
	watched.UseHost(served)
	paused := core.NewSuite(core.SuiteOptions{})
	paused.UseHost(served)
	paused.PauseAt(map[string][]int{"features/round.feature": {7}})
	for name, suite := range map[string]*core.Suite{"watched": watched, "paused": paused} {
		sc := core.NewScenario(t.Context(), core.ScenarioInfo{ID: name}, suite, nil)
		if got := Hosted(sc, "mobile-phones", "courier", nil, local); got != Platform(local) {
			t.Errorf("a %s run's phones are the host's", name)
		}
	}
	headless := core.NewSuite(core.SuiteOptions{})
	headless.UseHost(served)
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{ID: "headless"}, headless, nil)
	if got := Hosted(sc, "mobile-phones", "courier", nil, local); got == Platform(local) {
		t.Error("a run nobody watches has its phones from the host that serves it")
	}
}

// A step names the file an app's camera sees outright. The host plays a
// file of the project's, and refuses any other: what the camera sees, the
// app has.
func TestTheHostsCameraSeesTheProjectsFilesOnly(t *testing.T) {
	f := newHosted(t)
	label := filepath.Join(f.project, "labels", "torn.png")
	if err := os.MkdirAll(filepath.Dir(label), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "passport.png")
	for _, p := range []string{label, outside} {
		if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A link in the project to a file outside it is a file outside it.
	link := filepath.Join(f.project, "labels", "linked.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	sc, _ := f.scenario(t, "a courier scans a label")
	rows := &core.Table{Rows: [][]string{{"app", "build/courier.apk"}, {"device", "parcels-pixel"}, {"camera", "played"}}}
	if err := f.register(sc, "courier", rows); err != nil {
		t.Fatal(err)
	}
	app, _ := mobileApp(sc, "courier")
	cam, ok := app.Platform.(Camera)
	if !ok {
		t.Fatal("the hosted platform of an app whose camera axx plays is no Camera")
	}
	if err := cam.See(sc, label); err != nil {
		t.Fatalf("a picture of the project's: %v", err)
	}
	for _, p := range []string{outside, link} {
		if err := cam.See(sc, p); err == nil || !strings.Contains(err.Error(), "is not in the project") {
			t.Errorf("%s: %v", p, err)
		}
	}
	if got := f.log.all(); got != "the camera sees torn.png" {
		t.Errorf("the host's camera was given: %s", got)
	}
	_ = sc.Close()
}
