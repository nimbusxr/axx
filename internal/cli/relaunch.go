package cli

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// CodeRelaunch: a pack could not run a command again in another process.
const CodeRelaunch = "AXX-E0024"

// addRelaunch lets the project's packs run a command again in another
// process (core.Relauncher): desktop-macos runs the commands that drive Mac
// apps under axx's launcher (ADR 0016). It wraps every command that has
// packs, outside its hooks, so that the command run again runs its hooks
// itself, once. An axx without a pack that relaunches leaves its commands as
// they are.
func addRelaunch(app *App, root *cobra.Command) {
	relaunches := slices.ContainsFunc(engine.CompiledPacks(), func(np engine.NamedPack) bool {
		_, ok := np.Pack.(core.Relauncher)
		return ok
	})
	if !relaunches {
		return
	}
	var wrap func(cmd *cobra.Command)
	wrap = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			if cmd == root && slices.Contains(commandsWithoutPacks, sub.Name()) {
				continue
			}
			if sub.RunE != nil || sub.Run != nil {
				wrapWithRelaunch(app, sub)
			}
			wrap(sub)
		}
	}
	wrap(root)
}

// wrapWithRelaunch has the project's packs relaunch cmd, or runs it here.
func wrapWithRelaunch(app *App, cmd *cobra.Command) {
	run := cmd.RunE
	if run == nil {
		run = runE(cmd.Run)
		cmd.Run = nil
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		call, cfg, err := app.hookCall(c, args)
		if err != nil {
			// Without its axx.yaml there is no project, and no pack of it.
			return run(c, args)
		}
		packs, err := engine.ProjectPacks(cfg)
		if err != nil {
			return run(c, args)
		}
		for _, np := range packs {
			r, ok := np.Pack.(core.Relauncher)
			if !ok {
				continue
			}
			code, relaunched, err := r.Relaunch(c.Context(), call.Command, app.args)
			if err != nil {
				return axxerr.Wrap(err, CodeRelaunch, exitcode.Environment, "the %s pack could not run `axx %s` again", np.Name, call.Command).
					WithHint("the %s pack runs `axx %s` in another process, and its message says why it could not; `axx explain %s` says more", np.Name, call.Command, CodeRelaunch)
			}
			if relaunched {
				// The command run again has said what it had to say.
				if code == 0 {
					return nil
				}
				return silentExit{code: exitcode.Code(code)}
			}
		}
		return run(c, args)
	}
}
