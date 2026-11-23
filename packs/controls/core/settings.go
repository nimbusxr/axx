package controlscore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes of the controls packs.
const (
	// CodeSettings: packs.controls-core, or a list or an exceptions file it
	// names, cannot be read or says something the pack does not know.
	CodeSettings = "AXX-E1100"
	// CodeEvidence: a file given as evidence cannot be read, or is none.
	CodeEvidence = "AXX-E1101"
	// CodeMixed: the evidence given is of different commits, versions of
	// axx or lists, and cannot be judged together.
	CodeMixed = "AXX-E1102"
	// CodeNotCovered: `axx controls check` found a required control that is
	// not covered.
	CodeNotCovered = "AXX-E1103"
	// CodeUnknown: a command names a control, a list or a format the
	// project does not have.
	CodeUnknown = "AXX-E1104"
)

// configSchema is the pack's section of axx.yaml, packs.controls-core.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "require": {
      "type": "array", "items": {"type": "string"},
      "description": "The lists of controls the project is to cover: a list a pack carries, by its name (asvs-5.0-level-1), or a file of the project's own that lists controls (security/pci.yaml). 'axx controls list' shows the lists the project's packs carry."
    },
    "evidence": {
      "type": "array", "items": {"type": "string"},
      "description": "Files of what covers controls besides the scenarios (a pen test, a scanner, a review), which every report then shows: each names its source and the controls it covers or fails."
    },
    "exceptions": {
      "type": "array",
      "description": "The required controls the project does not cover, each with its reason: one that does not apply, or a risk accepted until a date. Every report shows them.",
      "items": {
        "type": "object", "additionalProperties": false, "required": ["control", "kind", "reason"],
        "properties": {
          "control": {"type": "string", "description": "The control, as a tag names it without its @: asvs:6.2.1."},
          "kind": {"type": "string", "enum": ["not-applicable", "accepted"], "description": "not-applicable: the control does not apply to this system. accepted: it is not covered, and that is accepted until a date."},
          "reason": {"type": "string", "description": "Why, for whoever reads the report."},
          "until": {"type": "string", "description": "The day an accepted exception ends (2027-06-30): from the day after, it no longer counts. An accepted exception needs one."}
        }
      }
    },
    "folder": {"type": "string", "description": "The folder a run's evidence goes to, in the project: run.json (default .axx/controls)."}
  }
}`

// config is the pack's settings.
type config struct {
	Require    []string    `json:"require"`
	Evidence   []string    `json:"evidence"`
	Exceptions []Exception `json:"exceptions"`
	Folder     string      `json:"folder"`
}

// Exception is a required control a project does not cover, and why.
type Exception struct {
	// Control is the control, as a tag names it without its @: asvs:6.2.1.
	Control string `json:"control"`
	// Kind is "not-applicable" or "accepted"; Reason says why.
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	// Until is the day an accepted exception ends, 2027-06-30.
	Until string `json:"until,omitempty"`
	// From is where the exception was recorded, when not in axx.yaml.
	From string `json:"from,omitempty"`
}

// The kinds of exception.
const (
	NotApplicable = "not-applicable"
	Accepted      = "accepted"
)

// in reports whether the exception counts at a time: an accepted one ends with
// its day.
func (e Exception) in(at time.Time) bool {
	if e.Until == "" {
		return true
	}
	end, err := time.Parse(time.DateOnly, e.Until)
	return err == nil && at.Before(end.AddDate(0, 0, 1))
}

// defaultFolder is where a run's evidence goes, in the project.
var defaultFolder = filepath.Join(".axx", "controls")

// project is what a project says of its controls, read and looked up: the
// frameworks its packs carry and its own files define, the lists it requires,
// its exceptions and its other evidence.
type project struct {
	dir string
	cfg config
	// frameworks are the frameworks the project knows, by key.
	frameworks map[string]*Framework
	// lists are the lists it requires, in the order it names them.
	lists []requiredList
	// exceptions are the exceptions that count; requested, the project's own
	// when the command was given others to go by.
	exceptions []Exception
	requested  []Exception
}

// requiredList is a list a project requires.
type requiredList struct {
	List
	Framework *Framework
}

// overrides are what a command was told to go by, in place of the project's
// settings: the lists and the exceptions their approver holds.
type overrides struct {
	require    []string
	exceptions string
}

func settingsError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeSettings, exitcode.Usage, format, args...)
}

// loadProject reads the project's settings and what they name. dir is the
// project's folder, which the paths are relative to.
func loadProject(dir string, settings func(v any) error, o overrides) (*project, error) {
	p := &project{dir: dir, frameworks: registered()}
	if settings != nil {
		if err := settings(&p.cfg); err != nil {
			return nil, axxerr.Wrap(err, CodeSettings, exitcode.Usage, "cannot read packs.%s", Name)
		}
	}
	if p.cfg.Folder == "" {
		p.cfg.Folder = defaultFolder
	}
	require := p.cfg.Require
	if len(o.require) > 0 {
		require = o.require
	}
	for _, name := range require {
		l, err := p.list(name)
		if err != nil {
			return nil, err
		}
		p.lists = append(p.lists, *l)
	}
	for i, e := range p.cfg.Exceptions {
		if err := p.checkException(&p.cfg.Exceptions[i], fmt.Sprintf("packs.%s.exceptions[%d]", Name, i)); err != nil {
			return nil, err
		}
		_ = e
	}
	p.exceptions = p.cfg.Exceptions
	if o.exceptions != "" {
		given, err := p.exceptionsFile(o.exceptions)
		if err != nil {
			return nil, err
		}
		// What the project asks for and the others do not have: one they have
		// counts, as theirs.
		for _, e := range p.cfg.Exceptions {
			if !slices.ContainsFunc(given, func(g Exception) bool { return g.Control == e.Control }) {
				p.requested = append(p.requested, e)
			}
		}
		p.exceptions = given
	}
	return p, nil
}

// path is a file the settings name, in the project.
func (p *project) path(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(p.dir, filepath.FromSlash(name))
}

// isFile reports whether a name of `require` is a file's: a list a pack
// carries has no folder and no extension.
func isFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".json":
		return true
	}
	return strings.ContainsAny(name, `/\`)
}

// list finds a list `require` names: one a framework carries, or a file.
func (p *project) list(name string) (*requiredList, error) {
	if isFile(name) {
		return p.listFile(name)
	}
	var known []string
	for _, key := range sortedKeys(p.frameworks) {
		f := p.frameworks[key]
		for _, l := range f.Lists {
			if l.Name == name {
				return &requiredList{List: l, Framework: f}, nil
			}
			known = append(known, l.Name)
		}
	}
	err := settingsError("packs.%s.require names %q, which is no list of this project's packs", Name, name)
	if len(known) > 0 {
		return nil, err.WithHint("the lists its packs carry: %s; a list of the project's own is a file, like security/controls.yaml", strings.Join(known, ", "))
	}
	return nil, err.WithHint("no pack of this project carries a list: add one that does (`axx pack add controls-asvs`), or name a file of the project's own that lists controls, like security/controls.yaml")
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// listFile reads a list of the project's own: a framework's key, and the
// controls it requires. For a framework a pack carries, the controls are that
// framework's; for any other key the file is the framework too, with what it
// says of each control.
func (p *project) listFile(name string) (*requiredList, error) {
	path := p.path(name)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, CodeSettings, exitcode.Usage, "cannot read the list of controls %s", name).
			WithHint("packs.%s.require names it: a list a pack carries is named without a folder or an extension", Name)
	}
	var doc struct {
		Framework string        `yaml:"framework"`
		Title     string        `yaml:"title"`
		Version   string        `yaml:"version"`
		Notice    string        `yaml:"notice"`
		Controls  yaml.MapSlice `yaml:"controls"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, axxerr.Wrap(err, CodeSettings, exitcode.Usage, "cannot read the list of controls %s", name)
	}
	key := strings.ToLower(strings.TrimSpace(doc.Framework))
	if !keyPattern.MatchString(key) {
		return nil, settingsError("%s names no framework", name).
			WithHint("say which framework its controls are of, by the word its tags start with: `framework: pci` for @pci:8.3.6 (lower case letters, digits and hyphens)")
	}
	if len(doc.Controls) == 0 {
		return nil, settingsError("%s lists no controls", name).
			WithHint("list them under `controls:`, each by its ID with a few words for it: `\"8.3.6\": Passwords are at least 12 characters long`")
	}
	carried := p.frameworks[key]
	own := &Framework{Key: key, Title: doc.Title, Version: doc.Version, Notice: doc.Notice}
	if own.Title == "" {
		own.Title = key
	}
	list := List{Name: name, Title: doc.Title}
	for _, item := range doc.Controls {
		id := strings.TrimSpace(fmt.Sprint(item.Key))
		c := Control{ID: id}
		req := Required{ID: id}
		if title, ok := item.Value.(string); ok {
			c.Title = title
		} else if keys, ok := pairs(item.Value); ok {
			for _, kv := range keys {
				switch kv.key {
				case "title":
					c.Title = fmt.Sprint(kv.value)
				case "text":
					c.Text = fmt.Sprint(kv.value)
				case "group":
					c.Group = fmt.Sprint(kv.value)
				case "scenarios":
					n, ok := asInt(kv.value)
					if !ok || n < 1 {
						return nil, settingsError("%s: the control %s takes %v scenarios", name, id, kv.value).
							WithHint("`scenarios` is how many passing scenarios prove the control: a whole number, 1 or more")
					}
					req.Scenarios = n
				default:
					return nil, settingsError("%s: the control %s has the key %q", name, id, kv.key).
						WithHint("a control of a list has title, text, group and scenarios")
				}
			}
		} else if item.Value != nil {
			return nil, settingsError("%s: the control %s is %v", name, id, item.Value).
				WithHint("a control is its ID with a few words for it, or with `title:` and `scenarios:` under it")
		}
		if carried != nil {
			known, ok := carried.control(id)
			if !ok {
				err := settingsError("%s requires %s:%s, which %s has not", name, key, id, describe(carried))
				if near := carried.nearest(id); near != "" {
					return nil, err.WithHint("the nearest it has is %s; `axx controls list` finds a control by a word of it", near)
				}
				return nil, err.WithHint("`axx controls list` finds a control by a word of it")
			}
			req.ID = known.ID
		} else {
			own.Controls = append(own.Controls, c)
		}
		list.Controls = append(list.Controls, req)
	}
	if carried != nil {
		if list.Title == "" {
			list.Title = name
		}
		return &requiredList{List: list, Framework: carried}, nil
	}
	// A framework two of the project's files are of has the controls of both.
	if before, ok := p.frameworks[key]; ok {
		own.Controls = append(append([]Control{}, before.Controls...), own.Controls...)
	}
	if list.Title == "" {
		list.Title = own.Title
	}
	p.frameworks[key] = own
	return &requiredList{List: list, Framework: own}, nil
}

// pair is a key of a mapping and its value.
type pair struct {
	key   string
	value any
}

// pairs are the keys of a mapping as YAML was read, in their order where it
// kept one.
func pairs(v any) ([]pair, bool) {
	switch m := v.(type) {
	case yaml.MapSlice:
		out := make([]pair, len(m))
		for i, kv := range m {
			out[i] = pair{fmt.Sprint(kv.Key), kv.Value}
		}
		return out, true
	case map[string]any:
		var out []pair
		for _, k := range sortedKeys(m) {
			out = append(out, pair{k, m[k]})
		}
		return out, true
	}
	return nil, false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

// describe names a framework with its version: "OWASP ASVS 5.0.0".
func describe(f *Framework) string {
	return strings.TrimSpace(f.Title + " " + f.Version)
}

// checkException holds an exception to what one says, and writes its control
// as its framework does.
func (p *project) checkException(e *Exception, where string) error {
	key, id, ok := splitRef(e.Control)
	f := p.frameworks[key]
	if !ok || f == nil {
		return settingsError("%s names the control %q, of no framework the project has", where, e.Control).
			WithHint("name it as a tag does, without the @: asvs:6.2.1")
	}
	c, known := f.control(id)
	if !known {
		err := settingsError("%s names %s, which %s has not", where, e.Control, describe(f))
		if near := f.nearest(id); near != "" {
			return err.WithHint("the nearest it has is %s", near)
		}
		return err
	}
	e.Control = key + ":" + c.ID
	if strings.TrimSpace(e.Reason) == "" {
		return settingsError("%s (%s) gives no reason", where, e.Control).
			WithHint("say why in `reason`: every report shows it")
	}
	switch e.Kind {
	case NotApplicable:
	case Accepted:
		if _, err := time.Parse(time.DateOnly, e.Until); err != nil {
			return settingsError("%s (%s) is accepted until %q", where, e.Control, e.Until).
				WithHint("an accepted exception ends on a day, `until: 2027-06-30`: from the day after, the control counts as not covered again")
		}
	default:
		return settingsError("%s (%s) is of the kind %q", where, e.Control, e.Kind).
			WithHint("an exception is not-applicable (the control does not apply to this system) or accepted (it is not covered, until a date)")
	}
	return nil
}

// exceptionsFile reads the exceptions a command was told to go by.
func (p *project) exceptionsFile(name string) ([]Exception, error) {
	b, err := os.ReadFile(p.path(name))
	if err != nil {
		return nil, axxerr.Wrap(err, CodeSettings, exitcode.Usage, "cannot read the exceptions %s", name)
	}
	var doc struct {
		Exceptions []Exception `json:"exceptions" yaml:"exceptions"`
	}
	raw, err := yaml.YAMLToJSON(b)
	if err != nil {
		return nil, axxerr.Wrap(err, CodeSettings, exitcode.Usage, "cannot read the exceptions %s", name)
	}
	if len(strings.TrimSpace(string(raw))) > 0 && strings.TrimSpace(string(raw)) != "null" {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, axxerr.Wrap(err, CodeSettings, exitcode.Usage, "cannot read the exceptions %s", name).
				WithHint("it lists them under `exceptions:`, as packs.%s.exceptions does", Name)
		}
	}
	for i := range doc.Exceptions {
		if err := p.checkException(&doc.Exceptions[i], fmt.Sprintf("%s: exception %d", name, i+1)); err != nil {
			return nil, err
		}
		doc.Exceptions[i].From = name
	}
	return doc.Exceptions, nil
}

// splitRef splits a control as a tag names it (with or without its @) into its
// framework's key and its ID.
func splitRef(ref string) (key, id string, ok bool) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	key, id, ok = strings.Cut(ref, ":")
	key = strings.ToLower(key)
	return key, id, ok && keyPattern.MatchString(key) && id != ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
