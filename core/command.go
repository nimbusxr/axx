package core

import (
	"context"
	"io"
	"log/slog"
)

// Command is a command a pack adds to axx's command line, like `axx models
// build`: an axx built with the pack has it beside its own commands. A
// command named like one of axx's own, or like one an earlier pack added, is
// left out.
type Command struct {
	// Name is the word that runs the command: "models".
	Name string `json:"name"`
	// Args shows its arguments in its usage line: "<stub>", "[file...]".
	Args string `json:"args,omitempty"`
	// Short says what it does, in a line; Long says more, for its help.
	Short string `json:"short"`
	Long  string `json:"long,omitempty"`
	// MinArgs and MaxArgs bound how many arguments it takes; a MaxArgs of -1
	// takes any number.
	MinArgs int    `json:"-"`
	MaxArgs int    `json:"-"`
	Flags   []Flag `json:"flags,omitempty"`
	// Commands are its subcommands. A command with subcommands and no Run
	// only groups them.
	Commands []Command `json:"commands,omitempty"`
	// Run runs it. A returned error is reported as axx reports its own, with
	// the command's exit code.
	Run func(call *CommandCall) error `json:"-"`
}

// Flag is a flag of a pack's command.
type Flag struct {
	// Name is its long form, --name.
	Name string `json:"name"`
	// Short is its one-letter form, like "o" for -o.
	Short string `json:"short,omitempty"`
	Usage string `json:"usage"`
	// Default is its value when the command line does not give it.
	Default string `json:"default,omitempty"`
	// Bool marks a flag that takes no value: given, it is "true".
	Bool bool `json:"bool,omitempty"`
	// Repeated marks a flag given as often as it takes values, like
	// --input a --input b: its values are the call's Lists, in their order.
	Repeated bool `json:"repeated,omitempty"`
}

// CommandCall is a run of a pack's command.
type CommandCall struct {
	Context context.Context
	// Args are the command's arguments.
	Args []string
	// Flags are the values of its flags, given or default, by name: of a
	// repeated flag, its last.
	Flags map[string]string
	// Lists are the values of its repeated flags, in the order given, by
	// name.
	Lists map[string][]string
	// Stderr is where it writes what is not its result, such as progress.
	Stderr io.Writer
	Logger *slog.Logger
	// Emit reports the command's result: with --json, data in axx's JSON
	// envelope; otherwise what human writes.
	Emit func(data any, human func(w io.Writer) error) error
}

// CommandPhase says when a command hook runs: before its command or after it.
// Those are the only two: a hook never runs inside a command.
type CommandPhase int

const (
	// BeforeCommand runs the hook once the command line is read, before the
	// command does anything. A hook that fails stops the command.
	BeforeCommand CommandPhase = iota + 1
	// AfterCommand runs the hook once the command has finished, whether it
	// succeeded or failed.
	AfterCommand
)

func (p CommandPhase) String() string {
	switch p {
	case BeforeCommand:
		return "before"
	case AfterCommand:
		return "after"
	}
	return "unknown"
}

// MarshalText implements encoding.TextMarshaler.
func (p CommandPhase) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// CommandHook runs before or after one of axx's commands, in a project that
// uses the pack: a command axx has, or one a pack adds, like `axx fixtures
// generate`. Several packs' hooks of one command run in the order of the
// packs: before hooks first to last, after hooks last to first.
type CommandHook struct {
	// Command is the command's words after axx: "fixtures generate".
	Command string       `json:"command"`
	Phase   CommandPhase `json:"phase"`
	// Doc says what the hook does, for the pack's reference.
	Doc string `json:"doc"`
	// Run runs it. Before the command, an error stops the command, and its
	// after hooks don't run; after it, an error fails a command that
	// succeeded. An *axxerr error keeps its code; any other is reported as
	// the hook's, with the pack named.
	Run func(call *CommandHookCall) error `json:"-"`
}

// CommandHookCall is a run of a command hook.
type CommandHookCall struct {
	Context context.Context
	// Command is the command's words after axx: "fixtures generate".
	Command string
	// Args are the command's arguments.
	Args []string
	// Flags are the values of the command's flags, given or default, by
	// name: of a repeated flag, its last. Lists are a repeated flag's
	// values, in the order given.
	Flags map[string]string
	Lists map[string][]string
	// ProjectDir is the directory of axx.yaml ("" when there is none).
	ProjectDir string
	// Settings decodes the pack's section of axx.yaml (packs.<name>), read
	// with the command's --config, --profile, -D and --set, into v. It
	// leaves v as it is when there is no section.
	Settings func(v any) error
	// Err is, after the command, the error it ended with: nil when it
	// succeeded. Before it, Err is nil.
	Err error
	// Stderr is where the hook writes what it has to say: stdout is the
	// command's own, its --json envelope included.
	Stderr io.Writer
	Logger *slog.Logger
}
