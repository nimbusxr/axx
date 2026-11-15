package desktopcore

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/files"
)

// The package's tests run a second time with the fake apps on a desktop the
// test harness's axx host runs (AXX_TEST_HOSTED, as CI sets it): every step
// then crosses to the host's side and back.

// fakeOS is the OS pack of the tests' fake apps: its side in the host
// registers an app from its rows with the fake driver the test gave it.
type fakeOS struct{}

const fakeOSName = "desktop-fake"

// hostScenario is what a test's pack sets up in a scenario the host makes.
var hostScenario func(sc *core.Scenario) error

func (fakeOS) Manifest() core.Manifest {
	return core.Manifest{Name: fakeOSName, Hooks: []core.Hook{{ID: fakeOSName + ".host", Phase: core.HostScenario, Run: func(sc *core.Scenario) error {
		if hostScenario == nil {
			return nil
		}
		return hostScenario(sc)
	}}}}
}
func (fakeOS) HostServes() string { return "the tests' fake apps" }

// fakeDrivers are the drivers of the apps the running test registered, by
// app: the host's side runs an app with the driver its test made.
var fakeDrivers sync.Map

func (fakeOS) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	return ServeHost(ctx, run, conn, sharedFake{}, func(sc *core.Scenario, name string, rows []Property) (*App, error) {
		app, err := ParseRows(sc, appcore.Host(), name, rows)
		if err != nil {
			return nil, err
		}
		d, _ := fakeDrivers.Load(name)
		app.Driver = d.(*fakeDriver)
		return app, nil
	})
}

// sharedFake is the host's driver: it claims the desktop with the driver of
// the app the test registered last, as a run claims it with its first
// app's.
type sharedFake struct{}

// lastFake is the driver of the app a test registered last.
var lastFake atomic.Pointer[fakeDriver]

func (sharedFake) Platform() string { return appcore.Host() }
func (sharedFake) Claim(sc *core.Scenario) (Desktop, error) {
	return lastFake.Load().Claim(sc)
}

// hostedTests reports whether the tests' apps run on the host's desktop.
func hostedTests() bool { return os.Getenv(cloudtest.HostedEnv) != "" }

// registerFake registers a fake app: as a registration step does, on the
// host's desktop when the tests run hosted.
func registerFake(h *cloudtest.Harness, app *App) error {
	if !hostedTests() {
		return Register(h.SC, app)
	}
	fakeDrivers.Store(app.Name, app.Driver)
	lastFake.Store(app.Driver.(*fakeDriver))
	rows := []Property{{Name: "app", Value: app.App}}
	if app.System {
		rows = append(rows, Property{Name: "owner", Value: "system"})
	}
	if len(app.Args) > 0 {
		rows = append(rows, Property{Name: "args", Value: strings.Join(app.Args, " ")})
	}
	for _, e := range app.EnvList() {
		name, value, _ := strings.Cut(e, "=")
		rows = append(rows, Property{Name: "env." + name, Value: value})
	}
	return Register(h.SC, Hosted(h.SC, fakeOSName, app, rows))
}

// skipHosted skips a test of what a run does itself, where the host does it
// for a hosted run.
func skipHosted(t *testing.T, why string) {
	t.Helper()
	if hostedTests() {
		t.Skip(why)
	}
}

func hostedHarness(t *testing.T) *cloudtest.Harness {
	t.Helper()
	t.Setenv(cloudtest.HostedEnv, "1")
	hostScenario = nil
	t.Cleanup(func() { hostScenario = nil })
	h := cloudtest.New(t, appcore.Pack(), Pack(), files.Pack(), fakeOS{})
	h.Start(&core.Plan{})
	return h
}

// The app is the agent's code, and the host starts it past the agent's
// sandbox: the person who runs the host approves it first, by everything of
// its registration, and nothing of the app is reset or started before.
func TestAHostedAppStartsOnceItsPersonApproved(t *testing.T) {
	h := hostedHarness(t)
	p, _, register := desk()
	register.enabled = true
	d := &fakeDriver{proc: p}
	h.File("build/depot", "#!/bin/sh\n")
	app := &App{
		Name: "depot", App: filepath.Join(h.Dir, "build", "depot"), Args: []string{"--depot", "Leipzig"},
		Env: map[string]string{"PARCELS_API": "http://127.0.0.1:8400"}, Driver: d, Dir: h.Dir,
	}
	h.HostAnswer = func(core.HostApproval) error {
		return errors.New("the person running the axx host refused to start the depot app for runs")
	}
	if err := registerFake(h, app); err != nil {
		t.Fatalf("registering asks for nothing: %v", err)
	}
	if d.claims != 1 {
		t.Errorf("registering claims the host's desktop: %d claims", d.claims)
	}
	_ = h.Fails("the depot app is launched", "refused to start the depot app")
	if d.resets != 0 || d.starts != 0 {
		t.Errorf("a refused app was reset %d times and started %d times", d.resets, d.starts)
	}
	asked := h.HostAsked()
	if len(asked) == 0 {
		t.Fatal("the host's person was not asked")
	}
	a := asked[0]
	wantText := "app: build/depot\nargs: --depot Leipzig\nenv.PARCELS_API: http://127.0.0.1:8400\n"
	if !strings.HasPrefix(a.Subject, "the depot app") || a.Text != wantText {
		t.Errorf("asked to approve %q:\n%s\nwant:\n%s", a.Subject, a.Text, wantText)
	}
	if w := strings.Join(a.Warnings, " "); !strings.Contains(w, "past the agent's sandbox") || !strings.Contains(w, "whatever is built there from now on") {
		t.Errorf("the warnings: %q", a.Warnings)
	}

	h.HostAnswer = nil
	h.OK("the depot app is launched")
	if d.resets != 1 || d.starts != 1 {
		t.Errorf("an approved app: %d resets, %d starts", d.resets, d.starts)
	}
	if len(d.envs) != 1 || d.envs[0]["PARCELS_API"] != "http://127.0.0.1:8400" {
		t.Errorf("the app started with %v", d.envs)
	}
	h.OK(`the "Register" button is clicked in the depot app`)
	if len(p.clicks) != 1 || p.clicks[0] != "Register" {
		t.Errorf("clicks: %v", p.clicks)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if !p.stopped || d.releases != 1 {
		t.Errorf("the scenario's end stops the app (%v) and gives the host's desktop back (%d)", p.stopped, d.releases)
	}
}

// The system's app is the person's own, as it runs: the host reads it for a
// run once they approved that.
func TestAHostedSystemAppIsReadOnceItsPersonApproved(t *testing.T) {
	h := hostedHarness(t)
	finder := &fakeProc{tree: node("window", ".snap", nil), texts: []string{"snap.log"}}
	d := &fakeDriver{system: finder}
	h.HostAnswer = func(core.HostApproval) error {
		return errors.New("the person running the axx host refused to start the files app for runs")
	}
	app := &App{Name: "files", App: "com.apple.finder", System: true, Driver: d, Dir: h.Dir}
	if err := registerFake(h, app); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a system app that may not be read: %v", err)
	}
	if d.watches != 0 {
		t.Errorf("a refused app was watched")
	}
	asked := h.HostAsked()
	if len(asked) != 1 || !strings.Contains(asked[0].Text, "app: com.apple.finder (the system's") ||
		!strings.Contains(strings.Join(asked[0].Warnings, " "), "reads what this app of yours shows") {
		t.Errorf("asked: %+v", asked)
	}
}

// A pack that builds on the desktops (the model stubs) does its work as an
// app launches where the app starts: in the host. Its hook is set up in the
// host's scenario; there it gives the app what it starts with, after the
// registration's, and reads the app's files, with what the run's steps
// wrote. The run's own hooks do not run.
func TestAHostedAppLaunchesWithThePacksHooks(t *testing.T) {
	h := hostedHarness(t)
	p, _, _ := desk()
	d := &fakeDriver{proc: p}
	var stubs []string
	hostScenario = func(sc *core.Scenario) error {
		OnLaunch(sc, func(l *Launch) error {
			fs, err := appcore.Files(sc, l.App.Name, "./axx-models")
			if err != nil {
				return err
			}
			for _, name := range fs.(interface{ Written() []string }).Written() {
				body, _, err := fs.Read(sc.Context(), name)
				if err != nil {
					return err
				}
				stubs = append(stubs, name+": "+string(body))
			}
			l.InsertLibrary("/cache/axx-models.dylib")
			l.Environment["AXX_MODELS_DIR"] = filepath.Join(l.Files, "axx-models")
			sc.Note("stubs/fed/"+l.App.Name, "true")
			return nil
		})
		return nil
	}
	app := &App{Name: "depot", App: "depot", Env: map[string]string{"DEPOT": "Leipzig"}, Driver: d, Dir: h.Dir}
	if err := registerFake(h, app); err != nil {
		t.Fatal(err)
	}
	ran := false
	OnLaunch(h.SC, func(*Launch) error {
		ran = true
		return nil
	})
	// A step of the run puts a stub in the app's files before the app starts.
	fs, err := appcore.Files(h.SC, "depot", "./axx-models")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Write(t.Context(), "labels.axxmodel", []byte("a stub")); err != nil {
		t.Fatal(err)
	}
	h.OK("the depot app is launched")
	if len(stubs) != 1 || stubs[0] != "labels.axxmodel: a stub" {
		t.Errorf("the stubs the hook read in the host: %v", stubs)
	}
	registered, _ := desktopApp(h.SC, "depot")
	want := map[string]string{
		"DEPOT":                 "Leipzig",
		"DYLD_INSERT_LIBRARIES": "/cache/axx-models.dylib",
		"AXX_MODELS_DIR":        filepath.Join(HomeOf(h.SC, registered), "Library", "Application Support", "axx-models"),
	}
	if len(d.envs) != 1 || len(d.envs[0]) != len(want) {
		t.Fatalf("the app started with %v, want %v", d.envs, want)
	}
	for k, v := range want {
		if d.envs[0][k] != v {
			t.Errorf("%s: %q, want %q", k, d.envs[0][k], v)
		}
	}
	if ran {
		t.Error("the run's own launch hook ran: the host starts the app")
	}
	if v, _ := h.SC.Noted("stubs/fed/depot"); v != "true" {
		t.Errorf("what the hook noted in the host did not reach the run's scenario: %q", v)
	}
}

// The host reads the project's files only: an app that is a file elsewhere
// is not one it starts, and an app the OS finds by its name stays a name.
func TestAnAppOutsideTheProjectIsNotTheHosts(t *testing.T) {
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "depot")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	suite := core.NewSuite(core.SuiteOptions{ProjectDir: project, ResolvePath: func(p string) (string, error) {
		if !filepath.IsAbs(p) {
			p = filepath.Join(project, p)
		}
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	}})
	suite.Confine(project)
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{}, suite, nil)
	if _, err := ParseRows(sc, appcore.Host(), "depot", []Property{{Name: "app", Value: outside}}); err == nil || !strings.Contains(err.Error(), "is not in the project") {
		t.Errorf("an app outside the project: %v", err)
	}
	app, err := ParseRows(sc, appcore.Host(), "editor", []Property{{Name: "app", Value: "com.apple.TextEdit"}})
	if err != nil || app.App != "com.apple.TextEdit" {
		t.Errorf("an app the OS finds by its name: %+v, %v", app, err)
	}
}
