package cli

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/fixtures"
)

func newSchemaCmd(app *App) *cobra.Command {
	var out, kind string
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for axx.yaml or a fixture spec file",
		Long: `Print a JSON Schema, for editors and agents: axx.yaml by default, or with
--kind factory|fixture|prototype the *.factory.yaml, *.fixture.yaml and
*.prototype.yaml files of ` + "`axx fixtures`" + `. Reference them from the files with:

  # yaml-language-server: $schema=` + config.SchemaID + `
  # yaml-language-server: $schema=` + fixtures.SchemaID("factory"),
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			schema := config.SchemaJSON
			if kind != "" && kind != "config" {
				b, ok := fixtures.SchemaJSON(kind)
				if !ok {
					return axxerr.New("AXX-E0010", exitcode.Usage, "unknown schema kind %q", kind).
						WithHint("use one of: config, %s", strings.Join(fixtures.SchemaKinds, ", "))
				}
				schema = b
			}
			if out != "" {
				return os.WriteFile(out, schema, 0o644)
			}
			if app.JSON {
				return app.Emit(json.RawMessage(schema), nil)
			}
			_, err := app.Stdout.Write(schema)
			return err
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "write to a file instead of stdout")
	cmd.Flags().StringVar(&kind, "kind", "config", "which schema: config (axx.yaml), factory, fixture or prototype")
	return cmd
}
