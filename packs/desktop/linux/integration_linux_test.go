//go:build linux && integration

package desktoplinux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/desktest"
	"github.com/nimbusxr/axx/packs/files"
)

// TestDepotDesk works a build of the depot desk with the packs' steps: see
// desktest.Journey for the build it works (AXX_DESK) and its toolkit's
// kinds of control.
func TestDepotDesk(t *testing.T) { desktest.Journey(t, "linux", Pack()) }

// Two scenarios at once, each on a desktop of its own: each app's home is at
// the same path, each sees its own files only, and each desktop's folder
// has its own scenario's. AXX_DESK names the build, as for TestDepotDesk.
func TestTwoDesktops(t *testing.T) {
	app := os.Getenv("AXX_DESK")
	if app == "" {
		t.Skip("AXX_DESK names a depot desk build")
	}
	h := cloudtest.NewWith(t, map[string]any{Name: map[string]any{"desktops": 2}}, appcore.Pack(), desktopcore.Pack(), Pack(), files.Pack())
	h.Start(&core.Plan{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, ref := range []string{"PX-DSK-4301", "PX-DSK-4302"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		t.Cleanup(cancel)
		sc := core.NewScenario(ctx, core.ScenarioInfo{ID: ref, Name: ref}, h.Suite, h.Sink)
		s := h.In(sc)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { _ = sc.Close() }()
			rows := [][]string{{"app", app}}
			if args := os.Getenv("AXX_DESK_ARGS"); args != "" {
				rows = append(rows, []string{"args", args})
			}
			other := map[string]string{"PX-DSK-4301": "PX-DSK-4302", "PX-DSK-4302": "PX-DSK-4301"}[ref]
			for _, step := range []struct {
				text string
				arg  any
			}{
				{"the depot linux app with the following properties:", rows},
				{"the desk folder with the following properties:", [][]string{{"owner", "app:depot"}, {"path", os.Getenv("AXX_DESK_DATA")}}},
				{"the depot app is launched", nil},
				{`the "Reference" field in the depot app is filled with "` + ref + `"`, nil},
				{"the Enter key is pressed in the depot app", nil},
				{`the depot app shows "Registered ` + ref + `: Standard"`, nil},
				{`the "arrivals.json" file in the desk folder contains "` + ref + `"`, nil},
				{`the depot app does not show "` + other + `"`, nil},
			} {
				var err error
				if step.arg != nil {
					err = s.Step(step.text, step.arg)
				} else {
					err = s.Step(step.text)
				}
				if err != nil {
					errs[i] = fmt.Errorf("%s: %s: %w", ref, step.text, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// Each desktop's folder has its scenario's arrivals, and not the other's.
	for n := 1; n <= 2; n++ {
		home := filepath.Join(h.Dir, ".axx", "desktop", "linux", fmt.Sprintf("desktop-%d", n), "depot")
		var found string
		_ = filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
			if err == nil && info.Name() == "arrivals.json" {
				b, _ := os.ReadFile(p)
				found = string(b)
			}
			return nil
		})
		if strings.Contains(found, "PX-DSK-4301") == strings.Contains(found, "PX-DSK-4302") {
			t.Errorf("desktop %d's arrivals are one scenario's: %q", n, found)
		}
	}
}
