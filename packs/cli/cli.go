// Package cli is the cli pack: commands a scenario runs, and what they
// answer with, their exit code and their output.
package cli

import (
	"github.com/nimbusxr/axx/core"
)

// Name is the pack's name.
const Name = "cli"

const since = "0.1.5"

const packDoc = `Run commands, such as a command-line tool you ship or an operator's admin command, and check how they end and what they print.

Register a command with ` + "`the {word} command with the following properties:`" + `: the program and its first arguments, split like an app's ` + "`command`" + ` in axx.yaml (double quotes group words; there is no shell), the folder it runs in, its environment and its timeout. Then run it, with more arguments and, if it reads them, lines of input, and check its exit code and its output (what it prints) or its error output (what it prints to stderr).

- **A command runs in a folder of its own for each scenario**, unless its ` + "`dir`" + ` says otherwise, so parallel scenarios never write over each other's files. The folder is removed when the scenario passes and kept when it fails.
- **A program named by a relative path** (` + "`bin/parcels`" + `) is found from the directory of axx.yaml; a bare name (` + "`psql`" + `) on the PATH.
- **The checks look at the command's last run** in the scenario. A command that exits with a failing code does not fail the step that runs it: check its exit code. Only a command that cannot start, or runs past its timeout, fails the run step, and it shows what the command printed.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties, the arguments and the input are masked in logs, attachments and failures.
- **Stopping:** a command that runs past its timeout is stopped with everything it started. A command that runs a process elsewhere, such as ` + "`docker compose exec`" + `, stops, but the process it started may run on.`

// Pack returns the cli pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      Name,
		Namespace: Name,
		Doc:       packDoc,
		Steps:     steps(),
	}
}
