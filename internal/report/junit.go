package report

import (
	"encoding/xml"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// junit writes JUnit XML as accepted by GitHub test reporters, Jenkins and
// GitLab: one <testsuite> per feature file, one <testcase> per scenario.
type junit struct {
	out  *writer
	opts Options
}

type junitSuites struct {
	XMLName   xml.Name      `xml:"testsuites"`
	Name      string        `xml:"name,attr"`
	Tests     int           `xml:"tests,attr"`
	Failures  int           `xml:"failures,attr"`
	Errors    int           `xml:"errors,attr"`
	Skipped   int           `xml:"skipped,attr"`
	Time      string        `xml:"time,attr"`
	Timestamp string        `xml:"timestamp,attr,omitempty"`
	Suites    []*junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Errors     int             `xml:"errors,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Time       string          `xml:"time,attr"`
	Timestamp  string          `xml:"timestamp,attr,omitempty"`
	File       string          `xml:"file,attr,omitempty"`
	Properties []junitProperty `xml:"properties>property,omitempty"`
	Cases      []*junitCase    `xml:"testcase"`

	started time.Time
	elapsed time.Duration
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	File      string        `xml:"file,attr,omitempty"`
	Line      int           `xml:"line,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	SystemOut *junitText    `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",cdata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

type junitText struct {
	Text string `xml:",cdata"`
}

func (j *junit) Envelope(*messages.Envelope) {}

func (j *junit) ScenarioFinished(*runner.ScenarioResult) {}

func (j *junit) Finish(res *runner.RunResult) error {
	started := res.Started
	if started.IsZero() && j.opts.Now != nil {
		started = j.opts.Now()
	}
	doc := &junitSuites{Name: "axx", Time: seconds(res.Duration), Timestamp: junitTime(started)}
	byURI := map[string]*junitSuite{}
	for _, r := range res.Scenarios {
		uri := j.opts.uri(r)
		suite := byURI[uri]
		if suite == nil {
			suite = &junitSuite{
				Name: xmlText(featureName(r)), File: xmlText(uri), started: r.Started,
				Properties: []junitProperty{{Name: "axx.version", Value: xmlText(j.opts.Version)}},
			}
			if suite.Name == "" {
				suite.Name = suite.File
			}
			byURI[uri] = suite
			doc.Suites = append(doc.Suites, suite)
		}
		if !r.Started.IsZero() && (suite.started.IsZero() || r.Started.Before(suite.started)) {
			suite.started = r.Started
		}
		suite.elapsed += r.Duration
		tc := j.testCase(r)
		suite.Cases = append(suite.Cases, tc)
		suite.Tests++
		switch {
		case tc.Failure != nil:
			suite.Failures++
		case tc.Skipped != nil:
			suite.Skipped++
		}
	}
	if len(res.RunErrors) > 0 {
		// Problems found after the scenarios get a suite of their own.
		suite := &junitSuite{Name: "axx run", started: started}
		for _, re := range res.RunErrors {
			suite.Cases = append(suite.Cases, &junitCase{
				Name: xmlText(re.Source), Classname: "axx run",
				Failure: &junitFailure{
					Type: "RunError", Message: xmlText(truncateRunes(firstLine(re.Message), 500)), Body: xmlText(re.Message),
				},
			})
			suite.Tests++
			suite.Failures++
		}
		doc.Suites = append(doc.Suites, suite)
	}
	for _, s := range doc.Suites {
		s.Time = seconds(s.elapsed)
		s.Timestamp = junitTime(s.started)
		doc.Tests += s.Tests
		doc.Failures += s.Failures
		doc.Skipped += s.Skipped
	}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	j.out.put(xml.Header + string(b) + "\n")
	return j.out.err
}

func (j *junit) testCase(r *runner.ScenarioResult) *junitCase {
	name := scenarioName(r)
	if isOutlineRow(r) {
		name += " (example line " + strconv.Itoa(r.Pickle.Line) + ")"
	}
	tc := &junitCase{
		Name: xmlText(name), Classname: xmlText(featureName(r)), Time: seconds(r.Duration),
		File: xmlText(j.opts.uri(r)),
	}
	if r.Pickle != nil {
		tc.Line = r.Pickle.Line
	}
	s := culprit(r)
	var f *failure
	if s != nil {
		f = describe(s)
	}
	switch r.Status {
	case runner.Failed, runner.Undefined, runner.Ambiguous:
		tc.Failure = &junitFailure{Type: "Error", Message: r.Status.String()}
		if f != nil {
			where := j.opts.stepLoc(r, s)
			title := stepTitle(s, phaseOf(r, s))
			tc.Failure.Type = f.errorType()
			tc.Failure.Message = xmlText(truncateRunes(firstLine(failureSummary(f, title)), 500))
			body := "Step: " + title
			if where != "" {
				body += " (" + where + ")"
			}
			if text := f.text(); text != "" {
				body += "\n\n" + text
			}
			tc.Failure.Body = xmlText(body)
		}
	case runner.Pending, runner.Skipped:
		tc.Skipped = &junitSkipped{}
		if f != nil {
			tc.Skipped.Message = xmlText(firstLine(failureSummary(f, stepTitle(s, phaseOf(r, s)))))
		}
	}
	tc.SystemOut = &junitText{Text: xmlText(junitStepListing(r))}
	return tc
}

// failureSummary is a one-line description suitable for a message attribute.
func failureSummary(f *failure, title string) string {
	switch f.kind {
	case KindUndefined:
		return "undefined step: " + title
	case KindAmbiguous:
		return f.message + ": " + title
	case KindPending:
		return "pending step: " + title
	}
	return f.headline()
}

// junitStepListing mirrors Cucumber-JVM's JUnit output: each step padded with
// dots to its status, followed by step logs.
func junitStepListing(r *runner.ScenarioResult) string {
	var b strings.Builder
	var logs []string
	allSteps(r, func(s *runner.StepResult, p phase) {
		if p != phaseStep && (s.Status == runner.Passed || s.Status == runner.Skipped) {
			return
		}
		line := stepTitle(s, p)
		n := utf8.RuneCountInString(line)
		b.WriteString(line)
		for {
			b.WriteByte('.')
			n++
			if n >= 76 {
				break
			}
		}
		b.WriteString(s.Status.String() + "\n")
		for _, l := range s.Logs {
			logs = append(logs, line+": "+l)
		}
	})
	if len(logs) > 0 {
		b.WriteString("\nLogs:\n")
		for _, l := range logs {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// junitTime formats a timestamp as the JUnit schema expects (ISO 8601
// without a zone; axx writes UTC).
func junitTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05")
}

func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}

// xmlText makes s safe for XML 1.0: ANSI escapes are removed and characters
// XML cannot represent are replaced with U+FFFD. (encoding/xml escapes
// markup but does not filter characters inside CDATA sections.)
func xmlText(s string) string {
	s = stripANSI(s)
	if utf8.ValidString(s) && strings.IndexFunc(s, func(r rune) bool { return !isXMLChar(r) }) < 0 {
		return s
	}
	// strings.Map also turns invalid UTF-8 bytes into U+FFFD.
	return strings.Map(func(r rune) rune {
		if isXMLChar(r) {
			return r
		}
		return '\uFFFD'
	}, s)
}

func isXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}
