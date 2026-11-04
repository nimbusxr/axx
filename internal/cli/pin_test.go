package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/version"
)

var errHandedOver = errors.New("handed over")

// stamp makes this test's axx a release of version v.
func stamp(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

// watchHandOver notes the binary axx hands over to, in place of becoming it.
func watchHandOver(t *testing.T) *string {
	t.Helper()
	var to string
	old := handOver
	handOver = func(bin string) error {
		to = bin
		return errHandedOver
	}
	t.Cleanup(func() { handOver = old })
	return &to
}

// pinnedProject is a project naming a version, on a machine with no copies yet.
func pinnedProject(t *testing.T, yaml string) (dir, cache string) {
	t.Helper()
	dir, home := agentProject(t, map[string]string{"axx.yaml": yaml, ".git": "", "features/a.feature": "Feature: a\n\n  Scenario: b\n"})
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir, cache
}

func runEnv(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{Stdout: &out, Stderr: &errb, Env: func(k string) string { return env[k] }}
	code = Run(context.Background(), app, args)
	return out.String(), errb.String(), code
}

func versionBinary(cache, v string) string {
	return filepath.Join(cache, "axx", "versions", v, selfupdate.BinaryName(runtime.GOOS))
}

var pinReleases = []fakeNotes{
	{version: "0.9.0", name: "yes", body: "### Features\n\n* the newest thing ([#3](u))\n"},
	{version: "0.6.0", name: "yes", body: "### ⚠ BREAKING CHANGES\n\n* a change that matters ([#2](u))\n"},
	{version: "0.5.0", name: "yes", body: "### Features\n\n* what the project has ([#1](u))\n"},
}

func TestAProjectRunsOnTheVersionItNames(t *testing.T) {
	for _, tc := range []struct{ name, installed, pin string }{
		{"a newer install than the project", "0.9.0", "0.5.0"},
		{"an older install than the project", "0.5.0", "0.9.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cache := pinnedProject(t, "version: 1\naxx: "+tc.pin+"\n")
			fakeGitHub(t, tc.installed, selfupdate.Archive, pinReleases, false)
			stamp(t, tc.installed)
			to := watchHandOver(t)

			_, stderr, _ := run(t, "validate")
			want := versionBinary(cache, tc.pin)
			if *to != want {
				t.Fatalf("handed over to %q, want %q; %s", *to, want, stderr)
			}
			if !strings.Contains(stderr, "this project runs on axx "+tc.pin+": downloading it (once") {
				t.Errorf("did not say why it downloads: %q", stderr)
			}
			if v := exeVersion(t, want); v != tc.pin {
				t.Errorf("the copy is %s", v)
			}
			entries, err := os.ReadDir(filepath.Dir(filepath.Dir(want)))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".download-") {
					t.Errorf("left %s", e.Name())
				}
			}

			// From now on it is on the machine: no download, and no network.
			*to = ""
			newUpgradeEnvBefore := newUpgradeEnv
			newUpgradeEnv = func() (*upgradeEnv, error) { return nil, errors.New("went to the network") }
			t.Cleanup(func() { newUpgradeEnv = newUpgradeEnvBefore })
			_, stderr, _ = run(t, "validate")
			if *to != want || strings.Contains(stderr, "downloading") {
				t.Errorf("second time: to %q, %q", *to, stderr)
			}
		})
	}
}

func TestWhenAxxStaysAsItIs(t *testing.T) {
	t.Run("the project's own version", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		stamp(t, "0.5.0")
		to := watchHandOver(t)
		if _, stderr, code := run(t, "validate"); *to != "" || code != int(exitcode.OK) {
			t.Errorf("to %q, exit %d: %s", *to, code, stderr)
		}
	})
	t.Run("a project that names none", func(t *testing.T) {
		pinnedProject(t, "version: 1\n")
		stamp(t, "0.9.0")
		to := watchHandOver(t)
		if _, stderr, code := run(t, "validate"); *to != "" || code != int(exitcode.OK) {
			t.Errorf("to %q, exit %d: %s", *to, code, stderr)
		}
	})
	t.Run("a development build", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		stamp(t, "0.0.0-dev")
		to := watchHandOver(t)
		if _, stderr, code := run(t, "validate"); *to != "" || code != int(exitcode.OK) {
			t.Errorf("to %q, exit %d: %s", *to, code, stderr)
		}
	})
	for _, args := range [][]string{
		{"version"}, {"--version"}, {"-c", "axx.yaml", "version"}, {"completion", "zsh"}, {"__complete", "ru"}, {"lsp", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			pinnedProject(t, "version: 1\naxx: 0.5.0\n")
			stamp(t, "0.9.0")
			to := watchHandOver(t)
			if run(t, args...); *to != "" {
				t.Errorf("handed over to %q", *to)
			}
		})
	}
}

func TestEveryOtherCommandFollowsTheProject(t *testing.T) {
	// `axx host` and `axx fixtures` too, and a flag or command only that version has.
	for _, args := range [][]string{
		{"run"},
		{"host"},
		{"fixtures", "check"},
		{"pack", "list"},
		{"init"},
		{"--json", "validate"},
		{"run", "--a-flag-of-another-version"},
		{"a-command-of-another-version"},
		{"lsp", "--project"},
		{"--help"},
		{},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
			fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
			stamp(t, "0.9.0")
			to := watchHandOver(t)
			run(t, args...)
			if *to != versionBinary(cache, "0.5.0") {
				t.Errorf("handed over to %q", *to)
			}
		})
	}
}

func TestConfigChoosesWhichProject(t *testing.T) {
	dir, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
	writeFiles(t, dir, map[string]string{"other/axx.yaml": "version: 1\naxx: 0.6.0\n"})
	fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.9.0")
	to := watchHandOver(t)
	for _, args := range [][]string{
		{"validate", "--config", "other/axx.yaml"},
		{"validate", "-c", "other/axx.yaml"},
		{"--config=other/axx.yaml", "validate"},
		{"-cother/axx.yaml", "validate"},
	} {
		*to = ""
		run(t, args...)
		if *to != versionBinary(cache, "0.6.0") {
			t.Errorf("%v handed over to %q", args, *to)
		}
	}
}

func TestVersionsThatCannotBeUsed(t *testing.T) {
	for _, tc := range []struct{ name, pin, code, want string }{
		{"part of a version", "0.5", codeBadVersion, "is not a version of axx"},
		{"a channel", "nightly", codeBadVersion, "is not a version of axx"},
		{"latest", "latest", codeBadVersion, "is not a version of axx"},
		{"no line value", "", codeBadVersion, "is not a version of axx"},
		{"from before pins", "0.4.0", codeBadVersion, "from before projects could name their version"},
		{"long before pins", "0.1.0", codeBadVersion, "from before projects could name their version"},
		{"no such release", "0.7.7", codeBadVersion, "axx has no release 0.7.7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cache := pinnedProject(t, "version: 1\naxx: "+tc.pin+"\n")
			fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
			stamp(t, "0.9.0")
			to := watchHandOver(t)
			_, stderr, code := run(t, "validate")
			if code == int(exitcode.OK) || !strings.Contains(stderr, tc.code) || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d: %s", code, stderr)
			}
			if *to != "" {
				t.Errorf("handed over to %q", *to)
			}
			if tc.pin != "" && isRegularFile(versionBinary(cache, tc.pin)) {
				t.Error("it downloaded something")
			}
		})
	}
}

func TestADownloadThatIsWrongIsNotKept(t *testing.T) {
	t.Run("the checksum", func(t *testing.T) {
		_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, true)
		stamp(t, "0.9.0")
		to := watchHandOver(t)
		_, stderr, code := run(t, "validate")
		if code != int(exitcode.Environment) || !strings.Contains(stderr, codeNoVersion) || !strings.Contains(stderr, "checksum mismatch") {
			t.Errorf("exit %d: %s", code, stderr)
		}
		if *to != "" || isRegularFile(versionBinary(cache, "0.5.0")) {
			t.Errorf("kept it: to %q", *to)
		}
	})
	t.Run("another version than it says", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the release's axx here is a shell script")
		}
		_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		mux := http.NewServeMux()
		fakeRelease(t, mux, "0.5.0", fakeAxx("0.5.1"), false)
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		old := newUpgradeEnv
		newUpgradeEnv = func() (*upgradeEnv, error) {
			return &upgradeEnv{client: &selfupdate.Client{Downloads: srv.URL + "/dl", HTTP: srv.Client()}}, nil
		}
		t.Cleanup(func() { newUpgradeEnv = old })
		stamp(t, "0.9.0")
		to := watchHandOver(t)
		_, stderr, code := run(t, "validate")
		if code != int(exitcode.Environment) || !strings.Contains(stderr, "does not run here") {
			t.Errorf("exit %d: %s", code, stderr)
		}
		if *to != "" || isRegularFile(versionBinary(cache, "0.5.0")) {
			t.Errorf("kept it: to %q", *to)
		}
	})
	t.Run("no network", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		old := newUpgradeEnv
		newUpgradeEnv = func() (*upgradeEnv, error) {
			return &upgradeEnv{client: &selfupdate.Client{Downloads: url, HTTP: &http.Client{}}}, nil
		}
		t.Cleanup(func() { newUpgradeEnv = old })
		stamp(t, "0.9.0")
		watchHandOver(t)
		_, stderr, code := run(t, "validate")
		if code != int(exitcode.Environment) || !strings.Contains(stderr, codeNoVersion) || !strings.Contains(stderr, selfupdate.EnvDownloadURL) {
			t.Errorf("exit %d: %s", code, stderr)
		}
		// Help for axx itself is still this axx's help.
		_, stderr, code = run(t)
		if code != int(exitcode.OK) {
			t.Errorf("help: exit %d: %s", code, stderr)
		}
	})
}

func TestAutoDownloadOptOut(t *testing.T) {
	for _, off := range []string{"0", "false", "no", "off"} {
		t.Run(off, func(t *testing.T) {
			_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
			fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
			stamp(t, "0.9.0")
			to := watchHandOver(t)
			env := map[string]string{envAutoDownload: off}

			_, stderr, code := runEnv(t, env, "validate")
			if code != int(exitcode.Environment) || !strings.Contains(stderr, codeNoVersion) || !strings.Contains(stderr, "`axx install` downloads it") {
				t.Errorf("exit %d: %s", code, stderr)
			}
			if *to != "" || isRegularFile(versionBinary(cache, "0.5.0")) {
				t.Fatalf("it downloaded: to %q", *to)
			}

			// `axx install` is asking, and what it got is used from then on.
			out, stderr, code := runEnv(t, env, "install")
			if code != int(exitcode.OK) || !strings.Contains(out, "axx 0.5.0 is ready") {
				t.Fatalf("install: exit %d: %s %s", code, out, stderr)
			}
			runEnv(t, env, "validate")
			if *to != versionBinary(cache, "0.5.0") {
				t.Errorf("handed over to %q", *to)
			}
		})
	}
}

func TestInstall(t *testing.T) {
	t.Run("a version named", func(t *testing.T) {
		_, cache := pinnedProject(t, "version: 1\n")
		fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
		stamp(t, "0.9.0")
		for _, arg := range []string{"0.6.0", "v0.6.0"} {
			if out, stderr, code := run(t, "install", arg); code != int(exitcode.OK) || !strings.Contains(out, "axx 0.6.0 is ready") {
				t.Errorf("exit %d: %s %s", code, out, stderr)
			}
		}
		if v := exeVersion(t, versionBinary(cache, "0.6.0")); v != "0.6.0" {
			t.Errorf("the copy is %s", v)
		}
	})
	t.Run("json", func(t *testing.T) {
		_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
		stamp(t, "0.9.0")
		out, stderr, code := run(t, "install", "--json")
		if code != int(exitcode.OK) {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		var env struct {
			Data InstallResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatal(err)
		}
		if env.Data.Version != "0.5.0" || env.Data.Path != versionBinary(cache, "0.5.0") {
			t.Errorf("%+v", env.Data)
		}
	})
	t.Run("nothing to install", func(t *testing.T) {
		pinnedProject(t, "version: 1\n")
		stamp(t, "0.9.0")
		if _, stderr, code := run(t, "install"); code != int(exitcode.Usage) || !strings.Contains(stderr, "names no version") {
			t.Errorf("exit %d: %s", code, stderr)
		}
	})
	t.Run("not a version", func(t *testing.T) {
		pinnedProject(t, "version: 1\n")
		stamp(t, "0.9.0")
		for _, arg := range []string{"latest", "../../evil", "https://example.com/axx"} {
			if _, stderr, code := run(t, "install", arg); code != int(exitcode.Usage) || !strings.Contains(stderr, codeBadVersion) {
				t.Errorf("%s: exit %d: %s", arg, code, stderr)
			}
		}
	})
}

func TestUseVersionForARun(t *testing.T) {
	t.Run("over the project's", func(t *testing.T) {
		_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
		stamp(t, "0.9.0")
		to := watchHandOver(t)
		runEnv(t, map[string]string{envUseVersion: "0.6.0"}, "validate")
		if *to != versionBinary(cache, "0.6.0") {
			t.Errorf("handed over to %q", *to)
		}
	})
	t.Run("the version that runs stays", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		stamp(t, "0.6.0")
		to := watchHandOver(t)
		if _, stderr, code := runEnv(t, map[string]string{envUseVersion: "0.6.0"}, "validate"); *to != "" || code != int(exitcode.OK) {
			t.Errorf("to %q, exit %d: %s", *to, code, stderr)
		}
	})
	t.Run("a project that names none", func(t *testing.T) {
		_, cache := pinnedProject(t, "version: 1\n")
		fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
		stamp(t, "0.9.0")
		to := watchHandOver(t)
		runEnv(t, map[string]string{envUseVersion: "0.5.0"}, "validate")
		if *to != versionBinary(cache, "0.5.0") {
			t.Errorf("handed over to %q", *to)
		}
	})
	t.Run("not a version", func(t *testing.T) {
		pinnedProject(t, "version: 1\n")
		stamp(t, "0.9.0")
		if _, stderr, code := runEnv(t, map[string]string{envUseVersion: "latest"}, "validate"); code != int(exitcode.Usage) || !strings.Contains(stderr, codeBadVersion) {
			t.Errorf("exit %d: %s", code, stderr)
		}
	})
	t.Run("older than pins, in a project with a pin", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		stamp(t, "0.9.0")
		if _, stderr, code := runEnv(t, map[string]string{envUseVersion: "0.4.0"}, "validate"); code != int(exitcode.Usage) || !strings.Contains(stderr, "from before projects") {
			t.Errorf("exit %d: %s", code, stderr)
		}
	})
}

func TestPeekArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want peekedArgs
	}{
		{nil, peekedArgs{}},
		{[]string{"run"}, peekedArgs{command: "run"}},
		{[]string{"--json", "-v", "--compact", "--no-color", "run"}, peekedArgs{command: "run"}},
		{[]string{"-c", "a.yaml", "run"}, peekedArgs{config: "a.yaml", command: "run"}},
		{[]string{"--config", "a.yaml", "run"}, peekedArgs{config: "a.yaml", command: "run"}},
		{[]string{"--config=a.yaml", "run"}, peekedArgs{config: "a.yaml", command: "run"}},
		{[]string{"-ca.yaml", "run"}, peekedArgs{config: "a.yaml", command: "run"}},
		{[]string{"-c=a.yaml", "run"}, peekedArgs{config: "a.yaml", command: "run"}},
		{[]string{"run", "--tags", "@x", "-c", "a.yaml"}, peekedArgs{config: "a.yaml", command: "run"}},
		{[]string{"run", "-Dname=value", "-D", "config=x"}, peekedArgs{command: "run"}},
		{[]string{"--version"}, peekedArgs{version: true}},
		{[]string{"upgrade", "--version", "0.5.0"}, peekedArgs{command: "upgrade"}},
		{[]string{"lsp", "--project"}, peekedArgs{command: "lsp", project: true}},
		{[]string{"run", "--", "--config", "a.yaml"}, peekedArgs{command: "run"}},
		{[]string{"-c"}, peekedArgs{}},
	} {
		if got := peekArgs(tc.args); got != tc.want {
			t.Errorf("%v: %+v, want %+v", tc.args, got, tc.want)
		}
	}
}

func TestPinnableVersion(t *testing.T) {
	for v, want := range map[string]string{
		"0.5.0": "0.5.0", "1.0.0-rc.1": "1.0.0-rc.1",
		"0.4.0": "", "0.1.0": "",
		"0.0.0-dev": "", "0.5.1-0.20260925184804-3bf0dd25927c": "", "0.5.0+dirty": "", "nightly-1": "",
	} {
		stamp(t, v)
		if got := pinnableVersion(version.Get()); got != want {
			t.Errorf("%s: %q, want %q", v, got, want)
		}
	}
}

func TestInitNamesTheVersion(t *testing.T) {
	t.Run("a release", func(t *testing.T) {
		dir, _ := pinnedProject(t, "")
		if err := os.Remove(filepath.Join(dir, "axx.yaml")); err != nil {
			t.Fatal(err)
		}
		stamp(t, "0.9.0")
		if _, stderr, code := run(t, "init", "--no-agents"); code != int(exitcode.OK) {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		b, err := os.ReadFile(filepath.Join(dir, "axx.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "\naxx: 0.9.0\n") {
			t.Errorf("axx.yaml:\n%s", b)
		}
		pin, err := config.ReadPin("")
		if err != nil || pin.Version != "0.9.0" {
			t.Errorf("%+v, %v", pin, err)
		}
		if _, err := config.Load(config.LoadOptions{}); err != nil {
			t.Errorf("it does not load: %v", err)
		}
	})
	t.Run("a development build names none", func(t *testing.T) {
		dir, _ := pinnedProject(t, "")
		if err := os.Remove(filepath.Join(dir, "axx.yaml")); err != nil {
			t.Fatal(err)
		}
		if _, stderr, code := run(t, "init", "--no-agents"); code != int(exitcode.OK) {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		if pin, _ := config.ReadPin(""); pin.File != "" {
			t.Errorf("%+v", pin)
		}
	})
}

func TestVersionCommandSaysTheProjects(t *testing.T) {
	pinnedProject(t, "version: 1\naxx: 0.5.0\n")
	stamp(t, "0.9.0")
	watchHandOver(t)
	out, _, _ := run(t, "version")
	if !strings.Contains(out, "v0.9.0") || !strings.Contains(out, "this project runs on axx 0.5.0") {
		t.Errorf("%s", out)
	}
	out, _, _ = run(t, "version", "--json")
	var env struct {
		Data version.Info `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Version != "0.9.0" || env.Data.Project != "0.5.0" {
		t.Errorf("%+v", env.Data)
	}
}

func TestDoctorSaysWhichVersionItIs(t *testing.T) {
	check := func(t *testing.T, env map[string]string, want string) {
		t.Helper()
		out, stderr, _ := runEnv(t, env, "doctor")
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s\n%s", want, out, stderr)
		}
	}
	t.Run("a development build", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		check(t, nil, "ignores this project's axx: 0.5.0")
	})
	t.Run("handed over", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		stamp(t, "0.5.0")
		t.Setenv(version.EnvLauncher, "0.9.0")
		check(t, nil, "this project's version (started by the installed axx v0.9.0)")
	})
	t.Run("a release known to be newer", func(t *testing.T) {
		pinnedProject(t, "version: 1\naxx: 0.5.0\n")
		stamp(t, "0.5.0")
		_, path := readReminder()
		writeReminder(path, reminderState{Newest: "0.9.0"})
		check(t, nil, "axx 0.9.0 is out, and this is 0.5.0")
	})
}

func TestWithEnv(t *testing.T) {
	got := withEnv([]string{"A=1", "AB=2", "A=3"}, "A", "9")
	if strings.Join(got, ",") != "AB=2,A=9" {
		t.Errorf("%v", got)
	}
}

// ---- the reminder

// aPerson is a person at a terminal, on a release.
func aPerson(t *testing.T, yaml string) {
	t.Helper()
	pinnedProject(t, yaml)
	stamp(t, "0.5.0")
	old := stderrIsTerminal
	stderrIsTerminal = func() bool { return true }
	t.Cleanup(func() { stderrIsTerminal = old })
}

func reminderApp(env map[string]string) (*App, *bytes.Buffer) {
	var errb bytes.Buffer
	return &App{Stdout: &bytes.Buffer{}, Stderr: &errb, Env: func(k string) string { return env[k] }}, &errb
}

func TestReminderIsOnlyForAPersonRunningTests(t *testing.T) {
	aPerson(t, "version: 1\naxx: 0.5.0\n")
	info := version.Get()
	app, _ := reminderApp(nil)
	if !app.wantsReminder([]string{"run"}, info) {
		t.Error("not for a person's run")
	}
	for _, args := range [][]string{{"validate"}, {"doctor"}, {"upgrade"}, {"run", "--json"}, {"--json", "run"}, {}} {
		if app.wantsReminder(args, info) {
			t.Errorf("for %v", args)
		}
	}
	for _, off := range []string{envNoReminder, "CI", envUseVersion, selfupdate.EnvDownloadURL, "CLAUDECODE"} {
		app, _ := reminderApp(map[string]string{off: "1"})
		if app.wantsReminder([]string{"run"}, info) {
			t.Errorf("with %s", off)
		}
	}
	stderrIsTerminal = func() bool { return false }
	if app.wantsReminder([]string{"run"}, info) {
		t.Error("with no terminal")
	}
	stderrIsTerminal = func() bool { return true }
	stamp(t, "0.0.0-dev")
	if app.wantsReminder([]string{"run"}, version.Get()) {
		t.Error("for a development build")
	}
}

func TestReminderSaysItOnceAWeek(t *testing.T) {
	aPerson(t, "version: 1\naxx: 0.5.0\n")
	_, path := readReminder()
	write := func(st reminderState) { writeReminder(path, st) }
	ask := func(code int) string {
		t.Helper()
		app, errb := reminderApp(nil)
		app.startUpgradeReminder(context.Background(), []string{"run"})(code)
		return errb.String()
	}
	checked := time.Now()

	write(reminderState{Checked: checked, Newest: "0.9.0"})
	if got := ask(1); got != "" {
		t.Errorf("after a failed run: %q", got)
	}
	got := ask(0)
	if !strings.Contains(got, "axx 0.9.0 is out, and this project is on 0.5.0: `axx upgrade` moves it") {
		t.Errorf("first: %q", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("more than a line: %q", got)
	}
	if got := ask(0); got != "" {
		t.Errorf("right after: %q", got)
	}
	// A newer release still waits for its week: axx releases often.
	st, _ := readReminder()
	st.Newest = "0.9.1"
	write(st)
	if got := ask(0); got != "" {
		t.Errorf("a new release: %q", got)
	}
	st.Shown = time.Now().Add(-remindEvery - time.Minute)
	write(st)
	if got := ask(0); !strings.Contains(got, "0.9.1") {
		t.Errorf("a week on: %q", got)
	}
	// Nothing to say at the newest release.
	write(reminderState{Checked: checked, Newest: "0.5.0"})
	if got := ask(0); got != "" {
		t.Errorf("up to date: %q", got)
	}
}

func TestReminderForAProjectWithoutAPin(t *testing.T) {
	aPerson(t, "version: 1\n")
	_, path := readReminder()
	writeReminder(path, reminderState{Checked: time.Now(), Newest: "0.9.0"})
	app, errb := reminderApp(nil)
	app.startUpgradeReminder(context.Background(), []string{"run"})(0)
	if !strings.Contains(errb.String(), "and you have 0.5.0: `axx upgrade` installs it") {
		t.Errorf("%q", errb.String())
	}
}

// readsTheList serves a list of releases and counts what asks for it.
func readsTheList(t *testing.T, status int) (*atomic.Int32, *atomic.Value) {
	t.Helper()
	var calls atomic.Int32
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		name := selfupdate.ArchiveName("0.9.0", runtime.GOOS, runtime.GOARCH)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v0.9.0", "assets": []map[string]string{{"name": "checksums.txt"}, {"name": name}}}})
	}))
	t.Cleanup(srv.Close)
	old := newUpgradeEnv
	newUpgradeEnv = func() (*upgradeEnv, error) {
		return &upgradeEnv{client: &selfupdate.Client{API: srv.URL, Token: "secret", HTTP: srv.Client()}}, nil
	}
	t.Cleanup(func() { newUpgradeEnv = old })
	return &calls, &auth
}

func waitForChecked(t *testing.T) reminderState {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := readReminder(); !st.Checked.IsZero() {
			return st
		}
		time.Sleep(10 * time.Millisecond) //nolint:forbidigo // waiting on the reminder's goroutine
	}
	t.Fatal("it never read the list")
	return reminderState{}
}

func TestReminderReadsTheListAtMostOnceADay(t *testing.T) {
	aPerson(t, "version: 1\naxx: 0.5.0\n")
	calls, auth := readsTheList(t, http.StatusOK)
	app, errb := reminderApp(nil)
	app.startUpgradeReminder(context.Background(), []string{"run"})
	st := waitForChecked(t)
	if st.Newest != "0.9.0" {
		t.Errorf("newest %q", st.Newest)
	}
	if got := auth.Load(); got != "" {
		t.Errorf("sent Authorization %q", got)
	}
	if errb.Len() != 0 {
		t.Errorf("said %q", errb.String())
	}
	n := calls.Load()
	app.startUpgradeReminder(context.Background(), []string{"run"})
	time.Sleep(100 * time.Millisecond) //nolint:forbidigo // long enough for a goroutine it should not start
	if calls.Load() != n {
		t.Error("read it again within the day")
	}
}

func TestReminderStaysSilentWhenItCannotRead(t *testing.T) {
	aPerson(t, "version: 1\naxx: 0.5.0\n")
	readsTheList(t, http.StatusForbidden)
	app, errb := reminderApp(nil)
	remind := app.startUpgradeReminder(context.Background(), []string{"run"})
	st := waitForChecked(t)
	remind(0)
	if st.Newest != "" || errb.Len() != 0 {
		t.Errorf("%+v, said %q", st, errb.String())
	}
}

func TestReminderDoesNothingElsewhere(t *testing.T) {
	aPerson(t, "version: 1\naxx: 0.5.0\n")
	calls, _ := readsTheList(t, http.StatusOK)
	app, errb := reminderApp(map[string]string{"CI": "true"})
	_, path := readReminder()
	writeReminder(path, reminderState{Newest: "0.9.0"})
	app.startUpgradeReminder(context.Background(), []string{"run"})(0)
	time.Sleep(100 * time.Millisecond) //nolint:forbidigo // long enough for a goroutine it should not start
	if calls.Load() != 0 || errb.Len() != 0 {
		t.Errorf("calls %d, said %q", calls.Load(), errb.String())
	}
}

// ---- axx upgrade in a project that names its version

func pinOnDisk(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "axx.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgradeMovesTheProject(t *testing.T) {
	dir, cache := pinnedProject(t, "# top\nversion: 1\naxx: 0.5.0 # kept\nrun:\n  paths: [features]\n")
	exe := fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.9.0")
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("AXX_FAKE_LOG", log)

	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := pinOnDisk(t, dir); got != "# top\nversion: 1\naxx: 0.9.0 # kept\nrun:\n  paths: [features]\n" {
		t.Errorf("axx.yaml:\n%s", got)
	}
	for _, want := range []string{
		"what changed since 0.5.0", "Breaking changes", "a change that matters", "the newest thing",
		"moved this project from axx 0.5.0 to 0.9.0", "commit it", "axx validate passes on 0.9.0", "axx lint passes on 0.9.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "what the project has") {
		t.Errorf("listed what the project already had:\n%s", out)
	}
	if v := exeVersion(t, versionBinary(cache, "0.9.0")); v != "0.9.0" {
		t.Errorf("the copy is %s", v)
	}
	if v := exeVersion(t, exe); v != "0.9.0" {
		t.Errorf("the installed axx is %s", v)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"validate --compact", "lint --compact"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("no %q in calls:\n%s", want, calls)
		}
	}
}

func TestUpgradeBringsAnOlderInstalledAxxAlong(t *testing.T) {
	dir, _ := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
	exe := fakeGitHub(t, "0.6.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.6.0")
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(pinOnDisk(t, dir), "axx: 0.9.0") {
		t.Errorf("axx.yaml:\n%s", pinOnDisk(t, dir))
	}
	if v := exeVersion(t, exe); v != "0.9.0" {
		t.Errorf("the installed axx is %s", v)
	}
	if !strings.Contains(out, "upgraded the installed axx from 0.6.0 to 0.9.0") {
		t.Errorf("%s", out)
	}
}

func TestUpgradeToAVersionMovesOnlyTheProject(t *testing.T) {
	dir, _ := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
	exe := fakeGitHub(t, "0.5.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.5.0")
	if _, stderr, code := run(t, "upgrade", "--version", "0.6.0"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(pinOnDisk(t, dir), "axx: 0.6.0") {
		t.Errorf("axx.yaml:\n%s", pinOnDisk(t, dir))
	}
	if v := exeVersion(t, exe); v != "0.5.0" {
		t.Errorf("the installed axx is %s", v)
	}
}

func TestUpgradeTakesAProjectBack(t *testing.T) {
	dir, _ := pinnedProject(t, "version: 1\naxx: 0.9.0\n")
	exe := fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.9.0")
	out, stderr, code := run(t, "upgrade", "--version", "0.5.0")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(pinOnDisk(t, dir), "axx: 0.5.0") || !strings.Contains(out, "took back this project from axx 0.9.0 to 0.5.0") {
		t.Errorf("%s\n%s", pinOnDisk(t, dir), out)
	}
	if v := exeVersion(t, exe); v != "0.9.0" {
		t.Errorf("the installed axx is %s", v)
	}
}

func TestUpgradeCheckOfAProjectChangesNothing(t *testing.T) {
	dir, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
	exe := fakeGitHub(t, "0.5.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.5.0")
	out, stderr, code := run(t, "upgrade", "--check")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"a change that matters", "this project is on axx 0.5.0 and 0.9.0 is available: `axx upgrade` moves it"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(pinOnDisk(t, dir), "axx: 0.5.0") || isRegularFile(versionBinary(cache, "0.9.0")) {
		t.Error("it changed something")
	}
	if v := exeVersion(t, exe); v != "0.5.0" {
		t.Errorf("the installed axx is %s", v)
	}
}

func TestUpgradeOfAProjectOnTheNewest(t *testing.T) {
	pinnedProject(t, "version: 1\naxx: 0.9.0\n")
	exe := fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.9.0")
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) || !strings.Contains(out, "this project is on axx 0.9.0, the newest release") {
		t.Errorf("exit %d: %s %s", code, out, stderr)
	}
	if strings.Contains(out, "upgraded the installed axx") {
		t.Errorf("the installed axx is the newest already:\n%s", out)
	}
	if v := exeVersion(t, exe); v != "0.9.0" {
		t.Errorf("the installed axx is %s", v)
	}
}

// Someone new to a project on the newest release has only the installed axx left to
// upgrade: it comes along as it does when the project moves.
func TestUpgradeOfAProjectOnTheNewestBringsAnOlderInstalledAxxAlong(t *testing.T) {
	dir, _ := pinnedProject(t, "version: 1\naxx: 0.9.0\n")
	exe := fakeGitHub(t, "0.5.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.5.0")
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"this project is on axx 0.9.0, the newest release", "upgraded the installed axx from 0.5.0 to 0.9.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if v := exeVersion(t, exe); v != "0.9.0" {
		t.Errorf("the installed axx is %s", v)
	}
	if !strings.Contains(pinOnDisk(t, dir), "axx: 0.9.0") {
		t.Errorf("axx.yaml:\n%s", pinOnDisk(t, dir))
	}
}

func TestUpgradeCheckOfAProjectOnTheNewestLeavesTheInstalledAxx(t *testing.T) {
	pinnedProject(t, "version: 1\naxx: 0.9.0\n")
	exe := fakeGitHub(t, "0.5.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.5.0")
	if _, stderr, code := run(t, "upgrade", "--check"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if v := exeVersion(t, exe); v != "0.5.0" {
		t.Errorf("the installed axx is %s", v)
	}
}

// On Linux a program's own path has its links followed: for Homebrew's axx, a
// version's directory, which `brew upgrade` removes.
func TestHomebrewsAxxRunsByHomebrewsLinkAfterAnUpgrade(t *testing.T) {
	prefix := t.TempDir()
	exe := filepath.Join(prefix, "Caskroom", "axx", "0.4.0", "axx")
	link := filepath.Join(prefix, "bin", "axx")
	for _, f := range []string{exe, link} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := stableAxxPath(exe, exe); got != link {
		t.Errorf("Homebrew's axx runs by %s", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if got := stableAxxPath(exe, exe); got != exe {
		t.Errorf("with no link, it runs by %s", got)
	}
	other := filepath.Join(prefix, "elsewhere", "axx")
	if got := stableAxxPath(other, other); got != other {
		t.Errorf("an axx that is not Homebrew's runs by %s", got)
	}
}

func TestUpgradeLeavesTheLineWhenItCannotGetTheVersion(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		corrupt      bool
		want         string
	}{
		{"no such release", "0.7.7", false, "axx has no release 0.7.7"},
		{"from before pins", "0.4.0", false, "from before projects"},
		{"not a version", "latest", false, "is not a version of axx"},
		{"the nightly", "nightly", false, "cannot run on the nightly"},
		{"a bad download", "0.6.0", true, "checksum mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
			fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, tc.corrupt)
			stamp(t, "0.9.0")
			_, stderr, code := run(t, "upgrade", "--version", tc.target)
			if code == int(exitcode.OK) || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d: %s", code, stderr)
			}
			if !strings.Contains(pinOnDisk(t, dir), "axx: 0.5.0") {
				t.Errorf("axx.yaml:\n%s", pinOnDisk(t, dir))
			}
		})
	}
}

func TestUpgradeSaysWhatTheNewVersionFinds(t *testing.T) {
	dir, _ := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
	fakeGitHub(t, "0.9.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.9.0")
	t.Setenv("AXX_FAKE_FAIL", "lint")
	out, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"axx validate passes on 0.9.0", "axx lint finds problems on 0.9.0", "problem found by lint on 0.9.0", "go back: `axx upgrade --version 0.5.0`"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(pinOnDisk(t, dir), "axx: 0.9.0") {
		t.Errorf("axx.yaml:\n%s", pinOnDisk(t, dir))
	}
}

func TestUpgradeWithoutAPinLeavesAxxYamlAlone(t *testing.T) {
	dir, _ := pinnedProject(t, "version: 1\n")
	exe := fakeGitHub(t, "0.5.0", selfupdate.Archive, pinReleases, false)
	stamp(t, "0.5.0")
	if _, stderr, code := run(t, "upgrade"); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := pinOnDisk(t, dir); got != "version: 1\n" {
		t.Errorf("axx.yaml:\n%s", got)
	}
	if v := exeVersion(t, exe); v != "0.9.0" {
		t.Errorf("the installed axx is %s", v)
	}
}

func TestMirrorNamesWhereDownloadsComeFrom(t *testing.T) {
	t.Setenv(selfupdate.EnvDownloadURL, "https://mirror.example.com/axx/")
	if got := selfupdate.New().Downloads; got != "https://mirror.example.com/axx" {
		t.Errorf("%s", got)
	}
}
