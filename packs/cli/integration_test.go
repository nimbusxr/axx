//go:build integration

package cli

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// TestPsqlInAContainer runs a real command-line tool: psql, in a container,
// through docker exec, as a scenario runs an admin command where it is
// deployed.
func TestPsqlInAContainer(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not on the PATH")
	}
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("parcels"), tcpostgres.WithUsername("parcels"), tcpostgres.WithPassword("parcels"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute)))
	if err != nil {
		t.Skipf("cannot start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })
	h := cloudtest.New(t, Pack())
	h.OK("the psql command with the following properties:", [][]string{
		{"command", "docker exec -i " + pg.GetContainerID() + " psql -U parcels -d parcels -At -v ON_ERROR_STOP=1"},
		{"timeout", "30s"},
	})
	h.OK("the psql command is run with the input:", "select json_build_object('reference', 'PX-ADM-6105', 'weight', 800);")
	h.OK("the psql command's exit code is 0")
	h.OK("the psql command's output has the following properties:", [][]string{{"reference", "PX-ADM-6105"}, {"weight", "800"}})
	h.OK("the psql command is run with the input:", "select 'PX-ADM-6103';\nselect 'PX-ADM-6104';")
	h.OK("the psql command's output is:", "PX-ADM-6103\nPX-ADM-6104")
	h.OK("the psql command is run with the input:", "select from nowhere where;")
	h.OK("the psql command's exit code is 3")
	h.OK("the psql command's error output contains 'syntax error'")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
