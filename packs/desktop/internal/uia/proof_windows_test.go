//go:build windows && integration

package uia

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Phase 1 of ADR 0011: an app as it ships, read and driven through UI
// Automation in pure Go. AXX_PROOF_APP is the app's executable;
// AXX_PROOF_ARGS its arguments; AXX_PROOF_PRESS the names of buttons to
// invoke, separated by "|". The test must run in the desktop session.
func TestAppThroughUIA(t *testing.T) {
	app := os.Getenv("AXX_PROOF_APP")
	if app == "" {
		t.Skip("AXX_PROOF_APP names the app's executable")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// A home of the app's own: its data starts empty.
	home := t.TempDir()
	for _, d := range []string{"AppData/Roaming", "AppData/Local"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	args := strings.Fields(strings.ReplaceAll(os.Getenv("AXX_PROOF_ARGS"), "{home}", home))
	cmd := exec.Command(app, args...)
	cmd.Env = append(os.Environ(), "USERPROFILE="+home, "APPDATA="+filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
		_ = cmd.Wait()
	})

	// The app's windows come as it starts, and a window's web content as its
	// page loads: look through every window of the process until one has a
	// document with buttons (Chromium builds a page's tree once a client
	// reads it).
	var all []*Element
	var win *Element
	seen := map[string]bool{}
	for wait := time.Now(); time.Since(wait) < 30*time.Second && win == nil; time.Sleep(250 * time.Millisecond) {
		windows, _ := c.WindowsOf(cmd.Process.Pid)
		for _, w := range windows {
			key := fmt.Sprintf("%q %q %+v", w.Name(), w.ClassName(), w.Bounds())
			if !seen[key] {
				seen[key] = true
				t.Logf("after %s, a window: %s", time.Since(wait).Round(time.Millisecond), key)
			}
			everything, _ := w.Descendants()
			for _, e := range everything {
				if ControlTypeName(e.ControlType()) != "Document" {
					continue
				}
				if all, _ = e.Descendants(); hasButtons(all) {
					t.Logf("the page's buttons came in %s", time.Since(wait).Round(time.Millisecond))
					win = e
				}
				break
			}
		}
	}
	if win == nil {
		t.Fatal("no window with a page with buttons within 30s")
	}
	started := time.Now()
	all, err = win.Descendants()
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(started).Round(time.Millisecond)
	var b strings.Builder
	for _, e := range all {
		line := []string{ControlTypeName(e.ControlType())}
		for _, kv := range [][2]string{{"name", e.Name()}, {"help", e.HelpText()}, {"description", e.FullDescription()}, {"id", e.AutomationID()}, {"class", e.ClassName()}} {
			if kv[1] != "" {
				line = append(line, fmt.Sprintf("%s=%q", kv[0], kv[1]))
			}
		}
		b.WriteString(strings.Join(line, " ") + "\n")
	}
	t.Logf("window %q: %d elements in %s:\n%s", win.Name(), len(all), took, b.String())

	for _, name := range strings.Split(os.Getenv("AXX_PROOF_PRESS"), "|") {
		if name == "" {
			continue
		}
		started := time.Now()
		var btn *Element
		for _, e := range all {
			if ControlTypeName(e.ControlType()) == "Button" && (e.Name() == name || e.HelpText() == name || e.FullDescription() == name) {
				btn = e
				break
			}
		}
		if btn == nil {
			t.Fatalf("no %q button", name)
		}
		found := time.Since(started).Round(time.Millisecond)
		before := btn.ClassName()
		if err := btn.Invoke(); err != nil {
			t.Fatalf("invoking %q: %v", name, err)
		}
		time.Sleep(300 * time.Millisecond)
		t.Logf("%q (name %q, found in %s at %+v): class %q, after Invoke %q", name, btn.Name(), found, btn.Bounds(), before, btn.ClassName())
	}
}

func hasButtons(all []*Element) bool {
	n := 0
	for _, e := range all {
		if ControlTypeName(e.ControlType()) == "Button" {
			n++
		}
	}
	return n > 0
}
