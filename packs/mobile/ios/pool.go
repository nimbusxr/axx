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
	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// device is a simulator of a pool: a clone axx made for the run, or one the
// registration names by its UDID. One scenario at a time has it.
type device struct {
	set       simSet // the set it is in
	udid      string
	name      string // the simulator's name
	source    string // the simulator it is a clone of
	version   string // its iOS version, like 18.1
	made      bool   // axx made it for the run, and deletes it at the end
	booted    bool   // axx booted it, and shuts it down at the end
	wdaPort   int    // WebDriverAgent's port on the Mac
	mjpegPort int    // WebDriverAgent's screen stream's port
	appium    *appium.Server
}

// pool is the simulators of one kind the run has: a clone for each worker,
// deleted when the run ends. For a device type, like iPhone 16, it clones
// axx's own simulator of it: made and booted once to set it up, then kept in
// axx's cache for the runs after, as no scenario ever runs on it. For a
// simulator the project set up, it clones that one. A UDID names a simulator
// the pool uses as it is. A scenario leases a simulator for its whole run,
// and gives it back at its end.
type pool struct {
	suite *core.Suite
	name  string // as the registration names it
	key   string // for screenshots: the device type and iOS version, or the simulator
	max   int
	boot  time.Duration

	// What the pool's simulators are: clones of a simulator in set (axx's
	// own of a device type and runtime, or the project's), or one simulator
	// to use as it is.
	set                      simSet
	typeID, runtime, version string
	template                 string // the name of axx's simulator of the device type
	source                   *simDevice
	own                      *simDevice
	prepare                  sync.Mutex // one worker at a time makes the template

	mu      sync.Mutex
	free    []*device
	all     []*device
	starts  int
	changed chan struct{}
	closed  bool
}

// madeRE names the simulators axx makes for a run: axx-<pid>-<n> <what>, so
// that a run that was killed leaves simulators the next run can tell are its
// own.
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
		sims, err := simulators(ctx, "")
		if err != nil {
			return nil, err
		}
		own, err := axxSet()
		if err != nil {
			return nil, err
		}
		sweep(ctx, s, own)
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
		p.set, p.template = axxSetOf(), t.Name+", "+rt.Name
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

// axxSetOf is where axx keeps its simulators: the simulators it sets up to
// clone, and the clones of a run. It is axx's own, apart from Xcode's.
func axxSetOf() simSet { return simSet(filepath.Join(npm.CacheDir("mobile"), "simulators")) }

func axxSet() (simSet, error) {
	set := axxSetOf()
	return set, os.MkdirAll(string(set), 0o755)
}

// sweep deletes the simulators a run that was killed made and left, in
// Xcode's set and in axx's: those named axx-<pid>-<n> whose axx is gone; and
// axx's simulators of iOS versions Xcode no longer has.
func sweep(ctx context.Context, s *core.Suite, own simSet) {
	_, _ = core.Cached(s, Name+"/sweep", func() (bool, error) {
		for _, set := range []simSet{"", own} {
			sims, err := simulators(ctx, set)
			if err != nil {
				continue
			}
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
				_, _ = set.simctl(ctx, "shutdown", d.UDID)
				_, _ = set.simctl(ctx, "delete", d.UDID)
			}
		}
		_, _ = own.simctl(ctx, "delete", "unavailable")
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

// start makes a simulator ready: cloned (or the one the registration names),
// booted, with WebDriverAgent installed and an Appium server of its own.
func (p *pool) start(ctx context.Context, logDir string) (*device, error) {
	d := &device{version: p.version, set: p.set}
	var err error
	if d.wdaPort, err = appium.FreePort(); err != nil {
		return nil, err
	}
	if d.mjpegPort, err = appium.FreePort(); err != nil {
		return nil, err
	}
	if p.own != nil {
		d.udid, d.name = p.own.UDID, p.own.Name
	} else if err := p.clone(ctx, d); err != nil {
		return nil, err
	}
	if err := p.bootDevice(ctx, d); err != nil {
		d.stop()
		return nil, err
	}
	wda, err := wdaFor(ctx, p.suite)
	if err == nil {
		_, err = d.set.simctl(ctx, "install", d.udid, wda)
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

// clone makes the worker's simulator: a clone of the project's simulator, or
// of axx's own of the device type. What is cloned has to be shut down.
func (p *pool) clone(ctx context.Context, d *device) error {
	src := p.source
	if src == nil {
		t, err := p.templateOf(ctx)
		if err != nil {
			return err
		}
		src = &t
	} else {
		sims, err := simulators(ctx, p.set)
		if err != nil {
			return err
		}
		if s, ok := findUDID(sims, src.UDID); ok && s.State != "Shutdown" {
			return fmt.Errorf("the %s simulator is %s: axx clones it for each worker, which needs it shut down (xcrun simctl shutdown %s)", s.Name, strings.ToLower(s.State), s.UDID)
		}
	}
	d.name, d.source = fmt.Sprintf("axx-%d-%d %s", os.Getpid(), made.Add(1), src.Name), src.Name
	out, err := p.set.simctl(ctx, "clone", src.UDID, d.name)
	if err != nil {
		return fmt.Errorf("cannot clone the %s simulator: %w", src.Name, err)
	}
	d.udid, d.made = strings.TrimSpace(out), true
	return nil
}

// templateOf is axx's simulator of the pool's device type and runtime, which
// the workers' simulators are clones of. The first time, axx makes it and
// boots it once: a new simulator's first boot sets it up, which takes over
// ten minutes on GitHub's macOS runners, and a clone of one set up boots in
// seconds. No scenario ever runs on it.
func (p *pool) templateOf(ctx context.Context) (simDevice, error) {
	p.prepare.Lock()
	defer p.prepare.Unlock()
	sims, err := simulators(ctx, p.set)
	if err != nil {
		return simDevice{}, err
	}
	for _, d := range sims {
		if d.Name == p.template && d.runtime == p.runtime {
			if d.State != "Shutdown" {
				_, _ = p.set.simctl(ctx, "shutdown", d.UDID)
			}
			return d, nil
		}
	}
	// Named for this axx while it is set up: a run killed meanwhile leaves
	// one the next run deletes.
	name := fmt.Sprintf("axx-%d-%d %s", os.Getpid(), made.Add(1), p.template)
	out, err := p.set.simctl(ctx, "create", name, p.typeID, p.runtime)
	if err != nil {
		return simDevice{}, fmt.Errorf("cannot make a %s simulator: %w", p.name, err)
	}
	udid := strings.TrimSpace(out)
	p.suite.Logger().Warn("setting up an iOS simulator to clone, once: its first boot takes minutes", "simulator", p.template, "udid", udid)
	started := time.Now()
	err = p.set.boot(ctx, udid, p.boot)
	_, _ = p.set.simctl(context.WithoutCancel(ctx), "shutdown", udid)
	if err == nil {
		_, err = p.set.simctl(ctx, "rename", udid, p.template)
	}
	if err != nil {
		_, _ = p.set.simctl(context.WithoutCancel(ctx), "delete", udid)
		return simDevice{}, fmt.Errorf("cannot set up the %s simulator axx clones (packs.%s.bootTimeout gives it longer): %w", p.template, Name, err)
	}
	p.suite.Logger().Info("set up an iOS simulator to clone", "simulator", p.template, "took", time.Since(started).Round(time.Second))
	return simDevice{Name: p.template, UDID: udid, State: "Shutdown", runtime: p.runtime}, nil
}

// bootDevice boots the simulator, unless it runs already.
func (p *pool) bootDevice(ctx context.Context, d *device) error {
	sims, err := simulators(ctx, d.set)
	if err != nil {
		return err
	}
	if s, ok := findUDID(sims, d.udid); ok && s.State == "Booted" {
		return nil
	}
	p.suite.Logger().Info("booting an iOS simulator", "simulator", d.name, "udid", d.udid)
	d.booted = true
	if err := d.set.boot(ctx, d.udid, p.boot); err != nil {
		return fmt.Errorf("the simulator %s did not boot (packs.%s.bootTimeout gives it longer): %w", d.name, Name, err)
	}
	return nil
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
		_, _ = d.set.simctl(ctx, "shutdown", d.udid)
	}
	if d.made {
		_, _ = d.set.simctl(ctx, "delete", d.udid)
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
