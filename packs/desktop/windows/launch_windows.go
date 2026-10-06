//go:build windows

package desktopwindows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if uia.Intersect(f, a) == f {
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
// stops it and every process it started when it has not within 10 seconds.
func stopTree(pid int, exited func() bool) error {
	ctx := context.Background()
	if !exited() {
		_ = exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T").Run()
		for wait := time.Now(); !exited() && time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
		}
	}
	_ = exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	for wait := time.Now(); !exited() && time.Since(wait) < 5*time.Second; time.Sleep(100 * time.Millisecond) {
	}
	if !exited() {
		return fmt.Errorf("process %d did not stop", pid)
	}
	return nil
}

// bridgeDLL is the Java Access Bridge's client DLL of the Java that runs the
// app: next to java.exe, in a packaged app's runtime, or in JAVA_HOME.
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
