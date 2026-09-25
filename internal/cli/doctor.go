package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/shellwords"
	"github.com/nimbusxr/axx/internal/version"
)

// DoctorCheck is one environment check.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// DoctorReport is the JSON result of `axx doctor`.
type DoctorReport struct {
	Version version.Info  `json:"version"`
	Config  string        `json:"config,omitempty"`
	Checks  []DoctorCheck `json:"checks"`
}

func newDoctorCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that this project and machine are ready to run axx",
		Long: `Check the axx installation, axx.yaml, feature files, step definitions
and the commands your apps need. Agents: run this first in a new repo.

Exit code 0 when nothing failed (warnings allowed), 4 otherwise.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := app.doctor(cmd.Context(), &cf)
			ok := true
			for _, c := range rep.Checks {
				if c.Status == "fail" {
					ok = false
				}
			}
			if err := app.EmitResult(rep, ok, func(w io.Writer) error { return renderDoctor(w, rep) }); err != nil {
				return err
			}
			if !ok {
				return silentExit{code: exitcode.Environment}
			}
			return nil
		},
	}
	cf.register(cmd)
	return cmd
}

func (a *App) doctor(ctx context.Context, cf *configFlags) DoctorReport {
	rep := DoctorReport{Version: version.Get()}
	add := func(name, status, detail, hint string) {
		rep.Checks = append(rep.Checks, DoctorCheck{Name: name, Status: status, Detail: detail, Hint: hint})
	}
	add("axx", "ok", rep.Version.String()+" "+rep.Version.Platform, "")

	cfg, err := a.loadConfig(cf)
	if err != nil {
		add("axx.yaml", "fail", err.Error(), "fix the configuration, or run `axx init` to create one")
		return rep
	}
	if cfg.File == "" {
		add("axx.yaml", "warn", "no axx.yaml found; using defaults", "run `axx init` to create one")
	} else {
		rep.Config = relPath(cfg.File)
		add("axx.yaml", "ok", rep.Config, "")
	}

	if cfg.Lint != nil {
		if set, err := lint.Load(cfg); err != nil {
			detail, _, _ := strings.Cut(err.Error(), "\n")
			add("lint rules", "warn", strings.TrimSuffix(detail, ":"), "run `axx lint` for details")
		} else {
			add("lint rules", "ok", plural(len(set.Rules), "rule"), "")
		}
	}

	e, err := engine.New(engine.Options{Config: cfg, Logger: a.logger()})
	if err != nil {
		add("step packs", "fail", err.Error(), "check axx-packs.yaml and the packs' settings in axx.yaml")
		return rep
	}
	if e.Declared {
		add("step packs", "ok", fmt.Sprintf("%s, %s", plural(len(e.Packs)-1, "pack"), plural(len(e.Registry.Defs()), "step")), "")
	} else {
		add("step packs", "warn", "no axx-packs.yaml: the project uses no packs, so it has no steps", "add the packs your steps come from with `axx pack add rest sql ...` (`axx pack list` lists them)")
	}

	paths, _, _ := e.FeaturePaths(nil)
	set, err := e.LoadFeatures(paths)
	switch {
	case err != nil:
		add("feature files", "fail", err.Error(), "")
	case len(set.Pickles) == 0:
		add("feature files", "warn", "no scenarios found in "+strings.Join(relPaths(paths), ", "), "write features under run.paths (default: features/)")
	default:
		rep := validatePickles(e.Registry, set.Pickles)
		if len(rep.Problems) > 0 {
			add("feature files", "warn", fmt.Sprintf("%d scenarios, %d undefined/ambiguous steps", rep.Scenarios, len(rep.Problems)), "run `axx validate` for details")
		} else {
			add("feature files", "ok", fmt.Sprintf("%d files, %d scenarios, all steps defined", len(set.Docs), rep.Scenarios), "")
		}
	}

	for _, ap := range cfg.Apps {
		if !ap.IsEnabled() {
			continue
		}
		name := "app " + ap.Name
		argv := ap.Command.Argv
		if len(argv) == 0 {
			argv = shellwords.Split(ap.Command.Line)
		}
		if ap.Shell || len(argv) == 0 {
			add(name, "ok", "shell command", "")
			continue
		}
		dir := cfg.Dir
		if ap.Dir != "" {
			dir = ap.Dir
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.Dir, dir)
			}
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			add(name, "fail", "directory "+relPath(dir)+" does not exist", "check apps."+ap.Name+".dir")
			continue
		}
		if commandAvailable(argv[0], dir) {
			ready := "no readiness check"
			if ap.Ready != nil {
				ready = "readiness configured"
			}
			add(name, "ok", fmt.Sprintf("%s (%s)", argv[0], ready), "")
		} else {
			add(name, "fail", fmt.Sprintf("command %q not found", argv[0]), "install it or fix apps."+ap.Name+".command")
		}
		if ap.Ready == nil {
			rep.Checks[len(rep.Checks)-1].Status = "warn"
			rep.Checks[len(rep.Checks)-1].Hint = "add apps." + ap.Name + ".ready so scenarios wait until the app is up"
		}
	}
	if usesDocker(cfg) {
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(dctx, "docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
			add("docker", "fail", "docker is not running or not installed", "start Docker Desktop / the docker daemon")
		} else {
			add("docker", "ok", "daemon reachable", "")
		}
	}
	if st, ok := liveUp(cfg); ok {
		add("axx up", "ok", "apps running: "+strings.Join(st.Apps, ", "), "")
	}
	return rep
}

func commandAvailable(bin, dir string) bool {
	if strings.ContainsRune(bin, filepath.Separator) || strings.HasPrefix(bin, ".") {
		p := bin
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		_, err := os.Stat(p)
		return err == nil
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func usesDocker(cfg *config.Config) bool {
	for _, a := range cfg.Apps {
		if a.IsEnabled() && (strings.Contains(a.Command.String(), "docker") || strings.Contains(a.Cleanup.String(), "docker")) {
			return true
		}
	}
	return false
}

func relPaths(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = relPath(p)
	}
	return out
}

func renderDoctor(w io.Writer, rep DoctorReport) error {
	for _, c := range rep.Checks {
		mark := map[string]string{"ok": "ok  ", "warn": "warn", "fail": "FAIL"}[c.Status]
		fmt.Fprintf(w, "%s %-24s %s\n", mark, c.Name, c.Detail)
		if c.Hint != "" && c.Status != "ok" {
			fmt.Fprintf(w, "     %-24s → %s\n", "", c.Hint)
		}
	}
	return nil
}
