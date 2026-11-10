package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// relaunchProbe is a pack that runs some commands again elsewhere, as
// desktop-macos runs those that drive Mac apps under axx's launcher: it
// relaunches the commands in relaunches, with their exit codes, and fails
// to relaunch those in failing.
type relaunchProbe struct{}

var relaunchCalls struct {
	sync.Mutex
	relaunches map[string]int
	failing    []string
	seen       []string // each call's command and arguments
}

func (relaunchProbe) Manifest() core.Manifest { return core.Manifest{Name: "relaunch-probe"} }

func (relaunchProbe) Relaunch(_ context.Context, command string, args []string) (int, bool, error) {
	relaunchCalls.Lock()
	defer relaunchCalls.Unlock()
	relaunchCalls.seen = append(relaunchCalls.seen, command+": "+strings.Join(args, " "))
	if slices.Contains(relaunchCalls.failing, command) {
		return 0, false, errors.New("the probe was told it cannot")
	}
	code, ok := relaunchCalls.relaunches[command]
	return code, ok, nil
}

func init() {
	engine.Register("./relaunch-probe", relaunchProbe{})
}

// relaunchProject is a project that uses the probe, and the hook probe after
// it; the probe relaunches relaunches and fails failing.
func relaunchProject(t *testing.T, packs string, relaunches map[string]int, failing ...string) {
	t.Helper()
	agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": packs})
	relaunchCalls.Lock()
	relaunchCalls.relaunches, relaunchCalls.failing, relaunchCalls.seen = relaunches, failing, nil
	relaunchCalls.Unlock()
	t.Cleanup(func() {
		relaunchCalls.Lock()
		relaunchCalls.relaunches, relaunchCalls.failing, relaunchCalls.seen = nil, nil, nil
		relaunchCalls.Unlock()
	})
	takeHookEvents()
}

func relaunchesSeen() []string {
	relaunchCalls.Lock()
	defer relaunchCalls.Unlock()
	return slices.Clone(relaunchCalls.seen)
}

// A command a pack relaunches runs there, not here, before its hooks, and
// axx ends with the code it ended with.
func TestACommandAPackRelaunchesEndsWithItsCode(t *testing.T) {
	relaunchProject(t, "packs:\n  - ./relaunch-probe\n  - ./hook-probe\n", map[string]int{"parcels label": 3})
	out, errOut, code := run(t, "parcels", "label", "PX-1042", "-f", "pdf")
	if code != 3 || out != "" || errOut != "" {
		t.Fatalf("exit %d, out %q, err %q: want 3 and nothing written here", code, out, errOut)
	}
	if seen := relaunchesSeen(); !slices.Equal(seen, []string{"parcels label: parcels label PX-1042 -f pdf"}) {
		t.Errorf("the probe was asked %q", seen)
	}
	if lines, _ := takeHookEvents(); len(lines) != 0 {
		t.Errorf("the hooks ran here: %q", lines)
	}
}

// A command no pack relaunches runs here, with its hooks.
func TestACommandNoPackRelaunchesRunsHere(t *testing.T) {
	relaunchProject(t, "packs:\n  - ./relaunch-probe\n  - ./hook-probe\n", map[string]int{"doctor": 0})
	out, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.OK) || !strings.Contains(out, "PX-1042") {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	if lines, _ := takeHookEvents(); len(lines) != 2 {
		t.Errorf("the hooks ran %q, want before and after", lines)
	}
}

// A pack that cannot relaunch a command it must says so, with a code of its own.
func TestAPackThatCannotRelaunchACommandStopsIt(t *testing.T) {
	relaunchProject(t, "packs:\n  - ./relaunch-probe\n  - ./hook-probe\n", nil, "parcels label")
	_, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Environment) || !strings.Contains(errOut, CodeRelaunch) || !strings.Contains(errOut, "relaunch-probe") ||
		!strings.Contains(errOut, "the probe was told it cannot") {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if lines, _ := takeHookEvents(); len(lines) != 0 {
		t.Errorf("the hooks ran: %q", lines)
	}
}

// Only the packs a project uses relaunch its commands.
func TestOnlyTheProjectsPacksRelaunch(t *testing.T) {
	relaunchProject(t, "packs:\n  - ./hook-probe\n", map[string]int{"parcels label": 3})
	if _, errOut, code := run(t, "parcels", "label", "PX-1042"); code != int(exitcode.OK) {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if seen := relaunchesSeen(); len(seen) != 0 {
		t.Errorf("a pack the project does not use was asked: %q", seen)
	}
}

// axx's own commands that manage it and its packs are never relaunched.
func TestTheCommandsWithoutPacksAreNotRelaunched(t *testing.T) {
	relaunchProject(t, "packs:\n  - ./relaunch-probe\n", map[string]int{"version": 3})
	if _, errOut, code := run(t, "version"); code != int(exitcode.OK) {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if seen := relaunchesSeen(); len(seen) != 0 {
		t.Errorf("the probe was asked: %q", seen)
	}
}
