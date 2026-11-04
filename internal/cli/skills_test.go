package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/skills"
)

// Skills installed for a user serve every repository: the steps are what
// `axx steps` lists in each, so the skills carry no pages of them.
func TestSkillsForAUserServeEveryRepository(t *testing.T) {
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
	skill := filepath.Join(home, ".agents", "skills", "axx-acceptance-tests")
	for _, f := range []string{"SKILL.md", "references/parameter-types.md"} {
		if _, err := os.Stat(filepath.Join(skill, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	for _, f := range []string{"references/step-index.md", "references/steps-rest.md"} {
		if _, err := os.Stat(filepath.Join(skill, f)); !os.IsNotExist(err) {
			t.Errorf("%s is there: %v", f, err)
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

// git runs git in dir, as a person would.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// A project's skills stay out of its repository, and git says so itself; the
// skills it wrote for itself stay in.
func TestSkillsStayOutOfTheProjectsRepository(t *testing.T) {
	dir, _ := agentProject(t, map[string]string{
		"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n", "CLAUDE.md": "# parcels\n",
		".agents/skills/our-release/SKILL.md": "---\nname: our-release\n---\n",
		".gitignore":                          "build/\n",
	})
	git(t, dir, "init", "-q")
	out, stderr, code := run(t, "skills", "install")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(out, "added them to .gitignore") {
		t.Errorf("output:\n%s", out)
	}
	status := git(t, dir, "status", "--porcelain", "--untracked-files=all")
	if strings.Contains(status, ".agents/skills/axx-") || strings.Contains(status, ".agents/skills/.axx-lock") || strings.Contains(status, ".claude/skills/axx-") {
		t.Errorf("git sees the skills:\n%s", status)
	}
	if !strings.Contains(status, ".agents/skills/our-release/SKILL.md") {
		t.Errorf("git does not see the project's own skill:\n%s", status)
	}
	if !exists(filepath.Join(dir, ".claude", "skills", "axx-debugging")) {
		t.Error("the skills were not linked for Claude Code")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); !strings.HasPrefix(string(b), "build/\n") {
		t.Errorf("the project's lines went:\n%s", b)
	}
	before, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if _, stderr, code := run(t, "skills", "install"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(after) != string(before) {
		t.Errorf("a second install changed .gitignore:\n%s", after)
	}
}

// A repository that holds the skills keeps them until someone takes them out:
// ignoring them then would keep only the new files out of it. The command it
// gives leaves the files in place, and the next install ignores them.
func TestCommittedSkillsLeaveGitignoreAlone(t *testing.T) {
	dir, _ := agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n", "CLAUDE.md": "# parcels\n"})
	git(t, dir, "init", "-q")
	if _, stderr, code := run(t, "skills", "install"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if err := os.Remove(filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "the skills")

	out, stderr, code := run(t, "skills", "install")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if exists(filepath.Join(dir, ".gitignore")) {
		t.Error("wrote .gitignore for skills the repository holds")
	}
	if !strings.Contains(out, skills.UntrackCommand) {
		t.Errorf("output does not say how to stop tracking them:\n%s", out)
	}

	cmd := exec.Command("sh", "-c", skills.UntrackCommand)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", skills.UntrackCommand, err, b)
	}
	if !exists(filepath.Join(dir, ".agents", "skills", "axx-debugging", "SKILL.md")) {
		t.Error("the command took the files too")
	}
	if tracked := skills.Tracked(dir); len(tracked) != 0 {
		t.Errorf("still tracked: %v", tracked)
	}
	if _, stderr, code := run(t, "skills", "install"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	git(t, dir, "add", "-A")
	if status := git(t, dir, "status", "--porcelain"); strings.Contains(status, "A  .agents") {
		t.Errorf("the skills went back in the repository:\n%s", status)
	}
}

// A home directory has no repository to keep them out of.
func TestSkillsForAUserTouchNoGitignore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir())
	if _, stderr, code := run(t, "skills", "install", "--scope", "user"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if exists(filepath.Join(home, ".gitignore")) {
		t.Error("wrote a .gitignore in the home directory")
	}
}

// staleLock says the skills under root came from another axx.
func staleLock(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, ".agents", "skills", skills.LockFile)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(b, &lock); err != nil {
		t.Fatal(err)
	}
	lock["axxVersion"] = "0.1.0"
	b, _ = json.Marshal(lock)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsUpdateUpdatesWhereTheyAreInstalled(t *testing.T) {
	dir, home := agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n"})
	_, stderr, code := run(t, "skills", "update")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0017") {
		t.Fatalf("with no skills: exit %d: %s", code, stderr)
	}

	if _, stderr, code := run(t, "skills", "install"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	staleLock(t, dir)
	checks := doctorChecks(t)
	if c := checks["agent skills"]; !strings.Contains(c.Detail, "from axx 0.1.0") || c.Status != "warn" || !strings.Contains(c.Hint, "axx skills update") {
		t.Errorf("doctor on old skills: %+v", c)
	}
	skill := filepath.Join(dir, ".agents", "skills", "axx-debugging", "SKILL.md")
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}

	out, stderr, code := run(t, "skills", "update")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(out, "from axx 0.1.0") {
		t.Errorf("output:\n%s", out)
	}
	if !exists(skill) {
		t.Error("the update did not write the skill back")
	}
	if exists(filepath.Join(home, ".agents")) {
		t.Error("the update installed skills in the home directory, which had none")
	}
	if c := doctorChecks(t)["agent skills"]; c.Status != "ok" {
		t.Errorf("doctor after the update: %+v", c)
	}
}
