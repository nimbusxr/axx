package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
	"github.com/nimbusxr/axx/internal/proc"
)

const (
	// CodePackHook: a hook a pack runs before or after a command failed,
	// with an error of its own rather than one of axx's.
	CodePackHook = "AXX-E0022"
	// CodeHookFailed: a hook of axx.yaml (hooks.commands) failed.
	CodeHookFailed = "AXX-E0023"
	// CodeHookCommand: a hook of axx.yaml names no command axx runs hooks
	// with, or runs nothing.
	CodeHookCommand = "AXX-E0106"
)

// packHook is a pack's command hook, with the pack's name.
type packHook struct {
	pack string
	hook core.CommandHook
}

// HookProblem is a pack's command hook that never runs, and why: `axx
// doctor` reports it.
type HookProblem struct {
	Pack    string `json:"pack"`
	Command string `json:"command"`
	Phase   string `json:"phase"`
	Problem string `json:"problem"`
}

// addCommandHooks runs hooks around every command that has them: the
// command hooks of the project's packs, and those of its axx.yaml
// (hooks.commands). Before the command, the packs' hooks run in the order
// the packs load, then axx.yaml's in its order; after it, axx.yaml's in its
// order, then the packs' in the reverse order. The commands of
// commandsWithoutPacks have none. A pack's hook that names no command with
// hooks never runs: `axx doctor` reports it.
func addCommandHooks(app *App, root *cobra.Command) {
	app.root, app.hookProblems = root, nil
	for _, np := range engine.CompiledPacks() {
		for _, h := range np.Pack.Manifest().CommandHooks {
			problem := ""
			switch {
			case h.Phase != core.BeforeCommand && h.Phase != core.AfterCommand:
				problem = "its phase is neither before nor after"
			case h.Run == nil:
				problem = "it has nothing to run"
			default:
				problem = hookProblem(root, h.Command)
			}
			if problem != "" {
				app.hookProblems = append(app.hookProblems, HookProblem{Pack: np.Name, Command: h.Command, Phase: h.Phase.String(), Problem: problem})
			}
		}
	}
	var wrap func(cmd *cobra.Command)
	wrap = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			if cmd == root && slices.Contains(commandsWithoutPacks, sub.Name()) {
				continue
			}
			if sub.RunE != nil || sub.Run != nil {
				wrapWithHooks(app, sub)
			}
			wrap(sub)
		}
	}
	wrap(root)
}

// hookProblem is why a hook's command, by its words after axx, is none of
// the commands with hooks: "" when it is one.
func hookProblem(root *cobra.Command, command string) string {
	words := strings.Fields(command)
	if len(words) == 0 {
		return "it names no command"
	}
	path := "axx " + strings.Join(words, " ")
	cmd, rest, err := root.Find(words)
	if err != nil || len(rest) > 0 || cmd == root || cmd.CommandPath() != path {
		return fmt.Sprintf("axx has no command `%s`", path)
	}
	if cmd.RunE == nil && cmd.Run == nil {
		return fmt.Sprintf("`%s` only groups other commands: name one of them", path)
	}
	if slices.Contains(commandsWithoutPacks, words[0]) {
		return fmt.Sprintf("`%s` has no hooks: it manages axx and its packs", path)
	}
	return ""
}

// wrapWithHooks runs cmd between its hooks.
func wrapWithHooks(app *App, cmd *cobra.Command) {
	run := cmd.RunE
	if run == nil {
		run = runE(cmd.Run)
		cmd.Run = nil
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		call, cfg, err := app.hookCall(c, args)
		if err != nil {
			// Without its axx.yaml there are no hooks to run: the command
			// reports what is wrong with the file, when it reads it.
			return run(c, args)
		}
		var packs []packHook
		if !app.withoutPackHooks {
			packs = projectHooks(cfg, call.Command)
		}
		own, err := configHooks(c.Root(), cfg, call.Command, call.Command != "doctor")
		if err != nil {
			return err
		}
		if len(packs) == 0 && len(own) == 0 {
			return run(c, args)
		}
		settings := func(pack string, v any) error { return packSettings(cfg, pack, v) }
		// What the hooks before it tell axx is the command's, until it ends.
		app.hookTold = nil
		defer func() {
			for _, t := range app.hookTold {
				_ = os.Remove(t.Path)
			}
			app.hookTold = nil
		}()
		for _, h := range packs {
			if h.hook.Phase == core.BeforeCommand {
				if err := runHook(h, call, settings); err != nil {
					return err
				}
			}
		}
		for _, h := range own {
			if h.Before != "" {
				if err := app.runConfigHook(call, cfg, h); err != nil {
					return err
				}
			}
		}
		// With --json, the command's answer waits for its after hooks.
		app.holding = app.JSON && hasAfterHooks(packs, own)
		err = run(c, args)
		app.holding = false
		call.Err = err
		var failed error // an after hook's, of a command that succeeded
		after := func(herr error) {
			switch {
			case herr == nil:
			case err == nil && failed == nil:
				failed = herr
			default:
				// The command's own error, or the first hook's, is what it ends with.
				fmt.Fprintf(app.Stderr, "warning: %s\n", hookMessage(herr))
			}
		}
		for _, h := range own {
			if h.After != "" {
				after(app.runConfigHook(call, cfg, h))
			}
		}
		for _, h := range slices.Backward(packs) {
			if h.hook.Phase == core.AfterCommand {
				after(runHook(h, call, settings))
			}
		}
		return app.release(err, failed)
	}
}

// hasAfterHooks reports whether any of the hooks runs after its command.
func hasAfterHooks(packs []packHook, own []configHook) bool {
	return slices.ContainsFunc(packs, func(h packHook) bool { return h.hook.Phase == core.AfterCommand }) ||
		slices.ContainsFunc(own, func(h configHook) bool { return h.After != "" })
}

// release writes the --json answer held for after hooks, and returns what
// the command ends with: its own error, or failed, an after hook's. A
// command that answered and then failed in a hook answers once, with the
// hook's error in its answer.
func (a *App) release(err, failed error) error {
	held := a.held
	a.held = nil
	if failed == nil {
		for _, env := range held {
			if werr := a.writeEnvelope(env); werr != nil {
				return werr
			}
		}
		return err
	}
	if len(held) == 0 {
		return failed
	}
	held[len(held)-1].OK = false
	held[len(held)-1].Errors = append(held[len(held)-1].Errors, envelopeError(failed))
	for _, env := range held {
		if werr := a.writeEnvelope(env); werr != nil {
			return werr
		}
	}
	return silentExit{code: axxerr.ExitCode(failed)}
}

// runE is a command's Run as a RunE, which ends without an error.
func runE(run func(*cobra.Command, []string)) func(*cobra.Command, []string) error {
	return func(c *cobra.Command, args []string) error {
		run(c, args)
		return nil
	}
}

// projectHooks are the hooks of command (its words after axx) of the packs
// the project cfg configures uses, in the order the packs load. A pack this
// axx has not got has no hooks here: a command that needs the packs says
// so.
func projectHooks(cfg *config.Config, command string) []packHook {
	packs, err := engine.ProjectPacks(cfg)
	if err != nil {
		return nil
	}
	var out []packHook
	for _, np := range packs {
		for _, h := range np.Pack.Manifest().CommandHooks {
			if strings.Join(strings.Fields(h.Command), " ") == command && h.Run != nil &&
				(h.Phase == core.BeforeCommand || h.Phase == core.AfterCommand) {
				out = append(out, packHook{pack: np.Name, hook: h})
			}
		}
	}
	return out
}

// configHook is a hook of axx.yaml, with its place in hooks.commands.
type configHook struct {
	config.CommandHook
	index int
}

// configHooks are axx.yaml's hooks of command, in their order. A hook that
// names no command with hooks is an error, unless strict is false: then it
// is left out, as `axx doctor` lists it.
func configHooks(root *cobra.Command, cfg *config.Config, command string, strict bool) ([]configHook, error) {
	var out []configHook
	for i, h := range cfg.Hooks.Commands {
		name, when := h.Names()
		if problem := hookProblem(root, name); problem != "" {
			if !strict {
				continue
			}
			return nil, hookCommandError(cfg, i, when, problem)
		}
		if name == command {
			out = append(out, configHook{CommandHook: h, index: i})
		}
	}
	return out, nil
}

// hookCommandError is the error of axx.yaml's hook i, whose command (before
// or after) is no command with hooks.
func hookCommandError(cfg *config.Config, i int, when, problem string) error {
	e := axxerr.New(CodeHookCommand, exitcode.Usage, "hooks.commands[%d].%s: %s", i, when, problem).
		WithHint("name an axx command by its words after axx, as `axx --help` lists them: every command has hooks but pack, init, upgrade, install and version")
	if loc, ok := cfg.Position("hooks", "commands", strconv.Itoa(i), when); ok {
		e = e.At(loc.File, loc.Line, loc.Column)
	}
	return e
}

// runConfigHook runs a hook of axx.yaml, from its directory, with its output
// on axx's stderr.
func (a *App) runConfigHook(call *core.CommandHookCall, cfg *config.Config, h configHook) error {
	name, when := h.Names()
	where := fmt.Sprintf("the hook %s `axx %s` (hooks.commands[%d])", when, name, h.index)
	argv, err := lifecycle.CommandArgv(h.Command, h.Shell)
	if err != nil {
		e := axxerr.New(CodeHookCommand, exitcode.Usage, "%s runs nothing", where).WithHint("give it a command to run")
		if loc, ok := cfg.Position("hooks", "commands", strconv.Itoa(h.index), "command"); ok {
			e = e.At(loc.File, loc.Line, loc.Column)
		}
		return e
	}
	fmt.Fprintf(a.Stderr, "axx: running %s %s `axx %s`\n", h.Command, when, name)
	env := append(os.Environ(), "AXX_COMMAND="+name)
	if when == "after" {
		env = append(env, "AXX_COMMAND_FAILED="+strconv.FormatBool(call.Err != nil))
	} else {
		// A hook before the command tells axx properties for its scenarios, as
		// a service does.
		told := lifecycle.PropertiesFile(lifecycle.PropertiesDir(cfg.Dir), fmt.Sprintf("hooks.commands.%d", h.index))
		if err := lifecycle.WriteTold(told, nil); err != nil {
			return axxerr.Wrap(err, CodeHookFailed, exitcode.Environment, "%s could not start", where)
		}
		a.hookTold = append(a.hookTold, lifecycle.Told{Who: where, Path: told})
		env = append(env, lifecycle.PropertiesEnv+"="+told)
	}
	ctx := call.Context
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := proc.Run(ctx, proc.Spec{Argv: argv, Dir: cfg.Dir, Env: env, Output: a.Stderr, Limit: 1})
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return axxerr.Wrap(err, CodeHookFailed, exitcode.Environment, "%s could not start", where).
			WithHint("check its command: it runs from the directory of axx.yaml")
	case res.ExitCode != 0:
		return axxerr.New(CodeHookFailed, exitcode.Environment, "%s exited with %d: %s", where, res.ExitCode, h.Command).
			WithHint("its output is above: fix what it says, or the hook in axx.yaml")
	}
	return nil
}

// hookMessage is a hook's error in one line, with its code.
func hookMessage(err error) string {
	var ae *axxerr.Error
	if errors.As(err, &ae) && ae.Code != "" {
		return fmt.Sprintf("%s [%s]", ae.Error(), ae.Code)
	}
	return err.Error()
}

// runHook runs h with call, and settings for its pack's section of axx.yaml.
func runHook(h packHook, call *core.CommandHookCall, settings func(pack string, v any) error) error {
	own := *call
	own.Settings = func(v any) error { return settings(h.pack, v) }
	err := h.hook.Run(&own)
	if err == nil {
		return nil
	}
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return err
	}
	return axxerr.Wrap(err, CodePackHook, exitcode.Environment, "the %s pack's hook %s `axx %s`", h.pack, h.hook.Phase, call.Command).
		WithHint("the %s pack runs it %s `axx %s`, and its message says what went wrong; see the pack's reference", h.pack, h.hook.Phase, call.Command)
}

// hookCall is what cmd's hooks are told: the command, its arguments and
// flags, and the project, whose axx.yaml it reads as the command does.
func (a *App) hookCall(cmd *cobra.Command, args []string) (*core.CommandHookCall, *config.Config, error) {
	call := &core.CommandHookCall{
		Context: cmd.Context(),
		Command: strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" "),
		Args:    args,
		Flags:   map[string]string{},
		Lists:   map[string][]string{},
		Stderr:  a.Stderr,
		Logger:  a.logger(),
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		list, ok := f.Value.(pflag.SliceValue)
		if !ok {
			call.Flags[f.Name] = f.Value.String()
			return
		}
		values := list.GetSlice()
		call.Lists[f.Name] = values
		call.Flags[f.Name] = ""
		if len(values) > 0 {
			call.Flags[f.Name] = values[len(values)-1]
		}
	})
	cfg, err := a.loadConfig(&configFlags{profile: call.Flags["profile"], defines: call.Lists["define"], settings: call.Lists["set"]})
	if err != nil {
		return nil, nil, err
	}
	if cfg.File != "" {
		call.ProjectDir = cfg.Dir
	}
	return call, cfg, nil
}

// packSettings decodes pack's section of cfg into v, leaving v as it is when
// there is none.
func packSettings(cfg *config.Config, pack string, v any) error {
	raw := cfg.Packs[pack]
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("packs.%s: %w", pack, err)
	}
	return nil
}

// hookChecks is `axx doctor`'s line of the project's command hooks, its
// packs' and its axx.yaml's: those that run, and those that never do. A
// project without hooks has none.
func hookChecks(root *cobra.Command, cfg *config.Config, packs []engine.NamedPack, all []HookProblem) (name, status, detail, hint string) {
	var runs, never []string
	status = "ok"
	for _, np := range packs {
		for _, h := range np.Pack.Manifest().CommandHooks {
			i := slices.IndexFunc(all, func(p HookProblem) bool {
				return p.Pack == np.Name && p.Command == h.Command && p.Phase == h.Phase.String()
			})
			if i < 0 {
				runs = append(runs, fmt.Sprintf("%s %s `axx %s`", np.Name, h.Phase, strings.Join(strings.Fields(h.Command), " ")))
				continue
			}
			never = append(never, fmt.Sprintf("the %s pack's hook of `%s` never runs: %s", np.Name, h.Command, all[i].Problem))
			status = "warn"
		}
	}
	for i, h := range cfg.Hooks.Commands {
		command, when := h.Names()
		if problem := hookProblem(root, command); problem != "" {
			never = append(never, fmt.Sprintf("hooks.commands[%d].%s: %s", i, when, problem))
			status = "fail"
			continue
		}
		runs = append(runs, fmt.Sprintf("%s %s `axx %s`", h.Command, when, command))
	}
	if len(runs) == 0 && len(never) == 0 {
		return "", "", "", ""
	}
	detail = strings.Join(append(runs, never...), "; ")
	if status != "ok" {
		hint = "a hook names a command by its words after axx, as `axx --help` lists them: every command has hooks but pack, init, upgrade, install and version"
	}
	return "command hooks", status, detail, hint
}
