//go:build windows && integration

package jab

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// A Java Swing window through the Java Access Bridge. AXX_JAVA names
// java.exe, whose bin has the bridge; AXX_NATIVE the folder of DepotDesk.java.
func TestSwingThroughTheBridge(t *testing.T) {
	java := os.Getenv("AXX_JAVA")
	if java == "" {
		t.Skip("AXX_JAVA names java.exe")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	c, err := New(filepath.Join(filepath.Dir(java), "WindowsAccessBridge-64.dll"))
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(java, filepath.Join(os.Getenv("AXX_NATIVE"), "DepotDesk.java"))
	// The bridge on in this Java, for this launch: nothing changes on the machine.
	cmd.Env = append(os.Environ(), "JAVA_TOOL_OPTIONS=-Djavax.accessibility.assistive_technologies=com.sun.java.accessibility.AccessBridge")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
		_ = cmd.Wait()
	})

	findWindow := syscall.NewLazyDLL("user32.dll").NewProc("FindWindowW")
	title, _ := syscall.UTF16PtrFromString("Depot desk (Swing)")
	var hwnd uintptr
	for wait := time.Now(); time.Since(wait) < 30*time.Second && hwnd == 0; {
		c.Pump(250 * time.Millisecond)
		hwnd, _, _ = findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	}
	if hwnd == 0 {
		t.Fatal("no Swing window within 30s")
	}
	var root *Element
	for wait := time.Now(); time.Since(wait) < 15*time.Second; {
		c.Pump(250 * time.Millisecond)
		if c.IsJavaWindow(hwnd) {
			if root, err = c.Window(hwnd); err == nil {
				t.Logf("the bridge knows the window after %s", time.Since(wait).Round(time.Millisecond))
				break
			}
		}
	}
	if root == nil {
		t.Fatalf("the bridge never knew the window (%v)", err)
	}

	var all []*Element
	var walk func(e *Element, depth int)
	var b strings.Builder
	walk = func(e *Element, depth int) {
		all = append(all, e)
		fmt.Fprintf(&b, "%s%s %q\n", strings.Repeat("  ", depth), e.Role(), e.Name())
		for _, k := range e.Children() {
			walk(k, depth+1)
		}
	}
	walk(root, 0)
	t.Logf("Java's tree, %d elements:\n%s", len(all), b.String())

	find := func(role, name string) *Element {
		for _, e := range all {
			if e.Role() == role && strings.HasPrefix(e.Name(), name) {
				return e
			}
		}
		t.Fatalf("no %s %q", role, name)
		return nil
	}
	if err := find("text", "Reference").SetText("PX-DESK-0001"); err != nil {
		t.Fatal(err)
	}
	if err := find("push button", "Register").Do("click"); err != nil {
		t.Fatal(err)
	}
	c.Pump(500 * time.Millisecond)
	status := find("label", "Registered")
	t.Logf("the status: %q", status.Name())
	if status.Name() != "Registered PX-DESK-0001" {
		t.Errorf("the status is %q", status.Name())
	}
}
