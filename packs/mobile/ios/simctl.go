package mobileios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// simctl runs xcrun simctl, Xcode's tool for simulators, and returns what it
// printed. A command may take up to 5 minutes.
func simctl(ctx context.Context, args ...string) (string, error) {
	return simctlWithin(ctx, 5*time.Minute, args...)
}

// simctlWithin runs xcrun simctl for a command that may take up to within,
// like waiting for a simulator to boot.
func simctlWithin(ctx context.Context, within time.Duration, args ...string) (string, error) {
	return simctlEnv(ctx, within, nil, args...)
}

// simctlEnv runs xcrun simctl with variables added to its environment: simctl passes each
// SIMCTL_CHILD_ one on to what it starts on the simulator.
func simctlEnv(ctx context.Context, within time.Duration, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	cmd := exec.CommandContext(ctx, "xcrun", append([]string{"simctl"}, args...)...) //nolint:gosec // simctl, with arguments axx builds
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("xcrun is not found: iOS simulators need Xcode (and macOS)")
		}
		return text, fmt.Errorf("simctl %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

type simDevice struct {
	Name       string `json:"name"`
	UDID       string `json:"udid"`
	State      string `json:"state"`
	DeviceType string `json:"deviceTypeIdentifier"`
	DataPath   string `json:"dataPath"` // its own data folder
	runtime    string // its runtime's identifier
}

type simRuntime struct {
	Identifier  string `json:"identifier"`
	Name        string `json:"name"` // iOS 18.1
	Version     string `json:"version"`
	Platform    string `json:"platform"` // iOS
	IsAvailable bool   `json:"isAvailable"`
	// SupportedDeviceTypes are the device types it runs (none listed: an
	// older Xcode's, taken to run every one).
	SupportedDeviceTypes []simDeviceType `json:"supportedDeviceTypes"`
}

// supports reports whether the runtime runs simulators of the device type.
func (r simRuntime) supports(typeID string) bool {
	if len(r.SupportedDeviceTypes) == 0 {
		return true
	}
	for _, t := range r.SupportedDeviceTypes {
		if t.Identifier == typeID {
			return true
		}
	}
	return false
}

type simDeviceType struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"` // iPhone 16
}

// simSet is a set of simulators: Xcode's own (""), or axx's, in its cache.
type simSet string

// simctl runs simctl on the set's simulators.
func (set simSet) simctl(ctx context.Context, args ...string) (string, error) {
	return simctl(ctx, set.args(args...)...)
}

// args are simctl's arguments for the set's simulators.
func (set simSet) args(args ...string) []string {
	if set == "" {
		return args
	}
	return append([]string{"--set", string(set)}, args...)
}

// simulators lists the set's simulators, available or not.
func simulators(ctx context.Context, set simSet) ([]simDevice, error) {
	out, err := set.simctl(ctx, "list", "devices", "-j")
	if err != nil {
		return nil, err
	}
	var l struct {
		Devices map[string][]simDevice `json:"devices"`
	}
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		return nil, fmt.Errorf("simctl's list of simulators: %w", err)
	}
	var all []simDevice
	for rt, ds := range l.Devices {
		for _, d := range ds {
			d.runtime = rt
			all = append(all, d)
		}
	}
	return all, nil
}

var deviceSpecRE = regexp.MustCompile(`^(.+?)(?:\s*,\s*(iOS\s+[0-9.]+))?$`)

// splitDevice splits a registration's device, like "iPhone 16, iOS 18.1",
// into a name and an iOS version ("" for any).
func splitDevice(device string) (name, ios string) {
	m := deviceSpecRE.FindStringSubmatch(strings.TrimSpace(device))
	if m == nil {
		return strings.TrimSpace(device), ""
	}
	return m[1], strings.Join(strings.Fields(m[2]), " ")
}

// deviceTypes are the simulators' device types Xcode has, like iPhone 16.
func deviceTypes(ctx context.Context) ([]simDeviceType, error) {
	out, err := simctl(ctx, "list", "devicetypes", "-j")
	if err != nil {
		return nil, err
	}
	var l struct {
		DeviceTypes []simDeviceType `json:"devicetypes"`
	}
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		return nil, fmt.Errorf("simctl's list of device types: %w", err)
	}
	return l.DeviceTypes, nil
}

// iosRuntimes are the iOS versions Xcode has for simulators.
func iosRuntimes(ctx context.Context) ([]simRuntime, error) {
	out, err := simctl(ctx, "list", "runtimes", "-j")
	if err != nil {
		return nil, err
	}
	var l struct {
		Runtimes []simRuntime `json:"runtimes"`
	}
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		return nil, fmt.Errorf("simctl's list of runtimes: %w", err)
	}
	var ios []simRuntime
	for _, r := range l.Runtimes {
		if r.IsAvailable && (r.Platform == "iOS" || r.Platform == "" && strings.HasPrefix(r.Name, "iOS ")) {
			ios = append(ios, r)
		}
	}
	return ios, nil
}

// pickRuntime is the runtime named ios (like iOS 18.1), or the newest, of
// those that run the device type: Xcode's newest may not run an older iPhone.
func pickRuntime(rts []simRuntime, ios, typeID string) (simRuntime, bool) {
	var best simRuntime
	for _, r := range rts {
		switch {
		case !r.supports(typeID):
		case ios != "" && strings.EqualFold(r.Name, ios):
			return r, true
		case ios == "" && (best.Identifier == "" || newer(r.Version, best.Version)):
			best = r
		}
	}
	return best, best.Identifier != ""
}

func runtimeNames(rts []simRuntime) string {
	var names []string
	for _, r := range rts {
		names = append(names, r.Name)
	}
	if len(names) == 0 {
		return "none (Xcode's Settings, Components, installs one)"
	}
	return strings.Join(names, ", ")
}

// newer reports whether version a (like 18.1) is after b.
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			_, _ = fmt.Sscan(pa[i], &x)
		}
		if i < len(pb) {
			_, _ = fmt.Sscan(pb[i], &y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// boot boots a simulator of the set and waits, up to within, until it has
// finished booting. Libraries are set afterwards by loadSpringboard so apps
// can supply their own initial library lists.
func (set simSet) boot(ctx context.Context, udid string, within time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	if _, err := set.simctl(ctx, "boot", udid); err != nil && !strings.Contains(err.Error(), "current state: Booted") {
		return err
	}
	if _, err := simctlWithin(ctx, within, set.args("bootstatus", udid, "-b")...); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("it did not finish booting within %s", within)
		}
		return err
	}
	return nil
}

// notificationsServed waits, up to within, until SpringBoard serves apps'
// requests for notifications, and reports whether it does. It starts a while
// after the simulator says it has booted: seconds, or over a minute on a busy
// Mac. Until then an app's request goes unanswered, so the dialog a scenario
// waits for never shows. It reads the simulator's log from since on (a clone
// has its template's log too): the notification server adding its content
// protection observer, as it starts. A log that cannot read SpringBoard's own
// strings (an iOS 27 simulator's on GitHub's runner, every message of
// SpringBoard's "<compose failure [shared UUID]>") still has the entry, the
// only one of its category that SpringBoard logs as it starts.
func (set simSet) notificationsServed(ctx context.Context, udid string, since time.Time, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	predicate := `process == "SpringBoard" AND subsystem == "com.apple.UserNotifications" AND category == "ContentProtection"`
	args := []string{"spawn", udid, "log", "show", "--start", since.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate", predicate}
	for {
		if out, _ := set.simctl(ctx, args...); strings.Contains(out, "UNSUserNotificationServer") ||
			strings.Contains(out, "[com.apple.UserNotifications:ContentProtection] <compose failure") {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
}

// notificationsUnserved tells why notificationsServed found nothing from since on, for the
// warning that the simulator does not serve notifications: what the same query answers now, the
// message it looks for in the simulator's last 15 minutes, with when each was logged, and the
// simulator's clock.
func (set simSet) notificationsUnserved(ctx context.Context, udid string, since time.Time) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	predicate := `process == "SpringBoard" AND subsystem == "com.apple.UserNotifications" AND category == "ContentProtection"`
	out, err := set.simctl(ctx, "spawn", udid, "log", "show", "--start", since.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate", predicate)
	why := fmt.Sprintf("the query from %s answers %d lines %q", since.Format("15:04:05"), strings.Count(out, "\n"), loggedMessages(out, 3))
	if err != nil {
		why += fmt.Sprintf(" (%v)", err)
	}
	// What SpringBoard's notification server logged as it started, in its other categories.
	started, _ := set.simctl(ctx, "spawn", udid, "log", "show", "--start", since.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate",
		`process == "SpringBoard" AND subsystem == "com.apple.UserNotifications" AND category IN {"Connections", "DataProviderFactory"}`)
	why += fmt.Sprintf("; it started: %q", loggedMessages(started, 3))
	wide, err := set.simctl(ctx, "spawn", udid, "log", "show", "--last", "15m", "--style", "compact", "--predicate", predicate)
	var found []string
	for _, line := range strings.Split(wide, "\n") {
		if f := strings.Fields(line); len(f) > 1 && strings.Contains(line, "UNSUserNotificationServer") {
			found = append(found, f[0]+" "+f[1])
		}
	}
	why += fmt.Sprintf("; the last 15 minutes have it %d times %v", len(found), found)
	if err != nil {
		why += fmt.Sprintf(" (%v)", err)
	}
	// The simulator's clock, as its log has it: when SpringBoard logged last.
	if last, err := set.simctl(ctx, "spawn", udid, "log", "show", "--last", "1m", "--style", "compact", "--predicate", `process == "SpringBoard"`); err == nil {
		lines := strings.Split(strings.TrimSpace(last), "\n")
		if f := strings.Fields(lines[len(lines)-1]); len(f) > 1 {
			why += fmt.Sprintf("; SpringBoard logged last at %s %s, the Mac's clock reads %s", f[0], f[1], time.Now().Format("2006-01-02 15:04:05"))
		}
	}
	return why
}

// loggedMessages are the first n messages of a compact log, without their timestamps and
// processes.
func loggedMessages(log string, n int) []string {
	var msgs []string
	for _, line := range strings.Split(log, "\n") {
		if _, msg, ok := strings.Cut(line, "] "); ok && len(msgs) < n {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// isUDID reports a simulator's or device's identifier.
func isUDID(s string) bool {
	return regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$|^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{16}$`).MatchString(s)
}

// findUDID is the simulator with that identifier, if Xcode has one.
func findUDID(sims []simDevice, udid string) (simDevice, bool) {
	i := slices.IndexFunc(sims, func(d simDevice) bool { return strings.EqualFold(d.UDID, udid) })
	if i < 0 {
		return simDevice{}, false
	}
	return sims[i], true
}
