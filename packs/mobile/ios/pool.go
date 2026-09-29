package mobileios

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// device is a simulator of a pool: one axx made for the run, or one the
// registration names by its UDID. One scenario at a time has it.
type device struct {
	udid      string
	name      string // the simulator's name
	version   string // its iOS version, like 18.1
	made      bool   // axx made it for the run, and deletes it at the end
	booted    bool   // axx booted it, and shuts it down at the end
	wdaPort   int    // WebDriverAgent's port on the Mac
	mjpegPort int    // WebDriverAgent's screen stream's port
	appium    *appium.Server
}

// pool is the simulators of one kind the run has. For a device type, like
// iPhone 16, it makes a new simulator of it for each worker; for a simulator
// the project set up, it clones it; each is deleted when the run ends. A
// UDID names a simulator the pool uses as it is. A scenario leases a
// simulator for its whole run, and gives it back at its end.
type pool struct {
	suite *core.Suite
	name  string // as the registration names it
	key   string // for screenshots: the device type and iOS version, or the simulator
	max   int
	boot  time.Duration

	// What the pool's simulators are: a device type and runtime to make new
	// ones of, a simulator to clone, or one simulator to use as it is.
	typeID, runtime, version string
	source                   *simDevice
	own                      *simDevice

	mu      sync.Mutex
	free    []*device
	all     []*device
	starts  int
	changed chan struct{}
	closed  bool
}

// madePrefix names the simulators axx makes: axx-<pid>-<n> <what>, so that
// a run that was killed leaves simulators the next run can tell are its own.
var (
	madeRE = regexp.MustCompile(`^axx-(\d+)-\d+ `)
	made   atomic.Int64
)

// poolFor is the run's pool of the simulators device names.
func poolFor(sc *core.Scenario, device string) (*pool, error) {
	s := sc.Suite()
	return core.Cached(s, Name+"/pool/"+device, func() (*pool, error) {
		cfg, err := settingsFor(s)
		if err != nil {
			return nil, err
		}
		ctx := sc.Context()
		sims, err := simulators(ctx)
		if err != nil {
			return nil, err
		}
		sweep(ctx, s, sims)
		p := &pool{suite: s, name: device, max: cfg.devices, boot: cfg.bootTimeout, changed: make(chan struct{})}
		if err := p.resolve(ctx, sims); err != nil {
			return nil, err
		}
		s.OnClose(func(context.Context) error { return p.close() })
		return p, nil
	})
}

// resolve finds what the pool's simulators are: a UDID's simulator, a
// device type, or a simulator of that name.
func (p *pool) resolve(ctx context.Context, sims []simDevice) error {
	if isUDID(p.name) {
		d, ok := findUDID(sims, p.name)
		if !ok {
			return fmt.Errorf("no simulator of Xcode's is %s (xcrun simctl list devices lists them)", p.name)
		}
		p.own, p.max, p.key = &d, 1, d.Name
		p.version = versionOf(d.runtime)
		return nil
	}
	name, ios := splitDevice(p.name)
	rts, err := iosRuntimes(ctx)
	if err != nil {
		return err
	}
	types, err := deviceTypes(ctx)
	if err != nil {
		return err
	}
	for _, t := range types {
		if !strings.EqualFold(t.Name, name) {
			continue
		}
		rt, ok := pickRuntime(rts, ios)
		if !ok {
			return fmt.Errorf("no %s runtime for simulators is in Xcode; it has: %s", cmp.Or(ios, "iOS"), runtimeNames(rts))
		}
		p.typeID, p.runtime, p.version = t.Identifier, rt.Identifier, rt.Version
		p.key = t.Name + "-" + rt.Name
		return nil
	}
	// A simulator the project set up: the pool clones it.
	var found []simDevice
	for _, d := range sims {
		if d.Name == name && !madeRE.MatchString(d.Name) && (ios == "" || strings.EqualFold(runtimeName(d.runtime), ios)) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		var names []string
		for _, t := range types {
			names = append(names, t.Name)
		}
		return fmt.Errorf("%q is no simulator and no device type of Xcode's: name a device type, like iPhone 16 or iPhone 16, iOS 18.1 (Xcode has %s), a simulator's name, or its UDID", p.name, strings.Join(names, ", "))
	case 1:
		p.source, p.version, p.key = &found[0], versionOf(found[0].runtime), found[0].Name
		return nil
	}
	return fmt.Errorf("%d simulators of Xcode's are named %q: name its iOS version too, like %s, iOS %s, or its UDID", len(found), name, name, versionOf(found[0].runtime))
}

// sweep deletes the simulators a run that was killed made and left: those
// named axx-<pid>-<n> whose axx is gone.
func sweep(ctx context.Context, s *core.Suite, sims []simDevice) {
	_, _ = core.Cached(s, Name+"/sweep", func() (bool, error) {
		for _, d := range sims {
			m := madeRE.FindStringSubmatch(d.Name)
			if m == nil {
				continue
			}
			pid, _ := strconv.Atoi(m[1])
			if pid == os.Getpid() || proc.ProcessAlive(pid) {
				continue
			}
			s.Logger().Info("deleting a simulator a run that ended early left", "simulator", d.Name, "udid", d.UDID)
			_, _ = simctl(ctx, "shutdown", d.UDID)
			_, _ = simctl(ctx, "delete", d.UDID)
		}
		return true, nil
	})
}

// lease gives the scenario a simulator of its own, making one when none is
// free and the pool may have one more; otherwise it waits for one.
func (p *pool) lease(ctx context.Context, logDir string) (*device, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, errors.New("the run is ending")
		}
		if n := len(p.free); n > 0 {
			d := p.free[n-1]
			p.free = p.free[:n-1]
			p.mu.Unlock()
			return d, nil
		}
		if len(p.all)+p.starts < p.max {
			p.starts++
			p.mu.Unlock()
			d, err := p.start(ctx, logDir)
			p.mu.Lock()
			p.starts--
			if err == nil {
				p.all = append(p.all, d)
			}
			p.signal()
			p.mu.Unlock()
			return d, err
		}
		wait := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("no %s simulator was free: %w", p.name, ctx.Err())
		case <-wait:
		}
	}
}

// release gives a simulator back to the pool.
func (p *pool) release(d *device) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.free = append(p.free, d)
	p.signal()
}

// signal wakes the leases that wait. The caller holds p.mu.
func (p *pool) signal() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// start makes a simulator ready: made (or the one the registration names),
// booted, with WebDriverAgent installed and an Appium server of its own.
func (p *pool) start(ctx context.Context, logDir string) (*device, error) {
	d := &device{version: p.version}
	var err error
	if d.wdaPort, err = appium.FreePort(); err != nil {
		return nil, err
	}
	if d.mjpegPort, err = appium.FreePort(); err != nil {
		return nil, err
	}
	switch {
	case p.own != nil:
		d.udid, d.name = p.own.UDID, p.own.Name
	case p.source != nil:
		if err := p.clone(ctx, d); err != nil {
			return nil, err
		}
	default:
		d.name = fmt.Sprintf("axx-%d-%d %s", os.Getpid(), made.Add(1), p.key)
		out, err := simctl(ctx, "create", d.name, p.typeID, p.runtime)
		if err != nil {
			return nil, fmt.Errorf("cannot make a %s simulator: %w", p.name, err)
		}
		d.udid, d.made = strings.TrimSpace(out), true
	}
	if err := p.bootDevice(ctx, d); err != nil {
		d.stop()
		return nil, err
	}
	wda, err := wdaFor(ctx, p.suite)
	if err == nil {
		_, err = simctl(ctx, "install", d.udid, wda)
	}
	if err != nil {
		d.stop()
		return nil, fmt.Errorf("cannot install WebDriverAgent on the %s simulator: %w", p.name, err)
	}
	install, err := installFor(ctx, p.suite)
	if err == nil {
		d.appium, err = install.Start(ctx, filepath.Join(logDir, "appium-"+d.udid+".log"))
	}
	if err != nil {
		d.stop()
		return nil, err
	}
	return d, nil
}

// clone copies the simulator the project set up; it has to be shut down.
func (p *pool) clone(ctx context.Context, d *device) error {
	sims, err := simulators(ctx)
	if err != nil {
		return err
	}
	if src, ok := findUDID(sims, p.source.UDID); ok && src.State != "Shutdown" {
		return fmt.Errorf("the %s simulator is %s: axx clones it for each worker, which needs it shut down (xcrun simctl shutdown %s)", src.Name, strings.ToLower(src.State), src.UDID)
	}
	d.name = fmt.Sprintf("axx-%d-%d %s", os.Getpid(), made.Add(1), p.source.Name)
	out, err := simctl(ctx, "clone", p.source.UDID, d.name)
	if err != nil {
		return fmt.Errorf("cannot clone the %s simulator: %w", p.source.Name, err)
	}
	d.udid, d.made = strings.TrimSpace(out), true
	return nil
}

// bootDevice boots the simulator, unless it runs already.
func (p *pool) bootDevice(ctx context.Context, d *device) error {
	sims, err := simulators(ctx)
	if err != nil {
		return err
	}
	if s, ok := findUDID(sims, d.udid); ok && s.State == "Booted" {
		return nil
	}
	p.suite.Logger().Info("booting an iOS simulator", "simulator", d.name, "udid", d.udid)
	d.booted = true
	return bootSim(ctx, d.udid, p.boot)
}

// stop stops the simulator's Appium server, shuts down a simulator axx
// booted and deletes one it made.
func (d *device) stop() {
	if d.appium != nil {
		d.appium.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if d.booted || d.made {
		_, _ = simctl(ctx, "shutdown", d.udid)
	}
	if d.made {
		_, _ = simctl(ctx, "delete", d.udid)
	}
}

func (p *pool) close() error {
	p.mu.Lock()
	p.closed = true
	all := p.all
	p.all, p.free = nil, nil
	p.signal()
	p.mu.Unlock()
	var wg sync.WaitGroup
	for _, d := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.stop()
		}()
	}
	wg.Wait()
	return nil
}

// versionOf is a runtime identifier's iOS version:
// com.apple.CoreSimulator.SimRuntime.iOS-18-1 is 18.1.
func versionOf(runtime string) string {
	_, v, ok := strings.Cut(runtime, ".SimRuntime.iOS-")
	if !ok {
		return ""
	}
	return strings.ReplaceAll(v, "-", ".")
}

// runtimeName is a runtime identifier's name, like iOS 18.1.
func runtimeName(runtime string) string { return "iOS " + versionOf(runtime) }
