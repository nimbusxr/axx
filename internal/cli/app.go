// Package cli wires axx's command-line interface.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
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
}

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
			Data:          data,
		})
	}
	if human != nil {
		return human(a.Stdout)
	}
	return nil
}

func (a *App) writeEnvelope(env Envelope) error {
	return encodeEnvelope(a.Stdout, env)
}

func encodeEnvelope(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

// reportError prints err in the active output mode and returns the exit code.
func (a *App) reportError(err error) exitcode.Code {
	code := axxerr.ExitCode(err)
	var ee EnvelopeError
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		ee = EnvelopeError{Code: ae.Code, Message: ae.Error(), Location: ae.Location, Hint: ae.Hint, Docs: ae.DocsURL()}
	} else {
		ee = EnvelopeError{Message: err.Error()}
	}
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
	if ee.Docs != "" {
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
