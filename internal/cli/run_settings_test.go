package cli

import (
	"strings"
	"testing"
)

// settingsProject is a project whose run.tags leaves out its known bugs, with a
// profile whose run.uses keeps the scenarios that file letters.
func settingsProject(t *testing.T) {
	t.Helper()
	agentProject(t, map[string]string{
		"axx.yaml": `version: 1
run:
  tags: "not @known-bug"
profiles:
  letters:
    run:
      uses: [files]
`,
		"axx-packs.yaml": "packs:\n  - rest\n  - files\n",
		"features/claims.feature": `Feature: Claims

  Scenario: A claim is recorded
    Given a POST request to /claims

  @known-bug
  Scenario: A duplicate claim is refused
    Given a POST request to /claims

  Scenario: The claim's letter is filed
    Then the letters folder has a file named claim.pdf
`,
		"features/known.feature": `Feature: Known bugs

  @known-bug
  Scenario: A claim without a parcel is refused
    Given a POST request to /claims
`,
	})
}

// ran is which of the scenarios a dry run ran, and what it said on stderr.
func ran(t *testing.T, args ...string) (names []string, stderr string) {
	t.Helper()
	out, stderr, _ := run(t, append([]string{"run", "--dry-run", "--format", "pretty"}, args...)...)
	for _, n := range []string{"A claim is recorded", "A duplicate claim is refused", "The claim's letter is filed", "A claim without a parcel is refused"} {
		if strings.Contains(out, "Scenario: "+n+"  #") {
			names = append(names, n)
		}
	}
	return names, stderr
}

// run.tags and run.uses narrow the paths someone names as they narrow a whole run,
// and the run says what they left out of them and how to run it; a scenario
// picked by its line runs whatever they leave out.
func TestRunSettingsNarrowNamedPaths(t *testing.T) {
	settingsProject(t)
	tagsNote := `note: run.tags "not @known-bug" left out 1 scenario of the paths you named:
      features/claims.feature:7  A duplicate claim is refused
      to run them: change run.tags, replace it for a run with --tags, or run one by its line
`
	usesNote := `note: run.uses [files] left out 1 scenario of the paths you named:
      features/claims.feature:3  A claim is recorded
      to run them: change run.uses, or run one by its line
`
	for _, c := range []struct {
		name  string
		args  []string
		want  []string
		notes []string
	}{
		{"a whole run", nil, []string{"A claim is recorded", "The claim's letter is filed"}, nil},
		{"a file", []string{"features/claims.feature"}, []string{"A claim is recorded", "The claim's letter is filed"}, []string{tagsNote}},
		{"a scenario's line", []string{"features/claims.feature:7"}, []string{"A duplicate claim is refused"}, nil},
		{"--tags", []string{"--tags", "@known-bug", "features/claims.feature"}, []string{"A duplicate claim is refused"}, nil},
		{"run.uses", []string{"--profile", "letters", "features/claims.feature"}, []string{"The claim's letter is filed"}, []string{tagsNote, usesNote}},
		{"run.uses and a line", []string{"--profile", "letters", "features/claims.feature:3"}, []string{"A claim is recorded"}, nil},
	} {
		got, stderr := ran(t, c.args...)
		if strings.Join(got, ", ") != strings.Join(c.want, ", ") {
			t.Errorf("%s: ran %v, not %v", c.name, got, c.want)
		}
		if strings.Contains(stderr, "note: ") != (len(c.notes) > 0) {
			t.Errorf("%s: notes:\n%s", c.name, stderr)
		}
		for _, n := range c.notes {
			if !strings.Contains(stderr, n) {
				t.Errorf("%s: no note\n%s\nin:\n%s", c.name, n, stderr)
			}
		}
	}
}

// A directory named on the command line gets the note too, and a run that the
// settings leave nothing of says why.
func TestRunSettingsSayWhyNothingRan(t *testing.T) {
	settingsProject(t)
	got, stderr := ran(t, "features/known.feature")
	if len(got) != 0 || !strings.Contains(stderr, `note: run.tags "not @known-bug" left out 1 scenario of the paths you named:
      features/known.feature:4  A claim without a parcel is refused`) || !strings.Contains(stderr, "no scenarios matched") {
		t.Errorf("ran %v:\n%s", got, stderr)
	}
	if _, stderr := ran(t, "features"); !strings.Contains(stderr, "left out 2 scenarios of the paths you named:") {
		t.Errorf("a directory:\n%s", stderr)
	}
}

// Run where there is no axx.yaml (an editor's project root above it), axx says
// so rather than pointing at an axx.yaml's run.paths.
func TestRunWithoutAxxYamlSaysSo(t *testing.T) {
	t.Chdir(t.TempDir())
	_, stderr, _ := run(t, "run", "--dry-run")
	if !strings.Contains(stderr, "AXX-E0201") || !strings.Contains(stderr, "no axx.yaml was found here or above") || strings.Contains(stderr, "run.paths") {
		t.Errorf("%s", stderr)
	}
}
