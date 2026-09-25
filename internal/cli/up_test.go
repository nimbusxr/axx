package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

func TestSupervisorErrorRoundTrip(t *testing.T) {
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	_ = supervisorError(axxerr.New("AXX-E0403", exitcode.Usage, "unknown app %q", "nope").WithHint("use api"))
	_ = supervisorError(errors.New("plain failure"))
	os.Stdout = stdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	e, ok := parseSupervisorError(lines[0])
	if !ok || e.Code != "AXX-E0403" || e.Exit != exitcode.Usage || e.Hint != "use api" || !strings.Contains(e.Message, `unknown app "nope"`) {
		t.Errorf("structured error: %+v", e)
	}
	e, ok = parseSupervisorError(lines[1])
	if !ok || e.Code != "AXX-E0407" || e.Exit != exitcode.Environment || e.Message != "plain failure" {
		t.Errorf("plain error: %+v", e)
	}
	if _, ok := parseSupervisorError("some app output"); ok {
		t.Error("ordinary lines are not errors")
	}
}
