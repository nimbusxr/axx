package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/compat/javare"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes for lint configuration and results.
const (
	CodeNoRules        = "AXX-E0800"
	CodeIncludeMissing = "AXX-E0801"
	CodeIncludeCycle   = "AXX-E0802"
	CodeIncludeInvalid = config.CodeLintInclude // AXX-E0803
	CodeInvalidRule    = "AXX-E0804"
	CodeOption         = "AXX-E0805"
	CodeUnparsable     = "AXX-E0810"
	CodeUnreadable     = "AXX-E0811"
	CodeTooLarge       = "AXX-E0812"
	CodeDuplicate      = "AXX-E0820"
	CodeOrdinalMissing = "AXX-E0830"
	CodeOrdinalLabel   = "AXX-E0831"
)

// Defaults of the lint config block.
const (
	DefaultMaxFileSize          = 5 << 20
	DefaultMaxReportedValues    = 3
	DefaultMaxReportedLocations = 5
)

// Modes and validations.
const (
	ModeError = "error"
	ModeWarn  = "warn"

	GlobalUnique    = "global-unique"
	FileUnique      = "file-unique"
	CrossFileUnique = "cross-file-unique"
)

// Set is a loaded and validated lint configuration.
type Set struct {
	// Configured is false when axx.yaml has no lint section.
	Configured bool
	// BaseDir is the absolute directory file patterns resolve against.
	BaseDir string
	// Config is the config block with defaults applied.
	Config config.LintConfig
	// Rules are the root rules followed by included ones, in include order.
	Rules []*Rule
}

// Rule is a compiled isolation rule.
type Rule struct {
	config.LintRule
	// ID is a stable identifier derived from the name (SARIF rule id).
	ID string
	// Source is where the rule is defined.
	Source axxerr.Location

	patterns []*filePattern
	excludes []*globMatcher
	regex    *javare.Regexp
	path     *jsonPath
	ignore   map[string]bool
}

// Issue is one configuration problem.
type Issue struct {
	Code     string           `json:"code"`
	Message  string           `json:"message"`
	Location *axxerr.Location `json:"location,omitempty"`
}

// ConfigError lists every problem found in the lint configuration.
type ConfigError struct{ Issues []Issue }

func (e *ConfigError) Error() string {
	var b strings.Builder
	for _, is := range e.Issues {
		b.WriteString("\n  ")
		if is.Location != nil {
			b.WriteString(is.Location.String())
			b.WriteString(": ")
		}
		fmt.Fprintf(&b, "[%s] %s", is.Code, is.Message)
	}
	return b.String()
}

var hints = map[string]string{
	CodeNoRules:        "add rules under lint.rules in axx.yaml (or include a rules file with lint.include)",
	CodeIncludeMissing: "includes resolve relative to the file that lists them; run `axx fixtures generate` if the file is generated",
	CodeIncludeCycle:   "each rules file may be included once; remove the repeated include",
	CodeIncludeInvalid: "an included lint file holds `rules:` (and optionally `include:`), in the format of the lint section of axx.yaml",
	CodeInvalidRule:    "run `axx schema` for the lint section, or `axx explain AXX-E0804`",
}

func (l *loader) fail() error {
	if len(l.issues) == 0 {
		return nil
	}
	if len(l.issues) == 1 {
		is := l.issues[0]
		e := axxerr.New(is.Code, exitcode.Usage, "%s", is.Message).WithHint("%s", hints[is.Code])
		e.Location = is.Location
		return e
	}
	code := l.issues[0].Code
	for _, is := range l.issues[1:] {
		if is.Code != code {
			code = CodeInvalidRule
		}
	}
	return axxerr.Wrap(&ConfigError{Issues: l.issues}, code, exitcode.Usage, "invalid lint configuration (%d problems)", len(l.issues)).
		WithHint("%s", hints[code])
}

type loader struct {
	set     *Set
	issues  []Issue
	visited map[string]bool
	ids     map[string]int
}

// origin locates keys of one configuration file.
type origin struct {
	file   string                                       // absolute path
	prefix []string                                     // path of the lint section within the file
	locate func(path ...string) (axxerr.Location, bool) // positions of keys
}

func (o origin) at(path ...string) *axxerr.Location {
	if loc, ok := o.locate(append(append([]string{}, o.prefix...), path...)...); ok {
		return &loc
	}
	loc := axxerr.Location{File: displayPath(o.file)}
	return &loc
}

func (l *loader) issue(code string, loc *axxerr.Location, format string, args ...any) {
	l.issues = append(l.issues, Issue{Code: code, Message: fmt.Sprintf(format, args...), Location: loc})
}

// Load resolves includes and validates cfg's lint section. A configuration
// without a lint section loads as an empty, unconfigured Set; a lint section
// that yields no rules is an error, as a check that validates nothing would
// pass silently.
func Load(cfg *config.Config) (*Set, error) {
	set := &Set{BaseDir: cfg.Dir, Config: defaults(nil)}
	if cfg.Lint == nil {
		return set, nil
	}
	set.Configured = true
	set.Config = defaults(cfg.Lint.Config)
	root := cfg.File
	if root == "" {
		root = filepath.Join(cfg.Dir, "axx.yaml")
	}
	l := &loader{set: set, visited: map[string]bool{root: true}, ids: map[string]int{}}
	o := origin{file: root, prefix: []string{"lint"}, locate: cfg.Position}

	base := set.Config.BaseDir
	if base == "" {
		base = "."
	}
	if !filepath.IsAbs(base) {
		base = filepath.Join(cfg.Dir, base)
	}
	set.BaseDir = filepath.Clean(base)
	if st, err := os.Stat(set.BaseDir); err != nil || !st.IsDir() {
		l.issue(CodeInvalidRule, o.at("config", "baseDir"), "lint.config.baseDir %s is not a directory", displayPath(set.BaseDir))
	}

	l.addRules(o, cfg.Lint.Rules)
	l.includes(o, cfg.Lint.Include)
	if err := l.fail(); err != nil {
		return nil, err
	}
	if len(set.Rules) == 0 {
		loc := o.at()
		return nil, axxerr.New(CodeNoRules, exitcode.Usage, "the lint section of %s defines no rules", displayPath(root)).
			WithHint("%s", hints[CodeNoRules]).At(loc.File, loc.Line, loc.Column)
	}
	return set, nil
}

func defaults(c *config.LintConfig) config.LintConfig {
	out := config.LintConfig{}
	if c != nil {
		out = *c
	}
	if out.Mode == "" {
		out.Mode = ModeError
	}
	if out.MaxFileSize <= 0 {
		out.MaxFileSize = DefaultMaxFileSize
	}
	if out.MaxReportedValues <= 0 {
		out.MaxReportedValues = DefaultMaxReportedValues
	}
	if out.MaxReportedLocations <= 0 {
		out.MaxReportedLocations = DefaultMaxReportedLocations
	}
	return out
}

func (l *loader) includes(from origin, list []string) {
	dir := filepath.Dir(from.file)
	for j, inc := range list {
		at := from.at("include", strconv.Itoa(j))
		if strings.TrimSpace(inc) == "" {
			l.issue(CodeInvalidRule, at, "empty include")
			continue
		}
		p := filepath.FromSlash(inc)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		p = filepath.Clean(p)
		if l.visited[p] {
			l.issue(CodeIncludeCycle, at, "lint include cycle: %s is already loaded (included again from %s)", displayPath(p), displayPath(from.file))
			continue
		}
		l.visited[p] = true
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			l.issue(CodeIncludeMissing, at, "lint include %s not found (included from %s)", displayPath(p), displayPath(from.file))
			continue
		}
		inc, err := config.LoadLintFile(p)
		if err != nil {
			code := CodeIncludeInvalid
			var ae *axxerr.Error
			if errors.As(err, &ae) {
				code = ae.Code
			}
			l.issue(code, at, "%s", err.Error())
			continue
		}
		data, _ := os.ReadFile(p)
		o := origin{file: p, locate: func(path ...string) (axxerr.Location, bool) {
			line, col, ok := config.YAMLPosition(data, path...)
			return axxerr.Location{File: displayPath(p), Line: line, Column: col}, ok
		}}
		if inc.Config != nil {
			l.issue(CodeIncludeInvalid, o.at("config"), "lint include %s must not define a config: block; the config lives in the including file", displayPath(p))
			continue
		}
		l.addRules(o, inc.Rules)
		l.includes(o, inc.Include)
	}
}

func (l *loader) addRules(o origin, rules []config.LintRule) {
	for i, rc := range rules {
		key := func(k ...string) *axxerr.Location { return o.at(append([]string{"rules", strconv.Itoa(i)}, k...)...) }
		r := &Rule{LintRule: rc, Source: *key(), ignore: map[string]bool{}}
		name := strings.TrimSpace(rc.Name)
		label := fmt.Sprintf("rule %q", rc.Name)
		if name == "" {
			l.issue(CodeInvalidRule, key("name"), "lint rule %d has no name", i+1)
			label = fmt.Sprintf("rule %d", i+1)
		}
		if r.Type == "" {
			r.Type = "regex"
		}
		if r.Validation == "" {
			r.Validation = GlobalUnique
		}
		if len(rc.FilePatterns) == 0 {
			l.issue(CodeInvalidRule, key("filePatterns"), "%s has no filePatterns", label)
		}
		for j, p := range rc.FilePatterns {
			fp, err := compileFilePattern(p)
			if err != nil {
				l.issue(CodeInvalidRule, key("filePatterns", strconv.Itoa(j)), "%s: invalid file pattern: %v", label, err)
				continue
			}
			r.patterns = append(r.patterns, fp)
		}
		for j, p := range rc.ExcludePatterns {
			m, err := compileGlob(p)
			if err != nil {
				l.issue(CodeInvalidRule, key("excludePatterns", strconv.Itoa(j)), "%s: invalid exclude pattern: %v", label, err)
				continue
			}
			r.excludes = append(r.excludes, m)
		}
		switch r.Type {
		case "jsonpath":
			if strings.TrimSpace(rc.JSONPath) == "" {
				l.issue(CodeInvalidRule, key("type"), "%s is type: jsonpath but defines no jsonPath", label)
				break
			}
			jp, err := compileJSONPath(rc.JSONPath)
			if err != nil {
				l.issue(CodeInvalidRule, key("jsonPath"), "%s: invalid jsonPath: %v", label, err)
				break
			}
			r.path = jp
		default:
			if rc.Regex == "" {
				l.issue(CodeInvalidRule, key(), "%s defines no regex (or set type: jsonpath + jsonPath)", label)
				break
			}
			re, err := javare.CompileFlags(rc.Regex, javare.Multiline)
			if err != nil {
				l.issue(CodeInvalidRule, key("regex"), "%s: invalid regex: %s", label, strings.ReplaceAll(err.Error(), "\n", "\n      "))
				break
			}
			if re.NumGroups() == 0 {
				l.issue(CodeInvalidRule, key("regex"), "%s: regex has no capture group, so it extracts nothing; wrap the value in parentheses", label)
				break
			}
			r.regex = re
		}
		for _, v := range rc.IgnoreValues {
			r.ignore[v] = true
		}
		r.ID = l.id(name)
		l.set.Rules = append(l.set.Rules, r)
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// id derives a unique slug from a rule name.
func (l *loader) id(name string) string {
	id := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if id == "" {
		id = "rule"
	}
	out := id
	for n := 2; l.ids[out] > 0; n++ {
		out = fmt.Sprintf("%s-%d", id, n)
	}
	l.ids[out]++
	return out
}

// displayPath renders an absolute path relative to the working directory
// when it is below it.
func displayPath(p string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(p)
}
