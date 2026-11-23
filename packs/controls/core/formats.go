package controlscore

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func init() {
	RegisterFormat(Format{Name: "text", Doc: "for a terminal: the controls by how they stand, those that are not covered first", Write: writeText})
	RegisterFormat(Format{Name: "markdown", Doc: "for a pull request or a wiki: a table of the controls, and what covers each", Write: writeMarkdown})
	RegisterFormat(Format{Name: "csv", Doc: "for a spreadsheet, and the tools that take one: a row a control", Write: writeCSV})
	RegisterFormat(Format{Name: "json", Doc: "the report as data: every control with its scenarios, its other evidence and its exception", Write: writeJSON})
}

// order is the order a report for a person lists the states in: what needs
// doing first.
var order = []State{Failing, NotCovered, PartlyProven, Excepted, Covered, Proven}

// headline is the report's one line: how many of the required controls are
// covered, and how.
func headline(r *Report) string {
	c := r.Counts()
	if c.Required == 0 {
		n := 0
		for _, cr := range r.Controls {
			if cr.State == Proven {
				n++
			}
		}
		return fmt.Sprintf("no list of controls is required; the scenarios prove %s", plural(n, "control"))
	}
	var parts []string
	covered := fmt.Sprintf("%d of %d required are covered", c.Proven+c.Covered, c.Required)
	if c.Covered > 0 {
		covered += fmt.Sprintf(" (%d proven, %d by other evidence)", c.Proven, c.Covered)
	}
	parts = append(parts, covered)
	if c.Excepted > 0 {
		parts = append(parts, fmt.Sprintf("%d excepted", c.Excepted))
	}
	if c.Failing > 0 {
		parts = append(parts, fmt.Sprintf("%d failing", c.Failing))
	}
	if c.Partly > 0 {
		parts = append(parts, fmt.Sprintf("%d partly proven", c.Partly))
	}
	if c.NotCovered > 0 {
		parts = append(parts, fmt.Sprintf("%d not covered", c.NotCovered))
	}
	return strings.Join(parts, ", ")
}

// of says which runs a report is of: their commit, the version of axx, their
// day.
func of(r *Report) string {
	var parts []string
	if r.Commit != "" {
		commit := "commit " + short(r.Commit)
		if r.Dirty {
			commit += " with changes that are not committed"
		}
		parts = append(parts, commit)
	}
	if r.Axx != "" {
		parts = append(parts, "axx "+r.Axx)
	}
	if !r.Ended.IsZero() {
		parts = append(parts, r.Ended.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return strings.Join(parts, ", ")
}

func byState(r *Report, s State, required bool) []ControlResult {
	var out []ControlResult
	for _, cr := range r.Controls {
		if cr.State == s && cr.Required == required {
			out = append(out, cr)
		}
	}
	return out
}

func writeText(w io.Writer, r *Report) error {
	fmt.Fprintf(w, "controls: %s\n", headline(r))
	if o := of(r); o != "" {
		fmt.Fprintf(w, "  of %s\n", o)
	}
	for _, l := range r.Lists {
		fmt.Fprintf(w, "  required: %s (%s, %s)\n", l.Name, l.Of, plural(l.Controls, "control"))
	}
	if r.Partial != "" {
		fmt.Fprintf(w, "  the runs are not of every scenario (%s): a control they leave not covered may be one they did not get to\n", r.Partial)
	}
	width := 0
	for _, cr := range r.Controls {
		width = max(width, len(cr.Control))
	}
	rows := func(title string, list []ControlResult) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d):\n", title, len(list))
		for _, cr := range list {
			line := fmt.Sprintf("  %-*s  %s", width, cr.Control, cr.Why())
			if cr.Title != "" {
				line += "  [" + cr.Title + "]"
			}
			fmt.Fprintln(w, line)
		}
	}
	titles := map[State]string{Failing: "Failing", NotCovered: "Not covered", PartlyProven: "Partly proven", Excepted: "Excepted", Covered: "Covered by other evidence", Proven: "Proven"}
	for _, s := range order {
		rows(titles[s], byState(r, s, true))
	}
	var extra []ControlResult
	for _, cr := range r.Controls {
		if !cr.Required {
			extra = append(extra, cr)
		}
	}
	if len(extra) > 0 {
		fmt.Fprintf(w, "\nCovered, and in no required list (%d):\n", len(extra))
		for _, cr := range extra {
			fmt.Fprintf(w, "  %-*s  %s: %s\n", width, cr.Control, cr.State, cr.Why())
		}
	}
	writeUnknown(w, r, "\nTags that name no control:\n", "  %s  %s\n")
	if len(r.Requested) > 0 {
		fmt.Fprintf(w, "\nExceptions the project asks for, which are not counted (%d):\n", len(r.Requested))
		for _, e := range r.Requested {
			fmt.Fprintf(w, "  %s  %s: %s\n", e.Control, e.Kind, e.Reason)
		}
	}
	return nil
}

func writeUnknown(w io.Writer, r *Report, title, row string) {
	if len(r.Unknown) == 0 {
		return
	}
	fmt.Fprint(w, title)
	for _, u := range r.Unknown {
		at := fmt.Sprintf("%s:%d", u.Scenarios[0].URI, u.Scenarios[0].Line)
		if len(u.Scenarios) > 1 {
			at += fmt.Sprintf(" and %d more", len(u.Scenarios)-1)
		}
		what := "its framework has no such control (" + at + ")"
		if u.Nearest != "" {
			what = "its framework has no such control; the nearest is " + u.Nearest + " (" + at + ")"
		}
		fmt.Fprintf(w, row, u.Tag, what)
	}
}

func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

func writeMarkdown(w io.Writer, r *Report) error {
	fmt.Fprintf(w, "# Controls\n\n**%s.**\n", upperFirst(headline(r)))
	if o := of(r); o != "" {
		fmt.Fprintf(w, "\nOf %s.\n", o)
	}
	if len(r.Lists) > 0 {
		fmt.Fprint(w, "\nRequired:\n\n")
		for _, l := range r.Lists {
			fmt.Fprintf(w, "- `%s`: %s, %s\n", l.Name, cell(l.Of), plural(l.Controls, "control"))
		}
	}
	if r.Partial != "" {
		fmt.Fprintf(w, "\n> The runs are not of every scenario (%s): a control they leave not covered may be one they did not get to.\n", r.Partial)
	}
	table := func(title string, list []ControlResult) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(w, "\n## %s (%d)\n\n| Control | What it is | What covers it |\n|---|---|---|\n", title, len(list))
		for _, cr := range list {
			var by []string
			for _, sc := range cr.Scenarios {
				by = append(by, fmt.Sprintf("%s (`%s:%d`, %s)", cell(sc.Name), sc.URI, sc.Line, sc.Status))
			}
			for _, o := range cr.Other {
				s := cell(o.Source)
				if o.Link != "" {
					s = fmt.Sprintf("[%s](%s)", s, o.Link)
				}
				s += ": " + string(o.State)
				if o.Note != "" {
					s += " (" + cell(o.Note) + ")"
				}
				by = append(by, s)
			}
			if cr.Exception != nil {
				e := "Exception, " + cr.Exception.Kind
				if cr.Exception.Until != "" {
					e += " until " + cr.Exception.Until
					if cr.Expired {
						e += " (ended)"
					}
				}
				by = append(by, e+": "+cell(cr.Exception.Reason))
			}
			if len(by) == 0 {
				by = []string{"Nothing: no scenario is tagged `@" + cr.Control + "`"}
			}
			what := cell(cr.Title)
			if cr.Group != "" {
				what = strings.TrimSpace(what + " (" + cell(cr.Group) + ")")
			}
			if cr.Needs > 1 {
				what += fmt.Sprintf(" (takes %d scenarios)", cr.Needs)
			}
			fmt.Fprintf(w, "| `%s` | %s | %s |\n", cr.Control, what, strings.Join(by, "<br>"))
		}
	}
	titles := map[State]string{Failing: "Failing", NotCovered: "Not covered", PartlyProven: "Partly proven", Excepted: "Excepted", Covered: "Covered by other evidence", Proven: "Proven"}
	for _, s := range order {
		table(titles[s], byState(r, s, true))
	}
	var extra []ControlResult
	for _, cr := range r.Controls {
		if !cr.Required {
			extra = append(extra, cr)
		}
	}
	table("Covered, and in no required list", extra)
	writeUnknown(w, r, "\n## Tags that name no control\n\n", "- `%s`: %s\n")
	if len(r.Sources) > 0 {
		fmt.Fprint(w, "\n## Other evidence\n\n")
		for _, s := range r.Sources {
			line := cell(s.Source)
			if s.Link != "" {
				line = fmt.Sprintf("[%s](%s)", line, s.Link)
			}
			if s.Date != "" {
				line += ", " + s.Date
			}
			fmt.Fprintf(w, "- %s: %s\n", line, plural(s.Controls, "control"))
		}
	}
	if len(r.Requested) > 0 {
		fmt.Fprint(w, "\n## Exceptions the project asks for, which are not counted\n\n")
		for _, e := range r.Requested {
			fmt.Fprintf(w, "- `%s`, %s: %s\n", e.Control, e.Kind, cell(e.Reason))
		}
	}
	return nil
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// csvHeader are the columns of the CSV report: a row a control.
var csvHeader = []string{
	"control", "framework", "id", "title", "group", "required", "list", "state", "scenarios needed", "scenarios passed",
	"scenarios", "other evidence", "exception", "exception reason", "exception until",
}

func writeCSV(w io.Writer, r *Report) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, cr := range r.Controls {
		var scenarios, other []string
		for _, sc := range cr.Scenarios {
			scenarios = append(scenarios, fmt.Sprintf("%s:%d %s [%s]", sc.URI, sc.Line, sc.Name, sc.Status))
		}
		for _, o := range cr.Other {
			s := fmt.Sprintf("%s [%s]", o.Source, o.State)
			if o.Link != "" {
				s += " " + o.Link
			}
			if o.Note != "" {
				s += " (" + o.Note + ")"
			}
			other = append(other, s)
		}
		row := []string{
			cr.Control, cr.Framework, cr.ID, cr.Title, cr.Group, yesNo(cr.Required), cr.List, string(cr.State),
			fmt.Sprint(max(cr.Needs, 1)), fmt.Sprint(passing(cr.Scenarios)),
			strings.Join(scenarios, "; "), strings.Join(other, "; "), "", "", "",
		}
		if e := cr.Exception; e != nil {
			kind := e.Kind
			if cr.Expired {
				kind += " (ended)"
			}
			row[12], row[13], row[14] = kind, e.Reason, e.Until
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func writeJSON(w io.Writer, r *Report) error {
	b, err := json.MarshalIndent(struct {
		*Report
		Counts Counts `json:"counts"`
	}{r, r.Counts()}, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
