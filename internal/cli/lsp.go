package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/lsp"
	"github.com/nimbusxr/axx/internal/version"
)

func newLSPCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Serve feature-file editing to editors over the Language Server Protocol (stdio)",
		Long: `Start a language server on stdin/stdout for feature files: undefined,
ambiguous and misused steps and Gherkin syntax errors as you type, step
completion, step documentation on hover, going to a step's definition,
highlighted step parameters, and links to, path completion for and warnings
about missing files that steps name.

Editors start it themselves: the axx plugin for IntelliJ IDEA and the axx
extension for VS Code do, and other editors can run "axx lsp" for *.feature
files. The steps come from the project's packs, custom packs included; restart
the server after changing a custom pack.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return lsp.Serve(cmd.Context(), os.Stdin, os.Stdout, lsp.Options{Version: version.Get().Version, Logger: app.logger()})
		},
	}
	// Many language clients pass --stdio; stdio is the only transport.
	cmd.Flags().Bool("stdio", true, "communicate over stdin and stdout (the default and only transport)")
	_ = cmd.Flags().MarkHidden("stdio")
	return cmd
}
