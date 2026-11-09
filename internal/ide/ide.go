// Package ide generates IDE run and debug configurations from axx.yaml, so
// debugging a service under test is one click in IntelliJ or VS Code.
package ide

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

// StepsDebugger is the IntelliJ run configuration that `axx run
// --debug-steps` asks the plugin to start, and StepsPort its Delve port.
const (
	StepsDebugger = "Debugger: axx-steps"
	StepsPort     = 2345
)

// File is a generated file and what happened to it.
type File struct {
	Path   string `json:"path"`
	Action string `json:"action"` // create | update | unchanged | kept
	Reason string `json:"reason,omitempty"`
}

// debuggerOf returns the service's debugger with defaults applied.
func debuggerOf(a config.Service) (config.Debugger, bool) {
	if a.Debug == nil || a.Debug.Debugger == nil {
		return config.Debugger{}, false
	}
	d := *a.Debug.Debugger
	if d.Type == "" {
		d.Type = "java"
	}
	if d.Host == "" {
		d.Host = "localhost"
	}
	if d.Mode == "" {
		d.Mode = "ide-listens"
		if d.Type != "java" {
			d.Mode = "app-listens"
		}
	}
	return d, true
}

// ---- IntelliJ ----

const markerPrefix = "<!-- axx-generated: "

// IntelliJ writes .run/*.run.xml files where IntelliJ has the project open
// (intellijDir), running axx in projectDir, the directory of axx.yaml.
func IntelliJ(cfg *config.Config, projectDir string) ([]File, error) {
	projectDir = filepath.Clean(projectDir)
	open := intellijDir(projectDir)
	dir := filepath.Join(open, ".run")
	workDir := "$PROJECT_DIR$"
	if rel := relSlash(open, projectDir); rel != "." {
		workDir += "/" + rel
	}
	shell := func(name, command string) string { return intellijShell(name, command, workDir) }
	type gen struct{ name, xml string }
	var gens []gen
	var debuggers []string
	for _, a := range cfg.Services {
		d, ok := debuggerOf(a)
		if !ok {
			continue
		}
		name := "Debugger: " + a.Name
		x, err := intellijDebugger(name, d)
		if err != nil {
			return nil, fmt.Errorf("services.%s.debug.debugger: %w", a.Name, err)
		}
		gens = append(gens, gen{name, x})
		debuggers = append(debuggers, name)
	}
	// Breakpoints in step code: `axx run --debug-steps` asks the plugin to
	// start "Debugger: axx-steps", a Go Remote configuration (GoLand, or the
	// Go plugin).
	steps, _ := intellijDebugger(StepsDebugger, config.Debugger{Type: "go", Host: "127.0.0.1", Port: StepsPort})
	gens = append(gens,
		gen{"axx: run", shell("axx: run", "axx run")},
		gen{"axx: debug", shell("axx: debug", "axx run --debug")},
		gen{"axx: debug steps", shell("axx: debug steps", "axx run --debug-steps")},
		gen{StepsDebugger, steps},
		gen{"axx: validate", shell("axx: validate", "axx validate")},
	)
	if len(debuggers) > 0 {
		gens = append(gens, gen{"axx: debug all", intellijCompound("axx: debug all", append(debuggers, "axx: debug"))})
	}
	var out []File
	for _, g := range gens {
		f, err := writeMarked(filepath.Join(dir, fileName(g.name)+".run.xml"), g.xml, projectDir)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if open == projectDir {
		return out, nil
	}
	// What an earlier axx wrote beside axx.yaml: IntelliJ finds run
	// configurations in any folder of the project, so these would show twice,
	// running axx where there is no axx.yaml.
	moved := relSlash(projectDir, dir)
	for _, g := range gens {
		f, err := removeMarked(filepath.Join(projectDir, ".run", fileName(g.name)+".run.xml"), projectDir, moved)
		if err != nil {
			return nil, err
		}
		if f.Action != "" {
			out = append(out, f)
		}
	}
	_ = os.Remove(filepath.Join(projectDir, ".run")) // only if it is empty
	return out, nil
}

// intellijDir is the directory IntelliJ has a project open in, for the
// project whose axx.yaml is in dir: the nearest from dir up to the
// repository's root that holds IntelliJ's .idea, or else the repository's
// root, where a repository is opened; outside one, dir.
func intellijDir(dir string) string {
	return editorDir(dir, func(d string) bool { return isDir(filepath.Join(d, ".idea")) })
}

// editorDir is the nearest directory from dir up to the repository's root
// (the directory that holds .git, where axx's own search for axx.yaml stops)
// that opened says an editor has open, or else the repository's root; outside
// a repository, dir, as nothing above it says what is open.
func editorDir(dir string, opened func(string) bool) string {
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return dir
		}
		root = parent
	}
	d := dir
	for d != root && !opened(d) {
		d = filepath.Dir(d)
	}
	return d
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// relSlash is target relative to base, with forward slashes, as editors'
// files write paths.
func relSlash(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

// removeMarked removes a file writeMarked wrote that is no longer where the
// editor reads it, unless the user edited it. No file says nothing.
func removeMarked(path, base, movedTo string) (File, error) {
	f := File{Path: relSlash(base, path)}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return f, err
	}
	if !isPristine(b) {
		f.Action, f.Reason = "kept", "edited by you; IntelliJ shows it beside the one in "+movedTo+" (delete it)"
		return f, nil
	}
	f.Action, f.Reason = "remove", "moved to "+movedTo
	return f, os.Remove(path)
}

func intellijDebugger(name string, d config.Debugger) (string, error) {
	switch d.Type {
	case "java":
		serverMode := d.Mode != "app-listens"
		module := ""
		if d.Module != "" {
			module = fmt.Sprintf("    <module name=\"%s\" />\n", xmlAttr(d.Module))
		}
		return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="Remote">
    <option name="USE_SOCKET_TRANSPORT" value="true" />
    <option name="SERVER_MODE" value="%t" />
    <option name="SHMEM_ADDRESS" />
    <option name="HOST" value="%s" />
    <option name="PORT" value="%d" />
    <option name="AUTO_RESTART" value="%t" />
%s    <method v="2" />
  </configuration>
</component>
`, xmlAttr(name), serverMode, xmlAttr(d.Host), d.Port, serverMode, module), nil
	case "go":
		return attributeXML(name, "GoRemoteDebugConfigurationType", d), nil
	case "nodejs":
		return attributeXML(name, "ChromiumRemoteDebugType", d), nil
	case "python":
		return simpleXML(name, "PyRemoteDebugConfigurationType", "HOST", "PORT", d), nil
	}
	return "", fmt.Errorf("unknown debugger type %q (java, go, nodejs, python)", d.Type)
}

// attributeXML writes a remote debugger whose configuration class keeps its
// host and port as attributes of the <configuration> element (the Go and
// JavaScript remote debuggers); as <option> elements they would be ignored.
func attributeXML(name, typ string, d config.Debugger) string {
	return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="%s" host="%s" port="%d">
    <method v="2" />
  </configuration>
</component>
`, xmlAttr(name), typ, xmlAttr(d.Host), d.Port)
}

func simpleXML(name, typ, hostOpt, portOpt string, d config.Debugger) string {
	return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="%s">
    <option name="%s" value="%s" />
    <option name="%s" value="%d" />
    <method v="2" />
  </configuration>
</component>
`, xmlAttr(name), typ, hostOpt, xmlAttr(d.Host), portOpt, d.Port)
}

func intellijShell(name, command, workDir string) string {
	return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="ShConfigurationType">
    <option name="SCRIPT_TEXT" value="%s" />
    <option name="INDEPENDENT_SCRIPT_PATH" value="true" />
    <option name="SCRIPT_PATH" value="" />
    <option name="SCRIPT_OPTIONS" value="" />
    <option name="INDEPENDENT_SCRIPT_WORKING_DIRECTORY" value="true" />
    <option name="SCRIPT_WORKING_DIRECTORY" value="%s" />
    <option name="INDEPENDENT_INTERPRETER_PATH" value="true" />
    <option name="INTERPRETER_PATH" value="" />
    <option name="INTERPRETER_OPTIONS" value="" />
    <option name="EXECUTE_IN_TERMINAL" value="false" />
    <option name="EXECUTE_SCRIPT_FILE" value="false" />
    <envs />
    <method v="2" />
  </configuration>
</component>
`, xmlAttr(name), xmlAttr(command), xmlAttr(workDir))
}

func intellijCompound(name string, members []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<component name=\"ProjectRunConfigurationManager\">\n  <configuration default=\"false\" name=\"%s\" type=\"CompoundRunConfigurationType\">\n", xmlAttr(name))
	for _, m := range members {
		typ := "ShConfigurationType"
		if strings.HasPrefix(m, "Debugger: ") {
			typ = "Remote"
		}
		fmt.Fprintf(&b, "    <toRun name=\"%s\" type=\"%s\" />\n", xmlAttr(m), typ)
	}
	b.WriteString("    <method v=\"2\" />\n  </configuration>\n</component>\n")
	return b.String()
}

// xmlAttrEscaper escapes a value for an XML attribute in double quotes, as
// IntelliJ writes its own: the markup characters and both quotes, and the line
// breaks and tabs an XML reader would turn into spaces.
var xmlAttrEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	"\n", "&#10;", "\r", "&#13;", "\t", "&#9;",
)

// xmlAttr is a value of axx.yaml (a service's name, a debugger's host) as an
// attribute's value in a run configuration.
func xmlAttr(s string) string { return xmlAttrEscaper.Replace(s) }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func fileName(name string) string {
	return strings.Trim(unsafeName.ReplaceAllString(name, "_"), "_")
}

// writeMarked writes content with a hash marker, leaving files the user
// edited (marker missing or hash mismatch) untouched.
func writeMarked(path, content, base string) (File, error) {
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])[:12]
	marked := markerPrefix + hash + " -->\n" + content
	rel, _ := filepath.Rel(base, path)
	f := File{Path: filepath.ToSlash(rel)}
	old, err := os.ReadFile(path)
	switch {
	case err == nil:
		if string(old) == marked {
			f.Action = "unchanged"
			return f, nil
		}
		if !isPristine(old) {
			f.Action, f.Reason = "kept", "edited by you (delete it to regenerate)"
			return f, nil
		}
		f.Action = "update"
	case errors.Is(err, os.ErrNotExist):
		f.Action = "create"
	default:
		return f, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return f, err
	}
	return f, os.WriteFile(path, []byte(marked), 0o644)
}

// isPristine reports whether a generated file still matches its marker hash.
func isPristine(b []byte) bool {
	s := string(b)
	if !strings.HasPrefix(s, markerPrefix) {
		return false
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return false
	}
	hash := strings.TrimSuffix(strings.TrimPrefix(s[:nl], markerPrefix), " -->")
	sum := sha256.Sum256([]byte(s[nl+1:]))
	return hex.EncodeToString(sum[:])[:12] == hash
}

// ---- VS Code ----

const vsPrefix = "axx: "

// VSCode merges attach configurations into .vscode/launch.json and an
// `axx: up --debug` task into .vscode/tasks.json, where VS Code has the
// project open (vscodeDir), its tasks running axx in projectDir, the
// directory of axx.yaml. Entries named "axx: ..." are regenerated; everything
// else is preserved.
func VSCode(cfg *config.Config, projectDir string) ([]File, error) {
	projectDir = filepath.Clean(projectDir)
	open := vscodeDir(projectDir)
	var options map[string]any
	if rel := relSlash(open, projectDir); rel != "." {
		options = map[string]any{"cwd": "${workspaceFolder}/" + rel}
	}
	var launches []map[string]any
	var names []string
	for _, a := range cfg.Services {
		d, ok := debuggerOf(a)
		if !ok {
			continue
		}
		name := vsPrefix + "attach " + a.Name
		c := map[string]any{"name": name, "request": "attach", "preLaunchTask": vsPrefix + "up --debug"}
		switch d.Type {
		case "java":
			c["type"], c["hostName"], c["port"] = "java", d.Host, d.Port
		case "go":
			c["type"], c["mode"], c["host"], c["port"] = "go", "remote", d.Host, d.Port
		case "nodejs":
			c["type"], c["address"], c["port"] = "node", d.Host, d.Port
		case "python":
			c["type"], c["connect"] = "debugpy", map[string]any{"host": d.Host, "port": d.Port}
		default:
			return nil, fmt.Errorf("services.%s.debug.debugger: unknown type %q", a.Name, d.Type)
		}
		launches = append(launches, c)
		names = append(names, name)
	}
	// Breakpoints in step code, with VS Code's Go extension: run
	// `axx run --debug-steps` and attach once it asks for a debugger.
	stepsAttach := func(name string) map[string]any {
		return map[string]any{"name": name, "type": "go", "request": "attach", "mode": "remote", "host": "127.0.0.1", "port": StepsPort}
	}
	debugSteps := stepsAttach(vsPrefix + "debug steps")
	debugSteps["preLaunchTask"] = vsPrefix + "run --debug-steps"
	launches = append(launches, debugSteps, stepsAttach(vsPrefix+"attach to steps"))
	var files []File
	lf, err := mergeVSCode(filepath.Join(open, ".vscode", "launch.json"), "configurations", launches, projectDir, func(doc map[string]any) {
		var comps []any
		if existing, ok := doc["compounds"].([]any); ok {
			for _, c := range existing {
				if m, ok := c.(map[string]any); ok && strings.HasPrefix(fmt.Sprint(m["name"]), vsPrefix) {
					continue
				}
				comps = append(comps, c)
			}
		}
		if len(names) > 1 {
			comps = append(comps, map[string]any{"name": vsPrefix + "attach all", "configurations": names})
		}
		if len(comps) > 0 {
			doc["compounds"] = comps
		} else {
			delete(doc, "compounds")
		}
	})
	if err != nil {
		return nil, err
	}
	files = append(files, lf)
	tasks := []map[string]any{
		{"label": vsPrefix + "up --debug", "type": "shell", "command": "axx up --debug", "problemMatcher": []any{}},
		{"label": vsPrefix + "run", "type": "shell", "command": "axx run", "group": "test", "problemMatcher": []any{}},
		{
			"label": vsPrefix + "run --debug-steps", "type": "shell", "command": "axx run --debug-steps", "isBackground": true,
			// Ready once axx asks for a debugger; "axx: debug steps" then attaches.
			"problemMatcher": map[string]any{
				"owner":   "axx",
				"pattern": map[string]any{"regexp": "^\\[AXX-IDE\\] never$"},
				"background": map[string]any{
					"activeBegin":   true,
					"beginsPattern": "^axx: ",
					"endsPattern":   "^\\[AXX-IDE\\] debug-attach-request name=axx-steps ",
				},
			},
		},
		{"label": vsPrefix + "down", "type": "shell", "command": "axx down", "problemMatcher": []any{}},
	}
	if options != nil {
		for _, t := range tasks {
			t["options"] = options
		}
	}
	tf, err := mergeVSCode(filepath.Join(open, ".vscode", "tasks.json"), "tasks", tasks, projectDir, nil)
	if err != nil {
		return nil, err
	}
	files = append(files, tf)
	if open == projectDir {
		return files, nil
	}
	// What an earlier axx wrote beside axx.yaml, which VS Code does not read
	// there: it holds only axx's entries, or vscodeDir would be this folder.
	for _, name := range []string{"launch.json", "tasks.json"} {
		path := filepath.Join(projectDir, ".vscode", name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		files = append(files, File{Path: relSlash(projectDir, path), Action: "remove", Reason: "moved to " + relSlash(projectDir, filepath.Join(open, ".vscode"))})
	}
	_ = os.Remove(filepath.Join(projectDir, ".vscode")) // only if it is empty
	return files, nil
}

// vscodeDir is the directory VS Code has a project open in, for the project
// whose axx.yaml is in dir: the nearest from dir up to the repository's root
// whose .vscode holds settings of the person's (anything but the entries axx
// writes), or else the repository's root, where a repository is opened;
// outside one, dir.
func vscodeDir(dir string) string {
	return editorDir(dir, func(d string) bool {
		entries, err := os.ReadDir(filepath.Join(d, ".vscode"))
		if err != nil {
			return false
		}
		for _, e := range entries {
			switch e.Name() {
			case "launch.json":
				if !onlyAxxs(filepath.Join(d, ".vscode", e.Name()), "configurations", "name") {
					return true
				}
			case "tasks.json":
				if !onlyAxxs(filepath.Join(d, ".vscode", e.Name()), "tasks", "label") {
					return true
				}
			default:
				return true
			}
		}
		return false
	})
}

// onlyAxxs reports whether a launch.json or tasks.json holds only what VSCode
// writes: its version and the entries (and compounds) named "axx: ...".
func onlyAxxs(path, key, nameKey string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC(b), &doc); err != nil {
		return false
	}
	for k, v := range doc {
		switch k {
		case "version":
		case key, "compounds":
			list, _ := v.([]any)
			for _, e := range list {
				m, ok := e.(map[string]any)
				name := "name"
				if k == key {
					name = nameKey
				}
				if !ok || !strings.HasPrefix(fmt.Sprint(m[name]), vsPrefix) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func mergeVSCode(path, key string, entries []map[string]any, base string, extra func(map[string]any)) (File, error) {
	rel, _ := filepath.Rel(base, path)
	f := File{Path: filepath.ToSlash(rel), Action: "create"}
	doc := map[string]any{"version": "0.2.0"}
	if key == "tasks" {
		doc["version"] = "2.0.0"
	}
	old, err := os.ReadFile(path)
	if err == nil {
		f.Action = "update"
		if err := json.Unmarshal(stripJSONC(old), &doc); err != nil {
			return f, fmt.Errorf("%s is not valid JSON (with comments): %w", rel, err)
		}
	}
	nameKey := "name"
	if key == "tasks" {
		nameKey = "label"
	}
	var list []any
	if existing, ok := doc[key].([]any); ok {
		for _, e := range existing {
			if m, ok := e.(map[string]any); ok && strings.HasPrefix(fmt.Sprint(m[nameKey]), vsPrefix) {
				continue
			}
			list = append(list, e)
		}
	}
	for _, e := range entries {
		list = append(list, e)
	}
	sort.SliceStable(list, func(i, j int) bool { return false })
	doc[key] = list
	if extra != nil {
		extra(doc)
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return f, err
	}
	b = append(b, '\n')
	if bytes.Equal(old, b) {
		f.Action = "unchanged"
		return f, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return f, err
	}
	return f, os.WriteFile(path, b, 0o644)
}

// stripJSONC removes // and /* */ comments and trailing commas (VS Code
// accepts both in its JSON files).
func stripJSONC(b []byte) []byte {
	var out []byte
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && (b[i] != '*' || b[i+1] != '/') {
				i++
			}
			i++
		default:
			out = append(out, c)
		}
	}
	return regexp.MustCompile(`,(\s*[}\]])`).ReplaceAll(out, []byte("$1"))
}
