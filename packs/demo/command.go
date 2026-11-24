package demo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/core"
)

func demoCommand() core.Command {
	return core.Command{
		Name: "demo", Args: "<file>",
		Short: "Make a demo of a scenario: run it, and film what it did",
		Long: `Make the demo of a feature file named *.demo.feature.

axx runs the file's scenario as ` + "`axx run`" + ` does, against the project's
services, and keeps what the run did as a take. From the take and the file's
cues (comments that start with "demo:") it makes a film: a step on a screen
shows the screen, and any other step a diagram of what it sent and what came
back. A scenario that does not pass makes no film.

--recut makes the film again from the last take, without a run: after a
change to the subtitles, the directions or the look.

The film and the take are in .axx/demo, in a folder named after the file.`,
		MinArgs: 1, MaxArgs: 1,
		Flags: []core.Flag{
			{Name: "recut", Bool: true, Usage: "make the film again from the last take, without running the scenario"},
			{Name: "out", Short: "o", Usage: "where to write the film (default: .axx/demo/<name>/<name>.mp4)"},
			{Name: "profile", Usage: "the profile of axx.yaml to run with"},
		},
		Run: runDemo,
	}
}

// Made is what `axx demo` reports.
type Made struct {
	Feature string `json:"feature"`
	// Films are the films made, one for each scenario that ran.
	Films []MadeFilm `json:"films"`
}

// MadeFilm is one film.
type MadeFilm struct {
	Scenario string  `json:"scenario"`
	Film     string  `json:"film"`
	Take     string  `json:"take"`
	Seconds  float64 `json:"seconds"`
}

func runDemo(call *core.CommandCall) error {
	if call.ProjectDir == "" {
		return errors.New("no axx.yaml here or above: a demo is made in an axx project, where its services and packs are")
	}
	var st Settings
	if err := call.Settings(&st); err != nil {
		return err
	}
	file := call.Args[0]
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("cannot read the demo's feature file: %w", err)
	}
	if !strings.HasSuffix(abs, Suffix) {
		return fmt.Errorf("%s is no demo: a demo's feature file is named like hold%s", file, Suffix)
	}
	rel, err := filepath.Rel(call.ProjectDir, abs)
	if err != nil {
		return err
	}
	uri := filepath.ToSlash(rel)
	script, err := ParseScript(uri, src, st.Directions)
	if err != nil {
		return err
	}
	if len(script.Problems) > 0 {
		var b strings.Builder
		for _, p := range script.Problems {
			fmt.Fprintf(&b, "\n  %s:%d:%d: %s", uri, p.Line, p.Column, p.Message)
		}
		return fmt.Errorf("the demo's cues have mistakes:%s", b.String())
	}
	dir := takeDir(call.ProjectDir, uri)
	var takes []string
	if call.Flags["recut"] == "true" {
		takes, _ = filepath.Glob(filepath.Join(dir, "take-*.json"))
		sort.Strings(takes)
		if len(takes) == 0 {
			return fmt.Errorf("there is no take of %s yet: run `axx demo %s` once, and --recut cuts its take again", uri, file)
		}
	} else {
		// A run's takes replace the last run's.
		old, _ := filepath.Glob(filepath.Join(dir, "take-*.json"))
		for _, o := range old {
			_ = os.Remove(o)
		}
		begin(&session{dir: dir, feature: uri})
		args := []string{abs, "--workers", "1"}
		if p := call.Flags["profile"]; p != "" {
			args = append(args, "--profile", p)
		}
		rerr := call.Run(call.Context, args...)
		s := end()
		if rerr != nil {
			// What failed is what the run said: a demo shows only what passed.
			fmt.Fprintln(call.Stderr, "axx: the demo's scenario did not pass, so there is no film of it")
			return rerr
		}
		takes = s.takes
		if len(takes) == 0 {
			return fmt.Errorf("no scenario of %s ran: the project's run.tags or run.uses may leave it out", uri)
		}
	}
	made := Made{Feature: uri, Films: []MadeFilm{}}
	for _, path := range takes {
		take, err := readTake(path)
		if err != nil {
			return err
		}
		if !take.Passed {
			return fmt.Errorf("the take of %q did not pass, so there is no film of it: run `axx demo %s` again", take.Scenario, file)
		}
		tl := Cut(take, script, DefaultPace)
		// A demo of one scenario is named after its file; of several, each
		// film after its scenario too.
		out := filepath.Join(dir, filepath.Base(dir)+".mp4")
		if len(takes) > 1 {
			out = filepath.Join(dir, fmt.Sprintf("%s-%s.mp4", filepath.Base(dir), slug(take.Scenario)))
		} else if call.Flags["out"] != "" {
			out = call.Flags["out"]
		}
		fmt.Fprintf(call.Stderr, "axx: filming %q (%.0f seconds)\n", take.Scenario, tl.Duration)
		if err := Film(call.Context, call.ProjectDir, st, &Staged{Take: take, Timeline: tl, Theme: st.Theme}, out, nil); err != nil {
			return err
		}
		made.Films = append(made.Films, MadeFilm{Scenario: take.Scenario, Film: out, Take: path, Seconds: tl.Duration})
	}
	return call.Emit(made, func(w io.Writer) error {
		for _, f := range made.Films {
			film := f.Film
			if r, err := filepath.Rel(call.ProjectDir, film); err == nil && !strings.HasPrefix(r, "..") {
				film = r
			}
			fmt.Fprintf(w, "%s (%.0f seconds): %s\n", f.Scenario, f.Seconds, film)
		}
		return nil
	})
}
