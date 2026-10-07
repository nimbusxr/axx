//go:build windows

package desktopwindows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Claim waits for the machine's desktop, once this session has one: a
// program that runs as a service, or in an SSH session, has none.
func (driver) Claim(sc *core.Scenario) (desktopcore.Desktop, error) {
	if ok, station := interactive(); !ok {
		return nil, fmt.Errorf("this session has no desktop to run apps on (its window station is %q): run axx on the signed-in user's desktop, "+
			"like a terminal there, or a scheduled task for that user (schtasks /it); not as a service or over SSH", station)
	}
	w, err := work()
	if err != nil {
		return nil, fmt.Errorf("cannot reach UI Automation: %w", err)
	}
	k, err := keptFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	release, err := desktopcore.ClaimMachine(sc)
	if err != nil {
		return nil, err
	}
	return &desk{sc: sc, w: w, kept: k, release: release}, nil
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	getProcessWindowStation = user32.NewProc("GetProcessWindowStation")
	getUserObjectInfo       = user32.NewProc("GetUserObjectInformationW")
)

// interactive is whether this process runs on the signed-in user's
// desktop, the interactive window station WinSta0, and the station's name.
func interactive() (bool, string) {
	station, _, _ := getProcessWindowStation.Call()
	if station == 0 {
		return false, "none"
	}
	const uoiName = 2
	buf := make([]uint16, 256)
	var n uint32
	if ok, _, _ := getUserObjectInfo.Call(station, uoiName, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&n))); ok == 0 {
		return false, "unknown"
	}
	name := syscall.UTF16ToString(buf)
	return strings.EqualFold(name, "WinSta0"), name
}

// desk is the machine's desktop, which a scenario has.
type desk struct {
	sc      *core.Scenario
	w       *worker
	kept    *kept
	release func()
}

func (d *desk) Home(app *desktopcore.App) string { return desktopcore.HomeOf(d.sc, app) }

func (d *desk) DataDir() string { return "AppData/Roaming" }

// Reset empties the registry keys the registration names: they are the
// user's, wherever the app's home is. The user's own are kept aside the
// first time, and put back when the run ends.
func (d *desk) Reset(sc *core.Scenario, app *desktopcore.App) error {
	for _, key := range registryKeys(app) {
		if err := d.kept.aside(sc.Context(), key); err != nil {
			return err
		}
		if err := deleteKey(sc.Context(), key); err != nil {
			return err
		}
	}
	return nil
}

func (d *desk) Start(sc *core.Scenario, app *desktopcore.App) (desktopcore.Process, error) {
	return start(sc, d.w, app, d.Home(app))
}

func (d *desk) Watch(sc *core.Scenario, app *desktopcore.App) (desktopcore.Process, error) {
	return watch(sc, d.w, app)
}

func (d *desk) Release() { d.release() }

// registryKeys are the keys under HKEY_CURRENT_USER the registration names,
// separated by commas.
func registryKeys(app *desktopcore.App) []string {
	var keys []string
	for _, k := range strings.Split(app.Extra["registry"], ",") {
		k = strings.Trim(strings.TrimSpace(k), `\`)
		k = strings.TrimPrefix(strings.TrimPrefix(k, `HKEY_CURRENT_USER\`), `HKCU\`)
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// kept are the registry keys a run reset, kept aside in the project's
// .axx/desktop/windows/kept: put back when the run ends, or as the next run
// starts if one ended before it could. A key that was not there is kept as
// an empty file.
type kept struct {
	mu   sync.Mutex
	dir  string
	keys map[string]bool
}

func keptFor(s *core.Suite) (*kept, error) {
	return core.Cached(s, Name+"/kept", func() (*kept, error) {
		k := &kept{dir: filepath.Join(s.ProjectDir(), ".axx", "desktop", "windows", "kept"), keys: map[string]bool{}}
		if err := k.putBack(context.Background()); err != nil {
			return nil, fmt.Errorf("cannot put back the registry keys a run before this one kept aside: %w", err)
		}
		s.OnClose(k.putBack)
		return k, nil
	})
}

// file is where a key is kept: its path, its backslashes as "~".
func (k *kept) file(key string) string {
	return filepath.Join(k.dir, strings.ReplaceAll(key, `\`, "~")+".reg")
}

func (k *kept) aside(ctx context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.keys[key] {
		return nil
	}
	if err := os.MkdirAll(k.dir, 0o755); err != nil {
		return err
	}
	f := k.file(key)
	if !keyExists(ctx, key) {
		if err := os.WriteFile(f, nil, 0o644); err != nil { // not there: nothing to put back
			return err
		}
	} else if out, err := exec.CommandContext(ctx, "reg", "export", `HKCU\`+key, f, "/y").CombinedOutput(); err != nil {
		return fmt.Errorf("cannot keep the registry key %s aside: %w %s", key, err, strings.TrimSpace(string(out)))
	}
	k.keys[key] = true
	return nil
}

func (k *kept) putBack(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	files, _ := filepath.Glob(filepath.Join(k.dir, "*.reg"))
	var errs []string
	for _, f := range files {
		key := strings.ReplaceAll(strings.TrimSuffix(filepath.Base(f), ".reg"), "~", `\`)
		if err := deleteKey(ctx, key); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if fi, err := os.Stat(f); err == nil && fi.Size() > 0 {
			if out, err := exec.CommandContext(ctx, "reg", "import", f).CombinedOutput(); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v %s", key, err, strings.TrimSpace(string(out))))
				continue
			}
		}
		_ = os.Remove(f)
	}
	k.keys = map[string]bool{}
	if len(errs) > 0 {
		return fmt.Errorf("putting back registry keys: %s", strings.Join(errs, "; "))
	}
	return nil
}

// deleteKey empties a registry key under HKEY_CURRENT_USER; one that is not
// there is empty.
func deleteKey(ctx context.Context, key string) error {
	if !keyExists(ctx, key) {
		return nil
	}
	if out, err := exec.CommandContext(ctx, "reg", "delete", `HKCU\`+key, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("cannot empty the registry key %s: %w %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// keyExists is whether the key is there under HKEY_CURRENT_USER.
func keyExists(ctx context.Context, key string) bool {
	return exec.CommandContext(ctx, "reg", "query", `HKCU\`+key).Run() == nil
}
