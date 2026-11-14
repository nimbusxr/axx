package webcore

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// ask sends the pack's side in the host one line, as a run's side does, and
// returns its answer.
func ask(t *testing.T, line string) hostedBrowser {
	t.Helper()
	run, host := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pack{}.ServeHost(context.Background(), core.HostRun{Tell: func(string) {}}, host)
	}()
	_ = run.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := run.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	answer, err := bufio.NewReader(run).ReadString('\n')
	if err != nil {
		t.Fatalf("the host's side did not answer %s: %v", line, err)
	}
	_ = run.Close()
	<-done
	var b hostedBrowser
	if err := json.Unmarshal([]byte(answer), &b); err != nil {
		t.Fatalf("the answer %q is not one: %v", answer, err)
	}
	return b
}

// A run names an engine, and nothing else of how its browser starts.
func TestTheHostTakesAnEngineAndNothingElse(t *testing.T) {
	for name, line := range map[string]string{
		"a program":           `{"engine":"chromium","executablePath":"/bin/sh"}`,
		"arguments":           `{"engine":"chromium","args":["--no-sandbox","--renderer-cmd-prefix=/bin/sh"]}`,
		"a profile":           `{"engine":"chromium","userDataDir":"/Users/dana/Library/Application Support/Google/Chrome"}`,
		"another browser":     `{"engine":"/Applications/Calculator.app"}`,
		"no engine":           `{}`,
		"something else":      `["chromium"]`,
		"a browser's channel": `{"engine":"chromium","channel":"chrome-canary"}`,
	} {
		b := ask(t, line)
		if b.Endpoint != "" || b.Tunnel != "" || b.Key != "" || !strings.Contains(b.Error, "the run asked for another") {
			t.Errorf("%s: the host's side answered %+v, want a refusal", name, b)
		}
	}
}

func TestNoWebKitFromAHostOnAMac(t *testing.T) {
	for _, c := range []struct {
		engine, goos string
		hosted       bool
	}{
		{"chromium", "darwin", true},
		{"firefox", "darwin", true},
		{"chrome", "darwin", true},
		{"msedge", "darwin", true},
		{"webkit", "darwin", false},
		{"webkit", "linux", true},
		{"webkit", "windows", true},
		{"chromium", "linux", true},
	} {
		if got := notHosted(c.engine, c.goos) == ""; got != c.hosted {
			t.Errorf("%s on %s: hosted %v, want %v", c.engine, c.goos, got, c.hosted)
		}
	}
}

// The options Playwright's server starts a hosted browser with are the
// host's, for every engine: its proxy is the relay, what it keeps is in its
// own folder, and it has no window.
func TestAHostedBrowserStartsWithTheHostsOptions(t *testing.T) {
	lease := t.TempDir()
	for _, engine := range engines {
		name, config := hostedLaunch(engine, lease, "127.0.0.1:4321", "s3cret")
		if want := map[string]string{"chromium": "chromium", "chrome": "chromium", "msedge": "chromium", "firefox": "firefox", "webkit": "webkit"}[engine]; name != want {
			t.Errorf("%s: Playwright's %s starts it, want %s", engine, name, want)
		}
		proxy, _ := config["proxy"].(map[string]any)
		if proxy["server"] != "http://127.0.0.1:4321" || len(proxy) != 1 {
			t.Errorf("%s: its proxy is %v, want the relay and no address past it", engine, proxy)
		}
		for _, dir := range []string{"artifactsDir", "tracesDir", "downloadsPath"} {
			if d, _ := config[dir].(string); filepath.Dir(d) != lease {
				t.Errorf("%s: its %s is %q, want a folder of its own", engine, dir, d)
			}
		}
		if config["headless"] != true || config["host"] != "127.0.0.1" || config["wsPath"] != "s3cret" {
			t.Errorf("%s: %v", engine, config)
		}
		if _, ok := config["executablePath"]; ok {
			t.Errorf("%s: it names a program", engine)
		}
		// A page's camera and microphone are the browser's stand-ins.
		args, _ := config["args"].([]string)
		prefs, _ := config["firefoxUserPrefs"].(map[string]any)
		switch name {
		case "chromium":
			for _, want := range []string{"--use-fake-device-for-media-stream", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"} {
				if !slices.Contains(args, want) {
					t.Errorf("%s starts without %s", engine, want)
				}
			}
		case "firefox":
			if prefs["media.navigator.streams.fake"] != true || prefs["media.peerconnection.ice.proxy_only"] != true {
				t.Errorf("firefox's preferences: %v", prefs)
			}
		}
	}
}

func TestWhyABrowserEndedAsItStarted(t *testing.T) {
	said := `node:internal/process/promises:394
    triggerUncaughtException(err, true /* fromPromise */);
    ^

Error: Failed to launch the browser process.
Browser logs:

<launching> /Users/dana/Library/Caches/ms-playwright/firefox-1555/firefox/Nightly.app/Contents/MacOS/firefox -no-remote -headless
<launched> pid=47299
[pid=47299][err] *** You are running in headless mode.
[pid=47299][err] Could not find profile folder.
[pid=47299][err] Could not find profile folder.
[pid=47299] <process did exit: exitCode=1, signal=null>`
	if got, want := whyEnded(said), "Failed to launch the browser process: Could not find profile folder."; got != want {
		t.Errorf("why: %q, want %q", got, want)
	}
	if got := whyEnded(""); got != "Playwright's server ended as it started" {
		t.Errorf("why, when it said nothing: %q", got)
	}
}

func TestAHostStartsOnlySoManyBrowsers(t *testing.T) {
	for range cap(hostedBrowsers) {
		hostedBrowsers <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(hostedBrowsers) {
			<-hostedBrowsers
		}
	})
	if b := ask(t, `{"engine":"chromium"}`); b.Endpoint != "" || !strings.Contains(b.Error, "starts no more until one of those runs ends") {
		t.Errorf("one browser too many: %+v", b)
	}
}
