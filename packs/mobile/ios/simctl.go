package mobileios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// simctl runs xcrun simctl, Xcode's tool for simulators, and returns what it
// printed.
func simctl(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", append([]string{"simctl"}, args...)...).CombinedOutput() //nolint:gosec // simctl, with arguments axx builds
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
	runtime    string // its runtime's identifier
}

type simRuntime struct {
	Identifier  string `json:"identifier"`
	Name        string `json:"name"` // iOS 18.1
	Version     string `json:"version"`
	Platform    string `json:"platform"` // iOS
	IsAvailable bool   `json:"isAvailable"`
}

type simDeviceType struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"` // iPhone 16
}

// simSet is a set of simulators: Xcode's own (""), or axx's, in its cache.
type simSet string

// simctl runs simctl on the set's simulators.
func (set simSet) simctl(ctx context.Context, args ...string) (string, error) {
	if set != "" {
		args = append([]string{"--set", string(set)}, args...)
	}
	return simctl(ctx, args...)
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

// pickRuntime is the runtime named ios (like iOS 18.1), or the newest.
func pickRuntime(rts []simRuntime, ios string) (simRuntime, bool) {
	var best simRuntime
	for _, r := range rts {
		switch {
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
// finished booting.
func (set simSet) boot(ctx context.Context, udid string, within time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	if _, err := set.simctl(ctx, "boot", udid); err != nil && !strings.Contains(err.Error(), "current state: Booted") {
		return err
	}
	if _, err := set.simctl(ctx, "bootstatus", udid, "-b"); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("it did not finish booting within %s", within)
		}
		return err
	}
	return nil
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
