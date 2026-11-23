package controlsoscal

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	controlscore "github.com/nimbusxr/axx/packs/controls/core"
)

// schemas are NIST's schemas of OSCAL's assessment results: the version the
// document says it is of, and the latest.
var schemas = Schemas

func schema(t *testing.T, file, sum string) *jsonschema.Schema {
	t.Helper()
	packed, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if h := sha256.Sum256(raw); hex.EncodeToString(h[:]) != sum {
		t.Fatalf("%s is not the schema NIST published: its SHA-256 is %s", file, hex.EncodeToString(h[:]))
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource("oscal.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("oscal.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// valid holds a document to NIST's schemas.
func valid(t *testing.T, doc []byte) {
	t.Helper()
	for _, s := range schemas {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema(t, s.File, s.SHA256).Validate(inst); err != nil {
			t.Errorf("not valid OSCAL %s:\n%v\n%s", s.Version, err, doc)
		}
	}
}

// depot is a report of every kind of thing a report has: two frameworks, an
// ASVS requirement and a NIST enhancement, each state, a scenario that claims
// two controls, another source's findings and both kinds of exception.
func depot() *controlscore.Report {
	signIn := controlscore.ScenarioRef{URI: "features/sign in/sign-in.feature", Line: 12, Name: "A password under 12 characters is refused", Status: "passed"}
	lockout := controlscore.ScenarioRef{URI: "features/sign in/sign-in.feature", Line: 21, Name: "An account locks after\nfive wrong passwords", Status: "failed"}
	penTest := controlscore.OtherFinding{Source: "Pen test, March 2027", Link: "https://example.com/reports/pen-test-2027-03", Date: "2027-03-14", State: controlscore.Covered, Note: "Section 4.2"}
	return &controlscore.Report{
		Axx: "0.8.0", Commit: "16777dd164c6dc8be54c4dd510addf814686139f", Dirty: true,
		Started: time.Date(2027, 3, 20, 9, 14, 7, 250, time.UTC), Ended: time.Date(2027, 3, 20, 9, 16, 41, 0, time.UTC),
		Partial: "it runs the scenarios --tags selects",
		Frameworks: []controlscore.ReportFramework{
			{Key: "asvs", Title: "OWASP ASVS", Version: "5.0.0"},
			{Key: "nist", Title: "NIST SP 800-53", Version: "5.2.0"},
			{Key: "house"},
		},
		Lists: []controlscore.ReportList{{Name: "asvs-5.0-level-1", Framework: "asvs", Version: "5.0.0", Controls: 70}},
		Controls: []controlscore.ControlResult{
			{Control: "asvs:6.2.1", Framework: "asvs", ID: "6.2.1", Token: "V6.2.1", Title: "Password Security", Required: true, List: "asvs-5.0-level-1", State: controlscore.Proven, Scenarios: []controlscore.ScenarioRef{signIn}},
			{Control: "asvs:6.3.1", Framework: "asvs", ID: "6.3.1", Token: "V6.3.1", Title: "General Authentication Security", Required: true, List: "asvs-5.0-level-1", State: controlscore.Failing, Scenarios: []controlscore.ScenarioRef{lockout}},
			{Control: "asvs:7.1.1", Framework: "asvs", ID: "7.1.1", Token: "V7.1.1", Title: "Session Management Documentation", Required: true, List: "asvs-5.0-level-1", State: controlscore.Covered, Other: []controlscore.OtherFinding{penTest}},
			{
				Control: "asvs:5.2.1", Framework: "asvs", ID: "5.2.1", Token: "V5.2.1", Title: "File Upload and Content", Required: true, List: "asvs-5.0-level-1", State: controlscore.Excepted,
				Exception: &controlscore.Exception{Control: "asvs:5.2.1", Kind: controlscore.NotApplicable, Reason: "The service takes no files."},
			},
			{
				Control: "asvs:6.3.3", Framework: "asvs", ID: "6.3.3", Token: "V6.3.3", Title: "General Authentication Security", Required: true, List: "asvs-5.0-level-1", State: controlscore.Excepted,
				Exception: &controlscore.Exception{Control: "asvs:6.3.3", Kind: controlscore.Accepted, Reason: "Multi-factor sign-in ships in the June release.", Until: "2027-06-30"},
			},
			{Control: "asvs:8.2.2", Framework: "asvs", ID: "8.2.2", Token: "V8.2.2", Title: "General Authorization Design", Required: true, List: "asvs-5.0-level-1", Needs: 2, State: controlscore.PartlyProven, Scenarios: []controlscore.ScenarioRef{signIn}},
			{
				Control: "asvs:9.1.1", Framework: "asvs", ID: "9.1.1", Token: "V9.1.1", Title: "Token source and integrity", Required: true, List: "asvs-5.0-level-1", State: controlscore.NotCovered,
				Exception: &controlscore.Exception{Control: "asvs:9.1.1", Kind: controlscore.Accepted, Reason: "Until the gateway checks tokens.", Until: "2027-03-01"}, Expired: true,
			},
			{Control: "nist:IA-5.1", Framework: "nist", ID: "IA-5.1", Token: "ia-5.1", Title: "Password-based Authentication", State: controlscore.Proven, Scenarios: []controlscore.ScenarioRef{signIn}},
			{Control: "house:lockout", Framework: "house", ID: "lockout", Token: "lockout", State: controlscore.NotCovered},
		},
		Unknown: []controlscore.UnknownClaim{{Tag: "@asvs:7.2.99", Nearest: "@asvs:7.2.1"}},
	}
}

func written(t *testing.T, r *controlscore.Report) ([]byte, assessmentResults) {
	t.Helper()
	var b bytes.Buffer
	if err := Write(&b, r); err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), doc.Results
}

// What the pack writes is valid OSCAL, by NIST's own schemas: for an ASVS
// requirement, a NIST enhancement and a control of a list in a file.
func TestTheDocumentIsValidOSCAL(t *testing.T) {
	doc, _ := written(t, depot())
	valid(t, doc)
}

func TestAResultForEachFramework(t *testing.T) {
	_, ar := written(t, depot())
	if ar.Metadata.OSCALVersion != "1.1.3" || ar.Metadata.Version != "16777dd164c6" || ar.Metadata.LastModified != "2027-03-20T09:16:41Z" {
		t.Errorf("the metadata: %+v", ar.Metadata)
	}
	if !strings.Contains(ar.Metadata.Remarks, "it runs the scenarios --tags selects") {
		t.Errorf("a run of part of the suite is not said: %q", ar.Metadata.Remarks)
	}
	if len(ar.BackMatter.Resources) != 1 || ar.ImportAP.Href != "#"+ar.BackMatter.Resources[0].UUID {
		t.Errorf("the plan the results name: %+v, %+v", ar.ImportAP, ar.BackMatter)
	}
	if len(ar.Results) != 3 {
		t.Fatalf("%d results", len(ar.Results))
	}
	asvs, nist, house := ar.Results[0], ar.Results[1], ar.Results[2]
	if asvs.Title != "OWASP ASVS 5.0.0" || nist.Title != "NIST SP 800-53 5.2.0" || house.Title != "house" {
		t.Errorf("the results: %q, %q, %q", asvs.Title, nist.Title, house.Title)
	}
	if asvs.Start != "2027-03-20T09:14:07Z" || asvs.End != "2027-03-20T09:16:41Z" {
		t.Errorf("the run's times: %s to %s", asvs.Start, asvs.End)
	}
	var reviewed []string
	for _, c := range asvs.ReviewedControls.ControlSelections[0].IncludeControls {
		reviewed = append(reviewed, c.ControlID)
	}
	if got := strings.Join(reviewed, " "); got != "V6.2.1 V6.3.1 V7.1.1 V5.2.1 V6.3.3 V8.2.2 V9.1.1" {
		t.Errorf("the controls reviewed: %s", got)
	}
	if got := nist.ReviewedControls.ControlSelections[0].IncludeControls[0].ControlID; got != "ia-5.1" {
		t.Errorf("NIST's enhancement is named %q", got)
	}
	if !strings.Contains(asvs.Remarks, "@asvs:7.2.99 names no control of OWASP ASVS 5.0.0") || nist.Remarks != "" {
		t.Errorf("a tag that names no control: %q, %q", asvs.Remarks, nist.Remarks)
	}
	// One scenario is one observation of a result, whatever claims it; the
	// same scenario is another's in another framework's result.
	if len(asvs.Observations) != 3 || len(nist.Observations) != 1 || asvs.Observations[0].UUID == nist.Observations[0].UUID {
		t.Errorf("observations: %d and %d", len(asvs.Observations), len(nist.Observations))
	}
}

func TestHowEachStateIsWritten(t *testing.T) {
	_, ar := written(t, depot())
	asvs := ar.Results[0]
	by := map[string]finding{}
	for _, f := range asvs.Findings {
		by[f.Target.TargetID] = f
	}
	for id, want := range map[string]status{
		"V6.2.1": {State: "satisfied", Reason: "pass"},
		"V6.3.1": {State: "not-satisfied", Reason: "fail"},
		"V7.1.1": {State: "satisfied", Reason: "pass"},
		"V5.2.1": {State: "satisfied", Reason: "not-applicable"},
		"V6.3.3": {State: "not-satisfied", Reason: "other"},
		"V8.2.2": {State: "not-satisfied", Reason: "other"},
		"V9.1.1": {State: "not-satisfied", Reason: "other"},
	} {
		got := by[id].Target.Status
		if got.State != want.State || got.Reason != want.Reason || got.Remarks == "" {
			t.Errorf("%s: %+v, want %s (%s)", id, got, want.State, want.Reason)
		}
		if by[id].Target.Type != "objective-id" {
			t.Errorf("%s: its target is a %s", id, by[id].Target.Type)
		}
	}

	// A scenario is an observation of a test, with its feature file.
	proven := by["V6.2.1"]
	if len(proven.RelatedObservations) != 1 || proven.RelatedObservations[0].ObservationUUID != asvs.Observations[0].UUID {
		t.Fatalf("the proven control's observations: %+v", proven.RelatedObservations)
	}
	ob := asvs.Observations[0]
	if ob.Methods[0] != "TEST" || ob.RelevantEvidence[0].Href != "features/sign%20in/sign-in.feature#L12" || ob.Props[0].Value != "passed" || ob.Collected != "2027-03-20T09:16:41Z" {
		t.Errorf("a scenario's observation: %+v", ob)
	}
	if failed := asvs.Observations[1]; failed.Title != "An account locks after five wrong passwords" || failed.Props[0].Value != "failed" {
		t.Errorf("a failed scenario's observation: %+v", failed)
	}
	// A partly proven control rests on the same scenario: no second observation.
	if partly := by["V8.2.2"]; partly.RelatedObservations[0].ObservationUUID != ob.UUID || !strings.Contains(partly.Description, "1 scenario of the 2 it takes") {
		t.Errorf("the partly proven control: %+v", partly)
	}

	// What another source says is an observation with its link and its day.
	other := asvs.Observations[2]
	if other.Title != "Pen test, March 2027" || other.Methods[0] != "UNKNOWN" || other.Links[0].Href != "https://example.com/reports/pen-test-2027-03" ||
		other.Collected != "2027-03-14T00:00:00Z" || !strings.Contains(other.Description, "Section 4.2") {
		t.Errorf("another source's observation: %+v", other)
	}

	// A control that does not apply says so in its implementation status.
	if na := by["V5.2.1"]; na.Target.ImplementationStatus == nil || na.Target.ImplementationStatus.State != "not-applicable" ||
		na.Target.ImplementationStatus.Remarks != "The service takes no files." || len(na.RelatedRisks) != 0 {
		t.Errorf("a control that does not apply: %+v", na.Target)
	}
	// A risk accepted until a day is a risk whose deviation is approved, to
	// the end of that day; one whose day has passed is no risk any more.
	if len(asvs.Risks) != 1 {
		t.Fatalf("%d risks", len(asvs.Risks))
	}
	rk := asvs.Risks[0]
	if rk.Status != "deviation-approved" || rk.Deadline != "2027-06-30T23:59:59Z" || rk.Statement != "Multi-factor sign-in ships in the June release." {
		t.Errorf("the accepted risk: %+v", rk)
	}
	if acc := by["V6.3.3"]; len(acc.RelatedRisks) != 1 || acc.RelatedRisks[0].RiskUUID != rk.UUID {
		t.Errorf("the control whose risk is accepted: %+v", acc.RelatedRisks)
	}
	if ended := by["V9.1.1"]; len(ended.RelatedRisks) != 0 || !strings.Contains(ended.Description, "its exception ended on 2027-03-01") {
		t.Errorf("the control whose exception ended: %+v", ended)
	}
}

// The same report gives the same document, byte for byte, and another run's
// has identifiers of its own.
func TestTheSameReportGivesTheSameDocument(t *testing.T) {
	one, ar := written(t, depot())
	two, _ := written(t, depot())
	if !bytes.Equal(one, two) {
		t.Error("two documents of one report differ")
	}
	later := depot()
	later.Ended = later.Ended.Add(time.Hour)
	_, next := written(t, later)
	if next.UUID == ar.UUID || next.Results[0].Findings[0].UUID == ar.Results[0].Findings[0].UUID {
		t.Error("another run's document has the same identifiers")
	}
	if next.ImportAP.Href != ar.ImportAP.Href {
		t.Error("the note of no assessment plan is another in another document")
	}
}

// A report of no run has the time it is written at, and one of no control is
// still a document.
func TestAReportOfNoRunAndOfNoControl(t *testing.T) {
	now = func() time.Time { return time.Date(2027, 4, 1, 8, 0, 0, 0, time.FixedZone("CEST", 7200)) }
	t.Cleanup(func() { now = time.Now })

	r := depot()
	r.Started, r.Ended, r.Commit = time.Time{}, time.Time{}, ""
	doc, ar := written(t, r)
	valid(t, doc)
	if ar.Metadata.LastModified != "2027-04-01T06:00:00Z" || ar.Metadata.Version != "2027-04-01T06:00:00Z" || ar.Results[0].Start != "2027-04-01T06:00:00Z" || ar.Results[0].End != "" {
		t.Errorf("the times of a report of no run: %+v, %s to %q", ar.Metadata, ar.Results[0].Start, ar.Results[0].End)
	}

	doc, ar = written(t, &controlscore.Report{})
	valid(t, doc)
	if len(ar.Results) != 1 || ar.Results[0].ReviewedControls.ControlSelections[0].IncludeAll == nil || len(ar.Results[0].Findings) != 0 {
		t.Errorf("a report of no control: %+v", ar.Results)
	}
}

// The schemas do hold a document to the format: a control named as OSCAL
// names none (a digit first) is refused, which is why a framework says how its
// IDs are written as names.
func TestTheSchemasRefuseWhatIsNotOSCAL(t *testing.T) {
	r := depot()
	r.Controls[0].Token = "6.2.1"
	doc, _ := written(t, r)
	for _, s := range schemas {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema(t, s.File, s.SHA256).Validate(inst); err == nil {
			t.Errorf("OSCAL %s takes the control-id 6.2.1", s.Version)
		}
	}
}

// The format is one `axx controls report --format` knows, by the pack's being
// among a project's.
func TestThePackRegistersTheFormat(t *testing.T) {
	m := Pack().Manifest()
	if m.Name != "controls-oscal" || !m.Experimental || len(m.Requires) != 1 || m.Requires[0] != controlscore.Name {
		t.Errorf("the manifest: %+v", m)
	}
	if !controlscore.HasFormat(FormatName) {
		t.Error("the format oscal is not registered")
	}
}
