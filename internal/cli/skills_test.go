package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

func TestSkillsForAUserHaveEveryPacksSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir()) // no project here
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
