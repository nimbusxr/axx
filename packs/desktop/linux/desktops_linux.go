//go:build linux

package desktoplinux

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/atspi"
)

// screen is the size of axx's desktops' screens: room for an app's window,
// with no window manager to place it.
const screen = "1920x1200x24"

// desktop is one of axx's desktops: a virtual screen (Xvfb), and the
// pointer and keyboard on it.
type desktop struct {
	n       int // among the run's desktops, from 1
	display string
	xvfb    *exec.Cmd
	in      *atspi.Input
	// wayland is whether its scenarios run GNOME Shell of their own, on
	// Wayland, rather than on its virtual screen.
	wayland bool
}

// pool are the run's desktops, started as scenarios first need them.
type pool struct {
	size  int
	slots chan *desktop
	mu    sync.Mutex
	all   []*desktop
}

func poolFor(s *core.Suite) (*pool, error) {
	return core.Cached(s, Name+"/pool", func() (*pool, error) {
		n, err := desktopsFor(s)
		if err != nil {
			return nil, err
		}
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		p := &pool{size: n, slots: make(chan *desktop, n)}
		for i := 1; i <= n; i++ {
			p.slots <- &desktop{n: i, wayland: c.Display == "wayland"}
		}
		s.OnClose(func(context.Context) error { p.stop(); return nil })
		return p, nil
	})
}

func (p *pool) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range p.all {
		if d.in != nil {
			d.in.Close()
		}
		if d.xvfb != nil && d.xvfb.Process != nil {
			_ = d.xvfb.Process.Kill()
			_ = d.xvfb.Wait()
		}
	}
}

// start starts the desktop's screen, once.
func (d *desktop) start(p *pool) error {
	if d.in != nil || d.wayland {
		return nil // a Wayland desktop starts with each scenario's session
	}
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		return errors.New("no Xvfb: install it (Debian and Ubuntu: apt install xvfb), which runs axx's desktops")
	}
	for num := 99 + d.n; num < 99+d.n+400; num += 32 {
		if exists(fmt.Sprintf("/tmp/.X%d-lock", num)) || exists(fmt.Sprintf("/tmp/.X11-unix/X%d", num)) {
			continue
		}
		display := ":" + strconv.Itoa(num)
		// It outlives the step that starts it: the run's end stops it.
		cmd := exec.CommandContext(context.Background(), xvfb, display, "-screen", "0", screen, "-nolisten", "tcp", "-noreset")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("cannot start Xvfb: %w", err)
		}
		for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
			if in, err := atspi.NewInputOn(display); err == nil {
				d.display, d.xvfb, d.in = display, cmd, in
				p.mu.Lock()
				p.all = append(p.all, d)
				p.mu.Unlock()
				return nil
			}
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return errors.New("cannot start a desktop: Xvfb did not take any display")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Claim takes one of the run's desktops for the scenario, waiting for one
// with the step's clock stopped.
func (driver) Claim(sc *core.Scenario) (desktopcore.Desktop, error) {
	p, err := poolFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	held := sc.Hold()
	var d *desktop
	select {
	case d = <-p.slots:
	default:
		sc.Log("waiting for a desktop: the run's %d are taken", p.size)
		select {
		case d = <-p.slots:
		case <-sc.Context().Done():
			held()
			return nil, sc.Context().Err()
		}
	}
	held()
	if err := d.start(p); err != nil {
		p.slots <- d
		return nil, err
	}
	return &desk{sc: sc, d: d, pool: p}, nil
}

// desk is a desktop a scenario has, and the scenario's session on it: its
// session bus and accessibility bus, started with the first app's home.
type desk struct {
	sc   *core.Scenario
	d    *desktop
	pool *pool
	s    *session
}

// Home is the app's home on the desktop: with one desktop, the project's
// .axx/desktop/linux/<app>.
func (k *desk) Home(app *desktopcore.App) string {
	if k.pool.size == 1 {
		return desktopcore.HomeOf(k.sc, app)
	}
	return filepath.Join(k.sc.Suite().ProjectDir(), ".axx", "desktop", "linux", "desktop-"+strconv.Itoa(k.d.n), app.Name)
}

func (k *desk) DataDir() string { return ".local/share" }

// Reset starts the scenario's session, with the app's home: the session's
// services (dconf, the keyring, the portals) keep the app's settings there,
// emptied with it.
func (k *desk) Reset(sc *core.Scenario, app *desktopcore.App) error {
	if k.s != nil {
		return nil
	}
	var st starter = &direct{}
	if k.pool.size > 1 {
		// The desktop's own folder, where its apps see the project's
		// .axx/desktop/linux.
		h, err := newHelper(helperSpec{Host: filepath.Dir(k.Home(app)), View: filepath.Dir(desktopcore.HomeOf(sc, app))})
		if err != nil {
			return err
		}
		st = h
	}
	s, err := startSession(sc, k.d, st, desktopcore.HomeOf(sc, app))
	if err != nil {
		st.close()
		return err
	}
	k.s = s
	return nil
}

// Start starts the app with its home where it sees it: on every desktop,
// the project's .axx/desktop/linux/<app>.
func (k *desk) Start(sc *core.Scenario, app *desktopcore.App) (desktopcore.Process, error) {
	if k.s == nil {
		if err := k.Reset(sc, app); err != nil {
			return nil, err
		}
	}
	return k.s.start(sc, app, desktopcore.HomeOf(sc, app))
}

func (k *desk) Release() {
	if k.s != nil {
		k.s.stop()
		k.s = nil
	}
	k.pool.slots <- k.d
}

// session is a scenario's session bus and accessibility bus on a desktop,
// and what starts its processes.
type session struct {
	d             *desktop
	st            starter
	run           string // its runtime folder
	addr          string // the session bus's
	bus, launcher int    // their processes
	c             *atspi.Client
	pub           *atspi.Display
	// seat is its pointer, keyboard and screen: the desktop's X11 ones, or
	// its own GNOME Shell's, whose processes (and PipeWire's) are shell.
	seat  seat
	shell []int
	// x11 and xauth are its GNOME Shell's Xwayland display and the X
	// authority file it takes, on Wayland.
	x11, xauth string
}

// homeEnv points an app or a session's services at a home: HOME and the XDG
// folders in it.
func homeEnv(home string) []string {
	return []string{
		"HOME=" + home, "XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
	}
}

// machineEnv is this process's environment without what axx sets for its
// desktops: the user's own display, buses and Wayland, and how toolkits
// pick one.
func machineEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS", "AT_SPI_BUS_ADDRESS", "NO_AT_BRIDGE", "XDG_RUNTIME_DIR",
			"XDG_SESSION_TYPE", "XDG_CURRENT_DESKTOP", "GDK_BACKEND", "QT_QPA_PLATFORM", "ELECTRON_OZONE_PLATFORM_HINT":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// startSession starts a session bus with home's environment, the
// accessibility bus on it, tells it an assistive technology is on, and puts
// the accessibility bus's address on the desktop's screen (Qt 5 reads it
// there).
func startSession(sc *core.Scenario, d *desktop, st starter, home string) (*session, error) {
	dbusDaemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		return nil, errors.New("no dbus-daemon: install dbus (Debian and Ubuntu: apt install dbus-daemon)")
	}
	launcher, err := busLauncher()
	if err != nil {
		return nil, err
	}
	// The session's runtime folder, as a desktop session has one: the buses'
	// sockets and dconf's live there, where axx reaches them from outside a
	// desktop's view of the file system.
	run, err := os.MkdirTemp("", "axx-desktop-")
	if err != nil {
		return nil, err
	}
	env := append(append(machineEnv(), homeEnv(home)...), "XDG_RUNTIME_DIR="+run)
	if !d.wayland {
		env = append(env, "DISPLAY="+d.display)
	}
	s := &session{d: d, st: st, run: run}
	if !d.wayland {
		s.seat = d.in
	}
	s.bus, s.addr, err = st.start(startRequest{Path: dbusDaemon, Args: []string{"--session", "--nofork", "--nopidfile", "--print-address=1"}, Env: env, FirstLine: true})
	if err != nil {
		return nil, fmt.Errorf("cannot start a session bus: %w", err)
	}
	if s.addr = strings.TrimSpace(s.addr); s.addr == "" {
		s.stop()
		return nil, errors.New("the session bus did not say its address")
	}
	if s.launcher, _, err = st.start(startRequest{Path: launcher, Args: []string{"--launch-immediately"}, Env: append(env, "DBUS_SESSION_BUS_ADDRESS="+s.addr)}); err != nil {
		s.stop()
		return nil, fmt.Errorf("cannot start the accessibility bus: %w", err)
	}
	for wait := time.Now(); s.c == nil; time.Sleep(100 * time.Millisecond) {
		c, err := atspi.NewOn(s.addr)
		if err == nil {
			s.c = c
			break
		}
		if time.Since(wait) > 10*time.Second {
			s.stop()
			return nil, fmt.Errorf("the accessibility bus did not start: %w", err)
		}
	}
	if err := atspi.AnnounceOn(s.addr); err != nil {
		s.stop()
		return nil, err
	}
	if d.wayland {
		pids, g, err := startWayland(st, append(env, "DBUS_SESSION_BUS_ADDRESS="+s.addr), run, s.addr)
		s.shell = pids
		if err != nil {
			s.stop()
			return nil, err
		}
		s.seat = g
		s.x11, s.xauth = g.X11Display()
		s.c.SetPlacer(placer(g))
		if s.x11 != "" {
			// X11 apps (Qt 5's) find the accessibility bus on Xwayland's
			// screen, as on X11 desktops.
			if s.pub, err = atspi.PublishBusOn(s.x11, s.xauth, s.c.Address()); err != nil {
				s.stop()
				return nil, err
			}
		}
		sc.Log("the scenario's session on desktop %d: GNOME Shell on Wayland", d.n)
		return s, nil
	}
	if s.pub, err = atspi.PublishBusOn(d.display, "", s.c.Address()); err != nil {
		s.stop()
		return nil, err
	}
	sc.Log("the scenario's session on desktop %d (%s)", d.n, d.display)
	return s, nil
}

// busLauncher is at-spi2-core's launcher of the accessibility bus, where the
// distribution keeps it.
func busLauncher() (string, error) {
	for _, pattern := range []string{"/usr/libexec/at-spi-bus-launcher", "/usr/lib/at-spi2-core/at-spi-bus-launcher", "/usr/lib/*/at-spi2-core/at-spi-bus-launcher", "/usr/lib/at-spi-bus-launcher"} {
		if m, _ := filepath.Glob(pattern); len(m) > 0 {
			return m[0], nil
		}
	}
	return "", errors.New("no at-spi-bus-launcher: install at-spi2-core (Debian and Ubuntu: apt install at-spi2-core)")
}

// stop stops the session's buses and every service they started.
func (s *session) stop() {
	if s.pub != nil {
		s.pub.Close()
	}
	if s.d.wayland && s.seat != nil {
		s.seat.Close()
	}
	for _, pid := range s.shell {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	if s.c != nil {
		s.c.Close()
	}
	for _, pid := range []int{s.launcher, s.bus} {
		if pid > 0 {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
	s.st.close()
	_ = os.RemoveAll(s.run)
}

// atkWrapper is java-atk-wrapper's jar, which bridges Java's accessibility
// to AT-SPI, where the distribution keeps it.
const atkWrapper = "/usr/share/java/java-atk-wrapper.jar"

// appEnv is what the app starts with: its desktop's display and session,
// its home, its language and time zone, what each toolkit needs to be read,
// and the registration's own.
func (s *session) appEnv(app *desktopcore.App, home string) []string {
	locale := strings.ReplaceAll(cmp.Or(app.Locale, "en-US"), "-", "_") + ".UTF-8"
	java := "-Duser.home=" + home
	if exists(atkWrapper) {
		java = "-Xbootclasspath/a:" + atkWrapper + " -Djavax.accessibility.assistive_technologies=org.GNOME.Accessibility.AtkWrapper " + java
	}
	env := append(machineEnv(), homeEnv(home)...)
	display := []string{
		// X11, where axx's desktops are.
		"DISPLAY=" + s.d.display, "GDK_BACKEND=x11", "QT_QPA_PLATFORM=xcb",
	}
	if s.d.wayland {
		// Or its GNOME Shell's Wayland, in a GNOME session: each toolkit
		// picks Wayland or X11 as it does on a GNOME desktop (GTK, Qt 6 and
		// Electron Wayland; Qt 5 and Java X11, on its Xwayland).
		display = []string{"WAYLAND_DISPLAY=wayland-0", "XDG_SESSION_TYPE=wayland", "XDG_CURRENT_DESKTOP=GNOME"}
		if s.x11 != "" {
			display = append(display, "DISPLAY="+s.x11, "XAUTHORITY="+s.xauth)
		}
	}
	env = append(env, display...)
	env = append(env,
		"DBUS_SESSION_BUS_ADDRESS="+s.addr, "XDG_RUNTIME_DIR="+s.run,
		"LANG="+locale, "LC_ALL="+locale, "TZ="+cmp.Or(app.Timezone, "UTC"),
		// Accessibility on: Chromium (Electron, CEF) takes it from here; Qt
		// from these.
		"ACCESSIBILITY_ENABLED=1", "QT_ACCESSIBILITY=1", "QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1",
		"JAVA_TOOL_OPTIONS="+strings.TrimSpace(java+" "+app.Env["JAVA_TOOL_OPTIONS"]),
	)
	for _, kv := range app.EnvList() {
		if !strings.HasPrefix(kv, "JAVA_TOOL_OPTIONS=") {
			env = append(env, kv)
		}
	}
	return env
}

// windowWait is how long an app has to come on the accessibility bus with a
// window.
const windowWait = 30 * time.Second

// start starts the app on the session's desktop, in the project's folder,
// in a process group of its own, and waits for it on the accessibility bus.
func (s *session) start(sc *core.Scenario, app *desktopcore.App, home string) (*proc, error) {
	path, err := exec.LookPath(app.App)
	if err != nil {
		return nil, err
	}
	pid, _, err := s.st.start(startRequest{Path: path, Args: app.Arguments(), Env: s.appEnv(app, home), Dir: app.Dir})
	if err != nil {
		return nil, err
	}
	p := &proc{sc: sc, app: app, pid: pid, in: s.seat, wayland: s.d.wayland}
	p.exited = func() bool { return s.st.exited(pid) }
	for wait := time.Now(); ; time.Sleep(250 * time.Millisecond) {
		if root, err := s.c.ApplicationOf(p.pid); err == nil {
			if kids, _ := root.Children(); len(kids) > 0 {
				p.root = root
				sc.Log("the %s app came on the accessibility bus after %s", app.Name, time.Since(wait).Round(time.Millisecond))
				break
			}
		}
		if p.exited() {
			return nil, errors.New("the app stopped before it showed a window")
		}
		if time.Since(wait) > windowWait {
			_ = p.Stop()
			return nil, fmt.Errorf("the app did not come on the accessibility bus with a window within %s", windowWait)
		}
	}
	name, version := p.root.Toolkit()
	p.gtk4 = name == "GTK" && strings.HasPrefix(version, "4.")
	if !p.gtk4 {
		// Only GTK 4 knows places in its window only.
		p.root.WithScreenPlaces()
	}
	p.java = strings.Contains(strings.ToLower(name), "java") || strings.Contains(name, "J2SE")
	if atspi.IsFlutter(p.pid) {
		p.flutter = true
		p.root.WithoutPlaces()
	}
	return p, nil
}

// desktopsFor is how many desktops the run may have.
func desktopsFor(s *core.Suite) (int, error) {
	var c Config
	if err := s.PackConfig(Name, &c); err != nil {
		return 0, err
	}
	switch {
	case c.Desktops == 0:
		return 1, nil
	case c.Desktops < 0:
		return 0, fmt.Errorf("packs.%s.desktops: %d is not a number of desktops", Name, c.Desktops)
	}
	return c.Desktops, nil
}
