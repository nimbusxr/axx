// Package config defines and loads axx.yaml.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Config is the root of axx.yaml. Every section is optional.
type Config struct {
	// Schema version of this file. Currently 1.
	Version int `json:"version,omitempty" jsonschema:"enum=1"`
	// Run controls which features run and how.
	Run Run `json:"run,omitzero"`
	// Resources are directories that relative resource paths in steps (seed
	// files, schemas, payloads) resolve against, in order. The directory of
	// axx.yaml is always searched last.
	Resources []string `json:"resources,omitempty"`
	// Properties back ${sys:name} references. `-D name=value` overrides them.
	Properties map[string]string `json:"properties,omitempty"`
	// OpenAPI holds default OpenAPI validation settings for REST services.
	OpenAPI OpenAPI `json:"openapi,omitzero"`
	// Active starts only the applications needed by the selected scenarios.
	Active Active `json:"active,omitzero"`
	// Apps are the applications under test (and their infrastructure),
	// started before scenarios run and stopped afterwards, in declaration order.
	Apps Apps `json:"apps,omitempty"`
	// Packs holds pack settings keyed by pack name.
	Packs map[string]json.RawMessage `json:"packs,omitempty"`
	// Lint configures `axx lint`: test-data isolation rules that keep values
	// such as seed ids unique so scenarios can run in parallel.
	Lint *Lint `json:"lint,omitempty"`
	// Fixtures configures `axx fixtures` (fixture factories).
	Fixtures *Fixtures `json:"fixtures,omitempty"`
	// Profiles are named overlays selected with --profile or AXX_PROFILE.
	Profiles map[string]json.RawMessage `json:"profiles,omitempty"`

	// Dir is the directory containing the loaded axx.yaml (or the working
	// directory when no file was found). Not part of the file.
	Dir string `json:"-"`
	// File is the loaded file path ("" when none).
	File string `json:"-"`

	// sources are the files the configuration was merged from, for Position.
	sources []source
}

// Run controls execution.
type Run struct {
	// Paths are feature files or directories. Default: ["features"].
	Paths []string `json:"paths,omitempty"`
	// Tags is a default tag expression, e.g. "not @wip".
	Tags string `json:"tags,omitempty"`
	// Workers is the number of scenarios run in parallel: a number or "auto"
	// (number of CPUs). Default: auto.
	Workers Workers `json:"workers,omitzero"`
	// Exclusive lists tags whose scenarios run alone, after the parallel phase.
	Exclusive []string `json:"exclusive,omitempty"`
	// Order is "defined" (default) or "random[:seed]".
	Order    string   `json:"order,omitempty"`
	Timeouts Timeouts `json:"timeouts,omitzero"`
	// Reporters, e.g. ["pretty", {"junit": "build/axx/junit.xml"}].
	Reporters []Reporter `json:"reporters,omitempty"`
}

// Timeouts bound steps, scenarios and hooks.
type Timeouts struct {
	Step     Duration `json:"step,omitzero"`
	Scenario Duration `json:"scenario,omitzero"`
	Hook     Duration `json:"hook,omitzero"`
}

// OpenAPI holds default validation levels, keyed by validation message key
// (e.g. validation.request.body) with values ERROR, WARN, INFO or IGNORE.
type OpenAPI struct {
	Levels map[string]string `json:"levels,omitempty"`
}

// Active configures tag-based application startup.
type Active struct {
	Enabled bool `json:"enabled,omitempty"`
	// OnNoTags decides what happens when the selected scenarios have no tags:
	// "fallback" (start all enabled apps, default) or "error".
	OnNoTags string `json:"onNoTags,omitempty" jsonschema:"enum=fallback,enum=error"`
}

// App is an application or piece of infrastructure managed by axx.
type App struct {
	// Name is the map key in axx.yaml.
	Name string `json:"-"`
	// Enabled defaults to true.
	Enabled *bool `json:"enabled,omitempty"`
	// Dir is the working directory, relative to axx.yaml.
	Dir string `json:"dir,omitempty"`
	// Command starts the app: a string (split like a shell word list, without
	// a shell) or an argv list. Set shell: true to run it through the shell.
	Command Command `json:"command"`
	// Shell runs command and cleanup through /bin/sh -c (cmd /C on Windows).
	Shell bool              `json:"shell,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	// DependsOn names apps that must be ready first.
	DependsOn []string `json:"dependsOn,omitempty"`
	Ready     *Ready   `json:"ready,omitempty"`
	Stop      Stop     `json:"stop,omitzero"`
	// Cleanup runs after the app stops, even if it crashed.
	Cleanup Command   `json:"cleanup,omitzero"`
	Active  AppActive `json:"active,omitzero"`
	Debug   *Debug    `json:"debug,omitempty"`
}

// IsEnabled reports whether the app should be managed.
func (a App) IsEnabled() bool { return a.Enabled == nil || *a.Enabled }

// Ready describes how to tell that an app is ready.
type Ready struct {
	HTTP *ReadyHTTP `json:"http,omitempty"`
	// TCP is a host:port that must accept connections.
	TCP string `json:"tcp,omitempty"`
	// Exec is a command that must exit 0.
	Exec Command `json:"exec,omitzero"`
	// Log is a regular expression matched against the app's output.
	Log string `json:"log,omitempty"`
	// Timeout for the app to become ready. Default: 60s.
	Timeout Duration `json:"timeout,omitzero"`
	// Interval between checks. Default: 1s.
	Interval Duration `json:"interval,omitzero"`
}

// ReadyHTTP checks that every URL returns a 2xx status.
type ReadyHTTP struct {
	URL StringList `json:"url"`
}

// Stop controls shutdown.
type Stop struct {
	// Signal sent first: SIGTERM (default) or SIGINT.
	Signal string `json:"signal,omitempty" jsonschema:"enum=SIGTERM,enum=SIGINT"`
	// Grace period before the process group is killed. Default: 10s.
	Grace Duration `json:"grace,omitzero"`
}

// AppActive lists tags that require this app.
type AppActive struct {
	Tags []string `json:"tags,omitempty"`
}

// Debug configures `axx run --debug`.
type Debug struct {
	// Command replaces the normal command in debug mode.
	Command  Command   `json:"command,omitzero"`
	Debugger *Debugger `json:"debugger,omitempty"`
	// OnUnavailable decides what happens when the debugger is not listening:
	// fail, fallback (run the normal command) or retry (default).
	OnUnavailable string `json:"onUnavailable,omitempty" jsonschema:"enum=fail,enum=fallback,enum=retry"`
	Retry         Retry  `json:"retry,omitzero"`
}

// Debugger describes the IDE debugger to hand the app to.
type Debugger struct {
	Type   string `json:"type,omitempty" jsonschema:"enum=java,enum=go,enum=nodejs,enum=python"`
	Port   int    `json:"port"`
	Host   string `json:"host,omitempty"`
	Module string `json:"module,omitempty"`
	// Mode is "ide-listens" (the IDE listens and the app connects, e.g. JDWP
	// server=n; default for java) or "app-listens" (delve, --inspect, debugpy).
	Mode string `json:"mode,omitempty" jsonschema:"enum=ide-listens,enum=app-listens"`
}

// Retry configures retries.
type Retry struct {
	Attempts int      `json:"attempts,omitempty"`
	Delay    Duration `json:"delay,omitzero"`
}

// Reporter is either a bare name ("pretty") or {name: output-path}.
type Reporter struct {
	Name string
	Path string
}

// UnmarshalJSON accepts "name" or {"name": "path"}.
func (r *Reporter) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		r.Name = s
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil || len(m) != 1 {
		return fmt.Errorf(`reporter must be a name or {name: path}`)
	}
	for k, v := range m {
		r.Name, r.Path = k, v
	}
	return nil
}

// MarshalJSON renders the compact form.
func (r Reporter) MarshalJSON() ([]byte, error) {
	if r.Path == "" {
		return json.Marshal(r.Name)
	}
	return json.Marshal(map[string]string{r.Name: r.Path})
}

// Workers is a worker count or "auto" (0).
type Workers int

// UnmarshalJSON accepts a positive number or "auto".
func (w *Workers) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		if s == "auto" || s == "" {
			*w = 0
			return nil
		}
		return fmt.Errorf(`workers must be a number or "auto", got %q`, s)
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil || n < 0 {
		return fmt.Errorf(`workers must be a number or "auto"`)
	}
	*w = Workers(n)
	return nil
}

// Duration is a time.Duration written like "60s", "1m30s" or "250ms".
type Duration time.Duration

// D returns the time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Or returns d, or def when d is zero.
func (d Duration) Or(def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return time.Duration(d)
}

// UnmarshalJSON parses a Go duration string (a bare number means seconds).
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		var n float64
		if err2 := json.Unmarshal(b, &n); err2 != nil {
			return fmt.Errorf("duration must be a string like \"30s\"")
		}
		*d = Duration(time.Duration(n * float64(time.Second)))
		return nil
	}
	v, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid duration %q (use forms like 500ms, 30s, 1m30s)", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON renders the duration string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Command is a command line given as a string or an argv list.
type Command struct {
	// Argv, when set, is used verbatim.
	Argv []string
	// Line is the original string form.
	Line string
}

// IsZero reports whether no command was given.
func (c Command) IsZero() bool { return len(c.Argv) == 0 && c.Line == "" }

// String renders the command for display.
func (c Command) String() string {
	if c.Line != "" {
		return c.Line
	}
	return strings.Join(c.Argv, " ")
}

// UnmarshalJSON accepts a string or a list of strings.
func (c *Command) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		c.Line = s
		return nil
	}
	var argv []string
	if err := json.Unmarshal(b, &argv); err != nil {
		return fmt.Errorf("command must be a string or a list of strings")
	}
	c.Argv = argv
	return nil
}

// MarshalJSON renders the original form.
func (c Command) MarshalJSON() ([]byte, error) {
	if c.Argv != nil {
		return json.Marshal(c.Argv)
	}
	return json.Marshal(c.Line)
}

// StringList accepts a single string or a list of strings.
type StringList []string

// UnmarshalJSON accepts a string or list.
func (l *StringList) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = StringList{s}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("must be a string or a list of strings")
	}
	*l = list
	return nil
}

// Apps is an ordered map of apps (declaration order is start order).
type Apps []App

// UnmarshalJSON decodes a JSON object while preserving key order.
func (a *Apps) UnmarshalJSON(b []byte) error {
	return decodeOrdered(b, func(name string, raw json.RawMessage) error {
		var app App
		if err := strictUnmarshal(raw, &app); err != nil {
			return fmt.Errorf("apps.%s: %w", name, err)
		}
		app.Name = name
		*a = append(*a, app)
		return nil
	})
}

// Get returns the named app.
func (a Apps) Get(name string) (App, bool) {
	for _, app := range a {
		if app.Name == name {
			return app, true
		}
	}
	return App{}, false
}

func decodeOrdered(b []byte, each func(string, json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected a mapping")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if err := each(name, raw); err != nil {
			return err
		}
	}
	return nil
}

func strictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// FixturesDir is the directory `axx fixtures` works in: fixtures.baseDir,
// relative to axx.yaml, or the directory of axx.yaml.
func (c *Config) FixturesDir() string {
	if c.Fixtures != nil && c.Fixtures.BaseDir != "" {
		return filepath.Join(c.Dir, filepath.FromSlash(c.Fixtures.BaseDir))
	}
	return c.Dir
}
