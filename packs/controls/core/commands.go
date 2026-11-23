package controlscore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// controlsCommand is `axx controls`.
func controlsCommand() core.Command {
	return core.Command{
		Name:     "controls",
		Short:    "The controls a project is required to cover, and what covers each",
		Commands: []core.Command{listCommand(), showCommand(), reportCommand(), checkCommand()},
	}
}

// judgedFlags are the flags of the commands that judge evidence: where the
// lists and the exceptions come from, when not from the project's settings.
var judgedFlags = []core.Flag{
	{Name: "require", Repeated: true, Usage: "a list of controls to go by, in place of the project's (packs.controls-core.require): a list a pack carries, or a file; repeat it for several"},
	{Name: "exceptions", Usage: "a file of the exceptions that count, in place of the project's: its own are then shown as asked for, and not counted"},
}

func overridesOf(call *core.CommandCall) overrides {
	return overrides{require: call.Lists["require"], exceptions: call.Flags["exceptions"]}
}

func projectOf(call *core.CommandCall) (*project, error) {
	dir := call.ProjectDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return loadProject(dir, call.Settings, overridesOf(call))
}

func listCommand() core.Command {
	return core.Command{
		Name: "list", Args: "[word...]", MinArgs: 0, MaxArgs: -1,
		Short: "The frameworks and lists this project has, or the controls that mention some words",
		Long: `Without words, list the frameworks whose controls the project's scenarios can
claim, with the word their tags start with, and the lists of controls it can
require (packs.controls-core.require), those it requires marked.

With words, list the controls that mention every one of them, in their ID, their
title or their text: axx controls list password length. Each is shown as a tag
names it, so the tag is that with an @ in front.`,
		Run: runList,
	}
}

// listResult is what `axx controls list` reports.
type listResult struct {
	Frameworks []frameworkInfo `json:"frameworks,omitempty"`
	Lists      []listInfo      `json:"lists,omitempty"`
	Controls   []controlInfo   `json:"controls,omitempty"`
	// More is how many more controls match than Controls has.
	More int `json:"more,omitempty"`
}

type frameworkInfo struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Version  string `json:"version,omitempty"`
	Controls int    `json:"controls"`
	Notice   string `json:"notice,omitempty"`
}

type listInfo struct {
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Framework string `json:"framework"`
	Controls  int    `json:"controls"`
	Required  bool   `json:"required"`
}

type controlInfo struct {
	Control string `json:"control"`
	Title   string `json:"title,omitempty"`
	Group   string `json:"group,omitempty"`
	Text    string `json:"text,omitempty"`
}

// maxListed is how many controls a search lists.
const maxListed = 40

func runList(call *core.CommandCall) error {
	p, err := projectOf(call)
	if err != nil {
		return err
	}
	var res listResult
	if len(call.Args) > 0 {
		words := make([]string, len(call.Args))
		for i, w := range call.Args {
			words[i] = strings.ToLower(w)
		}
		for _, key := range sortedKeys(p.frameworks) {
			f := p.frameworks[key]
			for _, c := range f.Controls {
				hay := strings.ToLower(key + ":" + c.ID + " " + c.Title + " " + c.Group + " " + c.Text)
				match := true
				for _, w := range words {
					match = match && strings.Contains(hay, w)
				}
				if !match {
					continue
				}
				if len(res.Controls) == maxListed {
					res.More++
					continue
				}
				res.Controls = append(res.Controls, controlInfo{Control: key + ":" + c.ID, Title: c.Title, Group: c.Group, Text: c.Text})
			}
		}
		return call.Emit(res, func(w io.Writer) error {
			if len(res.Controls) == 0 {
				_, err := fmt.Fprintf(w, "no control of this project's frameworks mentions %s\n", strings.Join(call.Args, " "))
				return err
			}
			for _, c := range res.Controls {
				fmt.Fprintf(w, "%s  %s\n", c.Control, oneLine(c.Title, c.Text, 110))
			}
			if res.More > 0 {
				fmt.Fprintf(w, "and %d more: add a word to narrow them\n", res.More)
			}
			_, err := fmt.Fprintln(w, "\n`axx controls show <control>` shows one with its text and what covers it; its tag is the control with an @ in front")
			return err
		})
	}
	required := map[string]bool{}
	for _, l := range p.lists {
		required[l.Name] = true
		if isFile(l.Name) {
			res.Lists = append(res.Lists, listInfo{Name: l.Name, Title: l.Title, Framework: l.Framework.Key, Controls: len(l.Controls), Required: true})
		}
	}
	for _, key := range sortedKeys(p.frameworks) {
		f := p.frameworks[key]
		res.Frameworks = append(res.Frameworks, frameworkInfo{Key: key, Title: f.Title, Version: f.Version, Controls: len(f.Controls), Notice: f.Notice})
		for _, l := range f.Lists {
			res.Lists = append(res.Lists, listInfo{Name: l.Name, Title: l.Title, Framework: key, Controls: len(l.Controls), Required: required[l.Name]})
		}
	}
	return call.Emit(res, func(w io.Writer) error {
		if len(res.Frameworks) == 0 {
			_, err := fmt.Fprintf(w, "this project has no framework of controls yet\n\n"+
				"add a pack that carries one (`axx pack add controls-asvs`), or list the controls of any framework in a file of the project's own and name it in packs.%s.require:\n\n"+
				"  # security/controls.yaml\n  framework: pci\n  title: PCI DSS\n  controls:\n    \"8.3.6\": Passwords are at least 12 characters long\n", Name)
			return err
		}
		fmt.Fprintln(w, "frameworks (a scenario claims a control with a tag: @<key>:<id>):")
		for _, f := range res.Frameworks {
			fmt.Fprintf(w, "  %-12s %s, %s\n", f.Key, strings.TrimSpace(f.Title+" "+f.Version), plural(f.Controls, "control"))
		}
		if len(res.Lists) > 0 {
			fmt.Fprintf(w, "\nlists (packs.%s.require names the ones the project is to cover):\n", Name)
			for _, l := range res.Lists {
				mark := ""
				if l.Required {
					mark = "  (required)"
				}
				fmt.Fprintf(w, "  %-28s %s, %s%s\n", l.Name, l.Title, plural(l.Controls, "control"), mark)
			}
		}
		_, err := fmt.Fprintln(w, "\n`axx controls list <words>` finds the controls that mention them")
		return err
	})
}

// oneLine is a control in a line: its title, or the start of its text.
func oneLine(title, text string, width int) string {
	s := strings.Join(strings.Fields(title), " ")
	if s == "" {
		s = strings.Join(strings.Fields(text), " ")
	}
	if r := []rune(s); len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

func showCommand() core.Command {
	return core.Command{
		Name: "show", Args: "<control>", MinArgs: 1, MaxArgs: 1,
		Short: "One control: what it requires, which scenarios claim it, and how it stands",
		Long: `Show a control, named as a tag names it (asvs:6.2.1, with or without the @): what
it requires, in its framework's words; the list that requires it; and how it stood
in the last run, with the scenarios that claim it, what other sources say of it
and the exception recorded for it.`,
		Flags: judgedFlags,
		Run:   runShow,
	}
}

// showResult is what `axx controls show` reports.
type showResult struct {
	ControlResult
	Text   string `json:"text,omitempty"`
	Notice string `json:"notice,omitempty"`
	// Judged is false when no run has written its evidence yet: the state is
	// then of what the settings alone say.
	Judged bool `json:"judged"`
}

func unknownError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeUnknown, exitcode.Usage, format, args...)
}

func runShow(call *core.CommandCall) error {
	p, err := projectOf(call)
	if err != nil {
		return err
	}
	key, id, ok := splitRef(call.Args[0])
	f := p.frameworks[key]
	if !ok || f == nil {
		return unknownError("%q names a control of no framework this project has", call.Args[0]).
			WithHint("name it as a tag does: asvs:6.2.1. `axx controls list` shows the project's frameworks")
	}
	c, known := f.control(id)
	if !known {
		err := unknownError("%s has no control %s", describe(f), id)
		if near := f.nearest(id); near != "" {
			return err.WithHint("the nearest it has is %s:%s; `axx controls list <words>` finds a control by a word of it", key, near)
		}
		return err.WithHint("`axx controls list <words>` finds a control by a word of it")
	}
	ref := key + ":" + c.ID
	r, judged, err := p.judge(nil)
	if err != nil {
		return err
	}
	res := showResult{Text: c.Text, Notice: f.Notice, Judged: judged}
	found := false
	for _, cr := range r.Controls {
		if cr.Control == ref {
			res.ControlResult, found = cr, true
		}
	}
	if !found {
		res.ControlResult = ControlResult{Control: ref, Framework: key, ID: c.ID, Title: c.Title, Group: c.Group, State: NotCovered}
	}
	return call.Emit(res, func(w io.Writer) error {
		cr := res.ControlResult
		fmt.Fprintf(w, "%s  %s\n", ref, strings.TrimSpace(cr.Title))
		if cr.Group != "" {
			fmt.Fprintf(w, "  in %s, %s\n", describe(f), cr.Group)
		} else {
			fmt.Fprintf(w, "  in %s\n", describe(f))
		}
		if c.Text != "" {
			fmt.Fprintf(w, "\n%s\n", wrap(c.Text, 96, "  "))
			if f.Notice != "" {
				fmt.Fprintf(w, "  (%s)\n", f.Notice)
			}
		}
		fmt.Fprintln(w)
		switch {
		case cr.Required && cr.Needs > 1:
			fmt.Fprintf(w, "required by %s, which takes %d passing scenarios to prove it\n", cr.List, cr.Needs)
		case cr.Required:
			fmt.Fprintf(w, "required by %s\n", cr.List)
		default:
			fmt.Fprintln(w, "in no list the project requires")
		}
		fmt.Fprintf(w, "%s: %s\n", cr.State, cr.Why())
		if !judged {
			fmt.Fprintln(w, "  (no run has written its evidence yet: `axx run` does)")
		}
		for _, sc := range cr.Scenarios {
			fmt.Fprintf(w, "  %s:%d  %s  [%s]\n", sc.URI, sc.Line, sc.Name, sc.Status)
		}
		for _, o := range cr.Other {
			line := fmt.Sprintf("  %s: %s", o.Source, o.State)
			if o.Note != "" {
				line += " (" + o.Note + ")"
			}
			if o.Link != "" {
				line += "  " + o.Link
			}
			fmt.Fprintln(w, line)
		}
		if e := cr.Exception; e != nil && cr.State != Excepted {
			ended := ""
			if cr.Expired {
				ended = ", which has ended"
			}
			fmt.Fprintf(w, "  an exception is recorded for it%s (%s): %s\n", ended, e.Kind, e.Reason)
		}
		_, err := fmt.Fprintf(w, "\na scenario claims it with the tag @%s\n", ref)
		return err
	})
}

// wrap breaks text into lines of at most width, each after indent.
func wrap(text string, width int, indent string) string {
	var b strings.Builder
	for i, para := range strings.Split(strings.TrimSpace(text), "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := indent
		for _, word := range strings.Fields(para) {
			if len(line)+len(word) > width && line != indent {
				b.WriteString(strings.TrimRight(line, " ") + "\n")
				line = indent
			}
			line += word + " "
		}
		b.WriteString(strings.TrimRight(line, " "))
	}
	return b.String()
}

// judge reads the evidence a command was given (a run's files or folders, and
// files of other sources), or the project's own run when given none, and
// works out how each control stands. judged is false when there is no run's
// evidence at all: the report is then of what the settings alone say.
func (p *project) judge(given []string) (r *Report, judged bool, err error) {
	var runs, others []*Evidence
	if len(given) == 0 {
		own := filepath.Join(p.path(p.cfg.Folder), runFile)
		if _, serr := os.Stat(own); serr == nil {
			given = []string{own}
		}
	}
	for _, path := range given {
		e, err := readEvidence(path)
		if err != nil {
			return nil, false, err
		}
		if e.Kind == KindOther {
			others = append(others, e)
		} else {
			runs = append(runs, e)
		}
	}
	others, err = p.otherEvidence(others)
	if err != nil {
		return nil, false, err
	}
	var run *Evidence
	if len(runs) > 0 {
		if run, err = merge(runs); err != nil {
			return nil, false, err
		}
		for key, version := range run.Frameworks {
			if f := p.frameworks[key]; f != nil && f.Version != version {
				return nil, false, axxerr.New(CodeMixed, exitcode.Usage, "the evidence is of %s %s, and the project has %s now", key, version, describe(f)).
					WithHint("a control's ID can mean another control in another version of its framework: run the scenarios again")
			}
		}
	}
	return p.report(run, others, time.Now()), len(runs) > 0, nil
}

func reportCommand() core.Command {
	return core.Command{
		Name: "report", Args: "[evidence...]", MinArgs: 0, MaxArgs: -1,
		Short: "Every control with what covers it, in a format of your choice",
		Long: `Write the report of the controls: each required control with how it stands and
what covers it, the scenarios that claim it, what other sources say of it and the
exception recorded for it; then the controls that are covered and in no required
list.

The evidence is what runs wrote (run.json; name the files, or the folders they are
in) and files of what covers controls elsewhere. Without any, it is the project's
own last run (packs.controls-core.folder, default .axx/controls). Several runs are
taken together, as the shards of one are.

--format is text (the default), markdown, csv or json, or a format one of the
project's packs adds, such as oscal (controls-oscal).`,
		Flags: append([]core.Flag{
			{Name: "format", Short: "f", Default: "text", Usage: "the report's format: text, markdown, csv, json, or one a pack adds"},
			{Name: "out", Short: "o", Usage: "write the report to a file instead of standard output"},
		}, judgedFlags...),
		Run: runReport,
	}
}

func runReport(call *core.CommandCall) error {
	p, err := projectOf(call)
	if err != nil {
		return err
	}
	name := call.Flags["format"]
	if name == "md" {
		name = "markdown"
	}
	format, ok := formatOf(name)
	if !ok {
		return unknownError("the project's packs write no %q report", call.Flags["format"]).
			WithHint("the formats they write: %s", strings.Join(formatNames(), ", "))
	}
	r, judged, err := p.judge(call.Args)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := format.Write(&b, r); err != nil {
		return axxerr.Wrap(err, CodeEvidence, exitcode.Usage, "cannot write the %s report", format.Name)
	}
	if out := call.Flags["out"]; out != "" {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
			return err
		}
		return call.Emit(map[string]any{"report": out, "format": format.Name, "counts": r.Counts()}, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "wrote %s (%s): %s\n", out, format.Name, headline(r))
			return err
		})
	}
	return call.Emit(struct {
		*Report
		Counts Counts `json:"counts"`
	}{r, r.Counts()}, func(w io.Writer) error {
		if _, err := w.Write(b.Bytes()); err != nil {
			return err
		}
		if format.Name == "text" {
			if !judged {
				fmt.Fprintf(w, "\nno run has written its evidence yet: `axx run` does, and a scenario claims a control with a tag, @<framework>:<id>\n")
			}
			for _, n := range r.Notices {
				fmt.Fprintf(w, "\n%s\n", n)
			}
		}
		return nil
	})
}

func checkCommand() core.Command {
	return core.Command{
		Name: "check", Args: "[evidence...]", MinArgs: 0, MaxArgs: -1,
		Short: "Fail when a required control is not covered",
		Long: `Judge the evidence against the lists the project requires, and fail when a
required control is not covered: one that is failing, not covered or partly
proven, or a tag that names a control its framework has not.

The evidence is what runs wrote (run.json; name the files, or the folders they are
in) and files of what covers controls elsewhere. Without any, it is the project's
own last run. Several runs are taken together: the jobs of a CI run each write
their own, of the scenarios they ran, and one job checks them all:

  axx controls check shard-1/ shard-2/ shard-3/

A control is proven when the runs' scenarios that claim it passed. Runs of
different commits, or by different versions of axx, are refused.

--require and --exceptions say which lists and exceptions to go by, in place of
the project's own settings: a workflow that every repository must pass names the
security team's, and then nothing in the project can lower the gate.

It writes what it judged to a file (--out, default checked.json in
packs.controls-core.folder) and prints the file's SHA-256, for signing.`,
		Flags: append([]core.Flag{
			{Name: "out", Short: "o", Usage: "where what was judged is written (default: checked.json in packs.controls-core.folder)"},
		}, judgedFlags...),
		Run: runCheck,
	}
}

// checkResult is what `axx controls check` reports.
type checkResult struct {
	OK     bool   `json:"ok"`
	Counts Counts `json:"counts"`
	// Unmet are the required controls that are not covered.
	Unmet   []ControlResult `json:"unmet,omitempty"`
	Unknown []UnknownClaim  `json:"unknown,omitempty"`
	// Checked is the file of what was judged, and SHA256 its hash.
	Checked string `json:"checked"`
	SHA256  string `json:"sha256"`
}

func runCheck(call *core.CommandCall) error {
	p, err := projectOf(call)
	if err != nil {
		return err
	}
	if len(p.lists) == 0 {
		return settingsError("the project requires no list of controls, so there is nothing to check").
			WithHint("name the lists in packs.%s.require, or give this command --require; `axx controls list` shows the lists the project's packs carry", Name)
	}
	r, judged, err := p.judge(call.Args)
	if err != nil {
		return err
	}
	if !judged {
		return evidenceError("no run has written its evidence yet").
			WithHint("`axx run` writes it to %s; name the files or folders of other runs", filepath.Join(p.cfg.Folder, runFile))
	}
	var b bytes.Buffer
	if err := writeJSON(&b, r); err != nil {
		return err
	}
	out := call.Flags["out"]
	if out == "" {
		out = filepath.Join(p.path(p.cfg.Folder), "checked.json")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(b.Bytes())
	unmet := r.Unmet()
	shown := out
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, out); err == nil && !strings.HasPrefix(rel, "..") {
			shown = rel
		}
	}
	res := checkResult{OK: len(unmet) == 0 && len(r.Unknown) == 0, Counts: r.Counts(), Unmet: unmet, Unknown: r.Unknown, Checked: filepath.ToSlash(shown), SHA256: hex.EncodeToString(sum[:])}
	if err := call.Emit(res, func(w io.Writer) error {
		fmt.Fprintf(w, "controls: %s\n", headline(r))
		width := 0
		for _, cr := range unmet {
			width = max(width, len(cr.Control))
		}
		for _, cr := range unmet {
			fmt.Fprintf(w, "  %-*s  %-13s  %s\n", width, cr.Control, cr.State, cr.Why())
		}
		writeUnknown(w, r, "", "  %s  %s\n")
		if r.Partial != "" {
			fmt.Fprintf(w, "  the runs are not of every scenario (%s): a control they leave not covered may be one they did not get to\n", r.Partial)
		}
		_, err := fmt.Fprintf(w, "sha256 %s  %s\n", res.SHA256, res.Checked)
		return err
	}); err != nil {
		return err
	}
	if res.OK {
		return nil
	}
	what := plural(len(unmet), "required control") + " not covered"
	if len(unmet) == 1 {
		what = "1 required control is not covered"
	} else if len(unmet) > 1 {
		what = fmt.Sprintf("%d required controls are not covered", len(unmet))
	}
	if len(r.Unknown) > 0 {
		tags := plural(len(r.Unknown), "tag") + " names no control"
		if len(r.Unknown) > 1 {
			tags = fmt.Sprintf("%d tags name no control", len(r.Unknown))
		}
		if len(unmet) == 0 {
			what = tags
		} else {
			what += ", and " + tags
		}
	}
	return axxerr.New(CodeNotCovered, exitcode.Failed, "%s", what).
		WithHint("`axx controls show <control>` shows what one requires; a scenario that proves it claims it with a tag, and what does not apply or is covered elsewhere is recorded in packs.%s (exceptions, evidence)", Name)
}
