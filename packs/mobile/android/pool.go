package mobileandroid

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// device is a device of a pool: an emulator axx started, or a device adb
// lists. One scenario at a time has it.
type device struct {
	serial     string
	port       int    // the console port of an emulator axx started, which it holds
	avd        string // the emulator's AVD; "" for a device axx did not start
	systemPort int    // the UiAutomator2 server's port on the host
	appium     *appium.Server
	emulator   *exec.Cmd
	group      *proc.Group
	exited     chan struct{}
	installed  map[string]bool // the APKs installed on it this run
}

// pool is the devices of one kind the run has: emulators of one AVD, which
// it starts as scenarios need them (up to max at once), or one device adb
// lists. A scenario leases a device for its whole run, and gives it back at
// its end.
type pool struct {
	suite *core.Suite
	sdk   *sdk
	name  string // the AVD or the device's serial
	avd   bool   // name is an AVD: the pool starts emulators of it
	max   int
	boot  time.Duration
	args  []string // more arguments for the emulators
	// window shows the emulators' windows: a person watches the run.
	window bool

	mu      sync.Mutex
	free    []*device
	all     []*device
	starts  int // emulators being started
	changed chan struct{}
	closed  bool
}

// poolFor is the run's pool of devices named name.
func poolFor(sc *core.Scenario, name string) (*pool, error) {
	s := sc.Suite()
	return core.Cached(s, Name+"/pool/"+name, func() (*pool, error) {
		cfg, err := settingsFor(s)
		if err != nil {
			return nil, err
		}
		tools, err := findSDK()
		if err != nil {
			return nil, err
		}
		watching, _ := s.Watching()
		p := &pool{suite: s, sdk: tools, name: name, max: cfg.devices, boot: cfg.bootTimeout, args: cfg.emulatorArgs, window: watching, changed: make(chan struct{})}
		ctx := sc.Context()
		serials, err := tools.devices(ctx)
		if err != nil {
			return nil, err
		}
		if slices.Contains(serials, name) {
			// A device already there: the pool is that device.
			p.max = 1
		} else {
			avds, err := tools.avds(ctx)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(avds, name) {
				return nil, fmt.Errorf("no Android device or emulator is named %q: the emulator's devices (AVDs, in %s) are %v, and adb lists %v", name, avdHome(), avds, serials)
			}
			p.avd = true
		}
		s.OnClose(func(context.Context) error { return p.close() })
		return p, nil
	})
}

// avdHome is where the emulator finds its devices.
func avdHome() string {
	if d := os.Getenv("ANDROID_AVD_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("ANDROID_EMULATOR_HOME"); d != "" {
		return filepath.Join(d, "avd")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".android", "avd")
}

// lease gives the scenario a device of its own, starting an emulator when
// none is free and the pool may have one more; otherwise it waits for one.
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
			return nil, fmt.Errorf("no %s device was free: %w", p.name, ctx.Err())
		case <-wait:
		}
	}
}

// release gives a device back to the pool.
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

// start makes a device ready: boots an emulator of the pool's AVD (or takes
// the device adb lists), and starts its Appium server.
func (p *pool) start(ctx context.Context, logDir string) (*device, error) {
	systemPort, err := appium.FreePort()
	if err != nil {
		return nil, err
	}
	d := &device{serial: p.name, systemPort: systemPort, installed: map[string]bool{}}
	if p.avd {
		if err := p.boot1(ctx, d, logDir); err != nil {
			return nil, err
		}
	}
	// Animations off: a screen settles at once, and screenshots compare.
	for _, key := range []string{"window_animation_scale", "transition_animation_scale", "animator_duration_scale"} {
		if _, err := p.sdk.shell(ctx, d.serial, "settings", "put", "global", key, "0"); err != nil {
			d.stop(p.sdk)
			return nil, err
		}
	}
	install, err := installFor(ctx, p.suite)
	if err != nil {
		d.stop(p.sdk)
		return nil, err
	}
	d.appium, err = install.Start(ctx, filepath.Join(logDir, "appium-"+d.serial+".log"))
	if err != nil {
		d.stop(p.sdk)
		return nil, err
	}
	return d, nil
}

// boot1 starts an emulator of the pool's AVD, read-only so that several run
// at once and what a scenario changes is gone when it stops.
func (p *pool) boot1(ctx context.Context, d *device, logDir string) error {
	port, err := consolePort()
	if err != nil {
		return err
	}
	d.serial, d.avd, d.port = "emulator-"+strconv.Itoa(port), p.name, port
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		releasePort(port)
		return err
	}
	log, err := os.Create(filepath.Join(logDir, "emulator-"+strconv.Itoa(port)+".log"))
	if err != nil {
		releasePort(port)
		return err
	}
	// The emulator outlives this step: it runs until the run ends. It shows
	// its window only while a person watches the run.
	args := []string{"-avd", p.name, "-port", strconv.Itoa(port), "-read-only", "-no-snapshot-save", "-no-audio", "-no-boot-anim"}
	if !p.window {
		args = append(args, "-no-window")
	}
	args = append(args, p.args...)
	if runtime.GOOS == "linux" && !slices.Contains(p.args, "-gpu") {
		// Linux machines that run tests rarely have a GPU the emulator can use.
		args = append(args, "-gpu", "swiftshader_indirect")
	}
	cmd := exec.CommandContext(context.Background(), p.sdk.emulator, args...) //nolint:gosec // the SDK's emulator
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = time.Second
	proc.Setup(cmd)
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		releasePort(port)
		return fmt.Errorf("cannot start the %s emulator: %w", p.name, err)
	}
	d.emulator, d.exited = cmd, make(chan struct{})
	if g, err := proc.NewGroup(cmd); err == nil {
		d.group = g
	}
	go func() {
		_ = cmd.Wait()
		_ = log.Close()
		releasePort(port)
		close(d.exited)
	}()
	p.suite.Logger().Info("starting an Android emulator", "avd", p.name, "serial", d.serial)
	booted := make(chan error, 1)
	go func() { booted <- p.sdk.waitBooted(ctx, d.serial, p.boot) }()
	select {
	case err := <-booted:
		if err != nil {
			d.stop(p.sdk)
			return fmt.Errorf("%w; its log: %s", err, log.Name())
		}
	case <-d.exited:
		return fmt.Errorf("the %s emulator stopped as it started; its log: %s", p.name, log.Name())
	}
	// The system says nothing over the app of another app that hangs or
	// crashes, like its Messages on a slow machine ("isn't responding").
	_, _ = p.sdk.shell(ctx, d.serial, "settings", "put", "global", "hide_error_dialogs", "1")
	skipBrowserWelcome(ctx, p.sdk, d.serial)
	return nil
}

// skipBrowserWelcome has Chrome open a page at once, without its first-run
// screens: an emulator starts afresh at each run, and an app that opens a
// browser tab (to sign in, say) means the page, not Chrome's welcome. On an
// emulator without Chrome it does nothing.
func skipBrowserWelcome(ctx context.Context, s *sdk, serial string) {
	_, _ = s.shell(ctx, serial, "am", "set-debug-app", "--persistent", "com.android.chrome")
	_, _ = s.shell(ctx, serial, "sh", "-c", "echo '_ --disable-fre --no-default-browser-check --no-first-run' > /data/local/tmp/chrome-command-line")
}

// consolePorts are the console ports the run's emulators hold: emulators
// that start at once each take one of their own, before any has bound it.
var consolePorts = struct {
	sync.Mutex
	taken map[int]bool
}{taken: map[int]bool{}}

// consolePort takes a free even port for an emulator's console; adb uses the
// odd one after it. releasePort gives it back.
func consolePort() (int, error) {
	consolePorts.Lock()
	defer consolePorts.Unlock()
	for port := 5554; port <= 5680; port += 2 {
		if !consolePorts.taken[port] && free(port) && free(port+1) {
			consolePorts.taken[port] = true
			return port, nil
		}
	}
	return 0, errors.New("no emulator console port from 5554 to 5680 is free")
}

func releasePort(port int) {
	consolePorts.Lock()
	defer consolePorts.Unlock()
	delete(consolePorts.taken, port)
}

func free(port int) bool {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// stop stops the device's Appium server and, for an emulator axx started,
// the emulator.
func (d *device) stop(tools *sdk) {
	if d.appium != nil {
		d.appium.Stop()
	}
	if d.emulator == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = tools.run(ctx, d.serial, "emu", "kill")
	select {
	case <-d.exited:
	case <-time.After(10 * time.Second):
	}
	if d.group != nil {
		_ = d.group.KillAndWait(5 * time.Second)
		d.group.Release()
	}
	<-d.exited
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
			d.stop(p.sdk)
		}()
	}
	wg.Wait()
	return nil
}
