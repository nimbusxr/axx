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

// experimentalPack is a pack that may still change: it says so in its manifest.
type experimentalProbe struct{}

func (experimentalProbe) Manifest() core.Manifest {
	return core.Manifest{
		Name: "experimental-probe", Experimental: true,
		Doc: "Counts the parcels a depot has not sorted yet.",
		Commands: []core.Command{{Name: "unsorted", Short: "Count the unsorted parcels", Run: func(call *core.CommandCall) error {
			return call.Emit(map[string]any{"unsorted": 3}, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, "3 parcels are not sorted yet")
				return err
			})
		}}},
	}
}

func init() { engine.Register("./experimental-probe", experimentalProbe{}) }

// A command of an experimental pack says, once and on standard error, that its pack may still
// change. A command of another pack says nothing of the kind, and what a command prints for a
// program stays as it is.
func TestAnExperimentalPacksCommandSaysSo(t *testing.T) {
	out, errOut, code := run(t, "unsorted")
	if code != int(exitcode.OK) || out != "3 parcels are not sorted yet\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
	if n := strings.Count(errOut, "the experimental-probe pack is experimental: it may change, or go, in any release"); n != 1 {
		t.Errorf("said %d times, want once:\n%s", n, errOut)
	}
	if !strings.Contains(errOut, "/explanations/stability/") {
		t.Errorf("the notice names no page that says what experimental means:\n%s", errOut)
	}

	out, errOut, code = run(t, "unsorted", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != int(exitcode.OK) || !env.OK {
		t.Fatalf("--json: exit %d, %v, %s", code, err, out)
	}
	if !strings.Contains(errOut, "is experimental") {
		t.Errorf("--json: the notice is not on standard error:\n%s", errOut)
	}

	if _, errOut, _ := run(t, "parcels", "label", "PX-1042"); strings.Contains(errOut, "experimental") {
		t.Errorf("a command of a pack that is not experimental:\n%s", errOut)
	}
}

func TestTheNoticeNamesEveryExperimentalPack(t *testing.T) {
	if got := experimentalNotice(nil); got != "" {
		t.Errorf("no pack: %q", got)
	}
	got := experimentalNotice([]string{"controls-core", "controls-asvs", "controls-oscal"})
	if !strings.HasPrefix(got, "the controls-core, controls-asvs and controls-oscal packs are experimental: they may change, or go, in any release") {
		t.Errorf("three packs: %q", got)
	}
}

// A project that takes up an experimental pack is told what it is as it adds it, and each run
// that uses the pack says so once. A project without such a pack is told nothing.
func TestAProjectIsToldItsPackIsExperimental(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{
		"axx.yaml":                         "version: 1\n",
		"experimental-probe/pack.go":       "package probe\n",
		"command-probe/pack.go":            "package probe\n",
		"features/sorting/sorting.feature": "Feature: Sorting\n",
	})

	out, _, code := run(t, "pack", "add", "./command-probe")
	if code != int(exitcode.OK) || strings.Contains(out, "experimental") {
		t.Fatalf("adding a pack that is not experimental: exit %d, %q", code, out)
	}
	out, _, code = run(t, "pack", "add", "./experimental-probe")
	if code != int(exitcode.OK) || !strings.Contains(out, "added ./experimental-probe\n") ||
		!strings.Contains(out, "the ./experimental-probe pack is experimental: it may change, or go, in any release") {
		t.Fatalf("adding an experimental pack: exit %d, %q", code, out)
	}
	out, _, _ = run(t, "pack", "add", "./experimental-probe", "--json")
	if data, _ := jsonData(t, out); data["experimental"] != nil {
		t.Errorf("a pack already listed is added again: %v", data)
	}

	out, _, code = run(t, "pack", "list")
	if code != int(exitcode.OK) {
		t.Fatalf("pack list: exit %d", code)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "./experimental-probe") != strings.Contains(line, "(experimental)") && strings.Contains(line, "-probe") {
			t.Errorf("pack list marks the experimental pack, and no other: %q", line)
		}
	}

	_, errOut, _ := run(t, "run")
	if n := strings.Count(errOut, "the experimental-probe pack is experimental"); n != 1 {
		t.Errorf("a run said it %d times, want once:\n%s", n, errOut)
	}

	writeFiles(t, dir, map[string]string{"axx-packs.yaml": "packs: [./command-probe]\n"})
	if _, errOut, _ := run(t, "run"); strings.Contains(errOut, "experimental") {
		t.Errorf("a run of a project without an experimental pack:\n%s", errOut)
	}
}

func TestPackShowSaysAPackIsExperimental(t *testing.T) {
	var b strings.Builder
	d := PackDetail{Pack: "controls-core", Summary: "Controls", Used: true, Experimental: true}
	if err := renderPackDetail(&b, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "controls-core: Controls\nExperimental: it may change, or go, in any release (") {
		t.Errorf("pack show of an experimental pack:\n%s", b.String())
	}
	b.Reset()
	d.Experimental = false
	if err := renderPackDetail(&b, d); err != nil || strings.Contains(b.String(), "Experimental") {
		t.Errorf("pack show of a pack that is not: %v\n%s", err, b.String())
	}
}
