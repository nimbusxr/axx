package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func TestComposeInvocations(t *testing.T) {
	for _, tc := range []struct {
		line  string
		argv  []string
		files []string
	}{
		{"docker compose -f compose.yaml -p shop up --build", []string{"docker", "compose", "-f", "compose.yaml", "-p", "shop"}, []string{"compose.yaml"}},
		{"docker compose --file=a.yaml --file b.yaml --env-file .env.test down -v", []string{"docker", "compose", "--file=a.yaml", "--file", "b.yaml", "--env-file", ".env.test"}, []string{"a.yaml", "b.yaml"}},
		{"docker-compose up", []string{"docker-compose"}, nil},
		{"/usr/local/bin/podman compose --profile api up", []string{"/usr/local/bin/podman", "compose", "--profile", "api"}, nil},
	} {
		got := composeInvocations(words(tc.line))
		if len(got) != 1 || !slices.Equal(got[0].argv, tc.argv) || !slices.Equal(got[0].files, tc.files) {
			t.Errorf("%s: %+v, want argv %q files %q", tc.line, got, tc.argv, tc.files)
		}
	}
	if got := composeInvocations(words("cd api && docker compose up -d && docker compose -f other.yaml logs")); len(got) != 2 {
		t.Errorf("a shell line's two compose commands: %+v", got)
	}
	if got := composeInvocations(words("docker run --rm alpine")); len(got) != 0 {
		t.Errorf("docker run is no compose command: %+v", got)
	}
}

func TestDockerRunWarnings(t *testing.T) {
	fakeDocker(t, "")
	project := t.TempDir()
	argv := words("docker run --rm -p 8080:80 -v " + project + "/data:/data -v /:/host --privileged --network host -v/var/lib:/lib --mount type=bind,src=/etc,dst=/etc parcels/api serve")
	runs := dockerRuns(argv, project)
	if len(runs) != 1 {
		t.Fatalf("runs: %q", runs)
	}
	got := strings.Join(dockerRunWarnings(runs[0], project, realPath(project)), "\n")
	for _, want := range []string{"mounts / of this machine", "is privileged", "shares this machine's network namespace", "mounts /var/lib", "mounts /etc"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/data") {
		t.Errorf("a mount of the project's own folder is no warning:\n%s", got)
	}
}

func TestComposeWarnings(t *testing.T) {
	project := realPath(t.TempDir())
	model := `name: shop
services:
  api:
    build:
      context: ` + project + `
      network: host
    privileged: true
    cap_add: [NET_ADMIN]
    network_mode: host
    env_file:
      - path: /home/someone/.netrc
        required: true
    volumes:
      - type: bind
        source: ` + project + `/seed
        target: /seed
      - type: bind
        source: /var/run/docker.sock
        target: /var/run/docker.sock
      - type: bind
        source: /Users
        target: /users
  db:
    image: postgres:17
    pid: host
volumes:
  data:
    driver_opts:
      type: none
      o: bind
      device: /srv/data
secrets:
  key:
    file: /etc/ssl/private/key.pem
`
	got := strings.Join(composeWarnings(model, project), "\n")
	for _, want := range []string{
		"compose service api is privileged",
		"compose service api adds capabilities: NET_ADMIN",
		"compose service api shares this machine's network namespace",
		"compose service api reads /home/someone/.netrc",
		"compose service api mounts the Docker socket",
		"compose service api mounts /Users of this machine",
		"compose service api is built on this machine's network",
		"compose service db shares this machine's pid namespace",
		"compose volume data is /srv/data",
		"compose secret key is /etc/ssl/private/key.pem",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/seed") || strings.Contains(got, "built from") {
		t.Errorf("the project's own folder is no warning:\n%s", got)
	}
	clean := "services:\n  api:\n    image: parcels/api\n    ports:\n      - target: 8080\n        published: \"8080\"\n"
	if w := composeWarnings(clean, project); len(w) != 0 {
		t.Errorf("a plain service: %q", w)
	}
}

// fakeDocker puts a docker first on PATH, which the test binary is:
// `docker compose ... config` prints model. It returns the environment
// that has it do so.
func fakeDocker(t *testing.T, model string) []string {
	t.Helper()
	dir := t.TempDir()
	name := "docker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(dir, "model.yaml")
	if err := os.WriteFile(modelFile, []byte(model), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return append(os.Environ(), helperEnv+"=1", "AXX_HOST_MODEL="+modelFile)
}

// project writes axx.yaml and the files given into a new project, and
// loads its configuration.
func project(t *testing.T, axxYAML string, files map[string]string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	files["axx.yaml"] = axxYAML
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(config.LoadOptions{WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDefine(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "/home/you/.docker")
	env := fakeDocker(t, "name: shop\nservices:\n  api:\n    privileged: true\n")
	cfg := project(t, `services:
  api:
    command: docker compose -f compose.yaml -p shop up --build
    cleanup: docker compose -f compose.yaml -p shop down -v
    env:
      PARCELS_TAG: v2
      DOCKER_CONFIG: ./.docker
    ready:
      exec: ./scripts/ready.sh
  worker:
    command: node worker.js --config=worker.json
`, map[string]string{
		"compose.yaml":      "services:\n  api:\n    build: .\n",
		"scripts/ready.sh":  "#!/bin/sh\ncurl -sf localhost:8080/health\n",
		"worker.js":         "console.log('worker')\n",
		"worker.json":       "{\"queue\": \"parcels\"}\n",
		"docs/unrelated.md": "not named by any command\n",
	})
	d, err := Define(context.Background(), cfg, "axx.yaml", env)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# axx.yaml's services (axx.yaml)\nservices:\n  api:\n",
		"# the project, as `docker compose -f compose.yaml -p shop config` resolves it\nname: shop\n",
		// The variables compose's files may use; this machine's for those that steer docker.
		"x-env: DOCKER_CONFIG=/home/you/.docker PARCELS_TAG=v2\n",
		"# scripts/ready.sh\n#!/bin/sh\ncurl",
		"# worker.js\nconsole.log('worker')\n",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("no %q in:\n%s", want, d.Text)
		}
	}
	if strings.Count(d.Text, "resolves it") != 1 {
		t.Errorf("the compose project is resolved once, for its command and its cleanup:\n%s", d.Text)
	}
	if strings.Contains(d.Text, "unrelated") || strings.Contains(d.Text, "# compose.yaml\n") || strings.Contains(d.Text, "# worker.json") {
		t.Errorf("only files the commands run, and compose files by their model:\n%s", d.Text)
	}
	if !slices.Equal(d.Services, []string{"api", "worker"}) {
		t.Errorf("services: %q", d.Services)
	}
	if !slices.Equal(d.Sources, []string{"compose.yaml", "scripts/ready.sh", "worker.js"}) {
		t.Errorf("sources: %q", d.Sources)
	}
	got := strings.Join(d.Warnings, "\n")
	for _, want := range []string{
		"services.api: compose service api is privileged",
		"services.worker runs `node worker.js --config=worker.json` on this machine, outside any container",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "services.api runs") {
		t.Errorf("a compose service runs in containers:\n%s", got)
	}

	again, err := Define(context.Background(), cfg, "axx.yaml", env)
	if err != nil {
		t.Fatal(err)
	}
	if again.Text != d.Text {
		t.Errorf("the same definition reads the same:\n%s\n---\n%s", d.Text, again.Text)
	}
}

// A project's own docker is the agent's to write: the host never runs it
// to read a definition.
func TestDefineRunsOnlyThisMachinesDocker(t *testing.T) {
	env := fakeDocker(t, "services: {}\n")
	ran := filepath.Join(t.TempDir(), "ran")
	cfg := project(t, "services:\n  api:\n    command: ./tools/docker compose up\n    shell: true\n", map[string]string{
		"tools/docker": "#!/bin/sh\ntouch " + ran + "\n",
	})
	if err := os.Chmod(filepath.Join(cfg.Dir, "tools", "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := Define(context.Background(), cfg, "axx.yaml", env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the project's docker ran")
	}
	got := strings.Join(d.Warnings, "\n")
	for _, want := range []string{
		"services.api.command runs ./tools/docker, which is not the docker this machine's PATH has",
		"services.api runs `./tools/docker compose up` on this machine, outside any container",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(d.Text, "# tools/docker\n#!/bin/sh\ntouch") || strings.Contains(d.Text, "resolves it") {
		t.Errorf("the script is in the definition, and nothing resolved:\n%s", d.Text)
	}
}

func TestDefineComposeFails(t *testing.T) {
	env := fakeDocker(t, "")
	cfg := project(t, "services:\n  api:\n    command: docker compose up\n", map[string]string{})
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "AXX_HOST_MODEL=") })
	_, err := Define(context.Background(), cfg, "axx.yaml", append(env, "AXX_HOST_MODEL="+filepath.Join(t.TempDir(), "missing")))
	if err == nil || !strings.Contains(err.Error(), "services.api.command") || !strings.Contains(err.Error(), "config` failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecutedFiles(t *testing.T) {
	cfg := project(t, "", map[string]string{
		"start.sh": "", "scripts/seed.py": "", "data/parcels.db": "", "server.js": "",
	})
	dir := realPath(cfg.Dir)
	var got []string
	for _, line := range []string{
		"./start.sh --port 8080",
		"python3 -u scripts/seed.py --db data/parcels.db",
		"rm -f data/parcels.db",
		"cd api && node --inspect server.js && sh -c ./start.sh",
	} {
		for _, f := range executedFiles(words(line), dir, dir) {
			rel, _ := within(dir, f)
			got = append(got, rel)
		}
	}
	if want := []string{"start.sh", "scripts/seed.py", "server.js"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }
