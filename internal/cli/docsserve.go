package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/docsite"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/version"
)

// envDocsURL names the docs site the links axx prints lead to: an
// organization's copy, or `axx docs serve`. It wins over axx.yaml's docs.url.
const envDocsURL = "AXX_DOCS_URL"

const (
	codeDocsURL     = "AXX-E0025"
	codeDocsPort    = "AXX-E0026"
	codeNoDocs      = "AXX-E0027"
	codeNotDocsSite = "AXX-E0028"
)

// docsURLForm is the address of a docs site, as axx.yaml's schema has it for
// docs.url: http or https, a host, and a path; no query or fragment.
var docsURLForm = regexp.MustCompile(`^https?://[^/?#]+(/[^?#]*)?$`)

// getenv is a.Env, or the process's environment for an App without one.
func (a *App) getenv(k string) string {
	if a.Env == nil {
		return os.Getenv(k)
	}
	return a.Env(k)
}

// useDocsURL makes the links axx prints lead to the site AXX_DOCS_URL names,
// or, without it, to the public site until a project's axx.yaml names one
// (useProjectDocsURL).
func (a *App) useDocsURL() error {
	v := strings.TrimSpace(a.getenv(envDocsURL))
	if v != "" && !docsURLForm.MatchString(v) {
		axxerr.SetDocsBase("")
		return axxerr.New(codeDocsURL, exitcode.Usage, "%s=%s is not the address of a docs site", envDocsURL, v).
			WithHint("set it to the address of the site, like https://axx-docs.example.com or http://127.0.0.1:%d (axx docs serve), or unset it for %s", config.DefaultDocsPort, axxerr.DefaultDocsBase)
	}
	axxerr.SetDocsBase(v)
	return nil
}

// useProjectDocsURL makes the links axx prints lead to the site a project's
// axx.yaml names (docs.url), unless AXX_DOCS_URL names one.
func (a *App) useProjectDocsURL(cfg *config.Config) {
	if cfg != nil && cfg.Docs.URL != "" && strings.TrimSpace(a.getenv(envDocsURL)) == "" {
		axxerr.SetDocsBase(cfg.Docs.URL)
	}
}

// DocsServeResult is the JSON result of `axx docs serve`, written as it
// starts serving.
type DocsServeResult struct {
	URL     string `json:"url"`
	Version string `json:"version,omitempty"`
	Dir     string `json:"dir"`
}

type docsServeOptions struct {
	port int
	from string
	open bool
}

func newDocsServeCmd(app *App) *cobra.Command {
	var o docsServeOptions
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve axx's documentation on this machine, for a network that cannot reach the site",
		Long: `Serve the documentation of the axx that runs, the site at https://axx.nimbusxr.us
as its release has it, on 127.0.0.1, until Ctrl-C. In a project that names its
axx version, that is the version's.

The first time, axx downloads the docs from the release on GitHub
(axx_<version>_docs.tar.gz, checked against the release's checksums.txt) into
its cache; AXX_DOWNLOAD_URL names a mirror. Without either, give it a copy:
--from serves a docs archive, or a built site's folder.

The port is 7357 unless --port or axx.yaml's docs.port names another; --port 0
takes any free one. A port another program has is an error: axx never picks
another by itself.

Point the links axx prints (the docs line of an error) at it with
` + "`AXX_DOCS_URL=http://127.0.0.1:7357`" + `, or docs.url in axx.yaml.`,
		Example: `  axx docs serve --open
  axx docs serve --port 9123
  axx docs serve --from axx_0.7.0_docs.tar.gz`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("port") {
				o.port = app.docsPort()
			}
			return app.serveDocs(cmd.Context(), o)
		},
	}
	cmd.Flags().IntVar(&o.port, "port", config.DefaultDocsPort, "the port on 127.0.0.1 (default: axx.yaml's docs.port, else 7357); 0 takes any free one")
	cmd.Flags().StringVar(&o.from, "from", "", "serve this docs archive (axx_<version>_docs.tar.gz) or built site's folder instead of the release's")
	cmd.Flags().BoolVar(&o.open, "open", false, "open the docs in the browser")
	return cmd
}

// docsPort is the port axx.yaml names for the docs, or the default. An
// axx.yaml it cannot read gives the default, with a warning: the docs are
// where to read how to fix it.
func (a *App) docsPort() int {
	cfg, err := a.loadConfig(&configFlags{})
	if err != nil {
		fmt.Fprintf(a.Stderr, "warning: axx.yaml cannot be read, so the docs are served on port %d: %s\n", config.DefaultDocsPort, firstLine(err))
		return config.DefaultDocsPort
	}
	return cfg.DocsPort()
}

func (a *App) serveDocs(ctx context.Context, o docsServeOptions) error {
	if o.port < 0 || o.port > 65535 {
		return axxerr.New("AXX-E0001", exitcode.Usage, "--port %d is not a port", o.port).WithHint("give a port from 1 to 65535, or 0 for any free one")
	}
	dir, v, cleanup, err := a.docsSite(ctx, o.from)
	if err != nil {
		return err
	}
	defer cleanup()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.port)))
	if err != nil {
		return axxerr.Wrap(err, codeDocsPort, exitcode.Environment, "the docs cannot be served on 127.0.0.1:%d", o.port).
			WithHint("another program has the port, most likely: pick another with --port, or docs.port in axx.yaml (--port 0 takes any free one)")
	}
	srv := &http.Server{Handler: docsite.Handler(dir), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	url := fmt.Sprintf("http://%s/", ln.Addr())
	if err := a.Emit(DocsServeResult{URL: url, Version: v, Dir: dir}, func(w io.Writer) error {
		what := "axx's docs"
		if v != "" {
			what = "axx " + v + "'s docs"
		}
		_, err := fmt.Fprintf(w, "%s at %s (Ctrl-C stops it)\n", what, url)
		return err
	}); err != nil {
		_ = srv.Close()
		return err
	}
	if o.open {
		if err := openBrowser(ctx, url); err != nil {
			fmt.Fprintf(a.Stderr, "warning: cannot open a browser (%s): open %s yourself\n", err, url)
		}
	}

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return ctx.Err()
	}
}

// docsSite is the folder of the site to serve and its version: the one from
// gives, or this axx release's, downloaded into the cache the first time.
func (a *App) docsSite(ctx context.Context, from string) (dir, v string, cleanup func(), err error) {
	if from != "" {
		dir, cleanup, err := docsite.Open(from)
		if err != nil {
			return "", "", nil, axxerr.Wrap(err, codeNotDocsSite, exitcode.Usage, "%s cannot be served", from).
				WithHint("give a release's docs archive (axx_<version>_docs.tar.gz) or the folder of a built site, which has index.html at its top (`npm run build` in axx's docs/ writes docs/dist)")
		}
		return dir, "", cleanup, nil
	}
	info := version.Get()
	if info.Channel == "dev" {
		return "", "", nil, axxerr.New(codeNoDocs, exitcode.Usage, "this axx (%s) is a development build, whose docs no release has", info.Version).
			WithHint("serve a built site with --from: `npm run build` in axx's docs/ writes docs/dist, so `axx docs serve --from docs/dist`")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", "", nil, err
	}
	dir, err = docsite.Release(ctx, selfupdate.New(), info.Version, filepath.Join(base, "axx", "docs"))
	if err != nil {
		return "", "", nil, noDocs(info.Version, err)
	}
	return dir, info.Version, func() {}, nil
}

// noDocs says why the docs of release v cannot be had.
func noDocs(v string, err error) error {
	name := selfupdate.DocsArchiveName(v)
	switch {
	case errors.Is(err, selfupdate.ErrNotListed):
		return axxerr.Wrap(err, codeNoDocs, exitcode.Environment, "axx %s's release has no docs to serve: it is from before releases had them", v).
			WithHint("upgrade (axx upgrade), whose release has them, or serve a later release's %s with --from", strings.Replace(name, v, "<version>", 1))
	case errors.Is(err, selfupdate.ErrChecksum):
		return axxerr.Wrap(err, codeNoDocs, exitcode.Environment, "the download of %s is not what axx %s's release published", name, v).
			WithHint("run it again; a mirror (%s) that keeps a different file needs fixing", selfupdate.EnvDownloadURL)
	}
	return axxerr.Wrap(err, codeNoDocs, exitcode.Environment, "axx cannot get %s from axx %s's release", name, v).
		WithHint("axx gets the docs from github.com, once. Check that this machine reaches it, name a mirror with %s, or copy %s from a machine that does and serve it with --from", selfupdate.EnvDownloadURL, name)
}

// openBrowser opens a URL in the person's browser.
func openBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
