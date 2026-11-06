package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repo is a repository whose axx.yaml is in a folder of any name, qa/suite,
// and that folder.
func repo(t *testing.T) (root, suite string) {
	t.Helper()
	root = t.TempDir()
	suite = filepath.Join(root, "qa", "suite")
	for _, d := range []string{filepath.Join(root, ".git"), suite} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, suite
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func actions(files []File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		out[f.Path] = f.Action
	}
	return out
}

// IntelliJ opens a repository at its root: the run configurations go there,
// and run axx where axx.yaml is, whatever that folder is called.
func TestIntelliJRunsAxxWhereAxxYamlIs(t *testing.T) {
	root, suite := repo(t)
	files, err := IntelliJ(cfg(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(files)["../../.run/axx_run.run.xml"]; got != "create" {
		t.Fatalf("written at the repository's root: %+v", files)
	}
	run, _ := os.ReadFile(filepath.Join(root, ".run", "axx_run.run.xml"))
	if !strings.Contains(string(run), `name="SCRIPT_WORKING_DIRECTORY" value="$PROJECT_DIR$/qa/suite"`) {
		t.Errorf("axx: run's working directory:\n%s", run)
	}
	if _, err := os.Stat(filepath.Join(suite, ".run")); err == nil {
		t.Error("wrote .run beside axx.yaml too")
	}
}

// A project IntelliJ opened in axx.yaml's folder has its .idea there.
func TestIntelliJOpenInAxxYamlsFolder(t *testing.T) {
	_, suite := repo(t)
	if err := os.MkdirAll(filepath.Join(suite, ".idea"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := IntelliJ(cfg(), suite); err != nil {
		t.Fatal(err)
	}
	run, err := os.ReadFile(filepath.Join(suite, ".run", "axx_run.run.xml"))
	if err != nil || !strings.Contains(string(run), `name="SCRIPT_WORKING_DIRECTORY" value="$PROJECT_DIR$"`) {
		t.Errorf("axx: run (%v):\n%s", err, run)
	}
}

// What an earlier axx wrote beside axx.yaml goes, as IntelliJ would show it
// twice; a file the person edited stays, and the result says so.
func TestIntelliJMovesWhatAnEarlierAxxWrote(t *testing.T) {
	root, suite := repo(t)
	// An earlier axx: the repository was not one yet.
	if err := os.Remove(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	if _, err := IntelliJ(cfg(), suite); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := IntelliJ(cfg(), suite)
	if err != nil {
		t.Fatal(err)
	}
	got := actions(files)
	if got[".run/axx_run.run.xml"] != "remove" || got["../../.run/axx_run.run.xml"] != "create" {
		t.Fatalf("%+v", files)
	}
	if _, err := os.Stat(filepath.Join(suite, ".run")); err == nil {
		t.Error("the emptied .run is still there")
	}

	// Edited, it stays.
	if err := os.Remove(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	if _, err := IntelliJ(cfg(), suite); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(suite, ".run", "axx_validate.run.xml")
	b, _ := os.ReadFile(edited)
	write(t, edited, string(b)+"<!-- mine -->\n")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err = IntelliJ(cfg(), suite)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == ".run/axx_validate.run.xml" && (f.Action != "kept" || !strings.Contains(f.Reason, "../../.run")) {
			t.Errorf("the edited file: %+v", f)
		}
	}
	if _, err := os.Stat(edited); err != nil {
		t.Errorf("the edited file is gone: %v", err)
	}
}

// VS Code reads .vscode in the folder it has open, the repository's root: its
// tasks run axx where axx.yaml is.
func TestVSCodeRunsAxxWhereAxxYamlIs(t *testing.T) {
	root, suite := repo(t)
	// What an earlier axx wrote beside axx.yaml, which VS Code never read.
	if err := os.Remove(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	if _, err := VSCode(cfg(), suite); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := VSCode(cfg(), suite)
	if err != nil {
		t.Fatal(err)
	}
	got := actions(files)
	if got["../../.vscode/tasks.json"] != "create" || got["../../.vscode/launch.json"] != "create" ||
		got[".vscode/tasks.json"] != "remove" || got[".vscode/launch.json"] != "remove" {
		t.Fatalf("%+v", files)
	}
	if _, err := os.Stat(filepath.Join(suite, ".vscode")); err == nil {
		t.Error("the emptied .vscode is still there")
	}
	var tasks struct {
		Tasks []map[string]any `json:"tasks"`
	}
	b, _ := os.ReadFile(filepath.Join(root, ".vscode", "tasks.json"))
	if err := json.Unmarshal(b, &tasks); err != nil || len(tasks.Tasks) != 4 {
		t.Fatalf("tasks (%v): %s", err, b)
	}
	for _, task := range tasks.Tasks {
		if o, _ := task["options"].(map[string]any); o["cwd"] != "${workspaceFolder}/qa/suite" {
			t.Errorf("%s runs in %v", task["label"], task["options"])
		}
	}
}

// A folder whose .vscode holds the person's own settings is the one VS Code
// has open.
func TestVSCodeOpenInAxxYamlsFolder(t *testing.T) {
	_, suite := repo(t)
	write(t, filepath.Join(suite, ".vscode", "settings.json"), "{}\n")
	if _, err := VSCode(cfg(), suite); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(suite, ".vscode", "tasks.json"))
	if err != nil || strings.Contains(string(b), "cwd") {
		t.Errorf("tasks (%v): %s", err, b)
	}
}

// Outside a repository nothing above axx.yaml's folder says what is open, not
// even a .vscode or .idea there (VS Code keeps its extensions in ~/.vscode).
func TestEditorDirOutsideARepository(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".vscode", "extensions", "x"), "")
	if err := os.MkdirAll(filepath.Join(home, ".idea"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(home, "parcels")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := vscodeDir(project); got != project {
		t.Errorf("VS Code: %s", got)
	}
	if got := intellijDir(project); got != project {
		t.Errorf("IntelliJ: %s", got)
	}
}
