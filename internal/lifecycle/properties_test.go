package lifecycle

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func code(err error) string {
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// A service tells axx the port it got once it started; its readiness check
// names it, and waits until it is told and answers there.
func TestAServiceTellsThePortItsReadinessCheckWaitsFor(t *testing.T) {
	dir := t.TempDir()
	port := strconv.Itoa(freePort(t))
	api := helperApp(t, "api", "tell-serve", "300ms", port)
	api.Ready = &config.Ready{
		HTTP:     &config.ReadyHTTP{URL: config.StringList{"http://127.0.0.1:${sys:api.port}/health"}},
		Timeout:  config.Duration(10 * time.Second),
		Interval: config.Duration(20 * time.Millisecond),
	}
	m, err := New(config.Services{api}, Options{ConfigDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(context.Background()) }()
	if err := m.Start(context.Background(), []string{"api"}); err != nil {
		t.Fatal(err)
	}
	told, err := m.Told()
	if err != nil || len(told) != 1 || told[0].Service != "api" || told[0].Who != "service api" || told[0].Values["api.port"] != port {
		t.Fatalf("told %+v, %v", told, err)
	}
	file := PropertiesFile(PropertiesDir(dir), "api")
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("what it told outlived it: %v", err)
	}
}

// A readiness check that names a property no one tells says so when it
// times out.
func TestAReadinessCheckSaysWhichPropertyItWaitedFor(t *testing.T) {
	api := helperApp(t, "api", "sleep")
	api.Ready = &config.Ready{
		TCP:      "127.0.0.1:${sys:db.port}",
		Timeout:  config.Duration(300 * time.Millisecond),
		Interval: config.Duration(20 * time.Millisecond),
	}
	m, err := New(config.Services{api}, Options{ConfigDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(context.Background()) }()
	err = m.Start(context.Background(), []string{"api"})
	if err == nil || !strings.Contains(err.Error(), "${sys:db.port} is not set yet: a service tells it in the file AXX_PROPERTIES names") {
		t.Errorf("start: %v", err)
	}
}

// axx.yaml's properties and -D fill in a readiness check too, and win over
// what a service told: what the person set wins.
func TestAReadinessCheckIsFilledInInTheRunsOrder(t *testing.T) {
	dir := t.TempDir()
	port := strconv.Itoa(freePort(t))
	cfg := &config.Config{Properties: map[string]string{"host": "127.0.0.1"}}
	api := helperApp(t, "api", "tell-serve", "0s", port)
	api.Ready = &config.Ready{
		HTTP:     &config.ReadyHTTP{URL: config.StringList{"http://${sys:host}:${sys:api.port}/"}},
		Timeout:  config.Duration(10 * time.Second),
		Interval: config.Duration(20 * time.Millisecond),
	}
	m, err := New(config.Services{api}, Options{ConfigDir: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background(), []string{"api"}); err != nil {
		t.Fatalf("the service's port, which no one set: %v", err)
	}
	_ = m.Stop(context.Background())

	api.Ready.Timeout = config.Duration(500 * time.Millisecond)
	for what, set := range map[string]*config.Config{
		"axx.yaml's": {Properties: map[string]string{"host": "127.0.0.1", "api.port": "1"}},
		"-D's":       {Properties: map[string]string{"host": "127.0.0.1"}, Defines: map[string]string{"api.port": "1"}},
	} {
		if m, err = New(config.Services{api}, Options{ConfigDir: dir, Config: set}); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background(), []string{"api"}); err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
			t.Errorf("%s port over the service's: %v", what, err)
		}
		_ = m.Stop(context.Background())
	}
}

// A value a service told that the run does not use, since the person set
// the property, is said, with where they set it.
func TestUnusedSaysWhichValueThePersonSetWins(t *testing.T) {
	cfg := &config.Config{Properties: map[string]string{"support.port": "8800", "db.port": "6543", "host": "localhost"}, Defines: map[string]string{"db.port": "6543"}}
	got := Unused(cfg, []ToldValues{
		{Who: "service support", Values: map[string]string{"support.port": "55123", "db.port": "55432", "host": "localhost", "mq.port": "55672"}},
	})
	want := []string{
		"using db.port=6543, which -D sets, not 55432, which service support wrote",
		"using support.port=8800 from axx.yaml, not 55123, which service support wrote: a value you set wins (remove it from axx.yaml's properties, or the profile's, to use service support's)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("unused:\n%s", strings.Join(got, "\n"))
	}
}

// A service starts with an empty file: what an earlier start told is gone.
// One attached, which axx does not start, keeps what it told.
func TestAServiceTellsAfreshAndAnAttachedOneKeepsItsOwn(t *testing.T) {
	dir := t.TempDir()
	for name, stale := range map[string]string{"api": "api.port=1\n", "db": "db.port=55432\n"} {
		if err := WriteTold(PropertiesFile(PropertiesDir(dir), name), nil); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(PropertiesFile(PropertiesDir(dir), name), []byte(stale), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	api := logReady(helperApp(t, "api", "tell", "0s", "api.url=http://127.0.0.1:55400"), "told")
	db := helperApp(t, "db", "sleep")
	m, err := New(config.Services{api, db}, Options{ConfigDir: dir, Attach: map[string]bool{"db": true}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(context.Background()) }()
	if err := m.Start(context.Background(), []string{"api", "db"}); err != nil {
		t.Fatal(err)
	}
	told, err := m.Told()
	if err != nil {
		t.Fatal(err)
	}
	values, err := MergeTold(told)
	if err != nil || len(values) != 2 || values["api.url"] != "http://127.0.0.1:55400" || values["db.port"] != "55432" {
		t.Errorf("told %v, %v", values, err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PropertiesFile(PropertiesDir(dir), "db")); err != nil {
		t.Errorf("an attached service's file went: %v", err)
	}
}

func TestToldLines(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "api")
	write := func(s string) {
		if err := os.WriteFile(file, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("api.port=55400\r\n\n  \ndb.url=postgres://u:p@127.0.0.1:55432/parcels?sslmode=disable\napi.port=55401\nempty=\n")
	told, err := ReadTold([]Told{{Who: "service api", Path: file}, {Who: "service gone", Path: filepath.Join(dir, "gone")}})
	if err != nil || len(told) != 1 {
		t.Fatalf("told %+v, %v", told, err)
	}
	want := map[string]string{"api.port": "55401", "db.url": "postgres://u:p@127.0.0.1:55432/parcels?sslmode=disable", "empty": ""}
	for k, v := range want {
		if told[0].Values[k] != v {
			t.Errorf("%s = %q, want %q", k, told[0].Values[k], v)
		}
	}
	for _, bad := range []string{"api.port 55400", "=55400", "${sys:api.port}=1", "api port=1"} {
		write("ok=1\n" + bad + "\n")
		_, err := ReadTold([]Told{{Who: "service api", Path: file}})
		if code(err) != CodePropertiesLine || !strings.Contains(err.Error(), "service api told axx properties it cannot read") ||
			!strings.Contains(err.Error(), ":2: ") || !strings.Contains(err.Error(), "is not name=value") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestTwoTellOnePropertyClash(t *testing.T) {
	_, err := MergeTold([]ToldValues{
		{Who: "service api", Values: map[string]string{"api.port": "55400", "db.port": "55432"}},
		{Who: "service shop", Values: map[string]string{"shop.port": "55401"}},
		{Who: "the hook before `axx run` (hooks.commands[0])", Values: map[string]string{"db.port": "55433"}},
	})
	if code(err) != CodePropertiesClash || !strings.Contains(err.Error(), "service api and the hook before `axx run` (hooks.commands[0]) both told axx the property db.port") {
		t.Errorf("clash: %v", err)
	}
	values, err := MergeTold([]ToldValues{{Who: "a", Values: map[string]string{"x": "1"}}, {Who: "a", Values: map[string]string{"x": "2"}}})
	if err != nil || values["x"] != "2" {
		t.Errorf("one teller twice: %v, %v", values, err)
	}
}

func TestToldIsWrittenAsAServiceTellsIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "props", "api")
	if err := WriteTold(file, map[string]string{"db.port": "55432", "api.port": "55400"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if string(b) != "api.port=55400\ndb.port=55432\n" {
		t.Errorf("written %q", b)
	}
	if PropertiesFile("d", "api/v2") != filepath.Join("d", "api%2Fv2") {
		t.Errorf("a service's name is escaped in its file's: %s", PropertiesFile("d", "api/v2"))
	}
}

// What the services `axx up` keeps told axx is read by later runs; without a
// supervisor, nothing is.
func TestUpTold(t *testing.T) {
	dir := t.TempDir()
	if told, err := UpTold(dir); err != nil || told != nil {
		t.Fatalf("no supervisor: %v, %v", told, err)
	}
	if err := WriteTold(PropertiesFile(PropertiesDir(dir), "api"), map[string]string{"api.port": "55400"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(UpFile(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	up := `{"pid": ` + strconv.Itoa(os.Getpid()) + `, "services": ["api", "db"]}`
	if err := os.WriteFile(UpFile(dir), []byte(up), 0o600); err != nil {
		t.Fatal(err)
	}
	told, err := UpTold(dir)
	if err != nil || len(told) != 1 || told[0].Service != "api" || told[0].Values["api.port"] != "55400" {
		t.Errorf("told %+v, %v", told, err)
	}
}

// A service's command, env and cleanup are filled in as it starts, with what
// the services it starts after told: those it depends on, or, when no
// service names its dependencies, those before it. -D's win over theirs.
func TestAServiceReadsWhatTheServicesBeforeItTold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		defines map[string]string
		graph   bool
		want    string
	}{
		{"depends on", nil, true, "postgres://localhost:55432/parcels parcels\ncleanup 55432\n"},
		{"declared after", nil, false, "postgres://localhost:55432/parcels parcels\ncleanup 55432\n"},
		{"-D", map[string]string{"db.port": "6543"}, true, "postgres://localhost:6543/parcels parcels\ncleanup 6543\n"},
		{"axx.yaml", map[string]string{"db.name": "kestrel"}, true, "postgres://localhost:55432/parcels kestrel\ncleanup 55432\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "out")
			db := logReady(helperApp(t, "db", "tell", "0s", "db.port=55432", "db.name=parcels"), "told")
			api := logReady(helperApp(t, "api", "getenv", out, "DB_URL", "${sys:db.name}"), "started")
			api.Env["DB_URL"] = "postgres://localhost:${sys:db.port}/parcels"
			api.Cleanup = helper(t, "append", out, "cleanup", "${sys:db.port}")
			services := config.Services{db, api}
			if tc.graph {
				api.DependsOn = []string{"db"}
				services = config.Services{api, db}
			}
			cfg := &config.Config{Properties: map[string]string{"depot": "kestrel"}}
			for k, v := range tc.defines {
				cfg.Properties[k] = v
			}
			if tc.name == "-D" {
				cfg.Defines = tc.defines
			}
			m, err := New(services, Options{ConfigDir: dir, Config: cfg})
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Start(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(out); string(got) != tc.want {
				t.Errorf("ran with %q, want %q", got, tc.want)
			}
			if !strings.Contains(services[0].Env["DB_URL"]+services[1].Env["DB_URL"], "${sys:db.port}") {
				t.Error("filling it in changed axx.yaml's")
			}
		})
	}
}

// A service that started before another told a property it names started
// without it: the run
// stops, and says which dependency it lacks. One that names what it tells
// itself is told why it cannot.
func TestAServiceStartedBeforeItWasToldStopsTheRun(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	api := logReady(helperApp(t, "api", "getenv", out, "DB_URL"), "started")
	api.Env["DB_URL"] = "postgres://localhost:${sys:db.port}/parcels"
	db := logReady(helperApp(t, "db", "tell", "0s", "db.port=55432"), "told")
	cfg := &config.Config{}
	m, err := New(config.Services{api, db}, Options{ConfigDir: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Start(context.Background(), nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeToldLate ||
		ae.Message != "service api started before service db told axx ${sys:db.port}, which its command, env or cleanup names: it started without it" ||
		ae.Hint != "add db to services.api.dependsOn, so that it starts once db is ready" {
		t.Fatalf("start: %v", err)
	}
	if started := m.Started(); len(started) != 0 {
		t.Errorf("still up: %q", started)
	}
	if got, _ := os.ReadFile(out); string(got) != "postgres://localhost:${sys:db.port}/parcels\n" {
		t.Errorf("ran with %q", got)
	}

	self := logReady(helperApp(t, "db", "tell", "0s", "db.port=55432"), "told")
	self.Env["DB_PORT"] = "${sys:db.port}"
	if m, err = New(config.Services{self}, Options{ConfigDir: dir, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	err = m.Start(context.Background(), nil)
	if code(err) != CodeToldLate || !strings.Contains(err.Error(), "service db's command, env or cleanup names ${sys:db.port}, which it tells axx itself") {
		t.Fatalf("start: %v", err)
	}
}
