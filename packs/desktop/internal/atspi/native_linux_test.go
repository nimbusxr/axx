//go:build linux && integration

package atspi

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func native(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("AXX_NATIVE") == "" {
		t.Skip("AXX_NATIVE runs the native app proofs")
	}
	if err := Announce(); err != nil {
		t.Fatal(err)
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	d, err := PublishBus(c.Address())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return c
}

// app starts a program and waits for its application on the bus.
func app(t *testing.T, c *Client, name string, args ...string) *Element {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	for wait := time.Now(); time.Since(wait) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
		if root, err := c.ApplicationOf(cmd.Process.Pid); err == nil {
			t.Logf("%s on the bus after %s", name, time.Since(wait).Round(time.Millisecond))
			return root
		}
	}
	t.Fatalf("%s did not come on the accessibility bus within 30s", name)
	return nil
}

// in waits for the element under e with the role and the name (or a name
// starting with it).
func in(t *testing.T, e *Element, role, name string) *Element {
	t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(200 * time.Millisecond) {
		var found *Element
		walk(e, 0, func(x *Element) {
			if found == nil && x.Role() == role && (x.Name() == name || strings.HasPrefix(x.Name(), name)) {
				found = x
			}
		})
		if found != nil {
			return found
		}
	}
	t.Fatalf("no %s %q", role, name)
	return nil
}

func do(t *testing.T, e *Element) {
	t.Helper()
	acts := e.Actions()
	if len(acts) == 0 {
		t.Fatalf("%q takes no action", e.Name())
	}
	if err := e.Do(acts[0]); err != nil {
		t.Fatal(err)
	}
}

func depotDesk(t *testing.T, root *Element) {
	t.Helper()
	field := in(t, root, "text", "Reference")
	if err := field.SetText("PX-DESK-0001"); err != nil {
		t.Fatal(err)
	}
	t.Logf("typed and read back: %q", field.Text())
	register := in(t, root, "push button", "Register")
	t.Logf("Register's actions: %v", register.Actions())
	do(t, register)
	status := in(t, root, "label", "Registered")
	t.Logf("the status: %q", status.Name())
	if status.Name() != "Registered PX-DESK-0001" {
		t.Errorf("the status is %q", status.Name())
	}
}

func TestNativeGTK3(t *testing.T) {
	depotDesk(t, app(t, native(t), "python3", "/native/gtk3_desk.py"))
}

func TestNativeQt5(t *testing.T) {
	depotDesk(t, app(t, native(t), "python3", "/native/qt_desk.py", "5"))
}

func TestNativeQt6(t *testing.T) {
	depotDesk(t, app(t, native(t), "python3", "/native/qt_desk.py", "6"))
}

// Dumps an app's tree, to learn the names its controls have.
func TestDumpNativeLinux(t *testing.T) {
	name := os.Getenv("AXX_DUMP")
	if name == "" {
		t.Skip("AXX_DUMP names the program")
	}
	root := app(t, native(t), name)
	time.Sleep(2 * time.Second)
	var b strings.Builder
	walk(root, 0, func(e *Element) {
		fmt.Fprintf(&b, "%s %q text=%q actions=%v\n", e.Role(), e.Name(), e.Text(), e.Actions())
	})
	t.Log(b.String())
}

// A GTK 4 app: GNOME Calculator, on libadwaita.
func TestNativeGTK4(t *testing.T) {
	root := app(t, native(t), "gnome-calculator")
	for _, key := range []string{"7", "+", "5", "="} {
		b := in(t, root, "push button", key)
		t.Logf("pressing %q", b.Name())
		do(t, b)
	}
	time.Sleep(500 * time.Millisecond)
	var texts []string
	walk(root, 0, func(e *Element) {
		if e.Role() == "text" {
			texts = append(texts, fmt.Sprintf("%s=%q", e.Name(), e.Text()))
		}
	})
	t.Logf("its text fields: %v", texts)
	display := in(t, root, "text", "GtkSourceView")
	if got := strings.TrimSpace(display.Text()); got != "12" {
		t.Errorf("the display shows %q, not 12", got)
	}
}

// A Java Swing app (AXX_JAVA names java), through java-atk-wrapper, which the
// JVM loads when its assistive_technologies property names it.
func TestNativeSwing(t *testing.T) {
	java := os.Getenv("AXX_JAVA")
	if java == "" {
		t.Skip("AXX_JAVA names the java executable")
	}
	depotDesk(t, app(t, native(t), java, "/native/DepotDesk.java"))
}

// A Flutter app (AXX_FLUTTER names its executable): its semantics, which the
// engine gives AT-SPI through GTK.
func TestNativeFlutter(t *testing.T) {
	exe := os.Getenv("AXX_FLUTTER")
	if exe == "" {
		t.Skip("AXX_FLUTTER names the Flutter app")
	}
	root := app(t, native(t), exe)
	field := in(t, root, "text", "Reference")
	t.Logf("the field's interfaces: %v", field.Interfaces())
	// Typed as a person types: Flutter's SetTextContents changes nothing, and
	// its accessibility stops answering after it.
	input, err := NewInput()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	// Its own Focus action, and the pointer over its window (Xvfb, without a
	// window manager, puts it at the top left; keys go to the window under
	// the pointer): Flutter reports no extents.
	if err := field.Do("Focus"); err != nil {
		t.Fatal(err)
	}
	if err := input.fake(6, 0, 40, 40); err != nil { // MotionNotify
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := input.Type("PX-DESK-0001"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	t.Logf("typed and read back: %q", field.Text())
	do(t, in(t, root, "push button", "Register"))
	var status string
	for wait := time.Now(); time.Since(wait) < 5*time.Second && !strings.HasPrefix(status, "Registered"); time.Sleep(200 * time.Millisecond) {
		walk(root, 0, func(e *Element) {
			if strings.HasPrefix(e.Name(), "Registered") {
				status = e.Name()
			}
		})
	}
	t.Logf("the status: %q", status)
	if status != "Registered PX-DESK-0001" {
		t.Errorf("the status is %q", status)
	}
}
