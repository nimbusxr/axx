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
	var outline bool
	cmd := &cobra.Command{
		Use:   "schema [config|factory|fixture|prototype]",
		Short: "Print the JSON Schema for axx.yaml or a fixture spec file",
		Long: `Print a JSON Schema, for editors and agents: axx.yaml by default (config, or
axx), or factory, fixture or prototype for the *.factory.yaml, *.fixture.yaml
and *.prototype.yaml files of ` + "`axx fixtures`" + `, as an argument or with --kind.
Reference them from the files with:

  # yaml-language-server: $schema=` + config.SchemaID + `
  # yaml-language-server: $schema=` + fixtures.SchemaID("factory"),
		Args: wrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if cmd.Flags().Changed("kind") && args[0] != kind {
					return axxerr.New("AXX-E0001", exitcode.Usage, "invalid usage: the schema kind is %q and --kind %q", args[0], kind).
						WithHint("name the kind once: `axx schema %s`", args[0])
				}
				kind = args[0]
			}
			switch kind {
			case "axx", "axx.yaml":
				kind = "config" // what agents call it
			}
			schema := config.SchemaJSON
			if kind != "" && kind != "config" {
				b, ok := fixtures.SchemaJSON(kind)
				if !ok {
					return axxerr.New("AXX-E0010", exitcode.Usage, "unknown schema kind %q", kind).
						WithHint("use one of: config, %s", strings.Join(fixtures.SchemaKinds, ", "))
				}
				schema = b
			}
			if outline {
				return schemaOutline(app.Stdout, schema)
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
	cmd.Flags().BoolVar(&outline, "outline", false, "print the keys the file may have, nested, each with one line of description (a fraction of the schema's size)")
	return cmd
}
