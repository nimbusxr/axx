package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/version"
)

func newVersionCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the axx version",
		Args:  wrapArgs(cobra.NoArgs),
		RunE:  func(*cobra.Command, []string) error { return printVersion(app) },
	}
}

// printVersion prints what `axx version` and `axx --version` print.
func printVersion(app *App) error {
	info := version.Get()
	if pin, err := config.ReadPin(app.Config); err == nil && pin.File != "" {
		info.Project = pin.Version
	}
	return app.Emit(info, func(w io.Writer) error {
		if _, err := fmt.Fprintf(w, "axx %s %s %s\n", info, info.Platform, shortCommit(info.Commit)); err != nil {
			return err
		}
		if info.Project != "" && info.Project != info.Version {
			_, err := fmt.Fprintf(w, "this project runs on axx %s\n", info.Project)
			return err
		}
		return nil
	})
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
