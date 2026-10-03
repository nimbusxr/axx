package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/lifecycle"
	"github.com/nimbusxr/axx/internal/mcp"
)

// newEnvCmd is the command line's twin of the MCP server's env tool: the
// apps axx started, and up and down to start and stop them.
func newEnvCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   "env",
		Short: "The apps axx started (running, left over, not cleaned up); env up and env down start and stop them",
		Long: `Report the apps axx started from axx.yaml, as the axx MCP server's env tool
does: running (axx up, or a run, still holds them), left over (they run, but
what started them does not: axx down stops them) or not cleaned up (their
cleanup failed or never ran: axx down runs it). env up and env down are
axx up and axx down.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			apps, err := lifecycle.Status(lifecycle.StateFile(cfg.Dir))
			if err != nil {
				return err
			}
			if apps == nil {
				apps = []lifecycle.AppStatus{}
			}
			out := map[string]any{"apps": apps}
			if st, ok := liveUp(cfg); ok {
				out["up"] = st.Apps
			}
			return app.Emit(out, func(w io.Writer) error {
				if len(apps) == 0 {
					_, err := fmt.Fprintln(w, "no apps started by axx are running or left behind")
					return err
				}
				for _, a := range apps {
					fmt.Fprintf(w, "%s: %s", a.Name, a.State)
					if a.PID > 0 {
						fmt.Fprintf(w, " (pid %d)", a.PID)
					}
					fmt.Fprintln(w)
				}
				return nil
			})
		},
	}
	cf.register(cmd)
	up, down := newUpCmd(app), newDownCmd(app)
	cmd.AddCommand(up, down)
	return cmd
}

// newConfigCmd is the command line's twin of the MCP server's config_show
// tool: the effective configuration, secrets masked, and the packs.
func newConfigCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   "config",
		Short: "The project's configuration: config show",
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the effective axx.yaml (profiles and -D applied, secrets masked) and the project's packs",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(_ *cobra.Command, _ []string) error {
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			b, err := json.Marshal(e.Config)
			if err != nil {
				return err
			}
			var cfg any
			if err := json.Unmarshal(mcp.Redact(b), &cfg); err != nil {
				return err
			}
			out := map[string]any{"file": e.Config.File, "config": cfg, "packs": e.PackNames()}
			return app.Emit(out, func(w io.Writer) error {
				file := e.Config.File
				if file == "" {
					file = "(no axx.yaml: the defaults)"
				}
				fmt.Fprintf(w, "file:  %s\npacks: %s\n\n", relPath(file), strings.Join(e.PackNames(), ", "))
				pretty, err := json.MarshalIndent(cfg, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(w, string(pretty))
				return err
			})
		},
	}
	cf.register(show)
	cmd.AddCommand(show)
	return cmd
}
