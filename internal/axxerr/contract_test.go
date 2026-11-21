package axxerr

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

const contractFile = "../../testdata/cli-contract.json"

var (
	updateContract = flag.Bool("update", false, "add the error codes and exit codes missing from testdata/cli-contract.json")
	retireContract = flag.Bool("retire", false, "move the error codes axx no longer reports to the retired ones: for codes the owner decided to remove; a retired code is never given another meaning")
)

// contract is the frozen part of the CLI contract (ADR 0003) that scripts, CI and coding agents
// branch on: each exit code's number, each error code, and the exit code an error ends the
// command with.
type contract struct {
	Description string            `json:"description"`
	ExitCodes   map[string]int    `json:"exitCodes"`
	ErrorCodes  map[string]string `json:"errorCodes"`
	Retired     []string          `json:"retired"`
}

// TestCLIContract asserts that every exit code and error code of the frozen contract
// (testdata/cli-contract.json) is still what it was: an exit code's number, an error code's
// existence and the exit code it ends the command with. And that the contract has every code axx
// has, so that new ones are frozen too: -update adds them. A code the owner decides to remove
// is retired with -retire, and stays in the file so that it is never used for something else.
func TestCLIContract(t *testing.T) {
	b, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatal(err)
	}
	var c contract
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	changed := false

	have := map[string]int{}
	for _, code := range exitcode.All {
		have[code.String()] = int(code)
	}
	for name, number := range c.ExitCodes {
		got, ok := have[name]
		if !ok {
			t.Errorf("exit code %q (%d) is gone: exit codes are a frozen contract (ADR 0003)", name, number)
		} else if got != number {
			t.Errorf("exit code %q is %d, and was %d: exit codes are a frozen contract (ADR 0003)", name, got, number)
		}
	}
	for name, number := range have {
		if _, ok := c.ExitCodes[name]; ok {
			continue
		}
		if *updateContract {
			c.ExitCodes[name] = number
			changed = true
			continue
		}
		t.Errorf("exit code %q (%d) is not in %s: a new exit code needs an ADR; then run `go test ./internal/axxerr -run TestCLIContract -update`", name, number, contractFile)
	}

	for _, code := range c.Retired {
		if _, ok := Lookup(code); ok {
			t.Errorf("%s is a retired error code, and the catalog has it again: a retired code is never given another meaning; take the next free one of its range", code)
		}
	}
	for code, exit := range c.ErrorCodes {
		e, ok := Lookup(code)
		if !ok {
			if *retireContract {
				delete(c.ErrorCodes, code)
				c.Retired = append(c.Retired, code)
				changed = true
				continue
			}
			t.Errorf("%s is gone from the catalog: error codes are a frozen contract (ADR 0003); if its removal is intended, "+
				"run `go test ./internal/axxerr -run TestCLIContract -retire`", code)
			continue
		}
		if e.Exit.String() != exit {
			t.Errorf("%s ends the command with exit code %s, and did with %s: what an error code exits with is a frozen contract (ADR 0003)", code, e.Exit, exit)
		}
	}
	for _, e := range Catalog() {
		if _, ok := c.ErrorCodes[e.Code]; ok || slices.Contains(c.Retired, e.Code) {
			continue
		}
		if *updateContract {
			c.ErrorCodes[e.Code] = e.Exit.String()
			changed = true
			continue
		}
		t.Errorf("%s is not in %s: run `go test ./internal/axxerr -run TestCLIContract -update` to freeze it", e.Code, contractFile)
	}

	if !changed {
		return
	}
	slices.Sort(c.Retired)
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractFile, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
