package cli

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// CodePackCommand: a command a pack adds failed, with an error of its own
// rather than one of axx's.
const CodePackCommand = "AXX-E0016"

// addPackCommands adds the commands of the packs compiled into this axx to
// root, after axx's own. A command named like one root has is left out: axx's
// own commands, then the packs' in their order, come first.
func addPackCommands(app *App, root *cobra.Command) {
	taken := map[string]bool{"help": true, "completion": true}
	for _, c := range root.Commands() {
		taken[c.Name()] = true
	}
	for _, np := range engine.CompiledPacks() {
		for _, c := range np.Pack.Manifest().Commands {
			if c.Name == "" || taken[c.Name] {
				continue
			}
			taken[c.Name] = true
			root.AddCommand(packCommand(app, np.Name, c))
		}
	}
}

// packCommand is a pack's command as one of axx's.
func packCommand(app *App, pack string, c core.Command) *cobra.Command {
	use := c.Name
	if c.Args != "" {
		use += " " + c.Args
	}
	cmd := &cobra.Command{Use: use, Short: c.Short, Long: c.Long, Annotations: map[string]string{"pack": pack}}
	values := map[string]func() string{}
	lists := map[string]func() []string{}
	for _, f := range c.Flags {
		if f.Repeated {
			v := cmd.Flags().StringArrayP(f.Name, f.Short, nil, f.Usage)
			lists[f.Name] = func() []string { return *v }
			values[f.Name] = func() string {
				if len(*v) == 0 {
					return f.Default
				}
				return (*v)[len(*v)-1]
			}
			continue
		}
		if f.Bool {
			def, _ := strconv.ParseBool(f.Default)
			v := cmd.Flags().BoolP(f.Name, f.Short, def, f.Usage)
			values[f.Name] = func() string { return strconv.FormatBool(*v) }
			continue
		}
		v := cmd.Flags().StringP(f.Name, f.Short, f.Default, f.Usage)
		values[f.Name] = func() string { return *v }
	}
	for _, sub := range c.Commands {
		cmd.AddCommand(packCommand(app, pack, sub))
	}
	if c.Run == nil {
		return cmd
	}
	cmd.Args = wrapArgs(argsBetween(c.MinArgs, c.MaxArgs))
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		flags := make(map[string]string, len(values))
		for name, v := range values {
			flags[name] = v()
		}
		repeated := make(map[string][]string, len(lists))
		for name, v := range lists {
			repeated[name] = v()
		}
		err := c.Run(&core.CommandCall{
			Context: cmd.Context(), Args: args, Flags: flags, Lists: repeated,
			Stderr: app.Stderr, Logger: app.logger(), Emit: app.Emit,
		})
		var ae *axxerr.Error
		if err == nil || errors.As(err, &ae) {
			return err
		}
		return axxerr.Wrap(err, CodePackCommand, exitcode.Usage, "%s", cmd.CommandPath()).
			WithHint("the %s pack's command says what went wrong; `%s --help` shows what it takes", pack, cmd.CommandPath())
	}
	return cmd
}

// argsBetween takes from min to max arguments; a max of -1 takes any number.
func argsBetween(minArgs, maxArgs int) cobra.PositionalArgs {
	if maxArgs < 0 {
		return cobra.MinimumNArgs(minArgs)
	}
	if minArgs == maxArgs {
		return cobra.ExactArgs(minArgs)
	}
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minArgs || len(args) > maxArgs {
			return fmt.Errorf("accepts between %d and %d arg(s), received %d", minArgs, maxArgs, len(args))
		}
		return nil
	}
}
