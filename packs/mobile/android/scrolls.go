package mobileandroid

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/npm"
)

// A scroll is what a view does when it is asked to; a swipe is a finger on the glass. Android's
// accessibility actions, which a screen reader uses, ask a view to scroll a page, or to bring one
// of its controls onto the screen: in a tenth of a second, by exactly what the view takes for a
// page, with nothing touched (no key of a keyboard, no edge that pulls the notifications down or
// goes home). The UiAutomator2 server has no command for them, so axx has a small accessibility
// service of its own perform them beside it (scroll/Scrolls.java): axx builds it with the SDK's
// tools, installs it, switches it on in the device's settings for the run, and talks to it over
// adb. Where it cannot (no JDK or build-tools to build it with, a device another Appium server
// drives), the steps swipe, as they did.

//go:embed scroll/AndroidManifest.xml scroll/Scrolls.java scroll/res/xml/service.xml
var scrollSource embed.FS

const (
	scrollPackage = "us.nimbusxr.axx.scroll"
	scrollService = scrollPackage + "/" + scrollPackage + ".Scrolls"
	// scrollSocket is the service's socket on the device (Scrolls.SOCKET), and scrollProtocol
	// the version its ping answers (Scrolls.VERSION).
	scrollSocket   = "axx-scroll"
	scrollProtocol = 1
)

// scrollBuild is the tools axx builds the scroll service with: the SDK's build-tools and a
// platform, and a JDK.
type scrollBuild struct {
	tools, platform, javaHome string
}

// scrollTools finds them, or says which is missing.
func scrollTools(ctx context.Context, sdk string) (scrollBuild, error) {
	var b scrollBuild
	tools, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*"))
	slices.Sort(tools)
	for i := len(tools) - 1; i >= 0 && b.tools == ""; i-- {
		if _, err := os.Stat(filepath.Join(tools[i], exe("aapt2", ".exe"))); err == nil {
			b.tools = tools[i]
		}
	}
	platforms, _ := filepath.Glob(filepath.Join(sdk, "platforms", "android-*", "android.jar"))
	slices.Sort(platforms)
	if len(platforms) > 0 {
		b.platform = platforms[len(platforms)-1]
	}
	b.javaHome = javaHome(ctx)
	var missing []string
	if b.tools == "" {
		missing = append(missing, "the SDK's build-tools")
	}
	if b.platform == "" {
		missing = append(missing, "an SDK platform")
	}
	if b.javaHome == "" {
		missing = append(missing, "a JDK")
	}
	if len(missing) > 0 {
		return b, fmt.Errorf("%s missing", strings.Join(missing, " and "))
	}
	return b, nil
}

// javaHome is the JDK axx builds what runs on a device with: JAVA_HOME, or macOS's, or javac's.
func javaHome(ctx context.Context) string {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		return home
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.CommandContext(ctx, "/usr/libexec/java_home").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	if javac, err := exec.LookPath("javac"); err == nil {
		if real, err := filepath.EvalSymlinks(javac); err == nil {
			return filepath.Dir(filepath.Dir(real))
		}
	}
	return ""
}

// exe is a tool's file name on this machine.
func exe(name, windows string) string {
	if runtime.GOOS == "windows" {
		return name + windows
	}
	return name
}

// buildScroll builds the scroll service's APK, once, into axx's cache, and gives the APK and its
// version: what it was built from, which the device says of the one it has.
func buildScroll(ctx context.Context, sdk string) (apk, version string, err error) {
	b, err := scrollTools(ctx, sdk)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	var files []string
	if err := fs.WalkDir(scrollSource, "scroll", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	}); err != nil {
		return "", "", err
	}
	slices.Sort(files)
	for _, f := range files {
		data, err := scrollSource.ReadFile(f)
		if err != nil {
			return "", "", err
		}
		_, _ = fmt.Fprintf(h, "%s %d\n", f, len(data))
		h.Write(data)
	}
	_, _ = io.WriteString(h, b.tools+b.platform+b.javaHome)
	version = fmt.Sprintf("%x", h.Sum(nil)[:8])
	cache := npm.CacheDir("mobile")
	apk = filepath.Join(cache, "scroll-"+version, "axx-scroll.apk")
	if _, err := os.Stat(apk); err == nil {
		return apk, version, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", "", err
	}
	tmp, err := os.MkdirTemp(cache, "prepare-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	for _, f := range files {
		data, _ := scrollSource.ReadFile(f)
		to := filepath.Join(tmp, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return "", "", err
		}
	}
	src, classes := filepath.Join(tmp, "scroll"), filepath.Join(tmp, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return "", "", err
	}
	run := func(tool string, args ...string) error {
		cmd := exec.CommandContext(ctx, tool, args...)
		cmd.Env = append(os.Environ(), "JAVA_HOME="+b.javaHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", filepath.Base(tool), err, out)
		}
		return nil
	}
	if err := run(filepath.Join(b.javaHome, "bin", exe("javac", ".exe")), "--release", "8", "-Xlint:-options", "-classpath", b.platform, "-d", classes, filepath.Join(src, "Scrolls.java")); err != nil {
		return "", "", err
	}
	dex := []string{"--min-api", "26", "--lib", b.platform, "--output", tmp}
	if err := filepath.WalkDir(classes, func(path string, _ os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".class") {
			dex = append(dex, path)
		}
		return err
	}); err != nil {
		return "", "", err
	}
	if err := run(filepath.Join(b.tools, exe("d8", ".bat")), dex...); err != nil {
		return "", "", err
	}
	aapt2 := filepath.Join(b.tools, exe("aapt2", ".exe"))
	res, linked := filepath.Join(tmp, "res.zip"), filepath.Join(tmp, "linked.apk")
	if err := run(aapt2, "compile", "--dir", filepath.Join(src, "res"), "-o", res); err != nil {
		return "", "", err
	}
	if err := run(aapt2, "link", "-I", b.platform, "--manifest", filepath.Join(src, "AndroidManifest.xml"), "--version-name", version, "-o", linked, res); err != nil {
		return "", "", err
	}
	// The service's code goes into the package aapt2 made, which is then aligned and signed. The
	// key is one axx makes for this machine: Android asks only that an app's updates have its
	// key, and axx installs the service afresh when another machine's is on the device.
	unsigned, aligned := filepath.Join(tmp, "unsigned.apk"), filepath.Join(tmp, "aligned.apk")
	if err := addToZip(linked, unsigned, "classes.dex", filepath.Join(tmp, "classes.dex")); err != nil {
		return "", "", err
	}
	if err := run(filepath.Join(b.tools, exe("zipalign", ".exe")), "-f", "4", unsigned, aligned); err != nil {
		return "", "", err
	}
	keystore := filepath.Join(cache, "scroll.keystore")
	if _, err := os.Stat(keystore); err != nil {
		made := filepath.Join(tmp, "scroll.keystore")
		if err := run(filepath.Join(b.javaHome, "bin", exe("keytool", ".exe")), "-genkeypair", "-keystore", made, "-storepass", "android", "-keypass", "android",
			"-alias", "axx", "-dname", "CN=axx scroll service", "-keyalg", "RSA", "-keysize", "2048", "-validity", "10000"); err != nil {
			return "", "", err
		}
		if err := os.Rename(made, keystore); err != nil {
			return "", "", err
		}
	}
	signed := filepath.Join(tmp, "axx-scroll.apk")
	if err := run(filepath.Join(b.tools, exe("apksigner", ".bat")), "sign", "--ks", keystore, "--ks-pass", "pass:android", "--out", signed, aligned); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(apk), 0o755); err != nil {
		return "", "", err
	}
	if err := os.Rename(signed, apk); err != nil {
		return "", "", err
	}
	return apk, version, nil
}

// addToZip copies a zip archive with one more file in it.
func addToZip(from, to, name, file string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, f := range reader.File {
		r, err := f.OpenRaw()
		if err != nil {
			return err
		}
		w, err := writer.CreateRaw(&f.FileHeader)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, r); err != nil {
			return err
		}
	}
	added, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	w, err := writer.Create(name)
	if err != nil {
		return err
	}
	if _, err := w.Write(added); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(to, out.Bytes(), 0o644)
}

// scrolls is axx's scroll service on one device, on the port adb forwards to its socket.
type scrolls struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
	in   *bufio.Reader
}

// scrolled is the service's answer.
type scrolled struct {
	Version   int       `json:"version"`
	Connected bool      `json:"connected"`
	Found     bool      `json:"found"`     // it has the control axx named
	Offered   bool      `json:"offered"`   // the control offers the action
	Scrolls   bool      `json:"scrolls"`   // the control offers any scroll at all
	Performed bool      `json:"performed"` // the control took the action
	Moved     bool      `json:"moved"`     // something scrolled, and is at rest
	Bounds    []float64 `json:"bounds"`    // where the control is now: left, top, right, bottom
	Error     string    `json:"error"`
}

// ask sends the service a request and reads its answer, on a connection it keeps, made anew
// once when it has gone.
func (s *scrolls) ask(ctx context.Context, req map[string]any) (scrolled, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, err := json.Marshal(req)
	if err != nil {
		return scrolled{}, err
	}
	var last error
	for range 2 {
		if s.conn == nil {
			conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", s.addr)
			if err != nil {
				return scrolled{}, err
			}
			s.conn, s.in = conn, bufio.NewReader(conn)
		}
		deadline := time.Now().Add(5 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		_ = s.conn.SetDeadline(deadline)
		var answer []byte
		if _, last = s.conn.Write(append(line, '\n')); last == nil {
			answer, last = s.in.ReadBytes('\n')
		}
		if last != nil {
			_ = s.conn.Close()
			s.conn, s.in = nil, nil
			continue
		}
		var a scrolled
		if err := json.Unmarshal(answer, &a); err != nil {
			return scrolled{}, fmt.Errorf("the scroll service answered %q", strings.TrimSpace(string(answer)))
		}
		if a.Error != "" {
			return a, errors.New(a.Error)
		}
		return a, nil
	}
	return scrolled{}, last
}

func (s *scrolls) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn, s.in = nil, nil
	}
}

// ensureScrolls has the device run axx's scroll service: built, installed, switched on in the
// device's settings beside what is on there, and reached on a port adb forwards. It is done
// before the UiAutomator2 server starts, which is told to leave other accessibility services
// running. A device without it scrolls by swiping; why is logged once.
func (p *pool) ensureScrolls(ctx context.Context, d *device) {
	if d.scrolls != nil {
		d.scrolls.close()
		d.scrolls = nil
	}
	err := func() error {
		apk, version, err := buildScroll(ctx, filepath.Dir(filepath.Dir(p.sdk.adb)))
		if err != nil {
			return fmt.Errorf("cannot build it: %w", err)
		}
		out, _ := p.sdk.shell(ctx, d.serial, "dumpsys", "package", scrollPackage)
		if m := versionName.FindStringSubmatch(out); m == nil || m[1] != version {
			if _, err := p.sdk.install(ctx, d.serial, apk); err != nil {
				// One another machine built has another key: Android takes this one once that is gone.
				_, _ = p.sdk.run(ctx, d.serial, "uninstall", scrollPackage)
				if _, err := p.sdk.install(ctx, d.serial, apk); err != nil {
					return fmt.Errorf("cannot install it: %w", err)
				}
			}
		}
		local := "tcp:" + strconv.Itoa(d.scrollPort)
		if d.scrollPort == 0 {
			local = "tcp:0"
		}
		fwd, err := p.sdk.run(ctx, d.serial, "forward", local, "localabstract:"+scrollSocket)
		if err != nil {
			return fmt.Errorf("cannot reach it: %w", err)
		}
		if d.scrollPort == 0 {
			if d.scrollPort, err = forwardedPort(fwd); err != nil {
				return fmt.Errorf("cannot reach it: %w", err)
			}
		}
		s := &scrolls{addr: "127.0.0.1:" + strconv.Itoa(d.scrollPort)}
		// Android binds a service a moment after its setting names it. It also takes out of the
		// setting a service it does not know yet, and it learns of a newly installed app a while
		// after the install returns (seconds, on a device that has only just booted): so the
		// setting is written until the service answers.
		var last error
		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
			if err := p.switchScrollsOn(ctx, d); err != nil {
				return fmt.Errorf("cannot switch it on: %w", err)
			}
			a, err := s.ask(ctx, map[string]any{"op": "ping"})
			if err == nil && a.Version == scrollProtocol && a.Connected {
				d.scrolls = s
				return nil
			}
			// A service installed anew is another process: the next question goes to that one.
			s.close()
			switch {
			case err != nil:
				last = err
			case a.Version != scrollProtocol:
				last = fmt.Errorf("it speaks version %d, and axx version %d", a.Version, scrollProtocol)
			default:
				last = errors.New("the device has not connected it to the screen yet")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		s.close()
		return fmt.Errorf("it does not answer: %w", last)
	}()
	if err != nil && !d.swipes {
		d.swipes = true
		if l := p.suite.Logger(); l != nil {
			l.Warn("axx's scroll service is not on the Android device: the steps scroll by swiping, which is slower", "device", d.serial, "error", err)
		}
	}
}

// switchScrollsOn names the service in the device's settings, beside the accessibility services
// already on, unless it is named there.
func (p *pool) switchScrollsOn(ctx context.Context, d *device) error {
	on, err := p.sdk.shell(ctx, d.serial, "settings", "get", "secure", "enabled_accessibility_services")
	if err != nil {
		return err
	}
	on = strings.TrimSpace(on)
	if on == "null" {
		on = ""
	}
	if slices.Contains(strings.Split(on, ":"), scrollService) {
		return nil
	}
	if on != "" {
		on += ":"
	}
	if _, err := p.sdk.shell(ctx, d.serial, "settings", "put", "secure", "enabled_accessibility_services", on+scrollService); err != nil {
		return err
	}
	_, err = p.sdk.shell(ctx, d.serial, "settings", "put", "secure", "accessibility_enabled", "1")
	return err
}

// stopScrolls takes the service out of the device's settings, where ensureScrolls put it.
func (p *pool) stopScrolls(ctx context.Context, d *device) {
	if d.scrolls != nil {
		d.scrolls.close()
		d.scrolls = nil
	}
	on, err := p.sdk.shell(ctx, d.serial, "settings", "get", "secure", "enabled_accessibility_services")
	if err != nil {
		return
	}
	var left []string
	for _, s := range strings.Split(strings.TrimSpace(on), ":") {
		if s != scrollService && s != "" && s != "null" {
			left = append(left, s)
		}
	}
	if len(left) == 0 {
		_, _ = p.sdk.shell(ctx, d.serial, "settings", "delete", "secure", "enabled_accessibility_services")
		return
	}
	_, _ = p.sdk.shell(ctx, d.serial, "settings", "put", "secure", "enabled_accessibility_services", strings.Join(left, ":"))
}
