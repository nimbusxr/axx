package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/ide"
)

func newIDECmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ide",
		Short: "Generate IDE run/debug configurations from axx.yaml",
		Long: `Generate run and debug configurations for the apps in axx.yaml that have a
debug.debugger block. Re-running updates them; files or entries you edited are
kept. Entries are named "Debugger: <app>" (IntelliJ) and "axx: ..." (VS Code).`,
	}
	var cf configFlags
	gen := func(name, short string) *cobra.Command {
		c := &cobra.Command{
			Use:   name,
			Short: short,
			Args:  wrapArgs(cobra.NoArgs),
			RunE: func(*cobra.Command, []string) error {
				cfg, err := app.loadConfig(&cf)
				if err != nil {
					return err
				}
				var files []ide.File
				switch name {
				case "intellij":
					files, err = ide.IntelliJ(cfg, cfg.Dir)
				default:
					files, err = ide.VSCode(cfg, cfg.Dir)
				}
				if err != nil {
					return err
				}
				return app.Emit(map[string]any{"files": files}, func(w io.Writer) error {
					for _, f := range files {
						if f.Reason != "" {
							fmt.Fprintf(w, "  %-9s %s (%s)\n", f.Action, f.Path, f.Reason)
						} else {
							fmt.Fprintf(w, "  %-9s %s\n", f.Action, f.Path)
						}
					}
					return nil
				})
			},
		}
		cf.register(c)
		return c
	}
	cmd.AddCommand(
		gen("intellij", "Write .run/*.run.xml: Debugger: <app> listeners, axx: run/debug/validate, and a one-click compound"),
		gen("vscode", "Merge axx attach configurations into .vscode/launch.json and tasks into tasks.json"),
	)
	return cmd
}
