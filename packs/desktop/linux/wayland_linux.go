//go:build linux

package desktoplinux

import (
	"embed"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/packs/desktop/internal/atspi"
)

// A Wayland desktop is a headless GNOME Shell for each scenario, on its
// session bus, with PipeWire (its remote desktop records the screen it moves
// the pointer on) and axx's extension (gnome/): it places each window at the
// screen's corner, shows no top bar, overview or notification, and takes
// screenshots. The extension runs from the session's runtime folder, in a
// session mode of its own: nothing is installed. X11 apps (Java's) show on
// its Xwayland, as on a GNOME desktop.

//go:embed gnome
var gnomeFiles embed.FS

// seat is a desktop's pointer, keyboard and screen: X11's or GNOME Shell's.
type seat interface {
	Click(x, y int) error
	Move(x, y int) error
	Drag(x1, y1, x2, y2 int) error
	Scroll(x, y, lines int) error
	Key(spec string) error
	Type(text string) error
	FocusUnderPointer() error
	Activate(pid int) error
	Window(pid int) (image.Rectangle, bool)
	Size() (width, height int)
	Screenshot() (*image.RGBA, error)
	Pointer() (x, y int, ok bool)
	Close()
}

// shellWait is how long GNOME Shell has to start with axx's extension.
const shellWait = 30 * time.Second

// startWayland starts the session's PipeWire and GNOME Shell, with env (the
// session's, its bus's address among it), and returns their processes and
// the shell's seat.
func startWayland(st starter, env []string, run, addr string) (pids []int, g *atspi.Gnome, err error) {
	shell, err := exec.LookPath("gnome-shell")
	if err != nil {
		return nil, nil, errors.New("no gnome-shell: install it (Debian and Ubuntu: apt install gnome-shell), which runs axx's Wayland desktops")
	}
	pipewire, err := exec.LookPath("pipewire")
	if err != nil {
		return nil, nil, errors.New("no pipewire: install it (Debian and Ubuntu: apt install pipewire), which GNOME Shell's remote desktop needs")
	}
	share := filepath.Join(run, "axx-shell")
	if err := writeGnomeFiles(filepath.Join(share, "gnome-shell")); err != nil {
		return nil, nil, err
	}
	pw, _, err := st.start(startRequest{Path: pipewire, Env: env})
	if err != nil {
		return nil, nil, fmt.Errorf("cannot start PipeWire: %w", err)
	}
	pids = append(pids, pw)
	w, h := screenSize()
	dataDirs := cmpEnv(env, "XDG_DATA_DIRS", "/usr/local/share:/usr/share")
	shellEnv := slices.Concat(env, []string{"XDG_DATA_DIRS=" + share + ":" + dataDirs})
	args := []string{"--headless", "--wayland", "--virtual-monitor", fmt.Sprintf("%dx%d", w, h), "--mode=axx"}
	if _, err := exec.LookPath("Xwayland"); err != nil {
		args = append(args, "--no-x11") // X11 apps have no display
	} else {
		x11SocketDir()
	}
	sh, _, err := st.start(startRequest{Path: shell, Args: args, Env: shellEnv})
	if err != nil {
		return pids, nil, fmt.Errorf("cannot start GNOME Shell: %w", err)
	}
	pids = append(pids, sh)
	if err := atspi.WaitForDesktop(addr, shellWait); err != nil {
		return pids, nil, fmt.Errorf("%w within %s: it needs a system bus, and logind where /run/systemd/seats is", err, shellWait)
	}
	g, err = atspi.NewGnome(addr, w, h, run)
	return pids, g, err
}

// x11SocketDir makes X11's sockets' folder where there is none (in a
// container), as a system makes it at boot: GNOME Shell starts Xwayland
// only with it writable by everyone, and sticky.
func x11SocketDir() {
	const dir = "/tmp/.X11-unix"
	if err := os.Mkdir(dir, 0o777); err == nil {
		_ = os.Chmod(dir, 0o777|os.ModeSticky)
	}
}

// writeGnomeFiles writes axx's GNOME Shell extension and session mode into
// dir, a gnome-shell folder of the session's.
func writeGnomeFiles(dir string) error {
	return fs.WalkDir(gnomeFiles, "gnome", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		to := filepath.Join(dir, strings.TrimPrefix(strings.TrimPrefix(p, "gnome"), "/"))
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		b, err := gnomeFiles.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(to, b, 0o644)
	})
}

// screenSize is the size of axx's desktops' screens, in pixels.
func screenSize() (int, int) {
	parts := strings.Split(screen, "x")
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

// cmpEnv is a variable of env, or otherwise.
func cmpEnv(env []string, name, otherwise string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], name+"="); ok && v != "" {
			return v
		}
	}
	return otherwise
}

// placer says where a window's places start on GNOME Shell's screen: on
// Wayland an app knows places in its windows only, counted from what its
// toolkit counts from. The window's size, as the app has it, says which: its
// surface's (GTK 3, which counts its shadow in), its frame's (GTK 4), or its
// content's (Qt, inside the frame it draws: as wide a border on each side
// and under it, its title above). A menu has a window of its own. An X11
// app knows its place on Xwayland's screen, which is GNOME Shell's.
func placer(g *atspi.Gnome) atspi.Placer {
	return func(_ *atspi.Element, pid int, w, h int32) (int32, int32, bool) {
		var frames []atspi.Frame
		for wait := time.Now(); time.Since(wait) < 5*time.Second; time.Sleep(100 * time.Millisecond) {
			if frames, _ = g.Frames(pid); len(frames) > 0 {
				break
			}
		}
		if len(frames) > 0 && frames[0].X11 {
			return 0, 0, true
		}
		near := func(r image.Rectangle) bool { return abs(r.Dx()-int(w)) <= 2 && abs(r.Dy()-int(h)) <= 2 }
		for _, fr := range frames {
			switch {
			case near(fr.Frame):
				return int32(fr.Frame.Min.X), int32(fr.Frame.Min.Y), true
			case near(fr.Surface):
				return int32(fr.Surface.Min.X), int32(fr.Surface.Min.Y), true
			}
		}
		for _, fr := range frames {
			f := fr.Frame
			if int(w) < f.Dx() && int(h) < f.Dy() && f.Dx()-int(w) < 40 {
				border := (f.Dx() - int(w)) / 2
				return int32(f.Min.X + border), int32(f.Max.Y - border - int(h)), true
			}
		}
		return 0, 0, false
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
