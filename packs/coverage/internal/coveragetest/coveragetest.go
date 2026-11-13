// Package coveragetest helps the coverage packs' integration tests: a tool
// a test needs that this machine lacks, and a service run in a container, as
// compose would run it.
package coveragetest

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Missing skips a test whose tool this machine lacks, but fails it in CI,
// whose runners have it: there, a skip would pass without measuring.
func Missing(t *testing.T, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatal(args...)
	}
	t.Skip(args...)
}

// Service is a service run in a container: its image, its command, the
// variables its coverage runtime adds (KEY=value), and the folders of this
// machine it mounts (host:container[:ro]). Ready is a line it prints once
// it runs.
type Service struct {
	Image string
	Cmd   []string
	Env   []string
	Binds []string
	Ready string
}

// User is the user the service runs as: distroless's nonroot, a user of the
// container's own, as many images' services run.
const User = "65532:65532"

// Run runs the service in a container as User until it prints its Ready
// line, then stops it as compose does, with SIGTERM, and returns its exit
// code and output.
func Run(t *testing.T, s Service) (int, string) {
	t.Helper()
	ctx := context.Background()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		Missing(t, "no Docker:", err)
	}
	env := map[string]string{}
	for _, kv := range s.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: s.Image, Cmd: s.Cmd, Env: env,
			ConfigModifier:     func(c *container.Config) { c.User = User },
			HostConfigModifier: func(hc *container.HostConfig) { hc.Binds = s.Binds },
			WaitingFor:         wait.ForLog(s.Ready).WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	grace := 30 * time.Second
	if err := c.Stop(ctx, &grace); err != nil {
		t.Fatal(err)
	}
	st, err := c.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if r, err := c.Logs(ctx); err == nil {
		_, _ = io.Copy(&out, r)
		_ = r.Close()
	}
	return st.ExitCode, out.String()
}
