//go:build darwin

package desktopmacos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

// Claim waits for the machine's desktop, once macOS lets axx use the
// accessibility tree.
func (driver) Claim(sc *core.Scenario) (desktopcore.Desktop, error) {
	trusted, err := ax.Trusted()
	if err != nil {
		return nil, err
	}
	if !trusted {
		return nil, fmt.Errorf("macOS does not let axx use the accessibility tree: allow %s in System Settings > Privacy & Security > Accessibility "+
			"(Device Control & Data Access, on macOS 27), and run again", topApp())
	}
	k, err := keptFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	release, err := desktopcore.ClaimMachine(sc)
	if err != nil {
		return nil, err
	}
	return &desk{sc: sc, kept: k, release: release}, nil
}

// desk is the machine's desktop, which a scenario has.
type desk struct {
	sc      *core.Scenario
	kept    *kept
	release func()
}

func (d *desk) Home(app *desktopcore.App) string { return desktopcore.HomeOf(d.sc, app) }

func (d *desk) DataDir() string { return "Library/Application Support" }

// Reset empties the app's preferences: they are the user's, kept by
// cfprefsd, wherever the app's home is. The user's own are kept aside the
// first time, and put back when the run ends.
func (d *desk) Reset(sc *core.Scenario, app *desktopcore.App) error {
	domains, err := preferenceDomains(app)
	if err != nil {
		return err
	}
	if id := bundleID(sc.Context(), app.App); id != "" && !slices.Contains(domains, id) {
		domains = append([]string{id}, domains...)
	}
	for _, domain := range domains {
		if err := d.kept.aside(sc.Context(), domain); err != nil {
			return err
		}
		if err := deleteDomain(sc.Context(), domain); err != nil {
			return err
		}
	}
	return nil
}

func (d *desk) Start(sc *core.Scenario, app *desktopcore.App) (desktopcore.Process, error) {
	return start(sc, app, d.Home(app))
}

func (d *desk) Release() { d.release() }

// kept are the preferences domains a run reset, kept aside in the project's
// .axx/desktop/macos/kept: put back when the run ends, or as the next run
// starts if one ended before it could.
type kept struct {
	mu      sync.Mutex
	dir     string
	domains map[string]bool
}

func keptFor(s *core.Suite) (*kept, error) {
	return core.Cached(s, Name+"/kept", func() (*kept, error) {
		k := &kept{dir: filepath.Join(s.ProjectDir(), ".axx", "desktop", "macos", "kept"), domains: map[string]bool{}}
		if err := k.putBack(context.Background()); err != nil {
			return nil, fmt.Errorf("cannot put back the preferences a run before this one kept aside: %w", err)
		}
		s.OnClose(k.putBack)
		return k, nil
	})
}

func (k *kept) file(domain string) string { return filepath.Join(k.dir, domain+".plist") }

// aside keeps the domain's preferences aside, the first time this run
// resets it.
func (k *kept) aside(ctx context.Context, domain string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.domains[domain] {
		return nil
	}
	if err := os.MkdirAll(k.dir, 0o755); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "defaults", "export", domain, k.file(domain)).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot keep the %s preferences aside: %w %s", domain, err, strings.TrimSpace(string(out)))
	}
	k.domains[domain] = true
	return nil
}

// putBack puts back every domain kept aside, and forgets them.
func (k *kept) putBack(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	files, _ := filepath.Glob(filepath.Join(k.dir, "*.plist"))
	var errs []string
	for _, f := range files {
		domain := strings.TrimSuffix(filepath.Base(f), ".plist")
		if err := deleteDomain(ctx, domain); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if !emptyPlist(ctx, f) {
			if out, err := exec.CommandContext(ctx, "defaults", "import", domain, f).CombinedOutput(); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v %s", domain, err, strings.TrimSpace(string(out))))
				continue
			}
		}
		_ = os.Remove(f)
	}
	k.domains = map[string]bool{}
	if len(errs) > 0 {
		return fmt.Errorf("putting back preferences: %s", strings.Join(errs, "; "))
	}
	return nil
}

// deleteDomain empties a preferences domain; one that is not there is
// empty.
func deleteDomain(ctx context.Context, domain string) error {
	out, err := exec.CommandContext(ctx, "defaults", "delete", domain).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "does not exist") && !strings.Contains(string(out), "not found") {
		return fmt.Errorf("cannot empty the %s preferences: %w %s", domain, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// emptyPlist is whether a property list holds nothing: what defaults
// exports of a domain that is not there.
func emptyPlist(ctx context.Context, path string) bool {
	out, err := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", path).Output()
	return err == nil && strings.TrimSpace(string(out)) == "{}"
}
