package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/evals/internal/mutant"
)

// TestTasks loads every task of the repository: task.toml and verify.toml
// must parse, name known mutants, and every mutant must be targeted by at
// least one task.
func TestTasks(t *testing.T) {
	tasks, err := LoadTasks(filepath.Join("..", "..", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) < 9 {
		t.Fatalf("%d tasks, want at least 9", len(tasks))
	}
	targeted := map[string]bool{}
	for _, task := range tasks {
		v, err := LoadVerify(filepath.Join(task.Dir, "tests", "verify.toml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range v.Mutants {
			targeted[m] = true
		}
		for _, f := range []string{"instruction.md", "solution/solve.sh"} {
			if _, err := os.Stat(filepath.Join(task.Dir, f)); err != nil {
				t.Errorf("%s: %v", task.ID(), err)
			}
		}
		if task.Meta.Title == "" || task.Meta.Category == "" {
			t.Errorf("%s: metadata title and category are required", task.ID())
		}
		if !strings.HasPrefix(task.Name, "axx-evals/") || !strings.HasSuffix(task.Name, "/"+task.ID()) {
			t.Errorf("%s: task name %q must be axx-evals/<directory>", task.ID(), task.Name)
		}
	}
	for _, m := range mutant.All {
		if !targeted[m.Name] {
			t.Errorf("mutant %s is not targeted by any task", m.Name)
		}
	}
}

func TestLoadVerifyRejects(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown-mutant": "allowed = [\"features/**\"]\nmin_scenarios = 1\nmutants = [\"nope\"]\n",
		"unknown-field":  "allowed = [\"features/**\"]\nmin_scenarios = 1\nmutants = [\"no-weight-limit\"]\nbogus = 1\n",
		"no-allowed":     "min_scenarios = 1\nmutants = [\"no-weight-limit\"]\n",
	} {
		p := filepath.Join(dir, name+".toml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadVerify(p); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
