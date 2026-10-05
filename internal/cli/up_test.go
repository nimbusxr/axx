package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
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

func TestTheSupervisorGetsTheSameConfiguration(t *testing.T) {
	cf := configFlags{profile: "ci", defines: []string{"local.host=docker"}, settings: []string{"run.workers=1", "packs.web-core.headless=false"}}
	args := supervisorArgs("", cf, "api", []string{"api"})
	cmd := newSuperviseCmd(&App{})
	if err := cmd.ParseFlags(args[1:]); err != nil {
		t.Fatal(err)
	}
	profile, _ := cmd.Flags().GetString("profile")
	defines, _ := cmd.Flags().GetStringArray("define")
	settings, _ := cmd.Flags().GetStringArray("set")
	debug, _ := cmd.Flags().GetString("debug")
	if profile != "ci" || !slices.Equal(defines, cf.defines) || !slices.Equal(settings, cf.settings) || debug != "api" || !slices.Equal(cmd.Flags().Args(), []string{"api"}) {
		t.Errorf("the supervisor got profile %q, -D %v, --set %v, --debug %q, services %v", profile, defines, settings, debug, cmd.Flags().Args())
	}
}
