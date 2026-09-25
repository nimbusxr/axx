package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/skills"
)

func newSkillsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Install agent skills that teach coding agents to use axx",
		Long: `axx ships Agent Skills (SKILL.md + references) for Claude Code, Codex, Cursor,
Gemini CLI, Copilot and other agents that read .agents/skills. The step
references are generated from this project's steps.`,
	}
	var cf configFlags
	var scope string
	var noClaude, force bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Install or update the skills (project: .agents/skills + .claude/skills)",
		Long: `Write the skills to .agents/skills/ (read by Codex, Cursor, Gemini CLI and
Copilot) and link them into .claude/skills/ for Claude Code. Re-running updates
them; files you edited are kept unless --force.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			var root string
			var cfg *config.Config
			switch scope {
			case "project":
				c, err := app.loadConfig(&cf)
				if err != nil {
					return err
				}
				cfg = c
				root = c.Dir
			case "user":
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				root = home
				cfg = &config.Config{}
			default:
				return axxerr.New("AXX-E0007", exitcode.Usage, "--scope must be project or user")
			}
			e, err := engine.New(engine.Options{Config: cfg})
			if err != nil {
				return err
			}
			sks, err := skills.Build(e)
			if err != nil {
				return err
			}
			res, err := skills.Install(sks, skills.InstallOptions{Root: root, Claude: !noClaude, Force: force})
			if err != nil {
				return err
			}
			return app.Emit(res, func(w io.Writer) error {
				fmt.Fprintf(w, "installed %s to %s (%d files updated)\n", plural(len(res.Skills), "skill"), relPath(res.Dir), len(res.Written))
				if res.Location != "" {
					fmt.Fprintf(w, "linked for Claude Code in %s\n", relPath(res.Location))
				}
				for _, k := range res.Kept {
					fmt.Fprintf(w, "kept your edits: %s (use --force to overwrite)\n", k)
				}
				return nil
			})
		},
	}
	cf.register(install)
	install.Flags().StringVar(&scope, "scope", "project", "project (this repo) or user (your home directory)")
	install.Flags().BoolVar(&noClaude, "no-claude", false, "do not link into .claude/skills")
	install.Flags().BoolVar(&force, "force", false, "overwrite skill files you edited")

	var out string
	export := &cobra.Command{
		Use:    "export",
		Short:  "Write the skills, for the packs compiled into this axx, to a directory (for plugin marketplaces)",
		Hidden: true,
		Args:   wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			e, err := engine.New(engine.Options{Config: &config.Config{}, Packs: engine.CompiledPacks()})
			if err != nil {
				return err
			}
			sks, err := skills.Build(e)
			if err != nil {
				return err
			}
			files, err := skills.Export(sks, out)
			if err != nil {
				return err
			}
			return app.Emit(map[string]any{"files": files}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "wrote %s to %s\n", plural(len(files), "file"), out)
				return err
			})
		},
	}
	export.Flags().StringVarP(&out, "out", "o", "", "output directory")
	_ = export.MarkFlagRequired("out")

	list := &cobra.Command{
		Use:   "list",
		Short: "List the skills axx provides",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			names := skills.Names()
			return app.Emit(map[string]any{"skills": names}, func(w io.Writer) error {
				for _, n := range names {
					fmt.Fprintln(w, n)
				}
				return nil
			})
		},
	}
	cmd.AddCommand(install, export, list)
	return cmd
}
