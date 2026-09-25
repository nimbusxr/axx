package cli

import (
	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/mcp"
)

func newMCPCmd(app *App) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve axx to coding agents over the Model Context Protocol (stdio)",
		Long: `Start an MCP server on stdin/stdout exposing tools to search and explain
steps, validate features, run scenarios, inspect failures and manage apps.

Add it to your agent, e.g. in .mcp.json:
  {"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcp.Serve(cmd.Context(), mcp.Options{ConfigPath: app.Config, Profile: profile})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "config profile for all tool calls")
	return cmd
}
