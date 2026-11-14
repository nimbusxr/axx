package webcore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The read the whole of Chromium makes as it starts, refused by a sandbox of
// macOS's own (Seatbelt, as agent harnesses use it): the test runs itself in
// one, which refuses it, and outside it, which allows it.
func TestFullChromiumRefused(t *testing.T) {
	if os.Getenv("AXX_SANDBOX_PROBE") != "" {
		os.Stdout.WriteString("refused: " + fullChromiumRefused() + "\n")
		return
	}
	if got := fullChromiumRefused(); got != "" {
		t.Fatalf("outside a sandbox: %q", got)
	}
	sandboxExec, err := exec.LookPath("sandbox-exec")
	if err != nil {
		t.Skip("no sandbox-exec on this Mac")
	}
	profile := filepath.Join(t.TempDir(), "refuse.sb")
	if err := os.WriteFile(profile, []byte(`(version 1)
(allow default)
(deny sysctl-read (sysctl-name "kern.hv_vmm_present"))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sandboxExec, "-f", profile, os.Args[0], "-test.run=^TestFullChromiumRefused$")
	cmd.Env = append(os.Environ(), "AXX_SANDBOX_PROBE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(string(out), "refused: this sandbox refuses a read that the full Chromium makes as it starts (kern.hv_vmm_present)") {
		t.Errorf("in a sandbox that refuses the read: %s", out)
	}
}

// The full Chromium makes its profile's lock in axx's temp folder, which a
// sandbox lets it write, unless it is told another or the folder's path is
// too long for the lock's socket.
func TestChromiumEnv(t *testing.T) {
	t.Setenv("MAC_CHROMIUM_TMPDIR", "")
	t.Setenv("TMPDIR", "/tmp/claude-501/")
	env := chromiumEnv()
	if env["MAC_CHROMIUM_TMPDIR"] != "/tmp/claude-501" || env["PATH"] != os.Getenv("PATH") {
		t.Errorf("a sandbox's temp folder: %q, PATH %q", env["MAC_CHROMIUM_TMPDIR"], env["PATH"])
	}
	// macOS's own per-user temp folder fits, as its paths are as long as this.
	t.Setenv("TMPDIR", "/var/folders/5p/gycz56fd7gl56dyzyhmn1lnw0000gn/T/")
	if env := chromiumEnv(); env["MAC_CHROMIUM_TMPDIR"] != "/var/folders/5p/gycz56fd7gl56dyzyhmn1lnw0000gn/T" {
		t.Errorf("the per-user temp folder: %q", env["MAC_CHROMIUM_TMPDIR"])
	}
	t.Setenv("TMPDIR", "/private/tmp/claude-501/-Users-someone-Projects-shop/0a1b2c3d/scratchpad")
	if env := chromiumEnv(); env != nil {
		t.Errorf("a temp folder too long for the lock's socket: %q", env["MAC_CHROMIUM_TMPDIR"])
	}
	t.Setenv("TMPDIR", "/tmp/claude-501/")
	t.Setenv("MAC_CHROMIUM_TMPDIR", "/tmp/chromium")
	if env := chromiumEnv(); env != nil {
		t.Errorf("one set already: %q", env["MAC_CHROMIUM_TMPDIR"])
	}
}
