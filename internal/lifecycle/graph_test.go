package lifecycle

import (
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func dep(name string, deps ...string) config.App {
	return config.App{Name: name, Command: config.Command{Line: "true"}, DependsOn: deps}
}

func disabled(app config.App) config.App {
	off := false
	app.Enabled = &off
	return app
}

func TestNewValidatesDependencies(t *testing.T) {
	tests := []struct {
		name     string
		apps     config.Apps
		wantCode string
		wantMsg  string
	}{
		{"no apps", nil, "", ""},
		{"chain", config.Apps{dep("db"), dep("api", "db"), dep("web", "api", "db")}, "", ""},
		{"forward reference", config.Apps{dep("api", "db"), dep("db")}, "", ""},
		{"dependency on disabled app", config.Apps{disabled(dep("db")), dep("api", "db")}, "", ""},
		{"unknown dependency", config.Apps{dep("api", "dbx")}, CodeUnknownDependency, `"dbx"`},
		{"self cycle", config.Apps{dep("a", "a")}, CodeDependencyCycle, "a -> a"},
		{"two cycle", config.Apps{dep("a", "b"), dep("b", "a")}, CodeDependencyCycle, "a -> b -> a"},
		{"three cycle", config.Apps{dep("x"), dep("a", "x", "c"), dep("b", "a"), dep("c", "b")}, CodeDependencyCycle, "a -> c -> b -> a"},
		{"duplicate name", config.Apps{dep("a"), dep("a")}, CodeInvalidConfig, "declared twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.apps, Options{})
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return
			}
			ae := mustCode(t, err, tt.wantCode)
			if !strings.Contains(ae.Error(), tt.wantMsg) {
				t.Errorf("error %q does not mention %q", ae.Error(), tt.wantMsg)
			}
			if ae.Hint == "" {
				t.Error("error has no hint")
			}
			if ae.Exit != exitConfig {
				t.Errorf("exit code = %v, want %v", ae.Exit, exitConfig)
			}
		})
	}
}

func TestNewValidatesSettings(t *testing.T) {
	withReady := func(r config.Ready) config.App {
		a := dep("api")
		a.Ready = &r
		return a
	}
	withDebug := func(d config.Debug) config.App {
		a := dep("api")
		a.Debug = &d
		return a
	}
	tests := []struct {
		name     string
		app      config.App
		opts     Options
		wantCode string
		wantMsg  string
	}{
		{"valid ready", withReady(config.Ready{
			HTTP: &config.ReadyHTTP{URL: config.StringList{"http://localhost:8080/health", "https://x/y"}},
			TCP:  "localhost:5432", Exec: config.Command{Line: "pg_isready"}, Log: `Started .* in \d+`,
		}), Options{}, "", ""},
		{"empty url list", withReady(config.Ready{HTTP: &config.ReadyHTTP{}}), Options{}, CodeInvalidConfig, "ready.http.url"},
		{"not an http url", withReady(config.Ready{HTTP: &config.ReadyHTTP{URL: config.StringList{"localhost:8080"}}}), Options{}, CodeInvalidConfig, "ready.http.url"},
		{"bad tcp address", withReady(config.Ready{TCP: "5432"}), Options{}, CodeInvalidConfig, "ready.tcp"},
		{"bad log regex", withReady(config.Ready{Log: "Started ("}), Options{}, CodeInvalidConfig, "ready.log"},
		{"empty exec", withReady(config.Ready{Exec: config.Command{Line: " "}}), Options{}, CodeInvalidConfig, "ready.exec"},
		{"negative timeout", withReady(config.Ready{Timeout: -1}), Options{}, CodeInvalidConfig, "ready.timeout"},
		{"bad stop signal", config.App{Name: "api", Stop: config.Stop{Signal: "SIGWINCH"}}, Options{}, CodeInvalidConfig, "stop.signal"},
		{"short stop signal", config.App{Name: "api", Stop: config.Stop{Signal: "int"}}, Options{}, "", ""},
		{"bad debug mode", withDebug(config.Debug{Debugger: &config.Debugger{Port: 5005, Mode: "both"}}), Options{}, CodeInvalidConfig, "debug.debugger.mode"},
		{"bad debug port", withDebug(config.Debug{Debugger: &config.Debugger{}}), Options{}, CodeInvalidConfig, "debug.debugger.port"},
		{"bad onUnavailable", withDebug(config.Debug{OnUnavailable: "ignore"}), Options{}, CodeInvalidConfig, "debug.onUnavailable"},
		{"attach unknown app", dep("api"), Options{Attach: map[string]bool{"apx": true}}, CodeUnknownApp, `"apx"`},
		{"debug unknown app", dep("api"), Options{Debug: map[string]bool{"web": true}}, CodeUnknownApp, `"web"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(config.Apps{tt.app}, tt.opts)
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return
			}
			ae := mustCode(t, err, tt.wantCode)
			if !strings.Contains(ae.Error(), tt.wantMsg) {
				t.Errorf("error %q does not mention %q", ae.Error(), tt.wantMsg)
			}
		})
	}
}

func TestWithDependencies(t *testing.T) {
	apps := config.Apps{dep("db"), dep("cache"), disabled(dep("mq")), dep("api", "db", "mq"), dep("web", "api"), dep("admin")}
	tests := []struct {
		names []string
		want  []string
	}{
		{nil, []string{}},
		{[]string{"web"}, []string{"db", "api", "web"}},
		{[]string{"admin", "db"}, []string{"db", "admin"}},
		{[]string{"mq"}, []string{}},
		{[]string{"nope"}, []string{}},
	}
	for _, tt := range tests {
		if got := withDependencies(apps, tt.names); !slices.Equal(got, tt.want) {
			t.Errorf("withDependencies(%q) = %q, want %q", tt.names, got, tt.want)
		}
	}
}
