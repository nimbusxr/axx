package results

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func file(agent string, rewards map[string]map[string]float64) *File {
	f := &File{SchemaVersion: SchemaVersion, Kind: Kind, Agent: agent, Conditions: []string{"none", "skills"}}
	for id, byCond := range rewards {
		t := Task{ID: id, Results: map[string]*Result{}}
		for c, r := range byCond {
			passed := 0
			if r >= 1 {
				passed = 1
			}
			t.Results[c] = &Result{Reward: r, Trials: 1, Passed: passed}
		}
		f.Tasks = append(f.Tasks, t)
	}
	f.Scores = f.Score(nil)
	return f
}

func TestCompare(t *testing.T) {
	base := file("claude-code", map[string]map[string]float64{
		"a": {"none": 1, "skills": 1}, "b": {"none": 1, "skills": 1}, "c": {"none": 0, "skills": 1}, "d": {"none": 0, "skills": 1},
	})
	same := file("claude-code", map[string]map[string]float64{
		"a": {"none": 1, "skills": 1}, "b": {"none": 1, "skills": 1}, "c": {"none": 0, "skills": 1}, "d": {"none": 1, "skills": 1},
		"new": {"none": 0, "skills": 0}, // not in the baseline: ignored
	})
	c, err := Compare(base, same, 10)
	if err != nil {
		t.Fatal(err)
	}
	if c.Regressed {
		t.Fatalf("no regression expected: %s", c)
	}
	worse := file("claude-code", map[string]map[string]float64{
		"a": {"none": 1, "skills": 1}, "b": {"none": 1, "skills": 0}, "c": {"none": 0, "skills": 1}, "d": {"none": 0, "skills": 1},
	})
	c, err = Compare(base, worse, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Regressed {
		t.Fatalf("skills dropped 25 points; want a regression: %s", c)
	}
	if !strings.Contains(c.String(), "REGRESSION") || len(c.TaskChanges) != 1 {
		t.Fatalf("report: %s", c)
	}
	if _, err := Compare(base, file("codex", nil), 10); err == nil {
		t.Fatal("comparing different agents must fail")
	}
}

func TestMarkdownAndRoundTrip(t *testing.T) {
	f := file("claude-code", map[string]map[string]float64{"a": {"none": 1, "skills": 0}})
	f.Skipped = []Skipped{{ID: "k", Reason: "requires the kafka pack(s), not in this axx build"}}
	md := f.Markdown()
	for _, want := range []string{"| a | - | pass | fail |", "**100.0** | **0.0**", "`k`"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	p := filepath.Join(t.TempDir(), "r.json")
	if err := f.Write(p); err != nil {
		t.Fatal(err)
	}
	back, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if back.Scores["none"] != 100 || back.Scores["skills"] != 0 {
		t.Fatalf("scores = %v", back.Scores)
	}
}

func TestFromHarborJob(t *testing.T) {
	job := t.TempDir()
	trial := func(name, body, verify string) {
		d := filepath.Join(job, name)
		if err := os.MkdirAll(filepath.Join(d, "verifier"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "result.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if verify != "" {
			if err := os.WriteFile(filepath.Join(d, "verifier", "verify.json"), []byte(verify), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	trial("a__1", `{"task_name":"axx-evals/a","task_id":{"path":"/x/a"},"verifier_result":{"rewards":{"reward":1}},"exception_info":null}`,
		`{"mutantsTotal":2,"mutants":{"m1":{"killed":true},"m2":{"killed":true}}}`)
	trial("a__2", `{"task_name":"axx-evals/a","task_id":{"path":"/x/a"},"verifier_result":{"rewards":{"reward":0}},"exception_info":null}`,
		`{"mutantsTotal":2,"mutants":{"m1":{"killed":true},"m2":{"killed":false}}}`)
	trial("b__1", `{"task_name":"axx-evals/b","task_id":{"path":"b"},"verifier_result":null,"exception_info":{"exception_type":"AgentTimeoutError"},"agent_result":{"cost_usd":0.5,"n_input_tokens":10,"n_output_tokens":2}}`, "")
	res, err := FromHarborJob(job)
	if err != nil {
		t.Fatal(err)
	}
	a, b := res["a"], res["b"]
	if a == nil || a.Trials != 2 || a.Passed != 1 || a.Reward != 0.5 || a.MutantsCaught != 3 {
		t.Fatalf("a = %+v", a)
	}
	if b == nil || b.Errors != 1 || b.Reward != 0 || b.CostUSD != 0.5 {
		t.Fatalf("b = %+v", b)
	}
}
