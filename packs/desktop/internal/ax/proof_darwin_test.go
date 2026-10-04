//go:build darwin && integration

package ax

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 0 of ADR 0011: an app as it ships, read and driven through AX in pure
// Go. AXX_PROOF_APP is the app's executable; AXX_PROOF_ARGS its arguments;
// AXX_PROOF_CAPTURE an image the app's screen capture returns (Snap's overlay
// captures the screen through screencapture, which a stand-in replaces).
func TestAppThroughAX(t *testing.T) {
	app := os.Getenv("AXX_PROOF_APP")
	if app == "" {
		t.Skip("AXX_PROOF_APP names the app's executable")
	}
	trusted, err := Trusted()
	if err != nil {
		t.Fatal(err)
	}
	if !trusted {
		t.Fatal("this process may not use the accessibility tree: allow the app that runs it in System Settings > Privacy & Security > Accessibility")
	}

	dir := t.TempDir()
	bin, home := filepath.Join(dir, "bin"), filepath.Join(dir, "home")
	for _, d := range []string{bin, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stand := map[string]string{
		"screencapture": "#!/bin/sh\nfor last; do :; done\ncp \"$AXX_PROOF_CAPTURE\" \"$last\"\n",
		"osascript":     "#!/bin/sh\nexit 0\n",
	}
	for name, body := range stand {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(app, strings.Fields(os.Getenv("AXX_PROOF_ARGS"))...)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+home, "SNAP_DATA_DIR="+filepath.Join(dir, "data"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	root, err := Application(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	var windows []*Element
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if windows, err = root.Elements("AXWindows"); err == nil && len(windows) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(windows) == 0 {
		t.Fatalf("no window within 20s (%v)", err)
	}
	// The web page's tree comes in as the page loads: wait for its web
	// area, first without anything a screen reader sets.
	webArea := func() *Element {
		var found *Element
		walk(root, func(e *Element) bool {
			if e.String("AXRole") == "AXWebArea" {
				found = e
				return false
			}
			return true
		})
		return found
	}
	var area *Element
	for wait := time.Now(); time.Since(wait) < 15*time.Second; time.Sleep(250 * time.Millisecond) {
		if area = webArea(); area != nil {
			t.Logf("the web area came in %s after the window, with nothing set", time.Since(wait).Round(time.Millisecond))
			break
		}
	}
	if area == nil {
		t.Logf("no web area within 15s; setting AXEnhancedUserInterface: %v", root.SetBool("AXEnhancedUserInterface", true))
		for wait := time.Now(); time.Since(wait) < 15*time.Second; time.Sleep(250 * time.Millisecond) {
			if area = webArea(); area != nil {
				t.Logf("the web area came in %s after AXEnhancedUserInterface", time.Since(wait).Round(time.Millisecond))
				break
			}
		}
	}
	if area == nil {
		t.Fatal("no web area")
	}

	// Find buttons by the names people see: a tooltip, or a color's name.
	button := func(name string) *Element {
		var found *Element
		walk(area, func(e *Element) bool {
			if e.String("AXRole") == "AXButton" && (e.String("AXHelp") == name || e.String("AXDescription") == name || e.String("AXTitle") == name) {
				found = e
				return false
			}
			return true
		})
		return found
	}
	classes := func(e *Element) string {
		v, _ := e.Attribute("AXDOMClassList")
		return fmt.Sprint(v)
	}
	for _, name := range []string{"Rectangle (R)", "Blue", "Thick"} {
		started := time.Now()
		b := button(name)
		if b == nil {
			t.Fatalf("no %q button", name)
		}
		found := time.Since(started).Round(time.Millisecond)
		pos, _ := b.Attribute("AXPosition")
		size, _ := b.Attribute("AXSize")
		before := classes(b)
		if err := b.Perform("AXPress"); err != nil {
			t.Fatalf("pressing %q: %v", name, err)
		}
		time.Sleep(300 * time.Millisecond)
		t.Logf("%q (title %q, found in %s at %v %v): classes %s, after AXPress %s", name, b.String("AXTitle"), found, pos, size, before, classes(b))
	}
}

// walk visits the tree under e, depth first, while visit returns true.
func walk(e *Element, visit func(*Element) bool) bool {
	if !visit(e) {
		return false
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if !walk(k, visit) {
			return false
		}
	}
	return true
}

// dump writes the tree under e, an element a line, and counts its elements.
func dump(b *strings.Builder, e *Element, depth int) int {
	if depth > 40 {
		return 0
	}
	line := []string{e.String("AXRole")}
	for _, a := range []string{"AXSubrole", "AXTitle", "AXDescription", "AXValue", "AXIdentifier", "AXDOMIdentifier", "AXRoleDescription"} {
		v, err := e.Attribute(a)
		if err != nil || v == nil || v == "" {
			continue
		}
		line = append(line, fmt.Sprintf("%s=%v", strings.TrimPrefix(a, "AX"), v))
	}
	if acts, _ := e.ActionNames(); len(acts) > 0 {
		line = append(line, "actions="+strings.Join(acts, ","))
	}
	fmt.Fprintf(b, "%s%s\n", strings.Repeat("  ", depth), strings.Join(line, " "))
	n := 1
	kids, _ := e.Children()
	for _, k := range kids {
		n += dump(b, k, depth+1)
	}
	return n
}
