package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
	found, _, ferr := root.Find(args)
	if ferr != nil {
		found = nil
	}
	if app.command == "" {
		// argument validation fails before PersistentPreRun sets the path
		app.command = "axx"
		if found != nil {
			app.command = found.CommandPath()
		}
	}
	var ue usageError
	isUsage := errors.As(err, &ue)
	if isUsage {
		err = ue.err
	}
	var ae *axxerr.Error
	if !errors.As(err, &ae) && (isUsage || !app.started) {
		err = axxerr.Wrap(err, "AXX-E0001", exitcode.Usage, "invalid usage").WithHint("%s", usageHint(found, app.command, err))
	}
	return int(app.reportError(err))
}

// unknownCommand reads the word of cobra's "unknown command" error.
var unknownCommand = regexp.MustCompile(`^unknown command "([^"]*)" for `)

// usageHint is the hint of a usage error: for a word that is not one of
// the command's subcommands, the closest one and what the command does
// take; always, where its help is.
func usageHint(c *cobra.Command, path string, err error) string {
	help := fmt.Sprintf("run `%s --help`", path)
	m := unknownCommand.FindStringSubmatch(err.Error())
	if c == nil || m == nil {
		return help
	}
	var subs []string
	for _, s := range c.Commands() {
		if s.IsAvailableCommand() {
			subs = append(subs, s.Name())
		}
	}
	var hint string
	if len(subs) > 0 {
		hint = fmt.Sprintf("`%s` has these commands: %s", path, strings.Join(subs, ", "))
		if sug := c.SuggestionsFor(m[1]); len(sug) > 0 {
			hint = fmt.Sprintf("did you mean `%s %s`? ", path, sug[0]) + hint
		}
	} else {
		var flags []string
		c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden && f.Name != "help" {
				flags = append(flags, "--"+f.Name)
			}
		})
		hint = fmt.Sprintf("`%s` takes no arguments", path)
		if c.Short != "" {
			hint += ": " + strings.ToLower(c.Short[:1]) + c.Short[1:]
		}
		if len(flags) > 0 {
			hint += " (flags: " + strings.Join(flags, ", ") + ")"
		}
	}
	return hint + "; " + help
}

// usageError marks cobra flag/argument errors so they map to exit code 2.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }

func newRootCmd(app *App) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "axx",
		Short: "Human-readable acceptance testing for the agentic era",
		Long: `axx runs acceptance criteria that people can read as black-box tests against
locally built services and web apps. Its packs cover REST and OpenAPI, WireMock,
SQL, MongoDB, Kafka, logs, files, web apps in real browsers, and AWS, Google Cloud
and Azure services: a project lists those it uses in axx-packs.yaml, beside packs
of its own steps.

Writing tests:
  axx steps                every step of the project's packs, one line each
  axx steps show <id>      one step's documentation and examples
  axx up                   start the services once and keep them running
  axx run --compact        run the features; also reports what validate and lint find
  (axx validate checks the features without running them)

Docs: https://axx.nimbusxr.us (every page as Markdown: add .md to its path; llms.txt lists them)

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
		// `axx --version` is `axx version`; `axx` alone prints the help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				return printVersion(app)
			}
			return cmd.Help()
		},
	}
	root.Flags().BoolVar(&showVersion, "version", false, "print the axx version (as `axx version`)")
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
		newEnvCmd(app),
		newConfigCmd(app),
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
