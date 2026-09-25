package fixtures

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

func itoa(i int) string { return strconv.Itoa(i) }

// CleanResult lists the ignored outputs deleted and the committed outputs
// kept.
type CleanResult struct {
	Deleted          []string `json:"deleted"`
	SkippedCommitted []string `json:"skippedCommitted"`
}

// isMeta reports the tool's own committed files: manifest, pairings, lint
// rules and .gitignores.
func (g *Generator) isMeta(path string) bool {
	return path == ManifestFile || path == PairingsFile || strings.HasSuffix(path, ".gitignore") || path == g.cfg.LintOutput
}

// Clean deletes ignored generated outputs from disk (the derivations
// generate rematerializes). Committed outputs, sources, the manifest,
// the pairings lock and the managed .gitignores are never touched, and
// directories the deletion empties are removed. With dryRun nothing is
// deleted and Deleted lists what would be.
func (g *Generator) Clean(dryRun bool) (*CleanResult, error) {
	x, err := g.Expand(ModeVerify)
	if err != nil {
		return nil, err
	}
	res := &CleanResult{Deleted: []string{}, SkippedCommitted: []string{}}
	var parents []string
	for _, rel := range x.Paths() {
		if !x.ignored(rel) {
			if !g.isMeta(rel) {
				res.SkippedCommitted = append(res.SkippedCommitted, rel)
			}
			continue
		}
		file := filepath.Join(g.baseDir, filepath.FromSlash(rel))
		if _, err := os.Lstat(file); err != nil {
			continue
		}
		if !dryRun {
			if err := os.Remove(file); err != nil {
				return nil, ioError(err, "cannot delete %s", rel)
			}
		}
		res.Deleted = append(res.Deleted, rel)
		parents = append(parents, filepath.Dir(file))
	}
	if dryRun {
		return res, nil
	}
	// Remove directories the deletion emptied (per-fixture CSV directories).
	for _, parent := range parents {
		for parent != filepath.Dir(parent) {
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) > 0 {
				break
			}
			if os.Remove(parent) != nil {
				break
			}
			parent = filepath.Dir(parent)
		}
	}
	return res, nil
}

// UntrackResult lists the ignored outputs taken out of the git index.
type UntrackResult struct {
	Untracked        []string `json:"untracked"`
	AlreadyUntracked int      `json:"alreadyUntracked"`
}

// Untrack runs `git rm --cached` for ignored outputs that are still tracked,
// so the managed .gitignores can do their job. Working-tree files are never
// touched and the removal is staged, not committed. With dryRun nothing is
// staged and Untracked lists what would be.
func (g *Generator) Untrack(ctx context.Context, dryRun bool) (*UntrackResult, error) {
	x, err := g.Expand(ModeVerify)
	if err != nil {
		return nil, err
	}
	candidates := x.IgnoredOutputs
	res := &UntrackResult{Untracked: []string{}}
	if len(candidates) == 0 {
		return res, nil
	}
	tracked := map[string]bool{}
	for i := 0; i < len(candidates); i += 100 {
		batch := candidates[i:min(i+100, len(candidates))]
		out, err := g.git(ctx, "git ls-files", append([]string{"ls-files", "--"}, batch...)...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			if l := strings.TrimSpace(line); l != "" {
				tracked[l] = true
			}
		}
	}
	for _, c := range candidates {
		if tracked[c] {
			res.Untracked = append(res.Untracked, c)
		}
	}
	if !dryRun {
		for i := 0; i < len(res.Untracked); i += 100 {
			batch := res.Untracked[i:min(i+100, len(res.Untracked))]
			if _, err := g.git(ctx, "git rm --cached", append([]string{"rm", "--cached", "--quiet", "--"}, batch...)...); err != nil {
				return nil, err
			}
		}
	}
	res.AlreadyUntracked = len(candidates) - len(res.Untracked)
	return res, nil
}

func (g *Generator) git(ctx context.Context, what string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.baseDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", axxerr.New(CodeGit, exitcode.Environment, "%s timed out", what).WithHint("run it by hand to see what git is waiting for")
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", axxerr.New(CodeGit, exitcode.Environment, "%s failed (is this a git work tree?): %s [exit %d]", what, strings.TrimSpace(out.String()), ee.ExitCode()).
				WithHint("run `axx fixtures untrack` inside the git repository that holds the fixtures")
		}
		return "", axxerr.Wrap(err, CodeGit, exitcode.Environment, "cannot run %s", what).WithHint("install git and make sure it is on PATH")
	}
	return out.String(), nil
}
