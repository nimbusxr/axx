package verify

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// Feature files are acceptance criteria written for people: a product owner
// must be able to read each scenario as a plain statement of behavior. These
// rules reject the ways a suite smuggles programming into Gherkin; they apply
// to every feature file the agent added or changed.
//
//   - variables: no ${var:...} or ${env:...}; data is chosen up front
//     (${sys:...} configuration such as service addresses is fine);
//   - doc-string-code: no code in doc strings (a doc string may carry a
//     payload: JSON, XML, YAML, CSV or text);
//   - scenario-name: every scenario has a name, unique within its feature;
//   - no-outcome: every scenario states an outcome (a Then step).

var (
	variableRef  = regexp.MustCompile(`\$\{(var|env):[^}]*\}`)
	payloadTypes = map[string]bool{"": true, "json": true, "xml": true, "yaml": true, "yml": true, "text": true, "txt": true, "plain": true, "csv": true, "html": true, "markdown": true, "md": true}
	keywordRe    = regexp.MustCompile(`^(Given|When|Then|And|But|\*)\s`)
	scenarioRe   = regexp.MustCompile(`^(Scenario Outline|Scenario Template|Scenario|Example):\s*(.*)$`)
)

type scenarioInfo struct {
	name    string
	line    int
	outcome bool
}

// CheckFeature applies the readability rules to one feature file.
func CheckFeature(rel string, content []byte) []Finding {
	var out []Finding
	add := func(rule string, line int, format string, args ...any) {
		out = append(out, Finding{Rule: rule, Path: fmt.Sprintf("%s:%d", rel, line), Message: fmt.Sprintf(format, args...)})
	}
	var scenarios []*scenarioInfo
	var cur *scenarioInfo
	inDoc, docFence := false, ""
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if m := variableRef.FindString(line); m != "" {
			add("variables", n, "%s: choose the data up front; feature files carry no variables or captured values", m)
		}
		if inDoc {
			if line == docFence {
				inDoc = false
			}
			continue
		}
		if strings.HasPrefix(line, `"""`) || strings.HasPrefix(line, "```") {
			docFence = line[:3]
			mediaType := strings.ToLower(strings.TrimSpace(line[3:]))
			if !payloadTypes[mediaType] {
				add("doc-string-code", n, "a %q doc string: no code in Gherkin (doc strings carry payloads)", mediaType)
			}
			inDoc = true
			continue
		}
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if m := scenarioRe.FindStringSubmatch(line); m != nil {
			cur = &scenarioInfo{name: strings.TrimSpace(m[2]), line: n}
			scenarios = append(scenarios, cur)
			continue
		}
		if strings.HasPrefix(line, "Background:") || strings.HasPrefix(line, "Rule:") || strings.HasPrefix(line, "Feature:") {
			cur = nil
			continue
		}
		if cur != nil && keywordRe.MatchString(line) && strings.HasPrefix(line, "Then ") {
			cur.outcome = true
		}
	}
	seen := map[string]int{}
	for _, s := range scenarios {
		if s.name == "" {
			add("scenario-name", s.line, "a scenario without a name: name it after the behavior it shows")
			continue
		}
		if first, dup := seen[strings.ToLower(s.name)]; dup {
			add("scenario-name", s.line, "scenario name %q repeats line %d", s.name, first)
		}
		seen[strings.ToLower(s.name)] = s.line
		if !s.outcome {
			add("no-outcome", s.line, "scenario %q has no Then step: state the observable outcome", s.name)
		}
	}
	return out
}
