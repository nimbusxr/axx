//go:build darwin

package desktopmacos

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

// windowWait is how long an app has to show its window.
const windowWait = 30 * time.Second

// bundle is the .app an app is, or "" for an executable: the registration's
// .app, or the installed app of its bundle identifier, as `open -b` finds
// it.
func bundle(_ context.Context, app string) (string, error) {
	if strings.HasSuffix(strings.TrimRight(app, "/"), ".app") {
		return app, nil
	}
	if filepath.IsAbs(app) || !strings.Contains(app, ".") {
		return "", nil // an executable, or a command
	}
	path, err := ax.AppPath(app)
	if err != nil {
		return "", fmt.Errorf("cannot look for the app %s: %w", app, err)
	}
	return path, nil // "" for a command on PATH, like python3
}

// bundleID is a .app's bundle identifier, the domain of its preferences.
func bundleID(ctx context.Context, app string) string {
	b, err := bundle(ctx, app)
	if err != nil || b == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "defaults", "read", filepath.Join(b, "Contents", "Info"), "CFBundleIdentifier").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// environment is what the app starts with beyond the machine's: its home
// (HOME, and CFFIXED_USER_HOME, which Foundation follows; Java's user.home),
// its language and region, its time zone, and the registration's own.
func environment(app *desktopcore.App, home string) []string {
	locale := strings.ReplaceAll(cmp.Or(app.Locale, "en-US"), "-", "_") + ".UTF-8"
	java := strings.TrimSpace("-Duser.home=" + home + " " + app.Env["JAVA_TOOL_OPTIONS"])
	env := []string{
		"HOME=" + home, "CFFIXED_USER_HOME=" + home, "JAVA_TOOL_OPTIONS=" + java,
		"TZ=" + cmp.Or(app.Timezone, "UTC"), "LANG=" + locale, "LC_ALL=" + locale,
	}
	for _, kv := range app.EnvList() {
		if !strings.HasPrefix(kv, "JAVA_TOOL_OPTIONS=") {
			env = append(env, kv)
		}
	}
	return env
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// start starts the app with home as its home: a .app through LaunchServices
// (macOS kills a system app started from its executable), anything else as
// it is, in the project's folder. It returns the app's process.
func start(sc *core.Scenario, app *desktopcore.App, home string) (*proc, error) {
	ctx := sc.Context()
	b, err := bundle(ctx, app.App)
	if err != nil {
		return nil, err
	}
	env := environment(app, home)
	p := &proc{sc: sc, app: app, flutter: b != "" && exists(filepath.Join(b, "Contents", "Frameworks", "FlutterMacOS.framework"))}
	if b != "" {
		lang := cmp.Or(app.Locale, "en-US")
		// Window restoration off; the language and region, as Cocoa reads
		// them from its arguments.
		// A file among the arguments is opened with the app, as Finder opens
		// it: a Cocoa app opens the documents it is asked to, not those among
		// its arguments.
		var docs, args []string
		for _, a := range app.Arguments() {
			if !strings.HasPrefix(a, "-") && exists(a) {
				docs = append(docs, a)
			} else {
				args = append(args, a)
			}
		}
		args = append(args, "-ApplePersistenceIgnoreState", "YES",
			"-AppleLanguages", "("+lang+")", "-AppleLocale", strings.ReplaceAll(lang, "-", "_"))
		open := []string{"-n", "-g"}
		for _, kv := range env {
			open = append(open, "--env", kv)
		}
		open = append(open, "-a", b)
		open = append(open, docs...)
		open = append(open, "--args")
		open = append(open, args...)
		before := pids(b)
		if out, err := exec.CommandContext(ctx, "open", open...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("open %s: %w %s", b, err, strings.TrimSpace(string(out)))
		}
		for wait := time.Now(); p.pid == 0 && time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
			for _, pid := range pids(b) {
				if !slices.Contains(before, pid) {
					p.pid = pid
				}
			}
		}
		if p.pid == 0 {
			return nil, fmt.Errorf("%s did not start", b)
		}
		pid := p.pid
		p.exited = func() bool { return syscall.Kill(pid, 0) != nil }
	} else {
		// The app outlives the step that starts it: the scenario's end stops it.
		cmd := exec.CommandContext(context.WithoutCancel(ctx), app.App, app.Arguments()...)
		cmd.Dir, cmd.Env = app.Dir, append(os.Environ(), env...)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		p.pid = cmd.Process.Pid
		p.exited = func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}
	}
	p.stop = func() error { return stopTree(p.pid, p.exited) }
	if err := p.await(); err != nil {
		_ = p.stop()
		return nil, err
	}
	return p, nil
}

// await waits for the app's window, switches on what screen readers switch
// on, and fits the window into the screen.
func (p *proc) await() error {
	root, err := ax.Application(p.pid)
	if err != nil {
		return err
	}
	p.root = root
	tray := false
	for wait := time.Now(); ; time.Sleep(200 * time.Millisecond) {
		if ws, _ := root.Elements("AXWindows"); len(ws) > 0 {
			p.sc.Log("the %s app's window came after %s", p.app.Name, time.Since(wait).Round(time.Millisecond))
			break
		}
		// An app that lives in the menu bar's status area (a tray app) shows
		// its item there, and no window until it is used.
		if v, err := root.Attribute("AXExtrasMenuBar"); err == nil && len(children(asElement(v))) > 0 {
			tray = true
			p.sc.Log("the %s app came to the menu bar's status area after %s, with no window", p.app.Name, time.Since(wait).Round(time.Millisecond))
			break
		}
		if p.exited() {
			return fmt.Errorf("the app stopped before it showed a window or a status item")
		}
		if time.Since(wait) > windowWait {
			return fmt.Errorf("the app showed no window and no status item within %s", windowWait)
		}
	}
	// What VoiceOver sets on an app as it starts reading it: Flutter builds
	// its semantics for it. Electron builds its tree for its own attribute.
	_ = root.SetBool("AXEnhancedUserInterface", true)
	_ = root.SetBool("AXManualAccessibility", true)
	if tray {
		// No window to fit: the windows it shows later are on the main
		// screen.
		pos, size, err := ax.VisibleFrame(ax.Point{X: 1, Y: 1})
		if err != nil {
			return err
		}
		p.screen = area{pos.X, pos.Y, size.Width, size.Height}
		return nil
	}
	return p.fit()
}

// fit moves the window into the screen's visible frame, and makes it smaller
// if it is the bigger, as a person does with a window that reaches past the
// screen or under the Dock: the app's own content scrolls.
func (p *proc) fit() error {
	w := p.window()
	f, ok := frameOf(w)
	if !ok {
		return fmt.Errorf("the app's window has no place on the screen")
	}
	pos, size, err := ax.VisibleFrame(ax.Point{X: f.x + f.w/2, Y: f.y + 10})
	if err != nil {
		return err
	}
	p.screen = area{pos.X, pos.Y, size.Width, size.Height}
	if p.screen.intersect(f) == f {
		return nil
	}
	// A full screen window has the whole screen, as its app asked; one is
	// the screen's size before it shows.
	if spos, ssize, err := ax.ScreenFrame(ax.Point{X: f.x + f.w/2, Y: f.y + 10}); err == nil && f == (area{spos.X, spos.Y, ssize.Width, ssize.Height}) {
		return nil
	}
	fitted := ax.Size{Width: min(f.w, p.screen.w), Height: min(f.h, p.screen.h)}
	to := ax.Point{X: min(max(f.x, p.screen.x), p.screen.x+p.screen.w-fitted.Width), Y: min(max(f.y, p.screen.y), p.screen.y+p.screen.h-fitted.Height)}
	if err := w.SetSize(fitted); err != nil {
		return fmt.Errorf("cannot fit the app's window into the screen: %w", err)
	}
	if err := w.SetPosition(to); err != nil {
		return fmt.Errorf("cannot fit the app's window into the screen: %w", err)
	}
	g, _ := frameOf(w)
	p.sc.Log("the %s app's window (%v) reached past the screen's visible frame (%v): now %v", p.app.Name, f, p.screen, g)
	return nil
}

// pids are the processes running the bundle's executables.
func pids(bundle string) []int {
	out, _ := exec.CommandContext(context.Background(), "pgrep", "-f", bundle+"/Contents/MacOS/").Output()
	var ps []int
	for _, f := range strings.Fields(string(out)) {
		if p, err := strconv.Atoi(f); err == nil {
			ps = append(ps, p)
		}
	}
	return ps
}

// descendants are the processes pid started, and those they started.
func descendants(pid int) []int {
	out, err := exec.CommandContext(context.Background(), "ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	kids := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		c, err1 := strconv.Atoi(f[0])
		par, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			kids[par] = append(kids[par], c)
		}
	}
	var out2 []int
	var add func(p int)
	add = func(p int) {
		for _, c := range kids[p] {
			out2 = append(out2, c)
			add(c)
		}
	}
	add(pid)
	return out2
}

// watch reads the system's app as it runs: an installed app, by its bundle
// identifier (Finder's com.apple.finder). The windows it shows from now on
// are closed as the scenario ends, with their close buttons, as a person
// closes them; the app runs on.
func watch(sc *core.Scenario, app *desktopcore.App) (*proc, error) {
	b, err := bundle(sc.Context(), app.App)
	if err != nil {
		return nil, err
	}
	if b == "" {
		return nil, fmt.Errorf("the system's app is an installed app, named by its bundle identifier, like com.apple.finder: %s is not one", app.App)
	}
	running := pids(b)
	if len(running) == 0 {
		return nil, fmt.Errorf("%s is not running: the system's app runs already", b)
	}
	p := &proc{sc: sc, app: app, pid: running[0]}
	if p.root, err = ax.Application(p.pid); err != nil {
		return nil, err
	}
	pos, size, err := ax.VisibleFrame(ax.Point{X: 1, Y: 1})
	if err != nil {
		return nil, err
	}
	p.screen = area{pos.X, pos.Y, size.Width, size.Height}
	p.before = p.windows()
	if p.before == nil {
		p.before = []*ax.Element{}
	}
	p.exited = func() bool { return false }
	p.stop = func() error {
		for _, w := range p.windows() {
			if v, err := w.Attribute("AXCloseButton"); err == nil {
				if b, ok := v.(*ax.Element); ok {
					_ = b.Perform("AXPress")
				}
			}
		}
		return nil
	}
	return p, nil
}

// stopTree asks the app to quit, as the system does at log out, and stops
// it and every process it started when it does not within 10 seconds: an
// app's helpers outlive it (Electron's renderers).
func stopTree(pid int, exited func() bool) error {
	tree := descendants(pid)
	if !exited() {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for wait := time.Now(); !exited() && time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
		}
		if !exited() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	for _, c := range tree {
		_ = syscall.Kill(c, syscall.SIGKILL)
	}
	for wait := time.Now(); !exited() && time.Since(wait) < 5*time.Second; time.Sleep(100 * time.Millisecond) {
	}
	if !exited() {
		return fmt.Errorf("process %d did not stop", pid)
	}
	return nil
}

// topApp is the app at the top of axx's process tree, which macOS asks to be
// allowed to use the accessibility tree and to capture the screen: the
// terminal, the IDE or the CI agent that runs axx.
func topApp() string {
	top := ""
	for pid, n := os.Getpid(), 0; pid > 1 && n < 30; n++ {
		out, err := exec.CommandContext(context.Background(), "ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			break
		}
		ppid, comm, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
		if i := strings.Index(comm, ".app/"); i >= 0 {
			top = filepath.Base(comm[:i+4])
		}
		next, err := strconv.Atoi(ppid)
		if err != nil {
			break
		}
		pid = next
	}
	if top == "" {
		return "the terminal or the IDE that runs axx"
	}
	return strings.TrimSuffix(top, ".app")
}

// asElement is an attribute's value as an element, or nil.
func asElement(v any) *ax.Element {
	e, _ := v.(*ax.Element)
	return e
}
