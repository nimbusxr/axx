package desktopcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/shellwords"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// App is a desktop app a scenario registered, for one OS.
type App struct {
	Name string
	// App is the app as its registration names it: a file of the project
	// (an absolute path), or what the OS finds by name (a bundle
	// identifier, a packaged app's ID, a command on PATH).
	App  string
	Args []string
	// Env is what the app starts with in its environment, beyond what axx
	// sets.
	Env map[string]string
	// Locale is the app's language and region (de-DE), and Timezone its time
	// zone (Europe/Berlin); empty when the registration does not set them.
	Locale, Timezone string
	// Extra are the properties of the OS's own, like Windows' registry.
	Extra map[string]string
	// System is whether the app is the system's (owner: system), one that
	// runs already, like Finder or File Explorer: the scenario reads it as
	// it runs, and never resets, starts or stops it.
	System bool
	// Dir is where the app starts: the project's folder, so its arguments'
	// paths are relative to axx.yaml, as the app's is.
	Dir    string
	Driver Driver
}

// Arguments are the app's arguments, those that name files of the project
// made absolute: they are relative to axx.yaml, as the app is, wherever the
// app starts (a .app starts in /).
func (a *App) Arguments() []string {
	var out []string
	for _, arg := range a.Args {
		switch {
		case strings.HasPrefix(arg, "-"):
			// An option's value: --user-data-dir=.axx/desktop/windows/edge.
			if name, value, ok := strings.Cut(arg, "="); ok {
				arg = name + "=" + a.projectFile(value)
			}
		case filepath.IsAbs(arg), runtime.GOOS == "windows" && strings.HasPrefix(arg, "/"):
		default:
			arg = a.projectFile(arg)
		}
		out = append(out, arg)
	}
	return out
}

// projectFile is the path of a file of the project, which the app may not
// be started in; anything else as it is.
func (a *App) projectFile(arg string) string {
	if arg == "" || filepath.IsAbs(arg) || strings.HasPrefix(arg, "-") {
		return arg
	}
	if p := filepath.Join(a.Dir, arg); exists(p) {
		return p
	}
	return arg
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// EnvList is the app's environment as NAME=value, sorted.
func (a *App) EnvList() []string {
	var out []string
	for k, v := range a.Env {
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out
}

var (
	localeRE  = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z][a-z]{3})?-[A-Z]{2}$`)
	envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Parse reads the registration of the desktop app name for the platform:
// app, args, env.<name>, locale and timezone, and the OS's own properties,
// extra (registry, on Windows).
func Parse(sc *core.Scenario, platform, name string, t *core.Table, extra ...string) (*App, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	a := &App{Name: appcore.Named(sc, name), Env: map[string]string{}, Extra: map[string]string{}, Dir: sc.Suite().ProjectDir()}
	supported := append([]string{"app", "args", "env.<name>", "locale", "timezone", "owner"}, extra...)
	for _, p := range pairs {
		key, value := p.Key, strings.TrimSpace(secrets.Expand(sc, p.Value))
		if env, ok := strings.CutPrefix(key, "env."); ok {
			if !envNameRE.MatchString(env) {
				return nil, fmt.Errorf("the %s %s app's %s: %q is not an environment variable's name", name, platform, key, env)
			}
			a.Env[env] = value
			continue
		}
		switch {
		case key == "app":
			// Another platform's app is there on that platform, not here.
			if platform != appcore.Host() {
				a.App = value
			} else if a.App, err = appPath(sc, value); err != nil {
				return nil, fmt.Errorf("the %s %s app's app: %w", name, platform, err)
			}
		case key == "args":
			a.Args = shellwords.Split(value)
		case key == "locale":
			if !localeRE.MatchString(value) {
				return nil, fmt.Errorf("the %s %s app's locale %q is not a language and region, like de-DE", name, platform, value)
			}
			a.Locale = value
		case key == "owner":
			if value != "system" {
				return nil, fmt.Errorf("the %s %s app's owner %q is not one: system is, for an app the system runs already (Finder, File Explorer); "+
					"leave it out for an app the scenario starts", name, platform, value)
			}
			a.System = true
		case key == "timezone":
			if _, err := time.LoadLocation(value); err != nil || value == "" || value == "Local" {
				return nil, fmt.Errorf("the %s %s app's timezone %q is not a time zone, like Europe/Berlin", name, platform, value)
			}
			a.Timezone = value
		case slices.Contains(extra, key):
			a.Extra[key] = value
		default:
			return nil, fmt.Errorf("unknown %s app property %q (supported: %s)", platform, key, strings.Join(supported, ", "))
		}
	}
	if a.App == "" {
		return nil, fmt.Errorf("the %s %s app has no app: name it, like | app | %s |", name, platform, exampleApp[platform])
	}
	if a.System && (len(a.Args) > 0 || len(a.Env) > 0 || a.Locale != "" || a.Timezone != "" || len(a.Extra) > 0) {
		return nil, fmt.Errorf("the %s %s app is the system's: it runs as it is, so it takes no args, env, locale, timezone or settings of the OS's own", name, platform)
	}
	return a, nil
}

var exampleApp = map[string]string{
	"macos":   "../depot-desk/build/Depot desk.app",
	"windows": `..\depot-desk\build\DepotDesk.exe`,
	"linux":   "../depot-desk/build/depot-desk",
}

// appPath is the app a registration names: a file of the project, relative
// to axx.yaml, or a name the OS finds (a bundle identifier, a packaged
// app's ID, a command).
func appPath(sc *core.Scenario, value string) (string, error) {
	if value == "" {
		return "", errors.New("it is empty")
	}
	p, err := sc.Suite().ResolvePath(value)
	if err == nil {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if strings.ContainsAny(value, `/\`) {
		return "", fmt.Errorf("%s is not there (%s): build it first", value, p)
	}
	return value, nil
}

// Register adds a desktop app to the scenario's apps, which app-core keeps.
// On the OS it is for, the scenario claims the desktop as it registers the
// app, before any phone's device, so two scenarios never each hold one and
// wait for the other.
func Register(sc *core.Scenario, app *App) error {
	runsHere := app.Driver.Platform() == appcore.Host()
	if err := appcore.Register(sc, &appcore.App{Name: app.Name, Platform: app.Driver.Platform(), Family: family{}, Data: app}, runsHere); err != nil {
		return err
	}
	if !runsHere {
		return nil
	}
	desk, err := desktopOf(sc, app.Driver)
	if err != nil || !app.System {
		return err
	}
	// The system's app is watched from now on: the windows it shows from
	// here on are the scenario's, closed as it ends.
	p, err := desk.Watch(sc, app)
	if err != nil {
		return secrets.Hide(sc, fmt.Errorf("cannot read the %s app: %w", app.Name, err))
	}
	s := scenarios.Of(sc)
	s.mu.Lock()
	s.running[app.Name] = p
	s.order = append(s.order, app.Name)
	s.mu.Unlock()
	return nil
}

// desktopApp is the scenario's app of the name, which must be a desktop
// app.
func desktopApp(sc *core.Scenario, name string) (*App, error) {
	a, err := appcore.Get(sc, name)
	if err != nil {
		return nil, err
	}
	d, ok := a.Data.(*App)
	if !ok {
		return nil, fmt.Errorf("the %s app runs on %s: this step is for apps on desktops", name, a.Platform)
	}
	return d, nil
}

// scenario is what a scenario has of the desktop: the desktop it claimed,
// the apps whose homes it reset, and the apps that run.
type scenario struct {
	mu      sync.Mutex
	desktop Desktop
	reset   map[string]bool
	running map[string]Process
	order   []string
	// recordings are its apps' traces and videos, by app; tracing are the
	// trace captures still being taken.
	recordings map[string]*recording
	tracing    sync.WaitGroup
	// recorder records its desktop for its video, from its first app's
	// start.
	recorder *recorder
}

var scenarios = core.NewStateKey(Name+"/scenario", func(sc *core.Scenario) *scenario {
	s := &scenario{reset: map[string]bool{}, running: map[string]Process{}}
	sc.Describe(Name, func() any { return s.describe(sc) })
	return s
}, release)

func (s *scenario) describe(sc *core.Scenario) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{}
	for name, p := range s.running {
		out[name] = p.Describe()
	}
	return secrets.Mask(sc, fmt.Sprint(out))
}

// release keeps what the settings say of the apps' traces and videos, stops
// the scenario's apps, the last started first (the system's apps close the
// windows they showed for it), and gives the desktop back.
func release(sc *core.Scenario, s *scenario) error {
	s.tracing.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	finishRecordings(sc, s)
	var errs []error
	for _, name := range slices.Backward(s.order) {
		p, ok := s.running[name]
		if !ok {
			continue
		}
		if err := p.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("the %s app: %w", name, err))
		}
	}
	s.running, s.order = map[string]Process{}, nil
	if s.desktop != nil {
		s.desktop.Release()
		s.desktop = nil
	}
	if len(errs) > 0 {
		return fmt.Errorf("stopping the desktop apps: %w", errors.Join(errs...))
	}
	return nil
}

// desktopOf is the desktop the scenario has, claimed on first use.
func desktopOf(sc *core.Scenario, d Driver) (Desktop, error) {
	s := scenarios.Of(sc)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktop != nil {
		return s.desktop, nil
	}
	desk, err := d.Claim(sc)
	if err != nil {
		return nil, err
	}
	s.desktop = desk
	return desk, nil
}

// home is the app's home on the scenario's desktop, emptied, and the app
// reset, the first time the scenario uses it: as the app starts, or as a
// step writes or reads its files before then.
func home(sc *core.Scenario, app *App) (Desktop, string, error) {
	if app.System {
		return nil, "", fmt.Errorf("the %s app is the system's: it has no home of the scenario's, and its files are the machine's", app.Name)
	}
	desk, err := desktopOf(sc, app.Driver)
	if err != nil {
		return nil, "", err
	}
	h := desk.Home(app)
	s := scenarios.Of(sc)
	s.mu.Lock()
	done := s.reset[app.Name]
	s.mu.Unlock()
	if done {
		return desk, h, nil
	}
	if err := removeAll(h, 5*time.Second); err != nil {
		return nil, "", fmt.Errorf("cannot empty the %s app's home (%s): %w", app.Name, h, err)
	}
	if err := os.MkdirAll(filepath.Join(h, filepath.FromSlash(desk.DataDir())), 0o755); err != nil {
		return nil, "", err
	}
	if err := desk.Reset(sc, app); err != nil {
		return nil, "", fmt.Errorf("cannot reset the %s app: %w", app.Name, err)
	}
	s.mu.Lock()
	s.reset[app.Name] = true
	s.mu.Unlock()
	return desk, h, nil
}

// removeAll removes a folder and what it holds, trying again for up to wait
// while it cannot: Windows keeps a file until every process that has it
// open has ended, and a stopped app's helpers (a browser's) end a moment
// after it does.
func removeAll(path string, wait time.Duration) error {
	err := os.RemoveAll(path)
	for start := time.Now(); err != nil && time.Since(start) < wait; {
		time.Sleep(250 * time.Millisecond)
		err = os.RemoveAll(path)
	}
	return err
}

// process is the running app of the name; started if start is true and it
// is not running (or has stopped on its own).
func process(sc *core.Scenario, app *App, start bool) (Process, error) {
	s := scenarios.Of(sc)
	s.mu.Lock()
	p, ok := s.running[app.Name]
	s.mu.Unlock()
	if ok && !p.Exited() {
		return traced{p, s}, nil
	}
	if app.System {
		return nil, fmt.Errorf("the %s app is the system's: it runs already, and the scenario does not start it", app.Name)
	}
	if !start {
		if ok {
			return nil, core.Failf("The %s app has stopped: launch it again with \"the %s app is launched\"", app.Name, app.Name)
		}
		return nil, fmt.Errorf("the %s app is not running: launch it with \"the %s app is launched\"", app.Name, app.Name)
	}
	desk, _, err := home(sc, app)
	if err != nil {
		return nil, err
	}
	p, err = desk.Start(sc, app)
	if err != nil {
		return nil, secrets.Hide(sc, fmt.Errorf("cannot start the %s app: %w", app.Name, err))
	}
	s.mu.Lock()
	if !slices.Contains(s.order, app.Name) {
		s.order = append(s.order, app.Name)
	}
	s.running[app.Name] = p
	s.mu.Unlock()
	startRecording(sc, s)
	return traced{p, s}, nil
}

// stop stops the app, if it runs, keeping its home. The system's app is
// never stopped.
func stop(sc *core.Scenario, app *App) error {
	if app.System {
		return fmt.Errorf("the %s app is the system's: the scenario does not stop it", app.Name)
	}
	s := scenarios.Of(sc)
	s.tracing.Wait()
	s.mu.Lock()
	p, ok := s.running[app.Name]
	delete(s.running, app.Name)
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return p.Stop()
}
