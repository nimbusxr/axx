package lifecycle

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// PropertiesEnv names the file a service's command, or a hook, tells axx
// properties in: a line name=value for each, which features read as
// ${sys:name}, like the ports a stack's containers got.
const PropertiesEnv = "AXX_PROPERTIES"

// Error codes of the properties services tell axx.
const (
	// CodePropertiesLine: a line of a properties file is not name=value.
	CodePropertiesLine = "AXX-E0416"
	// CodePropertiesClash: two services, or hooks, told axx one property.
	CodePropertiesClash = "AXX-E0417"
)

// PropertiesDir is where a project's services tell axx their properties,
// each in a file of its own.
func PropertiesDir(projectDir string) string {
	return filepath.Join(projectDir, ".axx", "properties")
}

// PropertiesFile is the file a service or hook (writer) tells axx its
// properties in, in dir.
func PropertiesFile(dir, writer string) string {
	return filepath.Join(dir, url.PathEscape(writer))
}

// Told is a file someone told axx properties in: a service (Service names
// it), or a hook.
type Told struct {
	// Who wrote it, for errors: "service api", "the hook before `axx run`".
	Who     string
	Service string
	Path    string
}

// ToldValues are the properties someone told axx: a service (Service names
// it), or a hook.
type ToldValues struct {
	Who     string            `json:"who"`
	Service string            `json:"service,omitempty"`
	Values  map[string]string `json:"values"`
}

// ReadTold reads the properties told in files, in order: a missing file
// tells none, and a line that is not name=value is an error (blank lines
// are none). A property a file tells twice is its last.
func ReadTold(files []Told) ([]ToldValues, error) {
	out := make([]ToldValues, 0, len(files))
	for _, f := range files {
		values, err := readProperties(f.Path)
		if err != nil {
			return nil, axxerr.Wrap(err, CodePropertiesLine, exitcode.Environment, "%s told axx properties it cannot read", f.Who).
				WithHint("write a line name=value for each, like db.port=55432, to the file %s names", PropertiesEnv)
		}
		if len(values) > 0 {
			out = append(out, ToldValues{Who: f.Who, Service: f.Service, Values: values})
		}
	}
	return out, nil
}

// MergeTold is the properties told by all of them: an error when two told
// one property.
func MergeTold(told []ToldValues) (map[string]string, error) {
	values := map[string]string{}
	by := map[string]string{}
	for _, t := range told {
		for _, name := range slices.Sorted(maps.Keys(t.Values)) {
			if other, ok := by[name]; ok && other != t.Who {
				return nil, axxerr.New(CodePropertiesClash, exitcode.Usage, "%s and %s both told axx the property %s", other, t.Who, name).
					WithHint("name each one after what it is, like api.port and db.port, so that no two tell one")
			}
			values[name], by[name] = t.Values[name], t.Who
		}
	}
	return values, nil
}

// Unused says, a line each, which values services and hooks told that the
// run does not use, because the person set the property: with -D, or in
// axx.yaml's properties (a profile's included). What the person sets wins.
func Unused(cfg *config.Config, told []ToldValues) []string {
	var out []string
	for _, t := range told {
		for _, name := range slices.Sorted(maps.Keys(t.Values)) {
			set, where := cfg.SetBy(name)
			if where == "" || set == t.Values[name] {
				continue
			}
			line := fmt.Sprintf("using %s=%s, which %s sets, not %s, which %s wrote", name, set, where, t.Values[name], t.Who)
			if where == "axx.yaml" {
				line = fmt.Sprintf("using %s=%s from axx.yaml, not %s, which %s wrote: a value you set wins (remove it from axx.yaml's properties, or the profile's, to use %s's)", name, set, t.Values[name], t.Who, t.Who)
			}
			out = append(out, line)
		}
	}
	return out
}

// WriteTold writes properties to a file as a service tells them: a line
// name=value for each.
func WriteTold(path string, values map[string]string) error {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(values)) {
		b.WriteString(name + "=" + values[name] + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// readProperties reads one file's lines.
func readProperties(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	out := map[string]string{}
	lines := bufio.NewScanner(file)
	for n := 1; lines.Scan(); n++ {
		line := strings.TrimRight(lines.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t{}$") {
			return nil, &lineError{path: path, line: n, text: line}
		}
		out[name] = value
	}
	return out, lines.Err()
}

type lineError struct {
	path string
	line int
	text string
}

func (e *lineError) Error() string {
	text := e.text
	if len(text) > 80 {
		text = text[:80] + "…"
	}
	return fmt.Sprintf("%s:%d: %q is not name=value", filepath.ToSlash(e.path), e.line, text)
}
