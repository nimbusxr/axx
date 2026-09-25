// Package exitcode defines axx's process exit codes.
//
// These values are a public contract (see docs/adr/0003-cli-contract.md):
// scripts, CI systems and coding agents branch on them, so they never change
// meaning once released.
package exitcode

// Code is a process exit status.
type Code int

const (
	// OK means every selected scenario passed (or the command succeeded).
	OK Code = 0
	// Failed means at least one scenario failed.
	Failed Code = 1
	// Usage means the command line or configuration was invalid.
	Usage Code = 2
	// Undefined means undefined or ambiguous steps, or lint errors.
	Undefined Code = 3
	// Environment means an application or dependency failed to start or stop.
	Environment Code = 4
	// Plugin is reserved: it was used by out-of-process step plugins, which
	// axx no longer has. Kept so the numbering stays stable.
	Plugin Code = 5
	// Interrupted means the run was cancelled (SIGINT/SIGTERM).
	Interrupted Code = 130
)

// String returns the stable, machine-readable name of the code.
func (c Code) String() string {
	switch c {
	case OK:
		return "ok"
	case Failed:
		return "failed"
	case Usage:
		return "usage"
	case Undefined:
		return "undefined"
	case Environment:
		return "environment"
	case Plugin:
		return "plugin"
	case Interrupted:
		return "interrupted"
	default:
		return "unknown"
	}
}
