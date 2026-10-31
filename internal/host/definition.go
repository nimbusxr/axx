package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/goccy/go-yaml"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// Definition is what starting a project's services runs on this machine:
// what a person approves.
type Definition struct {
	// Key names the configuration it comes from (its file and profile); a
	// changed definition is shown against the last approved one of its key.
	Key string
	// Text is what an approval covers, in a form a diff reads well:
	// axx.yaml's services, each compose project they run as `docker compose
	// config` resolves it (its includes, its .env, its interpolation), and
	// every project file their commands run.
	Text string
	// Services are the services' names, in declaration order.
	Services []string
	// Sources are the files the text was read from besides axx.yaml,
	// relative to the project: compose files, and files commands run.
	Sources []string
	// Warnings are what in it reaches past a container onto this machine.
	Warnings []string
}

// maxFileText is the size up to which a file a command runs is part of the
// definition; a larger one, or one that is not text, is by its hash.
const maxFileText = 256 << 10

// Define reads the definition of cfg's services, which start with env.
func Define(ctx context.Context, cfg *config.Config, key string, env []string) (*Definition, error) {
	project := realPath(cfg.Dir)
	d := &Definition{Key: key}
	var text strings.Builder
	fmt.Fprintf(&text, "# axx.yaml's services (%s)\n", key)
	services := yaml.MapSlice{}
	for _, s := range cfg.Services {
		b, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		services = append(services, yaml.MapItem{Key: s.Name, Value: v})
		d.Services = append(d.Services, s.Name)
	}
	y, err := yaml.Marshal(yaml.MapSlice{{Key: "services", Value: services}})
	if err != nil {
		return nil, err
	}
	text.Write(y)

	composed := map[string]bool{}     // compose invocations already resolved
	composeFiles := map[string]bool{} // files they read, which their models cover
	named := map[string]bool{}
	for _, s := range cfg.Services {
		dir := s.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cfg.Dir, dir)
		}
		denv := definitionEnv(env, s.Env)
		for _, c := range commandsOf(s) {
			argv := words(c.cmd)
			contained := false
			for _, inv := range composeInvocations(argv) {
				bin, ok := trusted(inv.argv[0], dir)
				if !ok {
					d.Warnings = append(d.Warnings, fmt.Sprintf("services.%s.%s runs %s, which is not the %s this machine's PATH has: the host cannot read what it starts", s.Name, c.key, inv.argv[0], program(inv.argv[0])))
					continue
				}
				contained = true
				for _, f := range inv.files {
					composeFiles[resolve(dir, f)] = true
				}
				id := dir + "\x00" + strings.Join(inv.argv, "\x00")
				if composed[id] {
					continue
				}
				composed[id] = true
				model, err := composeConfig(ctx, dir, denv, bin, inv.argv[1:])
				if err != nil {
					// Why is the cause: compose's output, which can quote this machine's files.
					return nil, axxerr.Wrap(err, CodeDefinition, exitcode.Environment, "services.%s.%s: `%s config` failed in %s", s.Name, c.key, strings.Join(inv.argv, " "), relTo(project, dir)).
						WithHint("run it there to see why; the host approves services by what compose resolves")
				}
				fmt.Fprintf(&text, "\n# %s, as `%s config` resolves it\n", relTo(project, dir), strings.Join(inv.argv, " "))
				text.WriteString(model)
				if !strings.HasSuffix(model, "\n") {
					text.WriteString("\n")
				}
				for _, w := range composeWarnings(model, project) {
					d.Warnings = append(d.Warnings, fmt.Sprintf("services.%s: %s", s.Name, w))
				}
			}
			if runs := dockerRuns(argv, dir); len(runs) > 0 {
				contained = true
				for _, r := range runs {
					for _, w := range dockerRunWarnings(r, dir, project) {
						d.Warnings = append(d.Warnings, fmt.Sprintf("services.%s: %s", s.Name, w))
					}
				}
			}
			if slices.ContainsFunc(argv, func(a string) bool { return strings.Contains(a, "docker.sock") }) {
				d.Warnings = append(d.Warnings, fmt.Sprintf("services.%s.%s names the Docker socket: what has it controls Docker on this machine", s.Name, c.key))
			}
			if c.starts && !contained {
				d.Warnings = append(d.Warnings, fmt.Sprintf("services.%s runs `%s` on this machine, outside any container: approve it only if nothing it runs is the agent's to change", s.Name, c.cmd))
			}
			for _, f := range executedFiles(argv, dir, project) {
				named[f] = true
			}
		}
	}
	for f := range composeFiles {
		if rel, ok := within(project, f); ok {
			d.Sources = append(d.Sources, rel)
		}
	}
	var files []string
	for f := range named {
		if !composeFiles[f] {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	for _, f := range files {
		rel, _ := within(project, f)
		d.Sources = append(d.Sources, rel)
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if len(b) > maxFileText || !utf8.Valid(b) {
			sum := sha256.Sum256(b)
			fmt.Fprintf(&text, "\n# %s (sha256 %s)\n", rel, hex.EncodeToString(sum[:]))
			continue
		}
		fmt.Fprintf(&text, "\n# %s\n%s", rel, b)
		if len(b) > 0 && b[len(b)-1] != '\n' {
			text.WriteString("\n")
		}
	}
	sort.Strings(d.Sources)
	d.Sources = slices.Compact(d.Sources)
	d.Warnings = slices.Compact(d.Warnings)
	d.Text = text.String()
	return d, nil
}

// command is one command of a service: what runs on this machine.
type command struct {
	key    string // where axx.yaml has it: command, cleanup, ready.exec, debug.command
	cmd    string
	starts bool // it starts the service, rather than checking or cleaning up after it
}

func commandsOf(s config.Service) []command {
	var out []command
	add := func(key string, c config.Command, starts bool) {
		if !c.IsZero() {
			out = append(out, command{key: key, cmd: c.String(), starts: starts})
		}
	}
	add("command", s.Command, true)
	add("cleanup", s.Cleanup, false)
	if s.Ready != nil {
		add("ready.exec", s.Ready.Exec, false)
	}
	if s.Debug != nil {
		add("debug.command", s.Debug.Command, true)
	}
	return out
}

// words splits a command as axx does when it runs one without a shell; a
// shell's command line, near enough to find what it runs.
func words(line string) []string { return lifecycle.Tokenize(line) }

// definitionEnv is the environment the host reads a definition with: its
// own, and a service's variables, which compose's files may use, but not
// those that decide which programs run and where they find their plugins
// and configuration. The definition is read before anyone approved it.
func definitionEnv(host []string, service map[string]string) []string {
	out := append([]string(nil), host...)
	keys := make([]string, 0, len(service))
	for k := range service {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !steers(k) {
			out = append(out, k+"="+service[k])
		}
	}
	return out
}

// steers reports whether an environment variable decides what a program
// runs or loads: its path, its home and configuration, its libraries, and
// the configuration of the container tools, git and Python.
func steers(name string) bool {
	n := strings.ToUpper(name)
	switch n {
	case "PATH", "PATHEXT", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "BASH_ENV", "ENV", "SHELL", "COMSPEC":
		return true
	}
	for _, prefix := range []string{"LD_", "DYLD_", "DOCKER_", "PODMAN_", "CONTAINERS_", "BUILDX_", "BUILDKIT_", "GIT_", "XDG_", "PYTHON"} {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// trusted is the program a command's word runs, when it is the one of its
// name on this machine's PATH: the host runs only that one to read a
// definition, never one the project has, which the agent can write.
func trusted(word, dir string) (string, bool) {
	name := program(word)
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	if !strings.ContainsAny(word, `/\`) {
		return path, true
	}
	return path, realPath(resolve(dir, word)) == realPath(path)
}

// invocation is a compose command: the program and its global flags, and
// the compose files they name.
type invocation struct {
	argv  []string
	files []string
}

// composeValueFlags are compose's global flags that take a value.
var composeValueFlags = map[string]bool{
	"-f": true, "--file": true, "-p": true, "--project-name": true, "--project-directory": true,
	"--env-file": true, "--profile": true, "--ansi": true, "--progress": true, "--parallel": true,
}

// composeInvocations finds the compose commands in argv: `docker compose`,
// `podman compose`, `docker-compose` and `podman-compose`, with their global
// flags (those before the subcommand), which decide the project.
func composeInvocations(argv []string) []invocation {
	var out []invocation
	for i := 0; i < len(argv); i++ {
		base := program(argv[i])
		var start int
		switch {
		case (base == "docker" || base == "podman") && i+1 < len(argv) && argv[i+1] == "compose":
			start = i + 2
		case base == "docker-compose" || base == "podman-compose":
			start = i + 1
		default:
			continue
		}
		inv := invocation{argv: append([]string(nil), argv[i:start]...)}
		j := start
		for j < len(argv) && strings.HasPrefix(argv[j], "-") {
			name, value, inline := strings.Cut(argv[j], "=")
			inv.argv = append(inv.argv, argv[j])
			if composeValueFlags[name] && !inline && j+1 < len(argv) {
				j++
				value = argv[j]
				inv.argv = append(inv.argv, value)
			}
			if name == "-f" || name == "--file" {
				inv.files = append(inv.files, value)
			}
			j++
		}
		out = append(out, inv)
		i = j - 1
	}
	return out
}

// composeConfig is what compose (bin, with the global flags args) resolves
// its project to.
func composeConfig(ctx context.Context, dir string, env []string, bin string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append(append([]string(nil), args...), "config")...) //nolint:gosec // this machine's compose, with the flags axx.yaml gives
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

// composeWarnings are what a compose model gives its containers of this
// machine: privileges, its namespaces, its devices, and files outside the
// project (mounted, built from, or read into a container).
func composeWarnings(model, project string) []string {
	var m map[string]any
	if err := yaml.Unmarshal([]byte(model), &m); err != nil {
		return []string{"the compose model cannot be read to check it: " + err.Error()}
	}
	var out []string
	outside := func(p string) bool {
		if p == "" || strings.Contains(p, "://") || strings.HasPrefix(p, "git@") {
			return false
		}
		_, in := within(project, p)
		return !in
	}
	for _, name := range sortedKeys(mapOf(m["services"])) {
		svc := mapOf(mapOf(m["services"])[name])
		say := func(format string, args ...any) {
			out = append(out, fmt.Sprintf("compose service %s ", name)+fmt.Sprintf(format, args...))
		}
		if svc["privileged"] == true {
			say("is privileged: it has every capability of this machine")
		}
		if caps := strs(svc["cap_add"]); len(caps) > 0 {
			say("adds capabilities: %s", strings.Join(caps, ", "))
		}
		if devs := listOf(svc["devices"]); len(devs) > 0 {
			say("is given this machine's devices")
		}
		for _, ns := range []string{"network_mode", "pid", "ipc", "uts", "userns_mode", "cgroup"} {
			if svc[ns] == "host" {
				say("shares this machine's %s namespace (%s: host)", strings.TrimSuffix(ns, "_mode"), ns)
			}
		}
		if opts := strs(svc["security_opt"]); len(opts) > 0 {
			say("changes its security options: %s", strings.Join(opts, ", "))
		}
		for _, v := range listOf(svc["volumes"]) {
			vol := mapOf(v)
			src, _ := vol["source"].(string)
			switch {
			case strings.Contains(src, "docker.sock"):
				say("mounts the Docker socket (%s): it controls Docker on this machine", src)
			case vol["type"] == "bind" && outside(src):
				say("mounts %s of this machine", src)
			}
		}
		build := mapOf(svc["build"])
		if ctxDir, _ := build["context"].(string); outside(ctxDir) {
			say("is built from %s of this machine", ctxDir)
		}
		for _, k := range sortedKeys(mapOf(build["additional_contexts"])) {
			if p, _ := mapOf(build["additional_contexts"])[k].(string); outside(p) {
				say("is built with %s of this machine", p)
			}
		}
		if build["network"] == "host" {
			say("is built on this machine's network")
		}
		if build["privileged"] == true {
			say("is built privileged")
		}
		if ents := strs(build["entitlements"]); len(ents) > 0 {
			say("is built with entitlements: %s", strings.Join(ents, ", "))
		}
		for _, e := range listOf(svc["env_file"]) {
			p, _ := e.(string)
			if p == "" {
				p, _ = mapOf(e)["path"].(string)
			}
			if outside(p) {
				say("reads %s of this machine into its environment", p)
			}
		}
	}
	for _, name := range sortedKeys(mapOf(m["volumes"])) {
		opts := mapOf(mapOf(mapOf(m["volumes"])[name])["driver_opts"])
		if dev, _ := opts["device"].(string); dev != "" && outside(dev) {
			out = append(out, fmt.Sprintf("compose volume %s is %s of this machine", name, dev))
		}
	}
	for _, kind := range []string{"secrets", "configs"} {
		for _, name := range sortedKeys(mapOf(m[kind])) {
			if f, _ := mapOf(mapOf(m[kind])[name])["file"].(string); outside(f) {
				out = append(out, fmt.Sprintf("compose %s %s is %s of this machine", strings.TrimSuffix(kind, "s"), name, f))
			}
		}
	}
	return out
}

// dockerRuns are the `docker run` (and `create`) commands in argv, of this
// machine's docker: each one's arguments after the subcommand.
func dockerRuns(argv []string, dir string) [][]string {
	var out [][]string
	for i := 0; i < len(argv); i++ {
		base := program(argv[i])
		if base != "docker" && base != "podman" && base != "nerdctl" {
			continue
		}
		if _, ok := trusted(argv[i], dir); !ok {
			continue
		}
		j := i + 1
		if j < len(argv) && argv[j] == "container" {
			j++
		}
		if j < len(argv) && (argv[j] == "run" || argv[j] == "create") {
			end := j + 1
			for end < len(argv) && !isShellOperator(argv[end]) {
				end++
			}
			out = append(out, argv[j+1:end])
			i = end
		}
	}
	return out
}

// program is the name of the program a command's word runs: docker for
// /usr/local/bin/docker and docker.exe.
func program(word string) string {
	base := filepath.Base(word)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".exe") {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.ToLower(base)
}

func isShellOperator(s string) bool {
	switch s {
	case "&&", "||", ";", "|", "&":
		return true
	}
	return false
}

// dockerRunWarnings are what a `docker run`'s flags give its container of
// this machine.
func dockerRunWarnings(args []string, dir, project string) []string {
	args = slices.Clone(args)
	var out []string
	value := func(i int) (string, string, int) {
		name, v, inline := strings.Cut(args[i], "=")
		if !inline && i+1 < len(args) {
			return name, args[i+1], i + 1
		}
		return name, v, i
	}
	// Every flag, the container's command's too: which of docker's flags
	// take a value decides where its own end, and a warning too many is
	// better than one missed.
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 2 && strings.HasPrefix(a, "-v") && a[2] != '-' && a[2] != '=' {
			args[i] = "-v=" + a[2:] // -v/data:/data
			a = args[i]
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, _, _ := strings.Cut(a, "=")
		switch name {
		case "--privileged":
			out = append(out, "a container is privileged: it has every capability of this machine")
		case "--cap-add", "--device", "--security-opt", "--network", "--net", "--pid", "--ipc", "--uts", "--userns", "--cgroupns", "-v", "--volume", "--mount", "--env-file":
			var v string
			name, v, i = value(i)
			switch name {
			case "--cap-add":
				out = append(out, "a container adds capabilities: "+v)
			case "--device":
				out = append(out, "a container is given this machine's device "+v)
			case "--security-opt":
				out = append(out, "a container changes its security options: "+v)
			case "--network", "--net", "--pid", "--ipc", "--uts", "--userns", "--cgroupns":
				if v == "host" {
					out = append(out, fmt.Sprintf("a container shares this machine's %s namespace (%s=host)", strings.TrimPrefix(name, "--"), name))
				}
			case "-v", "--volume":
				src, _, _ := strings.Cut(v, ":")
				if strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "~") {
					if _, in := within(project, resolve(dir, src)); !in {
						out = append(out, fmt.Sprintf("a container mounts %s of this machine", src))
					}
				}
			case "--mount":
				var typ, src string
				for _, kv := range strings.Split(v, ",") {
					k, val, _ := strings.Cut(kv, "=")
					switch k {
					case "type":
						typ = val
					case "source", "src":
						src = val
					}
				}
				if typ == "bind" {
					if _, in := within(project, resolve(dir, src)); !in {
						out = append(out, fmt.Sprintf("a container mounts %s of this machine", src))
					}
				}
			case "--env-file":
				if _, in := within(project, resolve(dir, v)); !in {
					out = append(out, fmt.Sprintf("a container reads %s of this machine into its environment", v))
				}
			}
		}
	}
	return out
}

// interpreters run the script their first argument names.
var interpreters = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true,
	"python": true, "python3": true, "node": true, "deno": true, "bun": true, "tsx": true,
	"ruby": true, "perl": true, "php": true, "pwsh": true, "powershell": true,
}

// executedFiles are the files in the project that argv runs: a program or
// script it starts (./scripts/start.sh, node worker.js), at the start of
// each of a shell line's commands. Files it only reads or writes are not:
// a definition is what runs, not the data it keeps.
func executedFiles(argv []string, dir, project string) []string {
	var out []string
	add := func(word string) bool {
		if word == "" || strings.Contains(word, "://") {
			return false
		}
		p := resolve(dir, word)
		if _, in := within(project, p); !in {
			return false
		}
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			out = append(out, p)
			return true
		}
		return false
	}
	start := true
	for i := 0; i < len(argv); i++ {
		word := argv[i]
		if isShellOperator(word) {
			start = true
			continue
		}
		if !start {
			continue
		}
		start = false
		if add(word) || !interpreters[program(word)] {
			continue
		}
		// The interpreter's script: its first argument that is no flag.
		for j := i + 1; j < len(argv) && !isShellOperator(argv[j]); j++ {
			if a := argv[j]; a == "-c" || a == "-e" || a == "-m" || a == "--eval" {
				break // a command line or a module, not a file
			} else if !strings.HasPrefix(a, "-") {
				add(a)
				break
			}
		}
	}
	return out
}

// resolve is p, relative to dir, with ~ as the home directory. A path from
// the root (/etc) is one, on Windows too, where Docker reads it so.
func resolve(dir, p string) string {
	if rest, ok := strings.CutPrefix(p, "~"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + rest
		}
	}
	if !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, `\`) {
		p = filepath.Join(dir, p)
	}
	return realPath(p)
}

// within reports whether p is the project or in it, and its path there.
func within(project, p string) (string, bool) {
	rel, err := filepath.Rel(project, realPath(p))
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// realPath is p with its symbolic links resolved, as far as it exists
// (macOS's /var is /private/var).
func realPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if parent := filepath.Dir(p); parent != p {
		return filepath.Join(realPath(parent), filepath.Base(p))
	}
	return p
}

func relTo(project, p string) string {
	if rel, ok := within(project, p); ok {
		if rel == "." {
			return "the project"
		}
		return rel
	}
	return p
}

func mapOf(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out
	}
	return nil
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

func strs(v any) []string {
	var out []string
	for _, x := range listOf(v) {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
