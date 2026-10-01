package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/lsp"
	"github.com/nimbusxr/axx/internal/version"
)

func newLSPCmd(app *App) *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Serve feature-file editing to editors over the Language Server Protocol (stdio)",
		Long: `Start a language server on stdin/stdout for feature files: undefined,
ambiguous and misused steps and Gherkin syntax errors as you type, step
completion, step documentation on hover, going to a step's definition,
highlighted step parameters, and links to, path completion for and warnings
about missing files that steps name.

Editors start it themselves: the axx plugin for IntelliJ IDEA and the axx
extension for VS Code do, and other editors can run "axx lsp" for *.feature
files. Each axx project the editor opens gets the steps of its own packs,
custom packs included. When a project's axx.yaml, its axx-packs.yaml or the
code of a pack of its own changes, the server loads the project's steps
again, in editors that report changed files (IntelliJ IDEA and VS Code do).`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project || app.Config != "" {
				return lsp.Serve(cmd.Context(), os.Stdin, os.Stdout, lsp.Options{Version: version.Get().Version, Logger: app.logger()})
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			return lsp.Route(cmd.Context(), os.Stdin, os.Stdout, lsp.RouteOptions{
				Version: version.Get().Version, Logger: app.logger(),
				Start: func(dir string) (*lsp.Backend, error) { return startProjectServer(exe, dir, app.Stderr) },
			})
		},
	}
	// Many language clients pass --stdio; stdio is the only transport.
	cmd.Flags().Bool("stdio", true, "communicate over stdin and stdout (the default and only transport)")
	_ = cmd.Flags().MarkHidden("stdio")
	// The server of one project, which `axx lsp` starts for each project.
	cmd.Flags().BoolVar(&project, "project", false, "serve the axx project of the working directory only")
	_ = cmd.Flags().MarkHidden("project")
	return cmd
}

// startProjectServer starts `axx lsp --project` in a project's directory,
// where it prepares an axx with the project's packs, as every command does.
// Its log goes to stderr, and the end of it explains a server that failed.
func startProjectServer(exe, dir string, stderr io.Writer) (*lsp.Backend, error) {
	cmd := exec.Command(exe, "lsp", "--project") //nolint:noctx // the router stops it
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, envPackBuild+"=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	tail := &tailBuffer{max: 4096}
	cmd.Stderr = io.MultiWriter(stderr, tail)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &lsp.Backend{
		In: in, Out: out,
		Wait: func() error {
			err := cmd.Wait()
			if err == nil {
				return nil
			}
			if msg := lastError(tail.String()); msg != "" {
				return errors.New(msg)
			}
			return err
		},
		Stop: func() { _ = cmd.Process.Kill() },
	}, nil
}

// lastError is the last error axx printed: its "error" line and the lines
// after it, like the hint.
func lastError(log string) string {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "error: ") || strings.HasPrefix(lines[i], "error[") {
			return strings.Join(lines[i:], "\n")
		}
	}
	return ""
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
