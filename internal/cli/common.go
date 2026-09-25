package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// configFlags are shared by commands that load axx.yaml.
type configFlags struct {
	profile string
	defines []string
}

func (f *configFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.profile, "profile", "", "apply profiles.<name> / axx.<name>.yaml (env: AXX_PROFILE)")
	cmd.Flags().StringArrayVarP(&f.defines, "define", "D", nil, "set a property for ${sys:name}, e.g. -D local.host=docker")
}

func (f *configFlags) properties() (map[string]string, error) {
	out := map[string]string{}
	for _, d := range f.defines {
		k, v, ok := strings.Cut(d, "=")
		if !ok || k == "" {
			return nil, axxerr.New("AXX-E0002", exitcode.Usage, "invalid -D %q", d).WithHint("use -D name=value")
		}
		out[k] = v
	}
	return out, nil
}

// loadConfig loads axx.yaml honoring --config, --profile and -D.
func (a *App) loadConfig(f *configFlags) (*config.Config, error) {
	props, err := f.properties()
	if err != nil {
		return nil, err
	}
	return config.Load(config.LoadOptions{Path: a.Config, Profile: f.profile, Properties: props})
}

// loadEngine loads configuration and assembles packs.
func (a *App) loadEngine(f *configFlags) (*engine.Engine, error) {
	cfg, err := a.loadConfig(f)
	if err != nil {
		return nil, err
	}
	return engine.New(engine.Options{Config: cfg, Logger: a.logger()})
}

// hintNoPacks tells a project without axx-packs.yaml where steps come from.
func (a *App) hintNoPacks(e *engine.Engine) {
	if !e.Declared {
		fmt.Fprintf(a.Stderr, "hint: this project uses no packs, so it has no steps; add the packs your steps come from with `axx pack add rest sql ...` (`axx pack list` lists them)\n")
	}
}

func (a *App) logger() *slog.Logger {
	level := slog.LevelWarn
	switch {
	case a.Verbose >= 2:
		level = slog.LevelDebug
	case a.Verbose == 1:
		level = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(a.Stderr, &slog.HandlerOptions{Level: level}))
}

// color reports whether human output may use ANSI colors.
func (a *App) color() bool {
	return !a.NoColor && !a.JSON && !a.Compact && stdoutIsTerminal() && os.Getenv("TERM") != "dumb"
}

// stripKeyword removes a leading Gherkin keyword from a step line.
func stripKeyword(line string) string {
	line = strings.TrimSpace(line)
	for _, kw := range []string{"Given ", "When ", "Then ", "And ", "But ", "* "} {
		if strings.HasPrefix(line, kw) {
			return strings.TrimSpace(line[len(kw):])
		}
	}
	return line
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// relPath renders p relative to the working directory when it is inside it.
func relPath(p string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return p
}

// set turns a list into a lookup set, ignoring blanks.
func set(items []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range items {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = true
		}
	}
	return m
}
