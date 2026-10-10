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
}

// ensureServer starts the device's server, or starts it again when it has
// stopped answering (an app that took the device down with it).
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
	for port, on := range map[int]int{d.systemPort: uia2Port, d.mjpegPort: uia2MJPEGPort} {
		if _, err := p.sdk.run(ctx, d.serial, "forward", "tcp:"+strconv.Itoa(port), "tcp:"+strconv.Itoa(on)); err != nil {
			return fmt.Errorf("cannot reach the UiAutomator2 server on %s: %w", d.serial, err)
		}
	}
	// What a scenario before left of a server: one instrumentation at a time.
	_, _ = p.sdk.shell(ctx, d.serial, "am", "force-stop", uia2Package)
	_, _ = p.sdk.shell(ctx, d.serial, "am", "force-stop", uia2Test)
	log, err := os.Create(filepath.Join(logDir, "uiautomator2-"+d.serial+".log"))
	if err != nil {
		return err
	}
	// The server outlives this step: it runs until the device leaves the pool.
	cmd := exec.CommandContext(context.Background(), p.sdk.adb, "-s", d.serial, "shell", //nolint:gosec // adb, with arguments axx builds
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
	}
	if g, err := proc.NewGroup(cmd); err == nil {
		s.group = g
	}
	go func() {
		_ = cmd.Wait()
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
		if _, err := p.sdk.run(ctx, serial, "install", "-r", "-t", "-g", apk); err != nil {
			return fmt.Errorf("cannot install the UiAutomator2 server on %s: %w", serial, err)
		}
	}
	return nil
}

// stop ends the server's instrumentation.
func (s *uia2Server) stop() {
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
