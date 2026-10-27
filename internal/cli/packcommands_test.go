package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// commandPack adds `axx parcels label`, which writes a parcel's label, and
// `axx version`, which axx has.
type commandPack struct{}

func (commandPack) Manifest() core.Manifest {
	label := core.Command{
		Name: "label", Args: "<reference>", Short: "Write a parcel's label", MinArgs: 1, MaxArgs: 1,
		Flags: []core.Flag{
			{Name: "format", Short: "f", Usage: "the label's format", Default: "zpl"},
			{Name: "return", Usage: "a return label", Bool: true},
			{Name: "note", Usage: "a note on the label", Repeated: true},
		},
		Run: func(call *core.CommandCall) error {
			if call.Args[0] == "PX-0000" {
				return errors.New("no parcel PX-0000")
			}
			if notes := call.Lists["note"]; len(notes) > 0 {
				return call.Emit(notes, func(w io.Writer) error {
					_, err := fmt.Fprintf(w, "notes %s (last %s)\n", strings.Join(notes, "; "), call.Flags["note"])
					return err
				})
			}
			data := map[string]any{"reference": call.Args[0], "format": call.Flags["format"], "return": call.Flags["return"] == "true"}
			return call.Emit(data, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "label of %s as %s (return: %s)\n", call.Args[0], call.Flags["format"], call.Flags["return"])
				return err
			})
		},
	}
	return core.Manifest{Name: "command-probe", Commands: []core.Command{
		{Name: "parcels", Short: "Work with parcels", Commands: []core.Command{label}},
		{Name: "version", Short: "Not axx's version", Run: func(*core.CommandCall) error { return errors.New("the pack's version ran") }},
	}}
}

func init() { engine.Register("./command-probe", commandPack{}) }

func TestAPacksCommandRunsAsOneOfAxxs(t *testing.T) {
	out, errOut, code := run(t, "parcels", "label", "PX-1042", "-f", "pdf", "--return")
	if code != int(exitcode.OK) || out != "label of PX-1042 as pdf (return: true)\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	out, _, code = run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.OK) || out != "label of PX-1042 as zpl (return: false)\n" {
		t.Errorf("the flags' defaults: exit %d, out %q", code, out)
	}

	out, _, code = run(t, "parcels", "label", "PX-1042", "--note", "fragile", "--note", "leave with a neighbour")
	if code != int(exitcode.OK) || out != "notes fragile; leave with a neighbour (last leave with a neighbour)\n" {
		t.Errorf("a repeated flag: exit %d, out %q", code, out)
	}

	out, _, code = run(t, "parcels", "label", "PX-1042", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != int(exitcode.OK) || !env.OK || env.Command != "axx parcels label" {
		t.Fatalf("--json: exit %d, %v, %s", code, err, out)
	}
	if data := env.Data.(map[string]any); data["reference"] != "PX-1042" || data["format"] != "zpl" || data["return"] != false {
		t.Errorf("--json data: %v", data)
	}
}

func TestAPacksCommandsErrorHasACode(t *testing.T) {
	out, _, code := run(t, "parcels", "label", "PX-0000", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != int(exitcode.Usage) || env.OK || len(env.Errors) != 1 {
		t.Fatalf("exit %d, %v, %s", code, err, out)
	}
	e := env.Errors[0]
	if e.Code != CodePackCommand || !strings.Contains(e.Message, "no parcel PX-0000") || !strings.Contains(e.Hint, "command-probe") {
		t.Errorf("error: %+v", e)
	}

	_, _, code = run(t, "parcels", "label")
	if code != int(exitcode.Usage) {
		t.Errorf("without its argument: exit %d, want %d", code, exitcode.Usage)
	}
}

func TestAPackCannotReplaceAxxsCommands(t *testing.T) {
	out, _, code := run(t, "version")
	if code != int(exitcode.OK) || strings.Contains(out, "pack's version") {
		t.Errorf("axx version: exit %d, out %q", code, out)
	}
}

func TestAxxsHelpListsThePacksCommands(t *testing.T) {
	out, _, code := run(t, "--help")
	if code != int(exitcode.OK) || !strings.Contains(out, "parcels") || !strings.Contains(out, "Work with parcels") {
		t.Errorf("exit %d, help:\n%s", code, out)
	}
}
