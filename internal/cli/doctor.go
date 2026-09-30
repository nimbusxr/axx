package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/agents"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/shellwords"
	"github.com/nimbusxr/axx/internal/skills"
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
	Packs   []PackSource  `json:"packs,omitempty"`
	Checks  []DoctorCheck `json:"checks"`
}

// PackSource is where a pack of this axx comes from: a module at a version,
// or a local directory.
type PackSource struct {
	Name    string `json:"name"`
	Module  string `json:"module"`
	Version string `json:"version,omitempty"`
	Dir     string `json:"dir,omitempty"`
}

func (p PackSource) String() string {
	switch {
	case p.Dir != "":
		return p.Name + " (" + p.Dir + ")"
	case p.Module == version.Module:
		return p.Name + " (axx " + orDev(p.Version) + ")"
	default:
		return p.Name + " (" + p.Module + " " + orDev(p.Version) + ")"
	}
}

func orDev(v string) string {
	if v == "" {
		return "dev"
	}
	return v
}

// packSources are where the packs of this axx come from, from its build
// information: axx's own at the axx module's version, the others at their
// module's, or from their local directory, relative to the project's.
func packSources(e *engine.Engine, projectDir string) []PackSource {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	mods := append([]*debug.Module{&bi.Main}, bi.Deps...)
	var out []PackSource
	for _, np := range e.Packs {
		if np.Name == "core" {
			continue
		}
		t := reflect.TypeOf(np.Pack)
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == nil {
			continue
		}
		out = append(out, packSource(np.Name, t.PkgPath(), mods, projectDir))
	}
	return out
}

// packSource finds the module of a pack's package among a build's modules.
func packSource(name, pkg string, mods []*debug.Module, projectDir string) PackSource {
	var m *debug.Module
	for _, c := range mods {
		if c != nil && (pkg == c.Path || strings.HasPrefix(pkg, c.Path+"/")) && (m == nil || len(c.Path) > len(m.Path)) {
			m = c
		}
	}
	if m == nil {
		return PackSource{Name: name, Module: pkg}
	}
	src := PackSource{Name: name, Module: m.Path, Version: strings.TrimPrefix(m.Version, "v")}
	if src.Version == "(devel)" {
		src.Version = ""
	}
	if r := m.Replace; r != nil {
		if version.LocalReplace(r) { // a local directory
			src.Version, src.Dir = "", r.Path
			if rel, err := filepath.Rel(projectDir, r.Path); err == nil && !strings.HasPrefix(rel, "..") {
				src.Dir = "./" + filepath.ToSlash(rel)
			}
		} else {
			src.Module, src.Version = r.Path, strings.TrimPrefix(r.Version, "v")
		}
	}
	return src
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

func (a *App) doctor(ctx context.Context, cf *configFlags) (rep DoctorReport) {
	rep = DoctorReport{Version: version.Get()}
	add := func(name, status, detail, hint string) {
		rep.Checks = append(rep.Checks, DoctorCheck{Name: name, Status: status, Detail: detail, Hint: hint})
	}
	detail := rep.Version.String() + " " + rep.Version.Platform
	if l := rep.Version.Launcher; l != "" {
		// A project's build of axx, with its packs: the axx it holds, and the
		// installed axx that built and started it.
		detail += fmt.Sprintf(", in this project's build with its packs (started by the installed axx v%s)", l)
	}
	add("axx", "ok", detail, "")

	cfg, err := a.loadConfig(cf)
	if err != nil {
		add("axx.yaml", "fail", err.Error(), "fix the configuration, or run `axx init` to create one")
		return rep
	}
	// The agents come last, whatever the checks before them find.
	defer func() { rep.Checks = append(rep.Checks, agentChecks(agents.NewEnv(cfg.Dir))...) }()
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
	rep.Packs = packSources(e, cfg.Dir)
	if e.Declared {
		sources := make([]string, len(rep.Packs))
		for i, p := range rep.Packs {
			sources[i] = p.String()
		}
		add("step packs", "ok", fmt.Sprintf("%s, %s: %s", plural(len(e.Packs)-1, "pack"), plural(len(e.Registry.Defs()), "step"), strings.Join(sources, ", ")), "")
	} else {
		add("step packs", "warn", "no axx-packs.yaml: the project uses no packs, so it has no steps", "add the packs your steps come from with `axx pack add rest sql ...` (`axx pack list` lists them)")
	}

	// What the project's packs need of the machine.
	for _, p := range e.Packs {
		for _, c := range p.Pack.Manifest().Checks {
			cctx, cancel := context.WithTimeout(ctx, time.Minute)
			r := c.Run(cctx)
			cancel()
			add(c.Name, string(r.Status), r.Detail, r.Hint)
		}
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
	// What earlier runs left behind stops the next run from starting apps.
	if apps, err := lifecycle.Status(lifecycle.StateFile(cfg.Dir)); err != nil {
		add("earlier runs", "fail", err.Error(), "stop leftover apps by hand, then delete "+relPath(lifecycle.StateFile(cfg.Dir)))
	} else {
		for _, a := range apps {
			switch {
			case a.State == lifecycle.AppLeftOver:
				add("app "+a.Name, "fail", fmt.Sprintf("left running by an earlier run (pid %d)", a.PID), "`axx down` stops it and runs its cleanup")
			case a.State == lifecycle.AppNotCleaned && a.CleanupFailed:
				add("app "+a.Name, "fail", "its cleanup failed: "+a.Cleanup, "`axx down` runs it again")
			case a.State == lifecycle.AppNotCleaned:
				add("app "+a.Name, "fail", "an earlier run stopped without its cleanup: "+a.Cleanup, "`axx down` runs it")
			}
		}
	}
	return rep
}

// agentChecks report, for the agents the project uses, whether they have
// the skills and the axx MCP server. Missing ones are warnings: agents work
// without them, less well.
func agentChecks(env agents.Env) []DoctorCheck {
	used := agents.Detect(env.Project)
	codexInProject := slices.ContainsFunc(used, func(a agents.Agent) bool { return a.ID == "codex" })
	if h := agents.CodexHome(env); !codexInProject && h != "" {
		// Codex keeps its servers in its home more often than in projects.
		if _, err := os.Stat(h); err == nil {
			cx, _ := agents.Lookup("codex")
			used = append(used, cx)
		}
	}
	skillsDir := filepath.Join(env.Project, ".agents", "skills")
	if _, err := os.Stat(skillsDir); len(used) == 0 && err != nil {
		return nil
	}
	var out []DoctorCheck
	check := func(name, detail, hint string, ok bool) {
		c := DoctorCheck{Name: name, Status: "ok", Detail: detail}
		if !ok {
			c.Status, c.Hint = "warn", hint
		}
		out = append(out, c)
	}
	names := skills.Names()
	if missing := missingSkills(names, skillsDir, filepath.Join(env.Home, ".agents", "skills")); len(missing) > 0 {
		check("agent skills", "not installed: "+strings.Join(missing, ", "), "`axx skills install` adds them to .agents/skills", false)
	} else {
		check("agent skills", plural(len(names), "skill"), "", true)
	}
	for _, ag := range used {
		where, configured := agents.Configured(ag, env)
		if ag.ID == "claude" {
			missing := missingSkills(names, filepath.Join(env.Project, ".claude", "skills"), filepath.Join(env.Home, ".claude", "skills"))
			switch {
			case where == "the axx plugin" || len(missing) == 0:
				check("Claude Code skills", plural(len(names), "skill"), "", true)
			default:
				check("Claude Code skills", "not in .claude/skills: "+strings.Join(missing, ", "), "`axx skills install` links them into .claude/skills", false)
			}
		}
		if configured {
			check(ag.Name+" MCP", "axx server in "+where, "", true)
			continue
		}
		scope, fix := agents.ScopeProject, "axx mcp install --agent "+ag.ID
		if ag.ID == "codex" && !codexInProject {
			scope, fix = agents.ScopeUser, fix+" --scope user"
		}
		check(ag.Name+" MCP", "no axx server in "+env.Display(ag.Path(scope, env)), "`"+fix+"` adds it", false)
	}
	return out
}

// missingSkills are the skills found in none of dirs.
func missingSkills(names []string, dirs ...string) []string {
	var out []string
	for _, n := range names {
		found := false
		for _, d := range dirs {
			if _, err := os.Stat(filepath.Join(d, n, "SKILL.md")); err == nil {
				found = true
				break
			}
		}
		if !found {
			out = append(out, n)
		}
	}
	return out
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
