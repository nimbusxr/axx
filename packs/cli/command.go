package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/shellwords"
)

// defaultTimeout is how long a command may run unless its timeout says
// otherwise.
const defaultTimeout = time.Minute

// Command is a command registered in a scenario.
type Command struct {
	Name string
	// Argv is the program and its first arguments.
	Argv []string
	// Dir is the folder it runs in ("" until the scenario's own is made).
	Dir string
	// Env is its environment: this process's, with the command's own.
	Env     []string
	Timeout time.Duration
}

// run is a command's last run.
type run struct {
	argv   []string
	dir    string
	result *proc.Result
}

// state is a scenario's commands, their last runs, and its own folder.
type state struct {
	mu       sync.Mutex
	commands *core.Services[*Command]
	runs     map[string]*run
	folder   string
}

var scenarioState = core.NewStateKey(Name, func(sc *core.Scenario) *state {
	st := &state{
		commands: core.NewServices[*Command]("Command",
			`No command is registered in this scenario; register one with "the {word} command with the following properties:"`).RegisteredBy("the {word} command with the following properties:"),
		runs: map[string]*run{},
	}
	sc.Describe(Name, func() any { return describe(sc, st) })
	return st
}, closeState)

// closeState removes the scenario's folder when it passed, and says where
// it is when it failed.
func closeState(sc *core.Scenario, st *state) error {
	if st.folder == "" {
		return nil
	}
	if sc.Status() == "failed" {
		sc.Log("the commands' folder is kept: %s", st.folder)
		return nil
	}
	return os.RemoveAll(st.folder)
}

// folder is the scenario's own folder, made the first time it is needed.
func (st *state) ownFolder() (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.folder == "" {
		dir, err := os.MkdirTemp("", "axx-cli-")
		if err != nil {
			return "", err
		}
		st.folder = dir
	}
	return st.folder, nil
}

// parse reads a command's properties. Values are expanded, and what their
// ${env:..} references expand to is kept as the scenario's secrets.
func parse(sc *core.Scenario, name string, t *core.Table) (*Command, error) {
	if t == nil {
		return nil, errors.New(`the command property "command" is required`)
	}
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	c := &Command{Name: name, Timeout: defaultTimeout}
	env := map[string]string{}
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return nil, err
		}
		switch {
		case p.Key == "command":
			c.Argv = shellwords.Split(v)
		case p.Key == "dir":
			d := filepath.FromSlash(strings.TrimSpace(v))
			if !filepath.IsAbs(d) {
				d = filepath.Join(sc.Suite().ProjectDir(), d)
			}
			if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
				return nil, fmt.Errorf("the %s command's dir %s is not a folder", name, d)
			}
			c.Dir = filepath.Clean(d)
		case strings.HasPrefix(p.Key, "env."):
			n := strings.TrimPrefix(p.Key, "env.")
			if n == "" || strings.ContainsAny(n, "= ") {
				return nil, fmt.Errorf("the %s command's property %q does not name an environment variable", name, p.Key)
			}
			env[n] = v
		case p.Key == "timeout":
			d, err := time.ParseDuration(strings.TrimSpace(v))
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("the %s command's timeout %q is not a duration, like 30s or 2m", name, p.Value)
			}
			c.Timeout = d
		default:
			return nil, fmt.Errorf("unknown command property %q (supported: command, dir, env.<NAME>, timeout)", p.Key)
		}
	}
	if len(c.Argv) == 0 {
		return nil, errors.New(`the command property "command" is required`)
	}
	// A program named by a relative path is found from the project's
	// directory; a bare name on the PATH.
	if p := c.Argv[0]; !filepath.IsAbs(p) && strings.ContainsAny(p, `/\`) {
		c.Argv[0] = filepath.Join(sc.Suite().ProjectDir(), filepath.FromSlash(p))
	}
	c.Env = environment(env)
	return c, nil
}

// environment is this process's environment with vars set over it.
func environment(vars map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(vars))
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !has(vars, k) {
			out = append(out, kv)
		}
	}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		out = append(out, k+"="+vars[k])
	}
	return out
}

func has(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

// describe is the scenario's commands' last runs, for failure reports.
func describe(sc *core.Scenario, st *state) any {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string]any{}
	for name, r := range st.runs {
		d := map[string]any{
			"command": secrets.Mask(sc, display(r.argv)),
			"dir":     r.dir,
		}
		if r.result != nil {
			d["exit code"] = r.result.ExitCode
			d["output"] = secrets.Mask(sc, tail(r.result.Stdout))
			d["error output"] = secrets.Mask(sc, tail(r.result.Stderr))
		}
		out[name] = d
	}
	if st.folder != "" {
		out["folder"] = st.folder
	}
	return out
}

// display is argv as one line, quoting words with spaces.
func display(argv []string) string {
	words := make([]string, len(argv))
	for i, w := range argv {
		if w == "" || strings.ContainsAny(w, " \t\"") {
			w = `"` + strings.ReplaceAll(w, `"`, `\"`) + `"`
		}
		words[i] = w
	}
	return strings.Join(words, " ")
}

// tailLines is how many of a command's last lines a failure shows.
const tailLines = 20

// tail is the last lines of b.
func tail(b []byte) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if n := len(lines) - tailLines; n > 0 {
		lines = append([]string{fmt.Sprintf("… %d lines before", n)}, lines[n:]...)
	}
	return strings.Join(lines, "\n")
}
