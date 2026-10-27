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
