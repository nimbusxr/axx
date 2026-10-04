//go:build linux && integration

package atspi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 1 of ADR 0011: an app as it ships, read and driven through AT-SPI
// in pure Go. AXX_PROOF_APP is the app's executable; AXX_PROOF_ARGS its
// arguments; AXX_PROOF_CAPTURE an image the app's screen capture returns
// (Snap captures through scrot on X11, which a stand-in replaces);
// AXX_PROOF_PRESS the names of buttons to click, separated by "|".
func TestAppThroughATSPI(t *testing.T) {
	app := os.Getenv("AXX_PROOF_APP")
	if app == "" {
		t.Skip("AXX_PROOF_APP names the app's executable")
	}
	if err := Announce(); err != nil {
		t.Fatal(err)
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	dir := t.TempDir()
	bin, home := filepath.Join(dir, "bin"), filepath.Join(dir, "home")
	for _, d := range []string{bin, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scrot := "#!/bin/sh\nfor last; do :; done\ncp \"$AXX_PROOF_CAPTURE\" \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "scrot"), []byte(scrot), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(app, strings.Fields(os.Getenv("AXX_PROOF_ARGS"))...)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "SNAP_DATA_DIR="+filepath.Join(dir, "data"))
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	var root *Element
	for wait := time.Now(); time.Since(wait) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
		if root, err = c.ApplicationOf(cmd.Process.Pid); err == nil {
			t.Logf("the application %q came on the bus after %s", root.Name(), time.Since(wait).Round(time.Millisecond))
			break
		}
	}
	if root == nil {
		t.Fatalf("no application within 30s (%v)", err)
	}

	// The page's tree comes in as it loads: wait for buttons.
	var all []*Element
	for wait := time.Now(); time.Since(wait) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
		all = all[:0]
		walk(root, 0, func(e *Element) { all = append(all, e) })
		if count(all, "push button") > 3 {
			t.Logf("%d buttons came in %s", count(all, "push button"), time.Since(wait).Round(time.Millisecond))
			break
		}
	}
	started := time.Now()
	all = all[:0]
	walk(root, 0, func(e *Element) { all = append(all, e) })
	took := time.Since(started).Round(time.Millisecond)
	var b strings.Builder
	for _, e := range all {
		line := []string{e.Role()}
		for _, kv := range [][2]string{{"name", e.Name()}, {"description", e.Description()}} {
			if kv[1] != "" {
				line = append(line, fmt.Sprintf("%s=%q", kv[0], kv[1]))
			}
		}
		if a := e.Attributes(); len(a) > 0 {
			line = append(line, fmt.Sprintf("attributes=%v", a))
		}
		if acts := e.Actions(); len(acts) > 0 {
			line = append(line, "actions="+strings.Join(acts, ","))
		}
		b.WriteString(strings.Join(line, " ") + "\n")
	}
	t.Logf("%d elements in %s:\n%s", len(all), took, b.String())

	for _, name := range strings.Split(os.Getenv("AXX_PROOF_PRESS"), "|") {
		if name == "" {
			continue
		}
		var btn *Element
		for _, e := range all {
			if e.Role() == "push button" && (e.Name() == name || e.Description() == name) {
				btn = e
				break
			}
		}
		if btn == nil {
			t.Fatalf("no %q button", name)
		}
		action := "click"
		if acts := btn.Actions(); len(acts) > 0 {
			action = acts[0]
		}
		if err := btn.Do(action); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		time.Sleep(300 * time.Millisecond)
		t.Logf("%q (name %q, at %+v): %s done", name, btn.Name(), btn.Extents(), action)
	}
}

func walk(e *Element, depth int, visit func(*Element)) {
	if depth > 40 {
		return
	}
	visit(e)
	kids, err := e.Children()
	if err != nil && !errors.Is(err, ErrNoApplication) {
		return
	}
	for _, k := range kids {
		walk(k, depth+1, visit)
	}
}

func count(all []*Element, role string) int {
	n := 0
	for _, e := range all {
		if e.Role() == role {
			n++
		}
	}
	return n
}
