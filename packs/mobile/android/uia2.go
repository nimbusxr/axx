package mobileandroid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// The UiAutomator2 server drives the apps on a device: Appium's, the two APKs
// of a release pinned with their sums, downloaded once into axx's cache and
// installed once on each device. axx starts it as an instrumentation, talks
// to it over a forwarded port, and keeps it running for as long as the device
// is in its pool: a scenario's session is a request, not a start.
const uia2Version = "10.6.6"

var uia2APKs = []struct{ name, sum string }{
	{"appium-uiautomator2-server-v" + uia2Version + ".apk", "8ff760a2a86b487f53090fbdcd5b0360e67d02bb811887d527a9557b0d59c80d"},
	{"appium-uiautomator2-server-debug-androidTest.apk", "e6f729287d72351388fd66597c35b69988b9091a7011954bf92a55d416cb1e1c"},
}

const (
	uia2Package = "io.appium.uiautomator2.server"
	uia2Test    = uia2Package + ".test"
	uia2Runner  = uia2Test + "/androidx.test.runner.AndroidJUnitRunner"
	// The server's ports on the device: its commands', its screen stream's.
	uia2Port      = 6790
	uia2MJPEGPort = 7810
	// uia2Start is how long the server has to answer once started.
	uia2Start = 60 * time.Second
)

// uia2For is the server's two APKs, downloaded once and checked.
func uia2For(ctx context.Context, s *core.Suite) ([]string, error) {
	return core.Cached(s, Name+"/uiautomator2", func() ([]string, error) {
		dir := filepath.Join(npm.CacheDir("mobile"), "uiautomator2-server-"+uia2Version)
		var paths []string
		for _, a := range uia2APKs {
			path := filepath.Join(dir, a.name)
			paths = append(paths, path)
			if b, err := os.ReadFile(path); err == nil && sumOf(b) == a.sum {
				continue
			}
			s.Logger().Warn("downloading the UiAutomator2 server, once", "apk", a.name)
			body, err := npm.Fetch(ctx, "https://github.com/appium/appium-uiautomator2-server/releases/download/v"+uia2Version+"/"+a.name)
			if err != nil {
				return nil, fmt.Errorf("cannot download the UiAutomator2 server: %w", err)
			}
			if got := sumOf(body); got != a.sum {
				return nil, fmt.Errorf("%s: SHA-256 %s does not match the pinned %s", a.name, got, a.sum)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
			tmp := path + ".part"
			if err := os.WriteFile(tmp, body, 0o644); err != nil {
				return nil, err
			}
			if err := os.Rename(tmp, path); err != nil {
				return nil, err
			}
		}
		return paths, nil
	})
}

func sumOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// uia2Server is the server on one device: the instrumentation that runs it,
// and a client of it on the forwarded port.
type uia2Server struct {
	client *appium.Client
	cmd    *exec.Cmd
	group  *proc.Group
	exited chan struct{}
	log    *os.File
	// stopOn stops the instrumentation on the device: ending adb's shell
	// does not.
	stopOn func()
	// stopping is whether axx ends the server, rather than its crashing.
	stopping atomic.Bool
}

// ensureServer starts the device's server, or starts it again when it has
// hiddenAPIPolicies are the device's settings that allow apps Android's hidden APIs.
var hiddenAPIPolicies = []string{"hidden_api_policy_pre_p_apps", "hidden_api_policy_p_apps", "hidden_api_policy"}

// stopped answering (an app that took the device down with it).
// startServer makes sure the device's UiAutomator2 server runs, as a scenario starts or after
// the server crashed under one. An emulator can stall for a while, and adb loses it meanwhile
// (adb: device offline, then not found): the server is started once adb has the device back, and
// once more if adb loses it on the way.
func (p *pool) startServer(ctx context.Context, d *device, logDir string) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = p.sdk.waitOnline(ctx, d.serial, 2*time.Minute); err != nil {
			return err
		}
		err = p.ensureServer(ctx, d, logDir)
		if err == nil || !lostDevice(err) {
			return err
		}
	}
	return err
}

// lostDevice is whether adb failed for not having the device at that moment.
func lostDevice(err error) bool {
	return strings.Contains(err.Error(), "device offline") || strings.Contains(err.Error(), "not found")
}

func (p *pool) ensureServer(ctx context.Context, d *device, logDir string) error {
	if d.server != nil {
		select {
		case <-d.server.exited:
		default:
			if d.server.client.Status(ctx) == nil {
				return nil
			}
		}
		d.server.stop()
		d.server = nil
	}
	apks, err := uia2For(ctx, p.suite)
	if err != nil {
		return err
	}
	if err := p.installServer(ctx, d.serial, apks); err != nil {
		return err
	}
	// adb picks the host's ports as it first forwards them (tcp:0), and says which: a port picked
	// ahead of time can be another program's by the time the emulator has booted ("cannot bind
	// listener: Address already in use"). A server started again keeps its ports, which adb
	// forwards anew, so the scenario's session reaches it where it did.
	for _, f := range []struct {
		port *int
		on   int
	}{{&d.systemPort, uia2Port}, {&d.mjpegPort, uia2MJPEGPort}} {
		local := "tcp:" + strconv.Itoa(*f.port)
		if *f.port == 0 {
			local = "tcp:0"
		}
		out, err := p.sdk.run(ctx, d.serial, "forward", local, "tcp:"+strconv.Itoa(f.on))
		if err != nil {
			return fmt.Errorf("cannot reach the UiAutomator2 server on %s: %w", d.serial, err)
		}
		if *f.port == 0 {
			if *f.port, err = forwardedPort(out); err != nil {
				return fmt.Errorf("cannot reach the UiAutomator2 server on %s: %w", d.serial, err)
			}
		}
	}
	// What a scenario before left of a server: one instrumentation at a time.
	_, _ = p.sdk.shell(ctx, d.serial, "am", "force-stop", uia2Package)
	_, _ = p.sdk.shell(ctx, d.serial, "am", "force-stop", uia2Test)
	// The server reads the screen through Android's hidden APIs, which Android 9 and later refuse
	// it unless the device allows them; without them, it fails to read a screen now and then
	// (it "cannot set AccessibilityNodeInfo's field 'mSealed'"). Appium's driver allows them the
	// same way, and puts the default back when it is done, as stop does.
	for _, k := range hiddenAPIPolicies {
		if _, err := p.sdk.shell(ctx, d.serial, "settings", "put", "global", k, "1"); err != nil {
			return fmt.Errorf("cannot allow the UiAutomator2 server Android's hidden APIs on %s: %w", d.serial, err)
		}
	}
	// A server started again after it crashed adds to the log of the one before, which says why.
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if d.served {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	log, err := os.OpenFile(filepath.Join(logDir, "uiautomator2-"+d.serial+".log"), flags, 0o644)
	if err != nil {
		return err
	}
	d.served = true
	_, _ = fmt.Fprintf(log, "--- the UiAutomator2 server starts, %s\n", time.Now().Format(time.RFC3339))
	// The server outlives this step: it runs until the device leaves the pool.
	cmd := p.sdk.command(context.Background(), "-s", d.serial, "shell",
		"am", "instrument", "-w", "--no-window-animation", "-e", "disableAnalytics", "true", uia2Runner)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = time.Second
	proc.Setup(cmd)
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return fmt.Errorf("cannot start the UiAutomator2 server on %s: %w", d.serial, err)
	}
	s := &uia2Server{client: &appium.Client{URL: "http://127.0.0.1:" + strconv.Itoa(d.systemPort), HTTP: &http.Client{}, Selectors: true}, cmd: cmd, exited: make(chan struct{}), log: log}
	s.stopOn = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = p.sdk.shell(ctx, d.serial, "am", "force-stop", uia2Package)
		for _, k := range hiddenAPIPolicies {
			_, _ = p.sdk.shell(ctx, d.serial, "settings", "delete", "global", k)
		}
	}
	if g, err := proc.NewGroup(cmd); err == nil {
		s.group = g
	}
	go func() {
		_ = cmd.Wait()
		// A server that ended by itself crashed, or was killed: its log keeps what the device
		// says of it.
		if !s.stopping.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			_, _ = fmt.Fprintf(log, "--- the UiAutomator2 server ended by itself, %s\n%s", time.Now().Format(time.RFC3339), p.endOf(ctx, d.serial))
			cancel()
		}
		close(s.exited)
	}()
	deadline := time.Now().Add(uia2Start)
	for {
		if s.client.Status(ctx) == nil {
			d.server = s
			return nil
		}
		select {
		case <-s.exited:
			s.stop()
			return fmt.Errorf("the UiAutomator2 server on %s stopped as it started: its log is %s", d.serial, log.Name())
		case <-ctx.Done():
			s.stop()
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			s.stop()
			return fmt.Errorf("the UiAutomator2 server on %s did not answer within %s: its log is %s", d.serial, uia2Start, log.Name())
		}
	}
}

var versionName = regexp.MustCompile(`versionName=(\S+)`)

// installServer installs the server's APKs on the device, unless the pinned
// version is there.
func (p *pool) installServer(ctx context.Context, serial string, apks []string) error {
	out, _ := p.sdk.shell(ctx, serial, "dumpsys", "package", uia2Package)
	test, _ := p.sdk.shell(ctx, serial, "pm", "list", "packages", uia2Test)
	if m := versionName.FindStringSubmatch(out); m != nil && m[1] == uia2Version && strings.Contains(test, uia2Test) {
		return nil
	}
	for _, apk := range apks {
		if _, err := p.sdk.install(ctx, serial, apk, "-t", "-g"); err != nil {
			return fmt.Errorf("cannot install the UiAutomator2 server on %s: %w", serial, err)
		}
	}
	return nil
}

// endOf is what the device says of how a process of the UiAutomator2 server ended: a crash, in
// its crash log; a kill (the low-memory killer, a force-stop), in its events and its system log.
func (p *pool) endOf(ctx context.Context, serial string) string {
	var b strings.Builder
	section := func(title string, args []string, keep func(string) bool) {
		out, err := p.sdk.run(ctx, serial, args...)
		if err != nil {
			fmt.Fprintf(&b, "%s: cannot read it: %v\n", title, err)
			return
		}
		var lines []string
		for _, l := range strings.Split(out, "\n") {
			if keep == nil || keep(l) {
				lines = append(lines, l)
			}
		}
		if len(lines) > 80 {
			lines = lines[len(lines)-80:]
		}
		if len(strings.TrimSpace(strings.Join(lines, ""))) == 0 {
			fmt.Fprintf(&b, "%s: nothing\n", title)
			return
		}
		fmt.Fprintf(&b, "%s:\n%s\n", title, strings.Join(lines, "\n"))
	}
	about := func(words ...string) func(string) bool {
		return func(l string) bool {
			for _, w := range words {
				if strings.Contains(l, w) {
					return true
				}
			}
			return false
		}
	}
	section("the device's crash log", []string{"logcat", "-d", "-b", "crash"}, nil)
	section("the device's process events", []string{"logcat", "-d", "-b", "events", "-t", "2000"},
		about("am_kill", "am_proc_died", "am_crash", "am_anr", "am_low_memory", "uiautomator2"))
	section("the device's log of the server and its end", []string{"logcat", "-d", "-b", "main,system", "-t", "5000"},
		about("uiautomator2", "UiAutomator", "lowmemorykiller", "lmkd", "Force stopping", "Killing", "FATAL", "AndroidRuntime", "DEBUG", "crash_dump", "Fatal signal"))
	return b.String()
}

// stop ends the server's instrumentation.
func (s *uia2Server) stop() {
	s.stopping.Store(true)
	s.stopOn()
	if s.group != nil {
		_ = s.group.KillAndWait(5 * time.Second)
		s.group.Release()
	} else if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.exited
	_ = s.log.Close()
}

// forwardedPort is the host's port adb chose for a forward to tcp:0, which it prints.
func forwardedPort(out string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("adb forwarded it to %q, which is not a port", strings.TrimSpace(out))
	}
	return port, nil
}
