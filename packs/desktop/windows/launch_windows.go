//go:build windows

package desktopwindows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/uia"
)

// windowWait is how long an app has to show its window.
const windowWait = 30 * time.Second

// bridgeOn switches the Java Access Bridge on for a Java app's launch.
const bridgeOn = "-Djavax.accessibility.assistive_technologies=com.sun.java.accessibility.AccessBridge"

// environment is what the app starts with beyond the machine's: its home
// (USERPROFILE, and APPDATA, LOCALAPPDATA, TEMP and TMP in it, which apps'
// data folders follow), Java's user.home and the Java Access Bridge, and
// the registration's own.
func environment(app *desktopcore.App, home string) []string {
	java := strings.TrimSpace(bridgeOn + " -Duser.home=" + home + " " + app.Env["JAVA_TOOL_OPTIONS"])
	env := []string{
		"USERPROFILE=" + home, "HOME=" + home,
		"APPDATA=" + filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"TEMP=" + filepath.Join(home, "AppData", "Local", "Temp"), "TMP=" + filepath.Join(home, "AppData", "Local", "Temp"),
		"JAVA_TOOL_OPTIONS=" + java,
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

// isFlutter is whether the app is a Flutter app: its engine's DLL is next
// to it.
func isFlutter(app string) bool {
	return filepath.IsAbs(app) && exists(filepath.Join(filepath.Dir(app), "flutter_windows.dll"))
}

// start starts the app with home as its home, in the project's folder, and
// waits for its window.
func start(sc *core.Scenario, w *worker, app *desktopcore.App, home string) (*proc, error) {
	for _, dir := range []string{`AppData\Roaming`, `AppData\Local\Temp`} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			return nil, err
		}
	}
	if app.Locale != "" || app.Timezone != "" {
		sc.Log("Windows has no language or time zone of an app's own: the %s app starts in the machine's", app.Name)
	}
	// The app outlives the step that starts it: the scenario's end stops it.
	cmd := exec.CommandContext(context.WithoutCancel(sc.Context()), app.App, app.Arguments()...)
	cmd.Dir, cmd.Env = app.Dir, append(os.Environ(), environment(app, home)...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	p := &proc{sc: sc, app: app, w: w, pid: cmd.Process.Pid, flutter: isFlutter(app.App)}
	p.u = &uiaTree{p: p, c: w.uia}
	p.exited = func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	p.stop = func() error { return stopTree(p.pid, p.exited) }
	if err := p.await(); err != nil {
		_ = p.stop()
		return nil, err
	}
	return p, nil
}

// await waits for the app's main window, fits it into the screen's work
// area, and reads a Java window through the bridge.
func (p *proc) await() error {
	return p.w.do(func() error {
		var win *uia.Element
		for wait := time.Now(); ; p.w.pump(250 * time.Millisecond) {
			if win = p.window(); win != nil {
				p.sc.Log("the %s app's window came after %s", p.app.Name, time.Since(wait).Round(time.Millisecond))
				break
			}
			if p.exited() {
				return fmt.Errorf("the app stopped before it showed a window")
			}
			if time.Since(wait) > windowWait {
				return fmt.Errorf("the app showed no window within %s", windowWait)
			}
		}
		// It opens as for a person who opened it with the mouse, whatever the
		// last scenario typed: once it is in front, as Windows sets a window's
		// cues as it activates it.
		if uia.Foreground(win.Handle()) {
			p.w.pump(200 * time.Millisecond)
		}
		uia.HideKeyboardCues(win.Handle())
		if err := p.fit(win); err != nil {
			return err
		}
		return p.attachJava(win)
	})
}

// fit moves the window into the screen's work area, and makes it smaller if
// it is the bigger, as a person does with a window that reaches past the
// screen or under the taskbar: the app's own content scrolls.
func (p *proc) fit(w *uia.Element) error {
	f, a := w.Bounds(), uia.WorkArea()
	// A full screen window has the whole screen, as its app asked.
	if uia.Intersect(f, a) == f || f == uia.Screen() {
		return nil
	}
	width, height := min(f.Right-f.Left, a.Right-a.Left), min(f.Bottom-f.Top, a.Bottom-a.Top)
	left, top := min(max(f.Left, a.Left), a.Right-width), min(max(f.Top, a.Top), a.Bottom-height)
	if err := uia.MoveWindow(w.Handle(), uia.Rect{Left: left, Top: top, Right: left + width, Bottom: top + height}); err != nil {
		return fmt.Errorf("cannot fit the app's window into the screen: %w", err)
	}
	p.w.pump(300 * time.Millisecond)
	p.sc.Log("the %s app's window (%+v) reached past the work area (%+v): now %+v", p.app.Name, f, a, w.Bounds())
	return nil
}

// stopTree asks the app to close its windows, as closing them does, and
// stops it and every process it started: at once when it takes messages yet
// keeps its windows, shown (a kiosk that refuses) or hidden (a tray app,
// whose windows' closing leaves it running); when it stays as it was or
// with no window a second on (a moment on, for one with no window to
// close); else when it has not gone within 10 seconds (one asking whether
// to save). It waits for each of them: one that lingers holds what the next
// start needs (a browser's profile).
func stopTree(pid int, exited func() bool) error {
	ctx := context.Background()
	tree := append([]int{pid}, uia.Descendants(pid)...)
	if !exited() {
		before := uia.VisibleWindows(tree)
		_ = exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T").Run()
		grace := time.Second
		if len(before) == 0 {
			grace = 300 * time.Millisecond
		}
		for wait := time.Now(); !exited() && time.Since(wait) < 10*time.Second; time.Sleep(50 * time.Millisecond) {
			since := time.Since(wait)
			if since < 250*time.Millisecond {
				continue
			}
			now := uia.VisibleWindows(tree)
			// Its windows kept as they were, or hidden and not closed (a tray
			// app's), by an app that takes messages: it refused.
			kept := len(before) > 0 && (slices.Equal(now, before) || len(now) == 0 && !slices.ContainsFunc(before, gone))
			if kept && answered(before) || since >= grace && (len(now) == 0 || slices.Equal(now, before)) {
				break
			}
		}
	}
	for _, p := range tree {
		uia.Terminate(p)
	}
	alive := func() bool { return !exited() || slices.ContainsFunc(tree[1:], uia.Alive) }
	for wait := time.Now(); alive() && time.Since(wait) < 5*time.Second; time.Sleep(50 * time.Millisecond) {
	}
	if alive() {
		return fmt.Errorf("process %d or one it started did not stop", pid)
	}
	return nil
}

// bridgeDLL is the Java Access Bridge's client DLL of the Java that runs the
// app: next to java.exe, in a packaged app's runtime, or in JAVA_HOME.
// gone is whether the window was closed.
func gone(w uintptr) bool { return !uia.IsWindow(w) }

// watch reads the system's app as it runs: the processes of its executable
// (explorer.exe), not those they started (File Explorer starts what a
// person opens from the taskbar). The windows it shows from now on are
// closed as the scenario ends, as their close buttons close them; the app
// runs on.
func watch(sc *core.Scenario, w *worker, app *desktopcore.App) (*proc, error) {
	exe := filepath.Base(app.App)
	if !strings.Contains(exe, ".") {
		exe += ".exe"
	}
	running := uia.Named(exe)
	if len(running) == 0 {
		return nil, fmt.Errorf("%s is not running: the system's app runs already", exe)
	}
	p := &proc{sc: sc, app: app, w: w, pid: running[0], named: exe}
	p.u = &uiaTree{p: p, c: w.uia}
	p.before = uia.VisibleWindows(p.pids())
	p.exited = func() bool { return false }
	p.stop = func() error {
		for _, h := range uia.VisibleWindows(p.pids()) {
			if !slices.Contains(p.before, h) {
				uia.Close(h)
			}
		}
		return nil
	}
	return p, nil
}

// answered is whether every window's app takes messages: one that kept its
// windows took the request to close them, and refused it.
func answered(windows []uintptr) bool {
	for _, w := range windows {
		if !uia.Answers(w, 100*time.Millisecond) {
			return false
		}
	}
	return true
}

func bridgeDLL(app *desktopcore.App) (string, error) {
	var dirs []string
	if filepath.IsAbs(app.App) {
		dir := filepath.Dir(app.App)
		dirs = append(dirs, dir, filepath.Join(dir, "runtime", "bin"))
	}
	if home := app.Env["JAVA_HOME"]; home != "" {
		dirs = append(dirs, filepath.Join(home, "bin"))
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "bin"))
	}
	if java, err := exec.LookPath("java"); err == nil {
		dirs = append(dirs, filepath.Dir(java))
	}
	for _, d := range dirs {
		if p := filepath.Join(d, "WindowsAccessBridge-64.dll"); exists(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("the app is a Java app, and no Java Access Bridge is found for it (WindowsAccessBridge-64.dll, next to java.exe): " +
		"run it with a JDK's java.exe, or set env.JAVA_HOME to one")
}
