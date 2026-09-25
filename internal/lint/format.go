package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
)

// Formats are the output formats of `axx lint`.
var Formats = []string{"human", "json", "junit", "sarif", "github"}

// HumanOptions controls the human rendering.
type HumanOptions struct {
	// Compact prints only rules with findings, and the summary.
	Compact bool
	// All lists every finding and location instead of truncating at
	// lint.config.maxReportedValues / maxReportedLocations.
	All bool
}

// WriteHuman renders the report for a terminal, grouped by rule.
func WriteHuman(w io.Writer, rep *Report, o HumanOptions) error {
	for _, n := range rep.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	for _, rr := range rep.Rules {
		if o.Compact && len(rr.Findings) == 0 {
			continue
		}
		status := "ok  "
		switch {
		case !rr.OK():
			status = "FAIL"
		case len(rr.Findings) > 0:
			status = "warn"
		}
		var facts []string
		if rr.Validation != "" {
			facts = append(facts, rr.Validation)
		}
		if rr.Mode == ModeWarn && rr.Type != "builtin" {
			facts = append(facts, "warn mode")
		}
		switch rr.Files {
		case 0:
			facts = append(facts, "no files matched")
		default:
			facts = append(facts, pluralN(rr.Files, "file"))
		}
		line := fmt.Sprintf("%s %s (%s)", status, rr.Name, strings.Join(facts, ", "))
		if n := len(rr.Findings); n > 0 {
			line += ": " + findingsSummary(rr.Findings)
		}
		fmt.Fprintln(w, line)
		if len(rr.Findings) == 0 {
			continue
		}
		if rr.Description != "" && rr.Type != "builtin" {
			fmt.Fprintf(w, "     %s\n", rr.Description)
		}
		shown := rr.Findings
		if !o.All && rep.maxValues > 0 && len(shown) > rep.maxValues {
			shown = shown[:rep.maxValues]
		}
		for _, f := range shown {
			prefix := ""
			if f.Severity == SeverityWarning && !rr.OK() {
				prefix = "warning: "
			}
			fmt.Fprintf(w, "     %s%s [%s]\n", prefix, f.Message, f.Code)
			locs := f.Locations
			if !o.All && rep.maxLocations > 0 && len(locs) > rep.maxLocations {
				locs = locs[:rep.maxLocations]
			}
			width := 0
			for _, l := range locs {
				width = max(width, len(l.String()))
			}
			for _, l := range locs {
				if l.Text == "" {
					fmt.Fprintf(w, "       %s\n", l.String())
					continue
				}
				fmt.Fprintf(w, "       %-*s  %s\n", width, l.String(), l.Text)
			}
			if more := len(f.Locations) - len(locs); more > 0 {
				fmt.Fprintf(w, "       ... and %d more %s\n", more, pluralWord(more, "occurrence"))
			}
		}
		if more := len(rr.Findings) - len(shown); more > 0 {
			fmt.Fprintf(w, "     ... and %d more %s (use --json for all)\n", more, pluralWord(more, "finding"))
		}
	}
	s := rep.Summary
	status := "ok"
	if s.Errors > 0 || s.Warnings > 0 {
		status = fmt.Sprintf("%s, %s", pluralN(s.Errors, "error"), pluralN(s.Warnings, "warning"))
	}
	_, err := fmt.Fprintf(w, "axx lint: %s, %s: %s\n", pluralN(s.Rules, "rule"), pluralN(s.Files, "file"), status)
	return err
}

// findingsSummary counts a rule's findings: "2 duplicate values, 1 warning".
func findingsSummary(fs []Finding) string {
	dups, problems, warnings := 0, 0, 0
	for _, f := range fs {
		switch {
		case f.Code == CodeDuplicate:
			dups++
		case f.Severity == SeverityError:
			problems++
		default:
			warnings++
		}
	}
	var parts []string
	for _, c := range []struct {
		n    int
		word string
	}{{dups, "duplicate value"}, {problems, "problem"}, {warnings, "warning"}} {
		if c.n > 0 {
			parts = append(parts, pluralN(c.n, c.word))
		}
	}
	return strings.Join(parts, ", ")
}

func pluralN(n int, word string) string { return fmt.Sprintf("%d %s", n, pluralWord(n, word)) }

func pluralWord(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// ---- JUnit ----

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Errors   int         `xml:"errors,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut *junitText    `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",cdata"`
}

type junitText struct {
	Text string `xml:",cdata"`
}

// WriteJUnit renders one test case per rule. A rule in warn mode passes and carries its findings as system-out.
func WriteJUnit(w io.Writer, rep *Report) error {
	suite := junitSuite{Name: "axx lint", Tests: len(rep.Rules)}
	for _, rr := range rep.Rules {
		c := junitCase{Classname: "axx.lint", Name: rr.Name}
		if len(rr.Findings) > 0 {
			var b strings.Builder
			for _, f := range rr.Findings {
				fmt.Fprintf(&b, "%s: %s [%s]\n", f.Severity, f.Message, f.Code)
				for _, l := range f.Locations {
					fmt.Fprintf(&b, "  %s\n", l.String())
				}
			}
			if rr.OK() {
				c.SystemOut = &junitText{Text: "WARNING: " + rr.Name + "\n" + b.String()}
			} else {
				c.Failure = &junitFailure{Message: findingsSummary(rr.Findings), Type: firstNonEmpty(rr.Validation, rr.Type), Text: b.String()}
				suite.Failures++
			}
		}
		suite.Cases = append(suite.Cases, c)
	}
	doc := junitSuites{Name: "axx lint", Tests: suite.Tests, Failures: suite.Failures, Suites: []junitSuite{suite}}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---- SARIF 2.1.0 ----

// SARIFOptions controls the SARIF rendering.
type SARIFOptions struct {
	// Version is the axx version reported as the tool version.
	Version string
	// Root is the directory artifact URIs are relative to (%SRCROOT%),
	// normally the repository root.
	Root string
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool               sarifTool                  `json:"tool"`
	OriginalURIBaseIDs map[string]sarifArtifactLo `json:"originalUriBaseIds,omitempty"`
	ColumnKind         string                     `json:"columnKind"`
	Results            []sarifResult              `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	ShortDescription     sarifText         `json:"shortDescription"`
	FullDescription      *sarifText        `json:"fullDescription,omitempty"`
	HelpURI              string            `json:"helpUri"`
	DefaultConfiguration sarifRuleConfig   `json:"defaultConfiguration"`
	Properties           map[string]string `json:"properties,omitempty"`
}

type sarifRuleConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	RelatedLocations    []sarifLocation   `json:"relatedLocations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
	Properties          map[string]string `json:"properties,omitempty"`
}

type sarifLocation struct {
	ID               *int                  `json:"id,omitempty"`
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
	Message          *sarifText            `json:"message,omitempty"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLo `json:"artifactLocation"`
	Region           *sarifRegion    `json:"region,omitempty"`
}

type sarifArtifactLo struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

// WriteSARIF renders SARIF 2.1.0 for code scanning: one result per
// location of a finding, with the other occurrences as related locations.
func WriteSARIF(w io.Writer, rep *Report, o SARIFOptions) error {
	run := sarifRun{
		Tool:       sarifTool{Driver: sarifDriver{Name: "axx lint", InformationURI: axxerr.DocsBase, Version: o.Version, Rules: []sarifRule{}}},
		ColumnKind: "unicodeCodePoints",
		Results:    []sarifResult{},
	}
	if o.Root != "" {
		run.OriginalURIBaseIDs = map[string]sarifArtifactLo{"%SRCROOT%": {URI: fileURI(o.Root, true)}}
	}
	for i, rr := range rep.Rules {
		level := "error"
		if rr.Mode == ModeWarn {
			level = "warning"
		}
		sr := sarifRule{
			ID: rr.ID, Name: rr.Name, ShortDescription: sarifText{Text: rr.Name},
			HelpURI:              axxerr.DocsBase + "/references/error-codes/#" + strings.ToLower(ruleCode(rr)),
			DefaultConfiguration: sarifRuleConfig{Level: level},
		}
		if rr.Description != "" {
			sr.FullDescription = &sarifText{Text: rr.Description}
		}
		if rr.Validation != "" || rr.Type != "" {
			sr.Properties = map[string]string{"type": rr.Type}
			if rr.Validation != "" {
				sr.Properties["validation"] = rr.Validation
			}
		}
		run.Tool.Driver.Rules = append(run.Tool.Driver.Rules, sr)
		for _, f := range rr.Findings {
			level := "error"
			if f.Severity == SeverityWarning {
				level = "warning"
			}
			for li, l := range f.Locations {
				res := sarifResult{
					RuleID: rr.ID, RuleIndex: i, Level: level,
					Message:             sarifText{Text: sarifMessage(f, li)},
					Locations:           []sarifLocation{sarifLoc(l, o.Root, nil)},
					PartialFingerprints: map[string]string{"axxLint/v1": fingerprint(rr, f, l, o.Root)},
					Properties:          map[string]string{"code": f.Code},
				}
				if f.Code == CodeDuplicate {
					res.Properties["value"] = f.Value
				}
				n := 0
				for oi, other := range f.Locations {
					if oi == li {
						continue
					}
					n++
					id := n
					rl := sarifLoc(other, o.Root, &id)
					rl.Message = &sarifText{Text: "also here"}
					res.RelatedLocations = append(res.RelatedLocations, rl)
				}
				run.Results = append(run.Results, res)
			}
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(sarifLog{Schema: "https://json.schemastore.org/sarif-2.1.0.json", Version: "2.1.0", Runs: []sarifRun{run}})
}

// ruleCode is the error code documenting a rule's findings.
func ruleCode(rr RuleResult) string {
	if rr.Type == "builtin" {
		return CodeOrdinalMissing
	}
	return CodeDuplicate
}

func sarifMessage(f Finding, at int) string {
	if len(f.Locations) < 2 {
		return f.Message
	}
	var others []string
	for i, l := range f.Locations {
		if i != at {
			others = append(others, l.String())
		}
	}
	if len(others) > 5 {
		others = append(others[:5], fmt.Sprintf("and %d more", len(others)-5))
	}
	return f.Message + "; also at " + strings.Join(others, ", ")
}

func sarifLoc(l Location, root string, id *int) sarifLocation {
	art := sarifArtifactLo{URI: fileURI(l.abs, false)}
	if root != "" {
		if rel, err := filepath.Rel(root, l.abs); err == nil && !strings.HasPrefix(rel, "..") {
			art = sarifArtifactLo{URI: uriPath(filepath.ToSlash(rel)), URIBaseID: "%SRCROOT%"}
		}
	}
	loc := sarifLocation{ID: id, PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: art}}
	if l.Line > 0 {
		loc.PhysicalLocation.Region = &sarifRegion{StartLine: l.Line, StartColumn: l.Column}
	}
	return loc
}

func fileURI(p string, dir bool) string {
	s := filepath.ToSlash(p)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	if dir && !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return "file://" + uriPath(s)
}

func uriPath(p string) string {
	return (&url.URL{Path: p}).EscapedPath()
}

// fingerprint identifies a result across runs: a duplicate by its rule,
// value and file (so it survives edits that move lines), anything else by
// its rule, code, file and line.
func fingerprint(rr RuleResult, f Finding, l Location, root string) string {
	key := []string{rr.ID, f.Code, ghPath(l, root)}
	if f.Code == CodeDuplicate {
		key = append(key, f.Value)
	} else {
		key = append(key, fmt.Sprint(l.Line))
	}
	sum := sha256.Sum256([]byte(strings.Join(key, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// ---- GitHub Actions ----

// WriteGitHub renders GitHub Actions workflow commands: an ::error or
// ::warning annotation per location, then the summary line. Paths are
// relative to root (GITHUB_WORKSPACE, normally the repository root).
func WriteGitHub(w io.Writer, rep *Report, root string) error {
	for _, rr := range rep.Rules {
		for _, f := range rr.Findings {
			kind := "error"
			if f.Severity == SeverityWarning {
				kind = "warning"
			}
			for i, l := range f.Locations {
				props := []string{"file=" + ghProp(ghPath(l, root))}
				if l.Line > 0 {
					props = append(props, fmt.Sprintf("line=%d", l.Line))
					if l.Column > 0 {
						props = append(props, fmt.Sprintf("col=%d", l.Column))
					}
				}
				props = append(props, "title="+ghProp("axx lint: "+rr.Name))
				fmt.Fprintf(w, "::%s %s::%s\n", kind, strings.Join(props, ","), ghData(sarifMessage(f, i)+" ["+f.Code+"]"))
			}
		}
	}
	s := rep.Summary
	_, err := fmt.Fprintf(w, "axx lint: %s, %s: %s, %s\n", pluralN(s.Rules, "rule"), pluralN(s.Files, "file"), pluralN(s.Errors, "error"), pluralN(s.Warnings, "warning"))
	return err
}

func ghPath(l Location, root string) string {
	if root != "" && l.abs != "" {
		if rel, err := filepath.Rel(root, l.abs); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return l.File
}

func ghData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func ghProp(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// RepoRoot returns the directory machine formats make paths relative to:
// GITHUB_WORKSPACE when set, else the nearest ancestor of dir containing
// .git, else dir.
func RepoRoot(dir string, getenv func(string) string) string {
	if getenv != nil {
		if ws := getenv("GITHUB_WORKSPACE"); ws != "" {
			return ws
		}
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
