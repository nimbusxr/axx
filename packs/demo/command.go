package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

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

--present shows the demo to an audience instead. A page opens for the screen
the audience sees, and the scenario runs as you go on: forward (the right
arrow, a clicker) shows the next subtitle and runs its steps, back shows again
what was shown and undoes nothing. The directions that set a film's time do
nothing there. When a step fails, the audience sees nothing of it until you
say what to do, on the presenter's page or with a key: try the step once more
(r), play on from the last take (t), with "recorded" in a corner, or start
again from the top (0). --recorded shows the last take and runs nothing.

The film and the take are in .axx/demo, in a folder named after the file.`,
		MinArgs: 1, MaxArgs: 1,
		Flags: []core.Flag{
			{Name: "recut", Bool: true, Usage: "make the film again from the last take, without running the scenario"},
			{Name: "present", Bool: true, Usage: "show the demo to an audience instead of filming it: the scenario runs as you go on, a subtitle at a time"},
			{Name: "recorded", Bool: true, Usage: "with --present, show the last take and run nothing"},
			{Name: "no-open", Bool: true, Usage: "with --present, print the show's addresses and open no browser"},
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
	if call.Flags["present"] == "true" || call.Flags["recorded"] == "true" {
		return present(call, st, script, dir, uri, abs)
	}
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
		footage, _ := filepath.Glob(filepath.Join(dir, "footage-*"))
		for _, f := range footage {
			_ = os.RemoveAll(f)
		}
		begin(&session{dir: dir, feature: uri, stamp: stamp()})
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
		st.footage = dir
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

// stamp names a run's folder of pictures.
func stamp() string { return time.Now().Format("20060102-150405") }

// lastTake is the last good take of a demo, or nil: of its first scenario,
// which is the one a show presents.
func lastTake(dir string) *Take {
	takes, _ := filepath.Glob(filepath.Join(dir, "take-*.json"))
	sort.Strings(takes)
	for _, path := range takes {
		if t, err := readTake(path); err == nil && t.Passed {
			return t
		}
	}
	return nil
}

// present shows a demo to an audience.
func present(call *core.CommandCall, st Settings, script *Script, dir, uri, abs string) error {
	st.footage = dir
	fallback := lastTake(dir)
	recorded := call.Flags["recorded"] == "true"
	if recorded && fallback == nil {
		return fmt.Errorf("there is no take of %s to show: run `axx demo %s` once", uri, call.Args[0])
	}
	// The pictures of runs before this one go, but for the last good take's.
	keep := ""
	if fallback != nil && len(fallback.Footage) > 0 {
		keep, _, _ = strings.Cut(fallback.Footage[0].File, "/")
	}
	footage, _ := filepath.Glob(filepath.Join(dir, "footage-*"))
	for _, f := range footage {
		if filepath.Base(f) != keep {
			_ = os.RemoveAll(f)
		}
	}
	title := strings.TrimSuffix(filepath.Base(uri), Suffix)
	sh := newShow(script, st, fallback, title)
	if recorded {
		sh.mu.Lock()
		sh.playFallback()
		sh.mu.Unlock()
	}
	srv, err := serveStage(call.ProjectDir, st, sh.staged, sh)
	if err != nil {
		return err
	}
	defer srv.Close()
	fmt.Fprintf(call.Stderr, "axx: the show, for the screen the audience sees: %s\n", srv.URL)
	fmt.Fprintf(call.Stderr, "axx: the presenter's page, for you:             %spresenter\n", srv.URL)
	fmt.Fprintln(call.Stderr, "axx: forward is the right arrow, back the left; Ctrl+C ends the show")
	if call.Flags["no-open"] != "true" {
		openBrowser(call.Context, srv.URL)
	}
	ctx, cancel := context.WithCancel(call.Context)
	defer cancel()
	go func() {
		// The presenter ends the show from the page too.
		sh.wait(ctx)
		cancel()
	}()
	for !recorded {
		begin(&session{dir: dir, feature: uri, stamp: stamp(), show: sh})
		sh.start()
		// Nobody but the presenter says how long a scenario may take.
		args := []string{abs, "--workers", "1", "--set", "run.timeouts.scenario=0"}
		if p := call.Flags["profile"]; p != "" {
			args = append(args, "--profile", p)
		}
		rerr := call.Run(ctx, args...)
		end()
		sh.ended(rerr == nil)
		sh.mu.Lock()
		again := sh.decision == "top"
		sh.mu.Unlock()
		if !again || ctx.Err() != nil {
			break
		}
	}
	sh.wait(ctx)
	return nil
}

// openBrowser opens an address in the person's browser, when it can.
func openBrowser(ctx context.Context, url string) {
	name, args := "xdg-open", []string{url}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	}
	_ = exec.CommandContext(ctx, name, args...).Start()
}
