package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Identity is the canonical one-liner used everywhere axx describes itself.
const Identity = `axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework for the agentic era.`

// Main runs the CLI with args and returns the process exit code.
func Main(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := Run(ctx, NewApp(), args)
	if file := os.Getenv(envExitFile); file != "" {
		// Running under Delve for --debug-steps, which does not pass the code on.
		_ = os.WriteFile(file, []byte(strconv.Itoa(code)), 0o600)
	}
	return code
}

// Run executes the CLI with an explicit App (for tests and embedding).
func Run(ctx context.Context, app *App, args []string) int {
	root := newRootCmd(app)
	// Honor --json even when cobra fails before parsing flags (unknown command).
	// Must follow newRootCmd: flag definition resets the bound variable.
	app.JSON = slices.Contains(args, "--json")
	root.SetArgs(args)
	root.SetOut(app.Stdout)
	root.SetErr(app.Stderr)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return int(exitcode.OK)
	}
	if ctx.Err() != nil {
		return int(exitcode.Interrupted)
	}
	var se silentExit
	if errors.As(err, &se) {
		return int(se.code)
	}
	if app.command == "" {
		// argument validation fails before PersistentPreRun sets the path
		app.command = "axx"
		if c, _, ferr := root.Find(args); ferr == nil {
			app.command = c.CommandPath()
		}
	}
	var ue usageError
	isUsage := errors.As(err, &ue)
	if isUsage {
		err = ue.err
	}
	var ae *axxerr.Error
	if !errors.As(err, &ae) && (isUsage || !app.started) {
		err = axxerr.Wrap(err, "AXX-E0001", exitcode.Usage, "invalid usage").WithHint("run `%s --help`", app.command)
	}
	return int(app.reportError(err))
}

// usageError marks cobra flag/argument errors so they map to exit code 2.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }

func newRootCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "axx",
		Short: "Human-readable acceptance testing for the agentic era",
		Long: `axx runs acceptance criteria that people can read, written as Gherkin
scenarios, as black-box tests against locally built services: REST + OpenAPI,
WireMock, SQL, MongoDB and Kafka out of the box, extended with packs of your own
steps.

` + Identity,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			app.started = true
			app.command = cmd.CommandPath()
			if !cmd.Flags().Changed("compact") && app.detectAgent() && !stdoutIsTerminal() {
				app.Compact = true
			}
			return app.ensurePacks(cmd.Context(), cmd)
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })

	pf := root.PersistentFlags()
	pf.BoolVar(&app.JSON, "json", false, "emit machine-readable JSON (stable envelope)")
	pf.BoolVar(&app.Compact, "compact", false, "print only what matters (auto-enabled for coding agents)")
	pf.StringVarP(&app.Config, "config", "c", "", "path to axx.yaml (default: search upward from the working directory)")
	pf.CountVarP(&app.Verbose, "verbose", "v", "increase log verbosity (-v, -vv)")
	pf.BoolVar(&app.NoColor, "no-color", os.Getenv("NO_COLOR") != "", "disable colored output")

	root.AddCommand(
		newRunCmd(app),
		newValidateCmd(app),
		newLintCmd(app),
		newFixturesCmd(app),
		newStepsCmd(app),
		newExplainCmd(app),
		newSchemaCmd(app),
		newDocsCmd(app),
		newInitCmd(app),
		newMCPCmd(app),
		newLSPCmd(app),
		newPackCmd(app),
		newSkillsCmd(app),
		newIDECmd(app),
		newDoctorCmd(app),
		newUpCmd(app),
		newDownCmd(app),
		newSuperviseCmd(app),
		newVersionCmd(app),
	)
	return root
}

// wrapArgs converts cobra positional-argument validation errors into usage errors.
func wrapArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}
