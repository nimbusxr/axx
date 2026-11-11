package cli

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// builtSite writes a docs site, as the docs' build writes one, into a folder.
func builtSite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"index.html":                "<h1>axx</h1>",
		"404.html":                  "<h1>not here</h1>",
		"guides/install/index.html": "<h1>install</h1>",
		"guides/install.md":         "# install",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// serving runs `axx docs serve` with args until the test ends, and gives
// its JSON result, written once it serves.
func serving(t *testing.T, args ...string) DocsServeResult {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r, w := io.Pipe()
	var stderr strings.Builder
	app := &App{Stdout: w, Stderr: &stderr, Env: func(string) string { return "" }}
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, app, append([]string{"docs", "serve", "--json"}, args...))
		_ = w.Close()
	}()
	t.Cleanup(func() {
		cancel()
		_, _ = io.Copy(io.Discard, r)
		if code := <-done; code != int(exitcode.Interrupted) {
			t.Errorf("stopped with %d, want %d (interrupted)", code, exitcode.Interrupted)
		}
	})
	var env struct {
		OK   bool            `json:"ok"`
		Data DocsServeResult `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&env); err != nil || !env.OK {
		t.Fatalf("no result (%v): %s", err, stderr.String())
	}
	return env.Data
}

func get(t *testing.T, url, accept string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDocsServeServesTheSite(t *testing.T) {
	t.Chdir(t.TempDir())
	site := builtSite(t)
	res := serving(t, "--port", "0", "--from", site)
	if !strings.HasPrefix(res.URL, "http://127.0.0.1:") || res.Dir != site {
		t.Fatalf("result: %+v", res)
	}
	if code, body := get(t, res.URL, ""); code != http.StatusOK || body != "<h1>axx</h1>" {
		t.Fatalf("home: %d %q", code, body)
	}
	if code, body := get(t, res.URL+"guides/install/", "text/markdown"); code != http.StatusOK || body != "# install" {
		t.Fatalf("an agent's request: %d %q", code, body)
	}
	if code, body := get(t, res.URL+"no/such/page/", ""); code != http.StatusNotFound || body != "<h1>not here</h1>" {
		t.Fatalf("a page that is not there: %d %q", code, body)
	}
}

func TestDocsServeTakesAxxYamlsPort(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	port := freePort(t)
	if err := os.WriteFile(filepath.Join(dir, "axx.yaml"), []byte("docs:\n  port: "+strconv.Itoa(port)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := serving(t, "--from", builtSite(t))
	if want := "http://127.0.0.1:" + strconv.Itoa(port) + "/"; res.URL != want {
		t.Fatalf("url %s, want %s", res.URL, want)
	}
}

func TestDocsServeRefusesAPortThatIsTaken(t *testing.T) {
	t.Chdir(t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_, stderr, code := run(t, "docs", "serve", "--port", port, "--from", builtSite(t))
	if code != int(exitcode.Environment) || !strings.Contains(stderr, codeDocsPort) || !strings.Contains(stderr, "127.0.0.1:"+port) || !strings.Contains(stderr, "--port") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
}

func TestDocsServeSaysWhatItCannotServe(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, c := range map[string]struct {
		args []string
		code string
	}{
		"a development build's docs": {nil, codeNoDocs},
		"a folder of no site":        {[]string{"--from", t.TempDir()}, codeNotDocsSite},
		"nothing":                    {[]string{"--from", filepath.Join(t.TempDir(), "nothing")}, codeNotDocsSite},
		"a port past the last":       {[]string{"--port", "70000", "--from", builtSite(t)}, "AXX-E0001"},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, code := run(t, append([]string{"docs", "serve", "--port", "0"}, c.args...)...)
			if code == int(exitcode.OK) || !strings.Contains(stderr, c.code) {
				t.Fatalf("exit %d, want %s:\n%s", code, c.code, stderr)
			}
		})
	}
}

// The links axx prints lead to AXX_DOCS_URL's site, else axx.yaml's
// docs.url, else the public site. Not parallel: the site is the process's.
func TestDocsLinksLeadToTheConfiguredSite(t *testing.T) {
	t.Cleanup(func() { axxerr.SetDocsBase("") })
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A feature that does not parse: axx fmt reports it, after it read axx.yaml.
	write("features/broken.feature", "Feature: broken\n  Scenario: s\n    Given a step\n      | a |\n    | b\n")
	docsLine := func(env map[string]string) string {
		t.Helper()
		var out, errb strings.Builder
		app := &App{Stdout: &out, Stderr: &errb, Env: func(k string) string { return env[k] }}
		if code := Run(context.Background(), app, []string{"fmt", "--check", "--json"}); code == int(exitcode.OK) {
			t.Fatalf("axx fmt passed a feature that does not parse:\n%s", out.String())
		}
		var res struct {
			Errors []EnvelopeError `json:"errors"`
		}
		if err := json.Unmarshal([]byte(out.String()), &res); err != nil || len(res.Errors) == 0 {
			t.Fatalf("no error in %s (%v)", out.String(), err)
		}
		return res.Errors[0].Docs
	}

	write("axx.yaml", "version: 1\n")
	if got := docsLine(nil); !strings.HasPrefix(got, "https://axx.nimbusxr.us/references/error-codes/#axx-e") {
		t.Fatalf("without a site named: %s", got)
	}
	write("axx.yaml", "docs:\n  url: https://docs.example.com/axx/\n")
	if got := docsLine(nil); !strings.HasPrefix(got, "https://docs.example.com/axx/references/error-codes/#axx-e") {
		t.Fatalf("axx.yaml's docs.url: %s", got)
	}
	if got := docsLine(map[string]string{envDocsURL: "http://127.0.0.1:7357"}); !strings.HasPrefix(got, "http://127.0.0.1:7357/references/error-codes/#axx-e") {
		t.Fatalf("AXX_DOCS_URL over docs.url: %s", got)
	}

	var errb strings.Builder
	app := &App{Stdout: io.Discard, Stderr: &errb, Env: func(k string) string {
		if k == envDocsURL {
			return "docs.example.com"
		}
		return ""
	}}
	if code := Run(context.Background(), app, []string{"version"}); code != int(exitcode.Usage) || !strings.Contains(errb.String(), codeDocsURL) {
		t.Fatalf("an AXX_DOCS_URL that is no address: exit %d:\n%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "docs: https://axx.nimbusxr.us/") {
		t.Fatalf("its own error should link to the public site:\n%s", errb.String())
	}
}
