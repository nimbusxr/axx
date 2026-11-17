package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/progress"
	"github.com/nimbusxr/axx/internal/selfupdate"
)

// lockedBuffer is a buffer a command and its reporter write to.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// slowGitHub is a GitHub that takes its time: before it lists the releases,
// and halfway through the archive of release v, which it serves with its
// size. It returns the installed axx an upgrade replaces.
func slowGitHub(t *testing.T, current, v string, wait time.Duration) (exe string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the release's axx here is a shell script")
	}
	archive := releaseArchive(t, fakeAxx(v))
	name := selfupdate.ArchiveName(v, runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(wait)
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"tag_name": "v" + v, "published_at": "2026-11-02T00:00:00Z", "body": "### Features\n\n* something new ([#1](u))\n",
			"assets": []map[string]string{{"name": "checksums.txt"}, {"name": name}},
		}})
	})
	mux.HandleFunc("/dl/v"+v+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sha256Hex(archive) + "  " + name + "\n"))
	})
	mux.HandleFunc("/dl/v"+v+"/"+name, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		half := len(archive) / 2
		_, _ = w.Write(archive[:half])
		w.(http.Flusher).Flush()
		time.Sleep(wait)
		_, _ = w.Write(archive[half:])
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	exe = filepath.Join(t.TempDir(), "axx")
	if err := os.WriteFile(exe, []byte(fakeAxx(current)), 0o755); err != nil {
		t.Fatal(err)
	}
	old := newUpgradeEnv
	newUpgradeEnv = func() (*upgradeEnv, error) {
		return &upgradeEnv{
			client: &selfupdate.Client{API: srv.URL + "/api", Downloads: srv.URL + "/dl", HTTP: srv.Client()},
			exe:    exe, invoked: exe, current: current, method: selfupdate.Archive,
		}, nil
	}
	t.Cleanup(func() { newUpgradeEnv = old })
	return exe
}

func TestUpgradeSaysWhatItDoesWhileItTakesTime(t *testing.T) {
	agentProject(t, nil)
	exe := slowGitHub(t, "0.3.0", "0.4.0", 300*time.Millisecond)
	var out bytes.Buffer
	var errb lockedBuffer
	app := &App{Stdout: &out, Stderr: &errb, Env: func(string) string { return "" }}
	// As for an agent: plain lines, and here one each 100ms for what goes on.
	app.reporter = progress.New(&errb, progress.Options{Still: 100 * time.Millisecond, Slow: 100 * time.Millisecond})
	if code := Run(context.Background(), app, []string{"upgrade"}); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if v := exeVersion(t, exe); v != "0.4.0" {
		t.Errorf("axx is %s", v)
	}
	stderr := errb.String()
	// GitHub's list took a while: said, once it had. The download says that
	// it goes on, and how far it is; nothing rewrites a line.
	for _, want := range []string{
		`(?m)^reading axx's releases on GitHub\n`,
		`(?m)^downloading axx 0\.4\.0\n`,
		`(?m)^axx: still downloading axx 0\.4\.0 \(0s, \d+% of \d+ (B|KB)\)\n`,
		`(?m)^checksum verified\n`,
	} {
		if !regexp.MustCompile(want).MatchString(stderr) {
			t.Errorf("stderr lacks %s:\n%s", want, stderr)
		}
	}
	if strings.Index(stderr, "reading axx's releases") > strings.Index(stderr, "downloading axx 0.4.0") {
		t.Errorf("out of order:\n%s", stderr)
	}
	if strings.ContainsAny(stderr, "\r\x1b") {
		t.Errorf("a line was rewritten:\n%q", stderr)
	}
	if !strings.Contains(out.String(), "upgraded axx from 0.3.0 to 0.4.0") {
		t.Errorf("stdout: %s", out.String())
	}
}

func TestAQuickUpgradeAddsNoLines(t *testing.T) {
	agentProject(t, nil)
	fakeGitHub(t, "0.3.0", selfupdate.Archive, twoReleases, false)
	_, stderr, code := run(t, "upgrade")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if stderr != "downloading axx 0.4.0\nchecksum verified\n" {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestOnlyAPersonAtATerminalGetsALiveLine(t *testing.T) {
	env := map[string]string{}
	app := &App{Stderr: &bytes.Buffer{}, Env: func(k string) string { return env[k] }}
	if !app.livesAtATerminal() {
		t.Error("nothing says it is not a person")
	}
	for _, k := range []string{"CI", "CLAUDECODE", "CODEX_SANDBOX", "CURSOR_AGENT", "AI_AGENT"} {
		env = map[string]string{k: "1"}
		if app.livesAtATerminal() {
			t.Errorf("%s is set", k)
		}
	}
	env = map[string]string{"TERM": "dumb"}
	if app.livesAtATerminal() {
		t.Error("a terminal that cannot rewrite a line")
	}
	// What is not a terminal never is one: a buffer, a pipe, a file.
	var buf lockedBuffer
	env = map[string]string{}
	app = &App{Stderr: &buf, Env: func(k string) string { return env[k] }}
	app.step("axx: preparing rest (once; cached for later runs)", "preparing rest").Done()
	if got := buf.String(); got != "axx: preparing rest (once; cached for later runs)\n" {
		t.Errorf("wrote %q", got)
	}
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
