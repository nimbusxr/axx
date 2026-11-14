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
