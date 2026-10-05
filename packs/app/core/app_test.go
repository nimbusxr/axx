package appcore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// fake is a family that records what it is asked to do.
type fake struct{ calls []string }

func (f *fake) rec(format string, args ...any) error {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return nil
}

func (f *fake) Launch(_ *core.Scenario, a *App) error  { return f.rec("launch %s", a.Name) }
func (f *fake) Restart(_ *core.Scenario, a *App) error { return f.rec("restart %s", a.Name) }
func (f *fake) Press(_ *core.Scenario, a *App, k Kind, name string) error {
	return f.rec("press %s %s in %s", name, k.Noun, a.Name)
}

func (f *fake) Fill(_ *core.Scenario, a *App, field, text string) error {
	return f.rec("fill %s with %s in %s", field, text, a.Name)
}

func (f *fake) ScrollIntoView(_ *core.Scenario, a *App, k Kind, name string) error {
	return f.rec("scroll to %s %s in %s", name, k.Noun, a.Name)
}

func (f *fake) Shows(_ *core.Scenario, a *App, text string, wait time.Duration) error {
	return f.rec("shows %s in %s within %s", text, a.Name, wait)
}

func (f *fake) DoesNotShow(_ *core.Scenario, a *App, text string, _ time.Duration) error {
	return f.rec("does not show %s in %s", text, a.Name)
}

func (f *fake) Shown(_ *core.Scenario, a *App, k Kind, name string, _ time.Duration) error {
	return f.rec("shown %s %s in %s", name, k.Noun, a.Name)
}

func (f *fake) Enabled(_ *core.Scenario, a *App, k Kind, name string, enabled bool, _ time.Duration) error {
	return f.rec("enabled %v %s %s in %s", enabled, name, k.Noun, a.Name)
}

func (f *fake) Value(_ *core.Scenario, a *App, field, want string, _ time.Duration) error {
	return f.rec("value %s is %s in %s", field, want, a.Name)
}

func (f *fake) LooksLike(_ *core.Scenario, a *App, shot string, _ time.Duration) error {
	return f.rec("looks like %s in %s", shot, a.Name)
}

func (f *fake) Files(_ *core.Scenario, a *App, path string) (core.Files, error) {
	_ = f.rec("files %s of %s", path, a.Name)
	return core.LocalFiles("/nowhere"), nil
}

func scenario() *core.Scenario {
	return core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "a desk"}, core.NewSuite(core.SuiteOptions{}), nil)
}

// An app runs on one platform in a scenario: a registration for one this
// machine cannot run does nothing here, and two it can run are an error.
func TestRegistrations(t *testing.T) {
	f := &fake{}
	sc := scenario()
	for _, r := range []struct {
		platform string
		runs     bool
	}{{"windows", false}, {"linux", false}, {"macos", true}} {
		if err := Register(sc, &App{Name: "depot", Platform: r.platform, Family: f}, r.runs); err != nil {
			t.Fatalf("%s: %v", r.platform, err)
		}
	}
	if a, err := Get(sc, "depot"); err != nil || a.Platform != "macos" {
		t.Errorf("the app is the registration this machine runs: %+v %v", a, err)
	}

	sc = scenario()
	if err := Register(sc, &App{Name: "courier", Platform: "android", Family: f}, true); err != nil {
		t.Fatal(err)
	}
	err := Register(sc, &App{Name: "courier", Platform: "ios", Family: f}, true)
	if err == nil || !strings.Contains(err.Error(), "the courier app is registered for android and for ios, and this machine can run both") {
		t.Errorf("one app on two platforms the machine runs is an error: %v", err)
	}
	if err := Register(sc, &App{Name: "depot", Platform: "macos", Family: f}, true); err != nil {
		t.Errorf("two apps may run on two platforms: %v", err)
	}

	sc = scenario()
	_ = Register(sc, &App{Name: "depot", Platform: "windows", Family: f}, false)
	if _, err := Get(sc, "depot"); err == nil || !strings.Contains(err.Error(), "the depot app is registered for windows, which this machine ("+Host()+") cannot run") {
		t.Errorf("an app registered only for other platforms names them: %v", err)
	}
	if _, err := Get(sc, "courier"); err == nil || !strings.Contains(err.Error(), `no app named "courier" is registered in this scenario (it registers depot)`) {
		t.Errorf("an unknown app names the registered ones: %v", err)
	}
}

func TestKinds(t *testing.T) {
	for word, noun := range map[string]string{"button": "button", "radio buttons": "radio button", "menu item": "menu item", "menus": "menu", "rows": "row", "checkboxes": "checkbox"} {
		if k, ok := KindOf(word); !ok || k.Noun != noun {
			t.Errorf("%q: %+v %v", word, k, ok)
		}
	}
	if _, ok := KindOf("slider"); ok {
		t.Error("a slider is no kind")
	}
}

// Each step names its app, and its family carries it out: "tapped" and
// "clicked" are one.
func TestSteps(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	f := &fake{}
	if err := Register(h.SC, &App{Name: "depot", Platform: Host(), Family: f}, true); err != nil {
		t.Fatal(err)
	}
	h.OK("the depot app is launched")
	h.OK(`the "Register" button is clicked in the depot app`)
	h.OK(`the "Register" button is tapped in the depot app`)
	h.OK(`the "PX-DSK-4138" row is scrolled into view in the depot app`)
	h.OK(`the "Reference" field in the depot app is filled with "PX-DSK-4201"`)
	h.OK(`within 3s the depot app shows "Registered PX-DSK-4201"`)
	h.OK(`the depot app does not show "PX-DSK-4201 is already registered"`)
	h.OK(`the "Close day" menu item is shown in the depot app`)
	h.OK(`the "Register" button is disabled in the depot app`)
	h.OK(`the "Reference" field in the depot app has the value ""`)
	h.OK(`the depot app looks like the "arrivals" screenshot`)
	h.OK("the depot app is restarted")
	want := []string{
		"launch depot", "press Register button in depot", "press Register button in depot", "scroll to PX-DSK-4138 row in depot",
		"fill Reference with PX-DSK-4201 in depot", "shows Registered PX-DSK-4201 in depot within 3s",
		"does not show PX-DSK-4201 is already registered in depot", "shown Close day menu item in depot",
		"enabled false Register button in depot", "value Reference is  in depot", "looks like arrivals in depot", "restart depot",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("the family was asked:\n%s", strings.Join(f.calls, "\n"))
	}
	_ = h.Fails(`the courier app is launched`, `no app named "courier" is registered in this scenario (it registers depot)`)
	_ = h.Fails(`the depot app looks like the "../x" screenshot`, `a screenshot's name is letters, digits, dots, dashes, underscores and spaces`)
}

// A folder whose owner is an app is the app's files, as its family gives
// them.
func TestFileOwner(t *testing.T) {
	s := core.NewSuite(core.SuiteOptions{})
	if err := (pack{}).Init(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	owner, ok := s.FileOwner("app")
	if !ok {
		t.Fatal("app-core gives the files of apps")
	}
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1"}, s, nil)
	f := &fake{}
	_ = Register(sc, &App{Name: "depot", Platform: Host(), Family: f}, true)
	if files, err := owner(sc, "depot", "./Parcels"); err != nil || files.Where() != filepath.Clean("/nowhere") || f.calls[0] != "files ./Parcels of depot" {
		t.Errorf("the family gives the app's files: %v %v %v", files, err, f.calls)
	}
	if _, err := owner(sc, "courier", "./x"); err == nil {
		t.Error("an app the scenario does not register has no files")
	}
}
