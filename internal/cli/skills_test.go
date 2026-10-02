package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

func TestSkillsForAUserHaveEveryPacksSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir())                                                    // no project here
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil { // Claude Code is installed
		t.Fatal(err)
	}
	_, stderr, code := run(t, "skills", "install", "--scope", "user")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	refs := filepath.Join(home, ".agents", "skills", "axx-acceptance-tests", "references")
	for _, f := range []string{"step-index.md", "steps-rest.md", "steps-web-core.md", "steps-aws-s3.md"} {
		if _, err := os.Stat(filepath.Join(refs, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "axx-acceptance-tests")); err != nil {
		t.Errorf("not linked for Claude Code: %v", err)
	}
}

// Skills are linked for Claude Code only in a project that uses it, so a
// project of another agent does not look like a Claude Code one to doctor.
func TestSkillsAreLinkedForClaudeCodeWhenTheProjectUsesIt(t *testing.T) {
	dir, _ := agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n", ".codex/config.toml": "model = \"o4\"\n"})
	if _, stderr, code := run(t, "skills", "install"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if exists(filepath.Join(dir, ".claude")) {
		t.Error("linked for Claude Code in a project that does not use it")
	}
	checks := doctorChecks(t)
	for name, c := range checks {
		if strings.HasPrefix(name, "Claude Code") {
			t.Errorf("a check for Claude Code, which the project does not use: %+v", c)
		}
	}

	// --claude links them anyway, and axx's own links do not make the
	// project a Claude Code one.
	if _, stderr, code := run(t, "skills", "install", "--claude"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !exists(filepath.Join(dir, ".claude", "skills", "axx-acceptance-tests")) {
		t.Error("--claude did not link the skills")
	}
	if _, ok := doctorChecks(t)["Claude Code MCP"]; ok {
		t.Error("axx's own skill links count as using Claude Code")
	}

	// A project that uses Claude Code gets them linked without asking.
	writeFiles(t, dir, map[string]string{"CLAUDE.md": "# parcels\n"})
	if err := os.RemoveAll(filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, "skills", "install"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !exists(filepath.Join(dir, ".claude", "skills", "axx-acceptance-tests")) {
		t.Error("not linked in a project that uses Claude Code")
	}

	if _, _, code := run(t, "skills", "install", "--claude", "--no-claude"); code != int(exitcode.Usage) {
		t.Errorf("--claude with --no-claude: exit %d", code)
	}
}

// Before `axx init` there are no packs, so no steps to teach: the error says
// how to get some.
func TestSkillsInAProjectWithoutPacksSayHowToGetThem(t *testing.T) {
	agentProject(t, nil)
	_, stderr, code := run(t, "skills", "install")
	if code != int(exitcode.Usage) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"AXX-E0015", "the project lists none", "axx init", "axx pack add", "--scope user"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the error lacks %q:\n%s", want, stderr)
		}
	}
}
