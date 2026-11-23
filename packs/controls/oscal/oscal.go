// Package controlsoscal is the controls-oscal pack: the report of controls-core
// written as OSCAL assessment results, the format of NIST's Open Security
// Controls Assessment Language that government programs and compliance tools
// read. It has no steps: it registers a format, which `axx controls report
// --format oscal` writes.
//
// One result is written for each framework the report has controls of. A
// scenario that claims a control is an observation, as is what another source
// says of one; a control is a finding, with the observations it rests on; and
// a risk a project accepted for a time is a risk whose deviation is approved.
// The document names controls by their IDs and titles, and has none of a
// framework's text.
package controlsoscal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nimbusxr/axx/core"
	controlscore "github.com/nimbusxr/axx/packs/controls/core"
)

// Name is the pack's name.
const Name = "controls-oscal"

// FormatName is the name of the format the pack registers.
const FormatName = "oscal"

// OSCALVersion is the version of OSCAL the document says it is of: the last
// of 1.1, which the tools that read 1.1 take. What the pack writes is held to
// NIST's schemas of this version and of the latest (Schemas).
const OSCALVersion = "1.1.3"

// A Schema is NIST's JSON Schema of OSCAL's assessment results, of one
// version: File is the pack's copy of it in testdata, compressed; Source,
// where NIST's release has it, on github.com; and SHA256, the hash the copy
// is held to.
type Schema struct {
	Version, File, Source, SHA256 string
}

// Schemas are the schemas the pack's tests hold its documents to.
var Schemas = []Schema{
	{
		Version: OSCALVersion, File: "oscal_assessment-results_schema-1.1.3.json.gz",
		Source: "/usnistgov/OSCAL/releases/download/v1.1.3/oscal_assessment-results_schema.json",
		SHA256: "d9e34757f0c12aff61f52b821f0b8f83ba0ba75b3a149a202b08ba82f82bc4c3",
	},
	{
		Version: "1.2.3", File: "oscal_assessment-results_schema-1.2.3.json.gz",
		Source: "/usnistgov/OSCAL/releases/download/v1.2.3/oscal_assessment-results_schema.json",
		SHA256: "4034e2032332dbf597e59e0646ec16c2c31df992962490371e91f47b219ff42c",
	},
}

// Namespace is the namespace of the properties that are axx's own.
const Namespace = "https://github.com/nimbusxr/axx/ns/controls"

// ids is the namespace the document's identifiers are made in: the same
// report gives the same identifiers.
var ids = uuid.MustParse("24f9a2f0-a1b5-4073-8909-566585873f56")

// now is the time of a report that has no run's.
var now = time.Now

func init() {
	controlscore.RegisterFormat(controlscore.Format{
		Name:  FormatName,
		Doc:   "OSCAL assessment results, as JSON (OSCAL " + OSCALVersion + ")",
		Write: Write,
	})
}

// Pack returns the controls-oscal pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Experimental: true,
		Requires: []string{controlscore.Name}, Steps: []core.StepDef{},
	}
}

const packDoc = "The report of `controls-core` as OSCAL assessment results: the format of NIST's Open Security Controls Assessment Language, which government programs and compliance tools read.\n\n" +
	"```sh\naxx controls report --format oscal --out assessment-results.json\n```\n\n" +
	"| In the report | In the document |\n|---|---|\n" +
	"| a framework | a result, with the controls it reviewed |\n" +
	"| a scenario that claims a control | an observation, of the method TEST, with the feature file as its evidence |\n" +
	"| what another source says of a control | an observation, with a link to the source |\n" +
	"| a control | a finding: satisfied or not satisfied, with the observations it rests on |\n" +
	"| a control that does not apply | a finding that is satisfied, whose implementation status is not-applicable |\n" +
	"| a risk accepted until a day | a risk whose deviation is approved, with that day as its deadline |\n\n" +
	"A control is named as its framework's OSCAL catalog names it (`ia-5.1` for NIST's IA-5(1)), or as a name OSCAL accepts (`V6.2.1` for ASVS). " +
	"The document is of OSCAL " + OSCALVersion + ", valid against NIST's schema, and has the identifiers and times the format requires: the same report gives the same document. " +
	"It names no assessment plan, as none is recorded with the scenarios: `import-ap` points to a note in the document that says so. " +
	"It has controls' IDs and titles, and none of a framework's text."

type document struct {
	Results assessmentResults `json:"assessment-results"`
}

type assessmentResults struct {
	UUID       string     `json:"uuid"`
	Metadata   metadata   `json:"metadata"`
	ImportAP   importAP   `json:"import-ap"`
	Results    []result   `json:"results"`
	BackMatter backMatter `json:"back-matter"`
}

type metadata struct {
	Title        string `json:"title"`
	LastModified string `json:"last-modified"`
	Version      string `json:"version"`
	OSCALVersion string `json:"oscal-version"`
	Props        []prop `json:"props,omitempty"`
	Remarks      string `json:"remarks,omitempty"`
}

type prop struct {
	Name  string `json:"name"`
	NS    string `json:"ns"`
	Value string `json:"value"`
}

type importAP struct {
	Href    string `json:"href"`
	Remarks string `json:"remarks,omitempty"`
}

type backMatter struct {
	Resources []resource `json:"resources"`
}

type resource struct {
	UUID        string `json:"uuid"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type result struct {
	UUID             string           `json:"uuid"`
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	Start            string           `json:"start"`
	End              string           `json:"end,omitempty"`
	Props            []prop           `json:"props,omitempty"`
	ReviewedControls reviewedControls `json:"reviewed-controls"`
	Observations     []observation    `json:"observations,omitempty"`
	Risks            []risk           `json:"risks,omitempty"`
	Findings         []finding        `json:"findings,omitempty"`
	Remarks          string           `json:"remarks,omitempty"`
}

type reviewedControls struct {
	ControlSelections []controlSelection `json:"control-selections"`
}

type controlSelection struct {
	IncludeAll      *struct{}  `json:"include-all,omitempty"`
	IncludeControls []selected `json:"include-controls,omitempty"`
}

type selected struct {
	ControlID string `json:"control-id"`
}

type observation struct {
	UUID             string     `json:"uuid"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Props            []prop     `json:"props,omitempty"`
	Links            []link     `json:"links,omitempty"`
	Methods          []string   `json:"methods"`
	RelevantEvidence []evidence `json:"relevant-evidence,omitempty"`
	Collected        string     `json:"collected"`
}

type link struct {
	Href string `json:"href"`
	Rel  string `json:"rel,omitempty"`
}

type evidence struct {
	Href        string `json:"href,omitempty"`
	Description string `json:"description"`
}

type risk struct {
	UUID        string `json:"uuid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Statement   string `json:"statement"`
	Status      string `json:"status"`
	Deadline    string `json:"deadline,omitempty"`
}

type finding struct {
	UUID                string    `json:"uuid"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	Props               []prop    `json:"props,omitempty"`
	Target              target    `json:"target"`
	RelatedObservations []related `json:"related-observations,omitempty"`
	RelatedRisks        []related `json:"related-risks,omitempty"`
}

type target struct {
	Type                 string      `json:"type"`
	TargetID             string      `json:"target-id"`
	Title                string      `json:"title,omitempty"`
	Status               status      `json:"status"`
	ImplementationStatus *implStatus `json:"implementation-status,omitempty"`
}

type status struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Remarks string `json:"remarks,omitempty"`
}

type implStatus struct {
	State   string `json:"state"`
	Remarks string `json:"remarks,omitempty"`
}

type related struct {
	ObservationUUID string `json:"observation-uuid,omitempty"`
	RiskUUID        string `json:"risk-uuid,omitempty"`
}

// Write writes the report as OSCAL assessment results.
func Write(w io.Writer, r *controlscore.Report) error {
	b, err := json.MarshalIndent(build(r), "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// build makes the document of a report.
func build(r *controlscore.Report) document {
	// The document's time is the run's; a report of no run has only the time
	// it is written at.
	when := r.Ended
	if when.IsZero() {
		when = now()
	}
	when = when.UTC().Truncate(time.Second)
	start := r.Started.UTC().Truncate(time.Second)
	if r.Started.IsZero() {
		start = when
	}
	of := r.Commit + "|" + stamp(when)
	id := func(parts ...string) string {
		return uuid.NewSHA1(ids, []byte(strings.Join(parts, "|")+"|"+of)).String()
	}

	version := stamp(when)
	if r.Commit != "" {
		version = short(r.Commit)
	}
	noPlan := uuid.NewSHA1(ids, []byte("no assessment plan")).String()
	doc := assessmentResults{
		UUID: id("assessment-results"),
		Metadata: metadata{
			Title:        "Assessment results: what covers the controls, by acceptance scenarios and other evidence",
			LastModified: stamp(when),
			Version:      version,
			OSCALVersion: OSCALVersion,
		},
		ImportAP: importAP{Href: "#" + noPlan},
		BackMatter: backMatter{Resources: []resource{{
			UUID:        noPlan,
			Title:       "No assessment plan",
			Description: "No assessment plan is recorded with these results. They are of the project's acceptance scenarios, which claim the controls they prove with tags, of what other sources say of the controls, and of the exceptions the project recorded.",
		}}},
	}
	if r.Axx != "" {
		doc.Metadata.Props = append(doc.Metadata.Props, own("axx-version", r.Axx))
	}
	if r.Commit != "" {
		doc.Metadata.Props = append(doc.Metadata.Props, own("commit", r.Commit))
	}
	if r.Dirty {
		doc.Metadata.Props = append(doc.Metadata.Props, own("uncommitted-changes", "true"))
	}
	if r.Partial != "" {
		doc.Metadata.Remarks = line("The runs are not of every scenario (" + r.Partial + "): a control they leave not covered may be one they did not get to.")
	}

	for _, f := range r.Frameworks {
		name := strings.TrimSpace(f.Title + " " + f.Version)
		if name == "" {
			name = f.Key
		}
		res := result{
			UUID:  id("result", f.Key),
			Title: line(name),
			Start: stamp(start),
			Props: []prop{own("framework", f.Key)},
		}
		if !r.Ended.IsZero() {
			res.End = stamp(when)
		}
		for _, l := range r.Lists {
			if l.Framework == f.Key {
				res.Props = append(res.Props, own("required-list", l.Name))
			}
		}
		observed := map[string]string{}
		observe := func(key string, o observation) string {
			if have, ok := observed[key]; ok {
				return have
			}
			o.UUID = id("observation", f.Key, key)
			o.Collected = orElse(o.Collected, stamp(when))
			observed[key] = o.UUID
			res.Observations = append(res.Observations, o)
			return o.UUID
		}
		var sel controlSelection
		for _, cr := range r.Controls {
			if cr.Framework != f.Key {
				continue
			}
			sel.IncludeControls = append(sel.IncludeControls, selected{ControlID: cr.Token})
			fd := finding{
				UUID:        id("finding", cr.Control),
				Title:       line(strings.TrimSpace(cr.ID + " " + cr.Title)),
				Description: line(describe(cr)),
				Props:       []prop{own("state", string(cr.State)), own("required", fmt.Sprint(cr.Required))},
				Target:      target{Type: "objective-id", TargetID: cr.Token, Title: line(cr.Title), Status: statusOf(cr)},
			}
			if cr.List != "" {
				fd.Props = append(fd.Props, own("required-list", cr.List))
			}
			for _, sc := range cr.Scenarios {
				at := fmt.Sprintf("%s:%d", sc.URI, sc.Line)
				uid := observe("scenario|"+at, observation{
					Title:       line(sc.Name),
					Description: line(fmt.Sprintf("The acceptance scenario %q (%s) %s.", sc.Name, at, sc.Status)),
					Props:       []prop{own("status", sc.Status)},
					Methods:     []string{"TEST"},
					RelevantEvidence: []evidence{{
						Href:        (&url.URL{Path: sc.URI, Fragment: fmt.Sprintf("L%d", sc.Line)}).String(),
						Description: line(fmt.Sprintf("The scenario, at line %d of its feature file.", sc.Line)),
					}},
				})
				fd.RelatedObservations = append(fd.RelatedObservations, related{ObservationUUID: uid})
			}
			for _, o := range cr.Other {
				says := fmt.Sprintf("%s says that %s is %s.", o.Source, cr.Control, o.State)
				if o.Note != "" {
					says += " " + o.Note
				}
				ob := observation{
					Title:       line(o.Source),
					Description: line(says),
					Props:       []prop{own("state", string(o.State))},
					Methods:     []string{"UNKNOWN"},
				}
				if u, err := url.Parse(o.Link); err == nil && o.Link != "" {
					ob.Links = []link{{Href: u.String(), Rel: "reference"}}
				}
				if day, err := time.Parse(time.DateOnly, o.Date); err == nil {
					ob.Collected = stamp(day)
				}
				uid := observe("source|"+o.Source+"|"+cr.Control, ob)
				fd.RelatedObservations = append(fd.RelatedObservations, related{ObservationUUID: uid})
			}
			if e := cr.Exception; e != nil && !cr.Expired {
				switch e.Kind {
				case controlscore.NotApplicable:
					fd.Target.ImplementationStatus = &implStatus{State: "not-applicable", Remarks: line(e.Reason)}
				case controlscore.Accepted:
					rk := risk{
						UUID:        id("risk", cr.Control),
						Title:       line("Accepted: " + strings.TrimSpace(cr.ID+" "+cr.Title)),
						Description: line(fmt.Sprintf("%s is not covered, and the project accepted that until %s.", cr.Control, e.Until)),
						Statement:   line(e.Reason),
						Status:      "deviation-approved",
					}
					if day, err := time.Parse(time.DateOnly, e.Until); err == nil {
						rk.Deadline = stamp(day.AddDate(0, 0, 1).Add(-time.Second))
					}
					res.Risks = append(res.Risks, rk)
					fd.RelatedRisks = append(fd.RelatedRisks, related{RiskUUID: rk.UUID})
				}
			}
			res.Findings = append(res.Findings, fd)
		}
		res.ReviewedControls.ControlSelections = []controlSelection{sel}
		res.Description = line(fmt.Sprintf("How %s of %s stand: what the project's acceptance scenarios prove, what other sources say, and the exceptions the project recorded.", count(len(res.Findings), "control"), name))
		var notes []string
		for _, u := range r.Unknown {
			if strings.HasPrefix(u.Tag, "@"+f.Key+":") {
				notes = append(notes, fmt.Sprintf("The tag %s names no control of %s.", u.Tag, name))
			}
		}
		if len(notes) > 0 {
			res.Remarks = line(strings.Join(notes, " "))
		}
		doc.Results = append(doc.Results, res)
	}
	if len(doc.Results) == 0 {
		// The format takes at least one result: a report of no control says so.
		doc.Results = []result{{
			UUID:             id("result"),
			Title:            "No controls",
			Description:      "The project requires no control, and no scenario claims one.",
			Start:            stamp(start),
			ReviewedControls: reviewedControls{ControlSelections: []controlSelection{{IncludeAll: &struct{}{}}}},
		}}
	}
	return document{Results: doc}
}

// statusOf is a control's state as OSCAL's: satisfied or not, and why. A
// control that does not apply is satisfied, and its implementation status
// says that it does not apply; a risk accepted for a time leaves its control
// not satisfied, with the risk beside it.
func statusOf(cr controlscore.ControlResult) status {
	st := status{State: "not-satisfied", Reason: "other", Remarks: line(describe(cr))}
	switch cr.State {
	case controlscore.Proven, controlscore.Covered:
		st.State, st.Reason = "satisfied", "pass"
	case controlscore.Failing:
		st.Reason = "fail"
	case controlscore.Excepted:
		if cr.Exception != nil && cr.Exception.Kind == controlscore.NotApplicable {
			st.State, st.Reason = "satisfied", "not-applicable"
		}
	}
	return st
}

// describe says how a control stands, in a line.
func describe(cr controlscore.ControlResult) string {
	why := strings.TrimSuffix(cr.Why(), ".")
	switch {
	case cr.State == controlscore.Proven:
		return fmt.Sprintf("%s is proven by %s that passed.", cr.Control, why)
	case cr.State == controlscore.Covered:
		return fmt.Sprintf("%s is covered by %s.", cr.Control, why)
	case cr.State == controlscore.Excepted && cr.Exception != nil && cr.Exception.Kind == controlscore.Accepted:
		return fmt.Sprintf("%s is not covered, which is %s.", cr.Control, why)
	case cr.State == controlscore.Excepted:
		return fmt.Sprintf("%s %s.", cr.Control, why)
	case why != "":
		return fmt.Sprintf("%s is %s: %s.", cr.Control, cr.State, why)
	}
	return fmt.Sprintf("%s is %s.", cr.Control, cr.State)
}

// own is a property of axx's own.
func own(name, value string) prop {
	return prop{Name: name, NS: Namespace, Value: line(value)}
}

// line is a text as the format takes one: on one line, with no space around
// it, and never empty.
func line(s string) string {
	if s = strings.Join(strings.Fields(s), " "); s == "" {
		return "-"
	}
	return s
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func orElse(s, other string) string {
	if s != "" {
		return s
	}
	return other
}

func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}
