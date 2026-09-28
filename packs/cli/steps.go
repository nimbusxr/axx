package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/javare"
	"github.com/nimbusxr/axx/internal/filecontent"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/shellwords"
)

const example = "Given the admin command with the following properties:\n" +
	"  | command | docker compose exec -T app parcels admin |\n" +
	"  | dir     | ../infra                                 |"

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".command", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} command with the following properties:",
			Doc:  "Register a command under a name: the program and its first arguments, where it runs, its environment and its timeout.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "command", Required: true, Takes: "the program and its first arguments, split like an app's `command` in axx.yaml: double quotes group words, and there is no shell. A relative path to the program is from the directory of axx.yaml"},
					{Name: "dir", Takes: "the folder it runs in, relative to the directory of axx.yaml", Default: "a folder of the scenario's own"},
					{Name: "env.<NAME>", Takes: "an environment variable, set over axx's own environment"},
					{Name: "timeout", Takes: "how long a run may take before the command is stopped, like `30s`", Default: "1m"},
				},
				Note: "Values are expanded (`${env:..}`, `${sys:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{example},
			Run: func(sc *core.Scenario, a core.Args) error {
				c, err := parse(sc, a.String(0), a.Table)
				if err != nil {
					return secrets.Hide(sc, err)
				}
				return scenarioState.Of(sc).commands.Add(c.Name, c)
			},
		},
		{
			ID: Name + ".run", Keyword: "When", Since: since,
			Expr:     "the {word} command is run[[ with {string}]]",
			Doc:      "Run the command, with more arguments after its own, split the same way. The step waits for the command to end; it fails only when the command cannot start or runs past its timeout.",
			Examples: []string{"When the admin command is run with 'label PX-ADM-6101'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return runCommand(sc, a.String(0), a, nil)
			},
		},
		{
			ID: Name + ".run.input", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr:     "the {word} command is run with[[ {string} and]] the input:",
			Doc:      "Run the command with the doc string as its input (stdin), and more arguments if the step names them.",
			Examples: []string{"When the admin command is run with 'cancel -' and the input:\n  \"\"\"\n  PX-ADM-6103\n  PX-ADM-6104\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				in := ""
				if a.DocString != nil {
					in = a.DocString.Content + "\n"
				}
				in, err := secrets.Resolve(sc, in)
				if err != nil {
					return err
				}
				return runCommand(sc, a.String(0), a, []byte(in))
			},
		},
		{
			ID: Name + ".exit", Keyword: "Then", Since: since,
			Expr:     "the {word} command's exit code is {int}",
			Doc:      "Check the exit code of the command's last run.",
			Examples: []string{"Then the admin command's exit code is 0"},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				want := a.Int(1)
				if got := r.result.ExitCode; got != want {
					return core.Fail(fmt.Sprintf("The %s command's exit code is %d, not %d.%s", name, got, want, errorOutput(r)), want, got)
				}
				return nil
			}),
		},
		{
			ID: Name + ".output.contains", Keyword: "Then", Since: since,
			Expr: "the {word} command's[[ error]] output contains {string}",
			Doc: "Check that the command's output (or its error output) contains the text. Runs of spaces and line breaks " +
				"count as one space, in both.",
			Examples: []string{"Then the admin command's error output contains 'PX-ADM-6102 is DISPATCHED'"},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				which, out := stream(a, r)
				want := secrets.Expand(sc, a.String(1))
				if !strings.Contains(filecontent.Collapse(string(out)), filecontent.Collapse(want)) {
					return core.Fail(fmt.Sprintf("The %s command's %s does not contain %q. %s", name, which, want, filecontent.Nearest(string(out), want)),
						want, nil)
				}
				return nil
			}),
		},
		{
			ID: Name + ".output.line", Keyword: "Then", Since: since,
			Expr: "the {word} command's[[ error]] output has a line matching {string}",
			Doc: "Check that a line of the command's output (or its error output) matches the regular expression (Java syntax), " +
				"anywhere in the line; `^` and `$` are the line's start and end.",
			Examples: []string{`Then the admin command's output has a line matching 'cancelled PX-ADM-610[34]'`},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				which, out := stream(a, r)
				pattern := a.String(1)
				re, err := javare.CompileFlags(pattern, javare.Multiline)
				if err != nil {
					return fmt.Errorf("invalid regular expression %q: %w", pattern, err)
				}
				ok, err := re.MatchString(string(out))
				if err != nil {
					return err
				}
				if !ok {
					return core.Fail(fmt.Sprintf("No line of the %s command's %s matches %s. %s", name, which, pattern, filecontent.Nearest(string(out), pattern)),
						pattern, nil)
				}
				return nil
			}),
		},
		{
			ID: Name + ".output.is", Keyword: "Then", Arg: core.ArgDocString, Since: since,
			Expr:     "the {word} command's[[ error]] output is:",
			Doc:      "Check the command's output (or its error output) is exactly the doc string. Windows line breaks count as plain ones, and so does one line break at the end.",
			Examples: []string{"Then the admin command's output is:\n  \"\"\"\n  cancelled PX-ADM-6103\n  cancelled PX-ADM-6104\n  \"\"\""},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				which, out := stream(a, r)
				want := ""
				if a.DocString != nil {
					want = secrets.Expand(sc, a.DocString.Content)
				}
				got := strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
				want = strings.TrimSuffix(strings.ReplaceAll(want, "\r\n", "\n"), "\n")
				if got != want {
					return core.Fail(fmt.Sprintf("The %s command's %s is not what the step says", name, which), want, got)
				}
				return nil
			}),
		},
		{
			ID: Name + ".output.empty", Keyword: "Then", Since: since,
			Expr:     "the {word} command's[[ error]] output is empty",
			Doc:      "Check that the command printed nothing at all to its output (or its error output).",
			Examples: []string{"Then the admin command's error output is empty"},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				which, out := stream(a, r)
				if len(out) > 0 {
					return core.Fail(fmt.Sprintf("The %s command's %s is not empty", name, which), "", tail(out))
				}
				return nil
			}),
		},
		{
			ID: Name + ".output.properties", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} command's output has the following properties:",
			Doc:  "Check the JSON the command printed: each row is a path into it (a field name, a dotted path or a JSONPath) and the value it has.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Values compare as text: `null` is null and `undefined` is absent.",
			},
			Examples: []string{"Then the admin command's output has the following properties:\n" +
				"  | shop                   | kestrel-books |\n" +
				"  | parcels[0].reference   | PX-ADM-6105   |\n" +
				"  | parcels[0].serviceLevel | EXPRESS       |"},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				if err := jsonassert.Properties(string(r.result.Stdout), a.Table, false); err != nil {
					return fmt.Errorf("the %s command's output: %w", name, err)
				}
				return nil
			}),
		},
		{
			ID: Name + ".output.identical", Keyword: "Then", Since: since,
			Expr:     "the {word} command's output is identical to the {filepath} file",
			Doc:      "Check that the command's output is, byte for byte, the file of the project.",
			Examples: []string{"Then the admin command's output is identical to the labels/PX-ADM-6101.zpl file"},
			Run: check(func(sc *core.Scenario, name string, r *run, a core.Args) error {
				path, err := sc.Suite().ResolvePath(a.String(1))
				if err != nil {
					return err
				}
				want, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if !bytes.Equal(r.result.Stdout, want) {
					return core.Fail(fmt.Sprintf("The %s command's output differs from the %s file (%d bytes, the file %d): %s",
						name, a.String(1), len(r.result.Stdout), len(want), firstDifference(r.result.Stdout, want)), nil, nil)
				}
				return nil
			}),
		},
	}
}

// runCommand runs a registered command, with args from the step's
// {string}, and keeps the run for the checks.
func runCommand(sc *core.Scenario, name string, a core.Args, stdin []byte) error {
	st := scenarioState.Of(sc)
	c, err := st.commands.Get(name)
	if err != nil {
		return fmt.Errorf("no command named %q in this scenario; register it first with \"the %s command with the following properties:\"", name, name)
	}
	argv := append([]string(nil), c.Argv...)
	if a.Present(1) {
		args, err := secrets.Resolve(sc, a.String(1))
		if err != nil {
			return err
		}
		argv = append(argv, shellwords.Split(args)...)
	}
	dir := c.Dir
	if dir == "" {
		if dir, err = st.ownFolder(); err != nil {
			return err
		}
	}
	res, err := proc.Run(sc.Context(), proc.Spec{Argv: argv, Dir: dir, Env: c.Env, Stdin: stdin, Timeout: c.Timeout})
	st.mu.Lock()
	st.runs[name] = &run{argv: argv, dir: dir, result: res}
	st.mu.Unlock()
	line := secrets.Mask(sc, display(argv))
	if err != nil && res == nil {
		return secrets.Hide(sc, fmt.Errorf("the %s command could not start: %s: %w", name, line, err))
	}
	for _, s := range []struct {
		name string
		b    []byte
	}{{"output", res.Stdout}, {"error output", res.Stderr}} {
		if len(s.b) > 0 {
			sc.Attach("text/plain", []byte(secrets.Mask(sc, string(s.b))), fmt.Sprintf("the %s command's %s", name, s.name))
		}
	}
	switch {
	case err != nil:
		return secrets.Hide(sc, fmt.Errorf("the %s command was stopped: %w%s", name, err, printed(res)))
	case res.TimedOut:
		return secrets.Hide(sc, fmt.Errorf("the %s command ran past its timeout of %s and was stopped: %s%s", name, c.Timeout, line, printed(res)))
	}
	sc.Log("ran %s in %s: exit code %d in %s", line, dir, res.ExitCode, res.Duration.Round(1e6))
	if res.Truncated {
		sc.Log("the %s command printed more than axx keeps: the checks see the start", name)
	}
	return nil
}

// check runs fn on the named command's last run, with every error masked.
func check(fn func(sc *core.Scenario, name string, r *run, a core.Args) error) core.StepFunc {
	return func(sc *core.Scenario, a core.Args) error {
		name := a.String(0)
		st := scenarioState.Of(sc)
		st.mu.Lock()
		r := st.runs[name]
		st.mu.Unlock()
		if r == nil || r.result == nil {
			if _, err := st.commands.Get(name); err != nil {
				return fmt.Errorf("no command named %q in this scenario; register it first with \"the %s command with the following properties:\"", name, name)
			}
			return fmt.Errorf("the %s command has not run in this scenario; run it with \"the %s command is run\"", name, name)
		}
		return secrets.Hide(sc, fn(sc, name, r, a))
	}
}

// stream is the output a step checks: the error output when it says so.
func stream(a core.Args, r *run) (string, []byte) {
	if strings.Contains(a.Text, "'s error output") {
		return "error output", r.result.Stderr
	}
	return "output", r.result.Stdout
}

// errorOutput is what a failed run printed to its error output, for a
// failure message.
func errorOutput(r *run) string {
	if len(r.result.Stderr) == 0 {
		return ""
	}
	return " Its error output:\n" + indent(tail(r.result.Stderr))
}

// printed is what a stopped command printed, for a failure message.
func printed(res *proc.Result) string {
	var b strings.Builder
	if len(res.Stdout) > 0 {
		b.WriteString("\nIts output:\n" + indent(tail(res.Stdout)))
	}
	if len(res.Stderr) > 0 {
		b.WriteString("\nIts error output:\n" + indent(tail(res.Stderr)))
	}
	return b.String()
}

func indent(s string) string { return "  " + strings.ReplaceAll(s, "\n", "\n  ") }

// firstDifference says where got and want first differ.
func firstDifference(got, want []byte) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	line := bytes.Count(got[:i], []byte("\n")) + 1
	if i == len(got) || i == len(want) {
		return fmt.Sprintf("one ends where the other goes on, at line %d", line)
	}
	return fmt.Sprintf("they first differ at byte %d, line %d", i, line)
}
