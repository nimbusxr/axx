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

func client(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("AXX_NATIVE") == "" {
		t.Skip("AXX_NATIVE names the folder of the native proofs' scripts")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func start(t *testing.T, app string, args ...string) int {
	t.Helper()
	cmd := exec.Command(app, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// window waits for a top-level window that match accepts.
func window(t *testing.T, c *Client, what string, match func(*Element) bool) *Element {
	t.Helper()
	for wait := time.Now(); time.Since(wait) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
		ws, _ := c.Windows()
		for _, w := range ws {
			if match(w) {
				t.Logf("%s after %s", what, time.Since(wait).Round(time.Millisecond))
				return w
			}
		}
	}
	t.Fatalf("no %s within 30s", what)
	return nil
}

// in waits for the element under e of the control type with the name or
// AutomationId.
func in(t *testing.T, e *Element, control, name string) *Element {
	t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(200 * time.Millisecond) {
		all, _ := e.Descendants()
		for _, x := range all {
			if ControlTypeName(x.ControlType()) == control && (x.Name() == name || x.AutomationID() == name || strings.HasPrefix(x.Name(), name)) {
				return x
			}
		}
	}
	t.Fatalf("no %s %q", control, name)
	return nil
}

// A window's reference field typed into, its Register button invoked, its
// status read.
func depotDesk(t *testing.T, win *Element) {
	t.Helper()
	field := in(t, win, "Edit", "Reference")
	if err := field.SetValue("PX-DESK-0001"); err != nil {
		t.Fatal(err)
	}
	t.Logf("typed and read back: %q", field.Value())
	if err := in(t, win, "Button", "Register").Invoke(); err != nil {
		t.Fatal(err)
	}
	status := in(t, win, "Text", "Registered")
	t.Logf("the status: %q", status.Name())
	if status.Name() != "Registered PX-DESK-0001" {
		t.Errorf("the status is %q", status.Name())
	}
}

func TestNativeWinForms(t *testing.T) {
	c := client(t)
	pid := start(t, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(os.Getenv("AXX_NATIVE"), "forms.ps1"))
	depotDesk(t, window(t, c, "the WinForms window", func(w *Element) bool { return w.ProcessID() == pid && w.Name() == "Depot desk (WinForms)" }))
}

func TestNativeWPF(t *testing.T) {
	c := client(t)
	pid := start(t, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(os.Getenv("AXX_NATIVE"), "wpf.ps1"))
	depotDesk(t, window(t, c, "the WPF window", func(w *Element) bool { return w.ProcessID() == pid && w.Name() == "Depot desk (WPF)" }))
}

// A packaged WinUI app: Calculator, whose window belongs to Windows' frame
// host, found by its name.
func TestNativeWinUI(t *testing.T) {
	c := client(t)
	start(t, "explorer.exe", `shell:AppsFolder\Microsoft.WindowsCalculator_8wekyb3d8bbwe!App`)
	t.Cleanup(func() { _ = exec.Command("taskkill", "/F", "/IM", "CalculatorApp.exe").Run() })
	win := window(t, c, "Calculator's window", func(w *Element) bool { return w.Name() == "Calculator" })
	t.Logf("its window belongs to process %d (%s)", win.ProcessID(), win.ClassName())
	for _, name := range []string{"clearButton", "num7Button", "plusButton", "num5Button", "equalButton"} {
		b := in(t, win, "Button", name)
		if err := b.Invoke(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("invoked %q (%s)", b.Name(), name)
	}
	time.Sleep(300 * time.Millisecond)
	display := in(t, win, "Text", "CalculatorResults")
	t.Logf("7 + 5: %q", display.Name())
	if display.Name() != "Display is 12" {
		t.Errorf("the display says %q", display.Name())
	}
}

// A Java Swing window (AXX_JAVA names java.exe): what UI Automation sees of
// it, and the depot desk if it can.
func TestNativeSwing(t *testing.T) {
	java := os.Getenv("AXX_JAVA")
	if java == "" {
		t.Skip("AXX_JAVA names java.exe")
	}
	c := client(t)
	pid := start(t, java, filepath.Join(os.Getenv("AXX_NATIVE"), "DepotDesk.java"))
	win := window(t, c, "the Swing window", func(w *Element) bool { return w.ProcessID() == pid && w.Name() == "Depot desk (Swing)" })
	time.Sleep(time.Second)
	all, _ := win.Descendants()
	var b strings.Builder
	for _, e := range all {
		fmt.Fprintf(&b, "%s %q id=%q class=%q\n", ControlTypeName(e.ControlType()), e.Name(), e.AutomationID(), e.ClassName())
	}
	t.Logf("UI Automation sees %d elements:\n%s", len(all), b.String())
	depotDesk(t, win)
}

// A Flutter app (AXX_FLUTTER names its exe): what UI Automation sees, then
// the field focused and typed into as a person would, Register invoked.
func TestNativeFlutter(t *testing.T) {
	exe := os.Getenv("AXX_FLUTTER")
	if exe == "" {
		t.Skip("AXX_FLUTTER names the Flutter app")
	}
	c := client(t)
	pid := start(t, exe)
	win := window(t, c, "the Flutter window", func(w *Element) bool { return w.ProcessID() == pid && w.Name() != "" })
	var field *Element
	for wait := time.Now(); time.Since(wait) < 10*time.Second && field == nil; time.Sleep(250 * time.Millisecond) {
		all, _ := win.Descendants()
		for _, e := range all {
			if ControlTypeName(e.ControlType()) == "Edit" {
				field = e
			}
		}
	}
	all, _ := win.Descendants()
	var b strings.Builder
	for _, e := range all {
		fmt.Fprintf(&b, "%s %q help=%q id=%q\n", ControlTypeName(e.ControlType()), e.Name(), e.HelpText(), e.AutomationID())
	}
	t.Logf("UI Automation sees %d elements:\n%s", len(all), b.String())
	if field == nil {
		t.Fatal("no text field")
	}
	if err := field.Focus(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := Type("PX-DESK-0001"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	t.Logf("typed and read back: %q", field.Value())
	if err := in(t, win, "Button", "Register").Invoke(); err != nil {
		t.Fatal(err)
	}
	status := in(t, win, "Text", "Registered")
	t.Logf("the status: %q", status.Name())
	if status.Name() != "Registered PX-DESK-0001" {
		t.Errorf("the status is %q", status.Name())
	}
}

// Qt 5 and Qt 6 apps: qt_desk.py, run by a Python (AXX_QT) with PyQt5 and
// PyQt6.
func TestNativeQt5(t *testing.T) { qtDesk(t, "5") }

func TestNativeQt6(t *testing.T) { qtDesk(t, "6") }

func qtDesk(t *testing.T, version string) {
	python := os.Getenv("AXX_QT")
	if python == "" {
		t.Skip("AXX_QT names a Python with PyQt5 and PyQt6")
	}
	c := client(t)
	pid := start(t, python, filepath.Join(os.Getenv("AXX_NATIVE"), "qt_desk.py"), version)
	name := "Depot desk (Qt " + version + ")"
	depotDesk(t, window(t, c, "the "+name+" window", func(w *Element) bool { return w.ProcessID() == pid && w.Name() == name }))
}
