// Package cli wires axx's command-line interface.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
	"github.com/nimbusxr/axx/internal/progress"
)

// EnvelopeSchemaVersion is the version of the --json output envelope.
const EnvelopeSchemaVersion = 1

// Envelope wraps every --json response so agents can parse any command
// uniformly (see docs/adr/0003-cli-contract.md).
type Envelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Command       string          `json:"command"`
	OK            bool            `json:"ok"`
	Data          any             `json:"data,omitempty"`
	Errors        []EnvelopeError `json:"errors,omitempty"`
}

// EnvelopeError is the JSON form of an error.
type EnvelopeError struct {
	Code     string           `json:"code,omitempty"`
	Message  string           `json:"message"`
	Location *axxerr.Location `json:"location,omitempty"`
	Hint     string           `json:"hint,omitempty"`
	Docs     string           `json:"docs,omitempty"`
}

// App carries process-wide state shared by all commands.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Env    func(string) string

	JSON    bool
	Compact bool
	Config  string
	Verbose int
	NoColor bool

	// command path of the executing command, e.g. "axx steps search".
	command string
	// started is set once a command's PersistentPreRun ran (flags parsed).
	started bool
	// notPrepared is why the project's packs could not be prepared, for a
	// command that answers without them (commandsFromReference); note says
	// so, in what the command answers.
	notPrepared error
	note        string
	// notedExperimental is set once the command said which of the packs it
	// uses are experimental.
	notedExperimental bool
	// hookProblems are the packs' command hooks that never run, for `axx
	// doctor`.
	hookProblems []HookProblem
	// withoutPackHooks is set when the project's packs could not be
	// prepared for a command that runs without them: it runs without their
	// hooks.
	withoutPackHooks bool
	// root is the command line's root command, which hooks are checked
	// against.
	root *cobra.Command
	// holding keeps the --json envelopes a command writes in held, until
	// its after hooks have run: one that fails is in the command's answer.
	holding bool
	held    []Envelope
	// hookTold is where the hooks before the command told axx properties,
	// for the scenarios of `axx run`.
	hookTold []lifecycle.Told
	// args are axx's arguments, as the command line gave them: a pack that
	// relaunches a command (core.Relauncher) runs it again with them.
	args []string
	// reporter says what axx is doing while a step takes time (report).
	reporter *progress.Reporter
	// afterCommand runs once the command and its hooks have ended, with
	// the error they ended with, and gives the one axx ends with: the axx
	// host becomes another version of axx there, after its after hooks.
	afterCommand func(err error) error
}

// report is where axx says what it is doing while a step takes time: on
// its standard error. A person at a terminal sees a step's line live; an
// agent, CI, a pipe and --json get plain lines, and one each half minute
// for a step that goes on (internal/progress).
func (a *App) report() *progress.Reporter {
	if a.reporter == nil {
		opts := progress.Options{}
		if f, ok := a.Stderr.(*os.File); ok && !a.JSON && a.livesAtATerminal() && term.IsTerminal(int(f.Fd())) {
			fd := int(f.Fd())
			opts.Live = true
			opts.Width = func() int {
				w, _, _ := term.GetSize(fd)
				return w
			}
			// The old Windows console has no braille.
			opts.ASCII = runtime.GOOS == "windows" && a.getenv("WT_SESSION") == ""
		}
		a.reporter = progress.New(a.Stderr, opts)
	}
	return a.reporter
}

// livesAtATerminal is whether what runs axx may be a person: not an agent,
// not CI, not a terminal that cannot rewrite a line.
func (a *App) livesAtATerminal() bool {
	if a.getenv("CI") != "" || a.getenv("TERM") == "dumb" {
		return false
	}
	for _, k := range agentEnvVars {
		if a.getenv(k) != "" {
			return false
		}
	}
	return true
}

// step says that axx begins something that can take a while (line, as it
// stays on the screen; short, the few words of "axx: still ..."). Done ends it.
func (a *App) step(line, short string) *progress.Step { return a.report().Start(line, short) }

// stepIfSlow is step for what is quick most times: it says nothing unless
// it goes on for a second.
func (a *App) stepIfSlow(line, short string) *progress.Step { return a.report().IfSlow(line, short) }

// NewApp returns an App bound to the process's standard streams.
func NewApp() *App {
	return &App{Stdout: os.Stdout, Stderr: os.Stderr, Env: os.Getenv}
}

// Emit writes a command result: the JSON envelope in --json mode, otherwise
// the human rendering produced by human (which may be nil).
func (a *App) Emit(data any, human func(w io.Writer) error) error {
	return a.EmitResult(data, true, human)
}

// EmitResult is Emit for commands whose outcome can be unsuccessful without
// an error (e.g. validation problems): ok is reported in the envelope and
// must agree with the exit code.
func (a *App) EmitResult(data any, ok bool, human func(w io.Writer) error) error {
	if a.JSON {
		return a.writeEnvelope(Envelope{
			SchemaVersion: EnvelopeSchemaVersion,
			Command:       a.command,
			OK:            ok,
			Data:          a.withNote(data),
		})
	}
	if human != nil {
		return human(a.Stdout)
	}
	return nil
}

// emitWithError ends a command with err after giving what it did find: both
// in one envelope in --json mode, otherwise the human rendering and the error
// as ever.
func (a *App) emitWithError(data any, human func(w io.Writer) error, err error) error {
	if !a.JSON {
		if herr := human(a.Stdout); herr != nil {
			return herr
		}
		return err
	}
	if werr := a.writeEnvelope(Envelope{
		SchemaVersion: EnvelopeSchemaVersion,
		Command:       a.command,
		OK:            false,
		Data:          a.withNote(data),
		Errors:        []EnvelopeError{envelopeError(err)},
	}); werr != nil {
		return werr
	}
	return silentExit{code: axxerr.ExitCode(err)}
}

// withNote adds what a command's answer needs saying to its data, whatever
// its shape.
func (a *App) withNote(data any) any {
	if a.note == "" {
		return data
	}
	var m map[string]any
	b, err := json.Marshal(data)
	if err != nil || json.Unmarshal(b, &m) != nil || m == nil {
		return data
	}
	m["note"] = a.note
	return m
}

func (a *App) writeEnvelope(env Envelope) error {
	if a.holding {
		a.held = append(a.held, env)
		return nil
	}
	return encodeEnvelope(a.Stdout, env)
}

func encodeEnvelope(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

// envelopeError is the JSON form of err.
func envelopeError(err error) EnvelopeError {
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return EnvelopeError{Code: ae.Code, Message: ae.Error(), Location: ae.Location, Hint: ae.Hint, Docs: ae.DocsURL()}
	}
	return EnvelopeError{Message: err.Error()}
}

// reportError prints err in the active output mode and returns the exit code.
func (a *App) reportError(err error) exitcode.Code {
	code := axxerr.ExitCode(err)
	ee := envelopeError(err)
	if a.JSON {
		_ = a.writeEnvelope(Envelope{
			SchemaVersion: EnvelopeSchemaVersion,
			Command:       a.command,
			OK:            false,
			Errors:        []EnvelopeError{ee},
		})
		return code
	}
	prefix := "error"
	if ee.Code != "" {
		prefix = "error[" + ee.Code + "]"
	}
	fmt.Fprintf(a.Stderr, "%s: %s\n", prefix, ee.Message)
	if ee.Hint != "" {
		fmt.Fprintf(a.Stderr, "  hint: %s\n", ee.Hint)
	}
	switch {
	case ee.Docs != "" && a.Compact:
		// The same page, offline and at this axx's version.
		fmt.Fprintf(a.Stderr, "  more: `axx explain %s`\n", ee.Code)
	case ee.Docs != "":
		fmt.Fprintf(a.Stderr, "  docs: %s\n", ee.Docs)
	}
	return code
}

// agentEnvVars are set by coding agents; when present and stdout is not a
// terminal, axx defaults to compact, token-efficient output.
var agentEnvVars = []string{"AI_AGENT", "AGENT", "CLAUDECODE", "CODEX_SANDBOX", "GEMINI_CLI", "CURSOR_AGENT"}

func (a *App) detectAgent() bool {
	for _, k := range agentEnvVars {
		if a.Env(k) != "" {
			return true
		}
	}
	return false
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
