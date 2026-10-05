//go:build darwin && integration

package ax

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// launch starts an app for a test and waits for its window. A .app bundle is
// opened as macOS opens apps, through LaunchServices (macOS kills a system
// app started from its executable); anything else is started as it is.
func launch(t *testing.T, app string, args ...string) (*Element, *Element) {
	t.Helper()
	root, win, _ := launchIn(t, t.TempDir(), app, args...)
	return root, win
}

// launchIn starts an app with home as its home: HOME, CFFIXED_USER_HOME,
// which Foundation follows (it ignores HOME), and Java's user.home.
// It returns the app's root, its window, and what stops it.
func launchIn(t *testing.T, home, app string, args ...string) (*Element, *Element, func()) {
	t.Helper()
	var pid int
	var stop func()
	if strings.HasSuffix(app, ".app") {
		before := pids(app)
		// Arguments that are files are opened with the app, as a person would.
		open := []string{"-n", "-g", "--env", "HOME=" + home, "--env", "CFFIXED_USER_HOME=" + home, "--env", "JAVA_TOOL_OPTIONS=-Duser.home=" + home, "-a", app}
		var rest []string
		for _, a := range args {
			if _, err := os.Stat(a); err == nil {
				open = append(open, a)
			} else {
				rest = append(rest, a)
			}
		}
		if len(rest) > 0 {
			open = append(append(open, "--args"), rest...)
		}
		if out, err := exec.Command("open", open...).CombinedOutput(); err != nil {
			t.Fatalf("open %s: %v %s", app, err, out)
		}
		for wait := time.Now(); pid == 0 && time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
			for _, p := range pids(app) {
				if !slices.Contains(before, p) {
					pid = p
				}
			}
		}
		if pid == 0 {
			t.Fatalf("no new process of %s", app)
		}
		stop = func() { _ = syscall.Kill(pid, syscall.SIGTERM) }
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	} else {
		cmd := exec.Command(app, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "CFFIXED_USER_HOME="+home, "JAVA_TOOL_OPTIONS=-Duser.home="+home)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		stop = func() {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		pid = cmd.Process.Pid
	}
	root, err := Application(pid)
	if err != nil {
		t.Fatal(err)
	}
	for wait := time.Now(); time.Since(wait) < 20*time.Second; time.Sleep(200 * time.Millisecond) {
		if ws, _ := root.Elements("AXWindows"); len(ws) > 0 {
			t.Logf("%s's window after %s", app, time.Since(wait).Round(time.Millisecond))
			return root, ws[0], stop
		}
	}
	t.Fatalf("no window of %s within 20s", app)
	return nil, nil, nil
}

// pids are the processes running the bundle's executables.
func pids(bundle string) []int {
	out, _ := exec.Command("pgrep", "-f", bundle+"/Contents/MacOS/").Output()
	var ps []int
	for _, f := range strings.Fields(string(out)) {
		if p, err := strconv.Atoi(f); err == nil {
			ps = append(ps, p)
		}
	}
	return ps
}

// Dumps a native app's window, to learn the names its controls have.
func TestDumpNative(t *testing.T) {
	app := os.Getenv("AXX_NATIVE_APP")
	if app == "" {
		t.Skip("AXX_NATIVE_APP names the app's executable")
	}
	root, win := launch(t, app, strings.Fields(os.Getenv("AXX_NATIVE_ARGS"))...)
	time.Sleep(time.Second)
	if os.Getenv("AXX_NATIVE_ANNOUNCE") != "" {
		// What VoiceOver sets on an app as it starts reading it.
		for _, a := range []string{"AXEnhancedUserInterface", "AXManualAccessibility"} {
			t.Logf("setting %s: %v", a, root.SetBool(a, true))
		}
		time.Sleep(time.Second)
	}
	var b strings.Builder
	n := dump(&b, win, 0)
	t.Logf("%d elements:\n%s", n, b.String())
}

// find is the first element under e that match accepts, waiting for it.
func find(t *testing.T, e *Element, what string, match func(*Element) bool) *Element {
	t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(200 * time.Millisecond) {
		var found *Element
		walk(e, func(x *Element) bool {
			if match(x) {
				found = x
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	t.Fatalf("no %s", what)
	return nil
}

func byRole(role, name string) func(*Element) bool {
	return func(e *Element) bool {
		return e.String("AXRole") == role && (name == "" || e.String("AXTitle") == name || e.String("AXDescription") == name || e.String("AXIdentifier") == name)
	}
}

func press(t *testing.T, e *Element, what string) {
	t.Helper()
	if err := e.Perform("AXPress"); err != nil {
		t.Fatalf("pressing %s: %v", what, err)
	}
}

// A SwiftUI app: Calculator, its buttons pressed by name, its display read.
func TestNativeSwiftUI(t *testing.T) {
	if os.Getenv("AXX_NATIVE") == "" {
		t.Skip("AXX_NATIVE runs the native app proofs")
	}
	_, win := launch(t, "/System/Applications/Calculator.app")
	for _, name := range []string{"All Clear", "7", "Add", "5", "Equals"} {
		press(t, find(t, win, name, byRole("AXButton", name)), name)
	}
	time.Sleep(300 * time.Millisecond)
	display := find(t, win, "the display", byRole("AXStaticText", "Edit field"))
	t.Logf("7 + 5 = %q", display.String("AXValue"))
	if got := display.String("AXValue"); got != "12" {
		t.Errorf("the display shows %q, not 12", got)
	}
}

// An AppKit app: TextEdit, its text typed and read, its menu used, a system
// dialog read and canceled.
func TestNativeAppKit(t *testing.T) {
	if os.Getenv("AXX_NATIVE") == "" {
		t.Skip("AXX_NATIVE runs the native app proofs")
	}
	doc := filepath.Join(t.TempDir(), "pickup.txt")
	if err := os.WriteFile(doc, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	root, win := launch(t, "/System/Applications/TextEdit.app", doc)
	text := find(t, win, "the text area", byRole("AXTextArea", ""))
	want := "PX-DESK-0001 is ready for pickup at the Leipzig depot."
	if err := text.SetString("AXValue", want); err != nil {
		t.Fatal(err)
	}
	if got := text.String("AXValue"); got != want {
		t.Errorf("the text area holds %q", got)
	}
	t.Logf("typed and read back: %q", text.String("AXValue"))

	// Menu commands go to the frontmost app's key window, as a person's do.
	if err := root.SetBool("AXFrontmost", true); err != nil {
		t.Fatal(err)
	}
	if err := win.Perform("AXRaise"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	bar, err := root.Attribute("AXMenuBar")
	if err != nil {
		t.Fatal(err)
	}
	menuBar, _ := bar.(*Element)
	press(t, find(t, menuBar, "Edit > Select All", byRole("AXMenuItem", "Select All")), "Select All")
	time.Sleep(200 * time.Millisecond)
	t.Logf("after Edit > Select All, the selection is %q", text.String("AXSelectedText"))
	if got := text.String("AXSelectedText"); got != want {
		t.Errorf("the selection is %q", got)
	}

	press(t, find(t, menuBar, "File > Export as PDF…", byRole("AXMenuItem", "Export as PDF…")), "Export as PDF…")
	sheet := find(t, win, "the export sheet", byRole("AXSheet", ""))
	cancel := find(t, sheet, "Cancel", byRole("AXButton", "Cancel"))
	var b strings.Builder
	dump(&b, sheet, 0)
	t.Logf("a system dialog, %d lines; pressing Cancel", strings.Count(b.String(), "\n"))
	press(t, cancel, "Cancel")
	for wait := time.Now(); time.Since(wait) < 5*time.Second; time.Sleep(200 * time.Millisecond) {
		if sheets, _ := win.Elements("AXSheets"); len(sheets) == 0 {
			t.Log("the dialog is gone")
			return
		}
	}
	t.Error("the dialog stayed")
}

// A depot desk window (AXX_NATIVE_APP, AXX_NATIVE_ARGS): its reference field
// typed into, its Register button pressed, its status read.
func TestNativeDepotDesk(t *testing.T) {
	app := os.Getenv("AXX_NATIVE_APP")
	if app == "" {
		t.Skip("AXX_NATIVE_APP names the app's executable")
	}
	root, win := launch(t, app, strings.Fields(os.Getenv("AXX_NATIVE_ARGS"))...)
	if os.Getenv("AXX_NATIVE_ANNOUNCE") != "" {
		// What VoiceOver sets on an app as it starts reading it: Flutter turns
		// its semantics on (and answers that it does not implement it).
		_ = root.SetBool("AXEnhancedUserInterface", true)
	}
	if os.Getenv("AXX_NATIVE_ATTRS") != "" {
		any := find(t, win, "a text field", byRole("AXTextField", ""))
		names, _ := any.AttributeNames()
		for _, n := range names {
			v, err := any.Attribute(n)
			t.Logf("  %s = %v (%v)", n, v, err)
		}
	}
	field := find(t, win, "the Reference field", func(e *Element) bool {
		// AXX_NATIVE_ANY_FIELD: the window's first text field, for an app that
		// gives its field no name (Flutter on macOS).
		return e.String("AXRole") == "AXTextField" && (os.Getenv("AXX_NATIVE_ANY_FIELD") != "" || byRole("AXTextField", "Reference")(e) || e.String("AXPlaceholderValue") == "Reference")
	})
	t.Logf("the field: title %q, description %q, placeholder %q", field.String("AXTitle"), field.String("AXDescription"), field.String("AXPlaceholderValue"))
	if os.Getenv("AXX_NATIVE_TYPE") == "" {
		if err := field.SetString("AXValue", "PX-DESK-0001"); err != nil {
			t.Fatal(err)
		}
	}
	// AXX_NATIVE_TYPE: typed always (Flutter on macOS reports a value set
	// through AX, yet its app never gets it).
	if os.Getenv("AXX_NATIVE_TYPE") != "" || field.String("AXValue") != "PX-DESK-0001" {
		// The field did not take the value (Java's bridge ignores it): type it,
		// as a person would, into the focused field of the frontmost app.
		t.Logf("the field ignored AXValue; typing instead")
		if err := root.SetBool("AXFrontmost", true); err != nil {
			t.Fatal(err)
		}
		if err := field.SetBool("AXFocused", true); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		if err := Type("PX-DESK-0001"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("typed and read back: %q", field.String("AXValue"))
	press(t, find(t, win, "Register", byRole("AXButton", "Register")), "Register")
	status := find(t, win, "the status", func(e *Element) bool {
		return e.String("AXRole") == "AXStaticText" && strings.HasPrefix(e.String("AXValue"), "Registered")
	})
	t.Logf("the status: %q", status.String("AXValue"))
	if got := status.String("AXValue"); got != "Registered PX-DESK-0001" {
		t.Errorf("the status is %q", got)
	}
}
