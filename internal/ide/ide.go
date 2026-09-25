// Package ide generates IDE run and debug configurations from axx.yaml, so
// debugging an app under test is one click in IntelliJ or VS Code.
package ide

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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

// debuggerOf returns the app's debugger with defaults applied.
func debuggerOf(a config.App) (config.Debugger, bool) {
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

// IntelliJ writes .run/*.run.xml files under projectDir.
func IntelliJ(cfg *config.Config, projectDir string) ([]File, error) {
	dir := filepath.Join(projectDir, ".run")
	type gen struct{ name, xml string }
	var gens []gen
	var debuggers []string
	for _, a := range cfg.Apps {
		d, ok := debuggerOf(a)
		if !ok {
			continue
		}
		name := "Debugger: " + a.Name
		x, err := intellijDebugger(name, d)
		if err != nil {
			return nil, fmt.Errorf("apps.%s.debug.debugger: %w", a.Name, err)
		}
		gens = append(gens, gen{name, x})
		debuggers = append(debuggers, name)
	}
	// Breakpoints in step code: `axx run --debug-steps` asks the plugin to
	// start "Debugger: axx-steps", a Go Remote configuration (GoLand, or the
	// Go plugin).
	steps, _ := intellijDebugger(StepsDebugger, config.Debugger{Type: "go", Host: "127.0.0.1", Port: StepsPort})
	gens = append(gens,
		gen{"axx: run", intellijShell("axx: run", "axx run")},
		gen{"axx: debug", intellijShell("axx: debug", "axx run --debug")},
		gen{"axx: debug steps", intellijShell("axx: debug steps", "axx run --debug-steps")},
		gen{StepsDebugger, steps},
		gen{"axx: validate", intellijShell("axx: validate", "axx validate")},
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
	return out, nil
}

func intellijDebugger(name string, d config.Debugger) (string, error) {
	esc := html.EscapeString
	switch d.Type {
	case "java":
		serverMode := d.Mode != "app-listens"
		module := ""
		if d.Module != "" {
			module = fmt.Sprintf("    <module name=%q />\n", esc(d.Module))
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
`, esc(name), serverMode, esc(d.Host), d.Port, serverMode, module), nil
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
	esc := html.EscapeString
	return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="%s" host="%s" port="%d">
    <method v="2" />
  </configuration>
</component>
`, esc(name), typ, esc(d.Host), d.Port)
}

func simpleXML(name, typ, hostOpt, portOpt string, d config.Debugger) string {
	esc := html.EscapeString
	return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="%s">
    <option name="%s" value="%s" />
    <option name="%s" value="%d" />
    <method v="2" />
  </configuration>
</component>
`, esc(name), typ, hostOpt, esc(d.Host), portOpt, d.Port)
}

func intellijShell(name, command string) string {
	esc := html.EscapeString
	return fmt.Sprintf(`<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="%s" type="ShConfigurationType">
    <option name="SCRIPT_TEXT" value="%s" />
    <option name="INDEPENDENT_SCRIPT_PATH" value="true" />
    <option name="SCRIPT_PATH" value="" />
    <option name="SCRIPT_OPTIONS" value="" />
    <option name="INDEPENDENT_SCRIPT_WORKING_DIRECTORY" value="true" />
    <option name="SCRIPT_WORKING_DIRECTORY" value="$PROJECT_DIR$" />
    <option name="INDEPENDENT_INTERPRETER_PATH" value="true" />
    <option name="INTERPRETER_PATH" value="" />
    <option name="INTERPRETER_OPTIONS" value="" />
    <option name="EXECUTE_IN_TERMINAL" value="false" />
    <option name="EXECUTE_SCRIPT_FILE" value="false" />
    <envs />
    <method v="2" />
  </configuration>
</component>
`, esc(name), esc(command))
}

func intellijCompound(name string, members []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<component name=\"ProjectRunConfigurationManager\">\n  <configuration default=\"false\" name=%q type=\"CompoundRunConfigurationType\">\n", html.EscapeString(name))
	for _, m := range members {
		typ := "ShConfigurationType"
		if strings.HasPrefix(m, "Debugger: ") {
			typ = "Remote"
		}
		fmt.Fprintf(&b, "    <toRun name=%q type=%q />\n", html.EscapeString(m), typ)
	}
	b.WriteString("    <method v=\"2\" />\n  </configuration>\n</component>\n")
	return b.String()
}

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
// `axx: up --debug` task into .vscode/tasks.json. Entries named "axx: ..."
// are regenerated; everything else is preserved.
func VSCode(cfg *config.Config, projectDir string) ([]File, error) {
	var launches []map[string]any
	var names []string
	for _, a := range cfg.Apps {
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
			return nil, fmt.Errorf("apps.%s.debug.debugger: unknown type %q", a.Name, d.Type)
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
	lf, err := mergeVSCode(filepath.Join(projectDir, ".vscode", "launch.json"), "configurations", launches, projectDir, func(doc map[string]any) {
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
	tf, err := mergeVSCode(filepath.Join(projectDir, ".vscode", "tasks.json"), "tasks", tasks, projectDir, nil)
	if err != nil {
		return nil, err
	}
	return append(files, tf), nil
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
