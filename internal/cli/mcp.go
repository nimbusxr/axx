package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/agents"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/mcp"
)

func newMCPCmd(app *App) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve axx to coding agents over the Model Context Protocol (stdio)",
		Long: `Start an MCP server on stdin/stdout exposing tools to search and explain
steps, validate features, run scenarios, inspect failures and manage apps.

Connect an agent to it with ` + "`axx mcp install --agent <agent>`" + ` (axx init does it
for the agents a repository already uses), or add it yourself, e.g. in .mcp.json:
  {"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcp.Serve(cmd.Context(), mcp.Options{ConfigPath: app.Config, Profile: profile})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "config profile for all tool calls")
	cmd.AddCommand(newMCPInstallCmd(app))
	return cmd
}

func newMCPInstallCmd(app *App) *cobra.Command {
	var agent, scope string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Add the axx MCP server to a coding agent's configuration",
		Long: `Add the axx MCP server to one agent's configuration, merged into the file the
agent reads: other servers and settings stay as they are, and a file that
already has axx is left alone.

  agent    project scope (the default)   user scope
  claude   .mcp.json                     prints ` + "`claude mcp add --scope user ...`" + `
  codex    .codex/config.toml            ~/.codex/config.toml ($CODEX_HOME)
  cursor   .cursor/mcp.json              ~/.cursor/mcp.json
  vscode   .vscode/mcp.json              prints ` + "`code --add-mcp ...`" + `
  gemini   .gemini/settings.json         ~/.gemini/settings.json

Claude Code keeps its user servers with its own state, and VS Code in its
profile, so for them axx prints the agent's own command instead. Codex loads a
project's .codex/config.toml once you trust the project. Only --scope user
writes to your home directory.`,
		Example: `  axx mcp install --agent claude
  axx mcp install --agent codex --scope user`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			a, ok := agents.Lookup(agent)
			if !ok {
				msg := "--agent is required"
				if agent != "" {
					msg = fmt.Sprintf("unknown agent %q", agent)
				}
				return axxerr.New(agents.CodeUsage, exitcode.Usage, "%s", msg).
					WithHint("use --agent %s", strings.Join(agents.IDs(), ", --agent "))
			}
			dir, err := config.ProjectDir(app.Config)
			if err != nil {
				return err
			}
			ch, err := agents.Install(a, scope, agents.NewEnv(dir), dryRun)
			if err != nil {
				return err
			}
			return app.Emit(map[string]any{"dryRun": dryRun, "change": ch}, func(w io.Writer) error {
				return renderMCPChange(w, a, ch, dryRun)
			})
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "the agent: "+strings.Join(agents.IDs(), ", "))
	cmd.Flags().StringVar(&scope, "scope", agents.ScopeProject, "project (this repository's files) or user (your home directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	return cmd
}

func renderMCPChange(w io.Writer, a agents.Agent, ch agents.Change, dryRun bool) error {
	var err error
	switch ch.Action {
	case agents.ActionManual:
		_, err = fmt.Fprintf(w, "%s; add axx with its own command:\n  %s\n", ch.Reason, ch.Command)
	case agents.ActionSkip:
		_, err = fmt.Fprintf(w, "%s: %s in %s\n", a.Name, ch.Reason, ch.Path)
	default:
		verb := map[bool]string{true: "would " + ch.Action, false: ch.Action}[dryRun]
		_, err = fmt.Fprintf(w, "%s %s: %s\n", verb, ch.Path, serverNote(a, ch))
	}
	return err
}

// serverNote says what a written file gives the agent, and when Codex
// loads a project's configuration.
func serverNote(a agents.Agent, ch agents.Change) string {
	note := "the axx MCP server for " + a.Name
	if a.ID == "codex" && ch.Scope == agents.ScopeProject {
		note += "; Codex loads it once you trust the project"
	}
	return note
}
