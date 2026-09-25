package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/nimbusxr/axx/evals/internal/results"
	"github.com/nimbusxr/axx/evals/internal/spec"
)

// Condition is one set of agent aids (evals/conditions.toml).
type Condition struct {
	Name        string `toml:"-"`
	Description string `toml:"description"`
	// AgentsMD writes the AGENTS.md section of `axx init`.
	AgentsMD bool `toml:"agents_md"`
	// Skills runs `axx skills install` in the project.
	Skills bool `toml:"skills"`
	// MCP registers `axx mcp` with the agent (Harbor's --mcp-config).
	MCP bool `toml:"mcp"`
}

func loadConditions(evalsDir string) (map[string]Condition, error) {
	b, err := os.ReadFile(filepath.Join(evalsDir, "conditions.toml"))
	if err != nil {
		return nil, err
	}
	var raw map[string]Condition
	if err := toml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("conditions.toml: %w", err)
	}
	for n, c := range raw {
		c.Name = n
		raw[n] = c
	}
	return raw, nil
}

func cmdPrepare(args []string) error {
	fs := flag.NewFlagSet("prepare", flag.ExitOnError)
	root := fs.String("root", "", "the evals directory")
	cond := fs.String("condition", "none", "condition from conditions.toml")
	out := fs.String("out", "", "dataset directory to write (replaced)")
	only := fs.String("tasks", "", "comma-separated task ids (default: all)")
	pending := fs.Bool("include-pending", false, "include tasks whose required packs are missing from the image")
	_ = fs.Parse(args)
	if *out == "" {
		return exitError{2, "--out is required"}
	}
	dir, err := evalsRoot(*root)
	if err != nil {
		return err
	}
	conds, err := loadConditions(dir)
	if err != nil {
		return err
	}
	c, ok := conds[*cond]
	if !ok {
		return fmt.Errorf("unknown condition %q", *cond)
	}
	packs := map[string]bool{}
	if !*pending {
		if packs, _, err = imagePacks(); err != nil {
			return err
		}
	}
	included, skipped, err := prepareDataset(dir, c, *out, splitList(*only), packs, *pending)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d task(s) to %s for condition %s\n", len(included), *out, c.Name)
	for _, s := range skipped {
		fmt.Printf("  skipped %s: %s\n", s.ID, s.Reason)
	}
	return nil
}

// prepareDataset copies the selected tasks into out, with the condition's
// agent aids added to each environment. Tasks that need packs the image lacks
// are skipped (unless includePending).
func prepareDataset(evalsDir string, c Condition, out string, only []string, packs map[string]bool, includePending bool) ([]string, []results.Skipped, error) {
	if _, err := syncTasks(evalsDir, false); err != nil {
		return nil, nil, err
	}
	tasks, err := spec.LoadTasks(filepath.Join(evalsDir, "tasks"))
	if err != nil {
		return nil, nil, err
	}
	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}
	if err := os.RemoveAll(out); err != nil {
		return nil, nil, err
	}
	var included []string
	var skipped []results.Skipped
	for _, t := range tasks {
		if len(want) > 0 && !want[t.ID()] {
			continue
		}
		delete(want, t.ID())
		if missing := t.Missing(packs); len(missing) > 0 && !includePending {
			skipped = append(skipped, results.Skipped{ID: t.ID(), Reason: "requires the " + strings.Join(missing, ", ") + " pack(s), not in this axx build"})
			continue
		}
		dst := filepath.Join(out, t.ID())
		if err := copyTree(t.Dir, dst); err != nil {
			return nil, nil, err
		}
		if err := applyCondition(filepath.Join(dst, "environment", "Dockerfile"), c); err != nil {
			return nil, nil, err
		}
		included = append(included, t.ID())
	}
	for id := range want {
		return nil, nil, fmt.Errorf("unknown task %s", id)
	}
	b, _ := json.MarshalIndent(map[string]any{"condition": c, "tasks": included, "skipped": skipped}, "", "  ")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(out, "evals-dataset.json"), b, 0o644); err != nil {
		return nil, nil, err
	}
	return included, skipped, nil
}

// applyCondition rewrites the marker line of a task Dockerfile with the
// condition's flags.
func applyCondition(dockerfile string, c Condition) error {
	b, err := os.ReadFile(dockerfile)
	if err != nil {
		return err
	}
	line := conditionMarker
	if c.AgentsMD {
		line += " --agents-md"
	}
	if c.Skills {
		line += " --skills"
	}
	s := string(b)
	if !strings.Contains(s, conditionMarker+"\n") {
		return fmt.Errorf("%s: no %q line (run `evals sync`)", dockerfile, conditionMarker)
	}
	s = strings.Replace(s, conditionMarker+"\n", fmt.Sprintf("# condition: %s\n%s\n", c.Name, line), 1)
	return os.WriteFile(dockerfile, []byte(s), 0o644)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return errors.New(p + ": symlinks are not allowed in tasks")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, fi.Mode().Perm())
	})
}
