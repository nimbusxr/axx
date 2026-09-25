package ide

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func cfg() *config.Config {
	return &config.Config{Apps: config.Apps{
		{Name: "api", Debug: &config.Debug{Debugger: &config.Debugger{Port: 5005}}},
		{Name: "worker", Debug: &config.Debug{Debugger: &config.Debugger{Type: "go", Port: 2345}}},
		{Name: "plain"},
	}}
}

func TestIntelliJ(t *testing.T) {
	dir := t.TempDir()
	files, err := IntelliJ(cfg(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		".run/Debugger_api.run.xml": true, ".run/Debugger_worker.run.xml": true, ".run/axx_run.run.xml": true,
		".run/axx_debug.run.xml": true, ".run/axx_validate.run.xml": true, ".run/axx_debug_all.run.xml": true,
		".run/axx_debug_steps.run.xml": true, ".run/Debugger_axx-steps.run.xml": true,
	}
	for _, f := range files {
		if !want[f.Path] || f.Action != "create" {
			t.Errorf("unexpected %+v", f)
		}
		b, _ := os.ReadFile(filepath.Join(dir, f.Path))
		var v any
		if err := xml.Unmarshal(b, &v); err != nil && !strings.Contains(err.Error(), "EOF") {
			t.Errorf("%s is not XML: %v", f.Path, err)
		}
	}
	api, _ := os.ReadFile(filepath.Join(dir, ".run/Debugger_api.run.xml"))
	if !strings.Contains(string(api), `name="SERVER_MODE" value="true"`) || !strings.Contains(string(api), `name="Debugger: api"`) {
		t.Errorf("java ide-listens config:\n%s", api)
	}
	steps, _ := os.ReadFile(filepath.Join(dir, ".run/Debugger_axx-steps.run.xml"))
	for _, want := range []string{`name="Debugger: axx-steps" type="GoRemoteDebugConfigurationType" host="127.0.0.1" port="2345"`} {
		if !strings.Contains(string(steps), want) {
			t.Errorf("steps debugger lacks %s:\n%s", want, steps)
		}
	}
	// The Go plugin reads host and port as attributes of <configuration>.
	worker, _ := os.ReadFile(filepath.Join(dir, ".run/Debugger_worker.run.xml"))
	if !strings.Contains(string(worker), `type="GoRemoteDebugConfigurationType" host="localhost" port="2345"`) || strings.Contains(string(worker), "<option") {
		t.Errorf("go debugger config:\n%s", worker)
	}
	all, _ := os.ReadFile(filepath.Join(dir, ".run/axx_debug_all.run.xml"))
	if !strings.Contains(string(all), `toRun name="Debugger: api"`) || !strings.Contains(string(all), `toRun name="axx: debug"`) {
		t.Errorf("compound:\n%s", all)
	}

	// Regenerating is a no-op; user-edited files are kept.
	edited := filepath.Join(dir, ".run/axx_run.run.xml")
	b, _ := os.ReadFile(edited)
	if err := os.WriteFile(edited, append(b, []byte("<!-- mine -->\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err = IntelliJ(cfg(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		switch f.Path {
		case ".run/axx_run.run.xml":
			if f.Action != "kept" {
				t.Errorf("edited file must be kept: %+v", f)
			}
		default:
			if f.Action != "unchanged" {
				t.Errorf("regeneration should be a no-op: %+v", f)
			}
		}
	}
}

func TestVSCodeMergesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	vs := filepath.Join(dir, ".vscode")
	if err := os.MkdirAll(vs, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  // user comment
  "version": "0.2.0",
  "configurations": [
    {"name": "My app", "type": "go", "request": "launch", "program": "."},
    {"name": "axx: attach stale", "type": "go", "request": "attach"},
  ]
}`
	if err := os.WriteFile(filepath.Join(vs, "launch.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VSCode(cfg(), dir); err != nil {
		t.Fatal(err)
	}
	var launch struct {
		Configurations []map[string]any `json:"configurations"`
		Compounds      []map[string]any `json:"compounds"`
	}
	b, _ := os.ReadFile(filepath.Join(vs, "launch.json"))
	if err := json.Unmarshal(b, &launch); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, c := range launch.Configurations {
		names = append(names, c["name"].(string))
	}
	if strings.Join(names, ",") != "My app,axx: attach api,axx: attach worker,axx: debug steps,axx: attach to steps" {
		t.Fatalf("configurations: %v", names)
	}
	if c := launch.Configurations[3]; c["type"] != "go" || c["mode"] != "remote" || c["port"] != float64(2345) || c["preLaunchTask"] != "axx: run --debug-steps" {
		t.Errorf("debug steps configuration: %v", c)
	}
	if len(launch.Compounds) != 1 || launch.Compounds[0]["name"] != "axx: attach all" {
		t.Fatalf("compounds: %v", launch.Compounds)
	}
	var tasks struct {
		Tasks []map[string]any `json:"tasks"`
	}
	b, _ = os.ReadFile(filepath.Join(vs, "tasks.json"))
	if err := json.Unmarshal(b, &tasks); err != nil || len(tasks.Tasks) != 4 || tasks.Tasks[2]["label"] != "axx: run --debug-steps" || tasks.Tasks[2]["isBackground"] != true {
		t.Fatalf("tasks: %v %v", tasks, err)
	}
	// idempotent
	files, err := VSCode(cfg(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Action != "unchanged" {
			t.Errorf("second run should be unchanged: %+v", f)
		}
	}
}

func TestStripJSONC(t *testing.T) {
	in := `{"a": "http://x//y", /* c */ "b": [1, 2,], // tail
}`
	var v map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(in)), &v); err != nil || v["a"] != "http://x//y" {
		t.Fatalf("%v %v", v, err)
	}
}

// Go and JavaScript remote debuggers keep host and port as attributes; a
// non-default port must survive.
func TestRemoteDebuggerAttributes(t *testing.T) {
	for typ, want := range map[string]string{
		"go":     `type="GoRemoteDebugConfigurationType" host="10.0.0.5" port="40000"`,
		"nodejs": `type="ChromiumRemoteDebugType" host="10.0.0.5" port="40000"`,
	} {
		x, err := intellijDebugger("Debugger: x", config.Debugger{Type: typ, Host: "10.0.0.5", Port: 40000})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(x, want) || strings.Contains(x, "<option") {
			t.Errorf("%s:\n%s", typ, x)
		}
	}
}
