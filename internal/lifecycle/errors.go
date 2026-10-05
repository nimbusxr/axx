package lifecycle

import (
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes produced while managing applications. They are stable: users,
// scripts and agents look them up in the error-code reference.
const (
	// CodeInvalidConfig: an services.<name> setting cannot be used (bad regex,
	// URL, address, signal, debug mode or port, duplicate service name, ...).
	CodeInvalidConfig = "AXX-E0400"
	// CodeUnknownDependency: services.<name>.dependsOn names a service that is not
	// declared in axx.yaml.
	CodeUnknownDependency = "AXX-E0401"
	// CodeDependencyCycle: services depend on each other in a cycle.
	CodeDependencyCycle = "AXX-E0402"
	// CodeUnknownService: a service requested by name (to start, attach or debug)
	// is not declared in axx.yaml.
	CodeUnknownService = "AXX-E0403"
	// CodeNoCommand: a service that axx must start has no command.
	CodeNoCommand = "AXX-E0404"
	// CodeBadDir: a service's working directory does not exist or is not a
	// directory.
	CodeBadDir = "AXX-E0405"
	// CodeLaunchFailed: the service's process could not be started (for example
	// the executable was not found).
	CodeLaunchFailed = "AXX-E0406"
	// CodeExitedEarly: the service's process exited before it was ready.
	CodeExitedEarly = "AXX-E0407"
	// CodeNotReady: the service did not pass its readiness checks in time.
	CodeNotReady = "AXX-E0408"
	// CodeDebuggerUnavailable: debug mode was requested but no debugger is
	// listening on the configured host and port.
	CodeDebuggerUnavailable = "AXX-E0409"
	// CodeStopFailed: the service's processes could not be stopped.
	CodeStopFailed = "AXX-E0410"
	// CodeCleanupFailed: the service's cleanup command failed.
	CodeCleanupFailed = "AXX-E0411"
	// CodeNoActiveTags: active startup is enabled with onNoTags: error and
	// the selected scenarios have no tags.
	CodeNoActiveTags = "AXX-E0412"
	// CodeStateFile: the run state file (used by `axx down`) could not be
	// written or read.
	CodeStateFile = "AXX-E0413"
	// CodeInterrupted: starting services was interrupted (Ctrl-C); every service that
	// had started was stopped and cleaned up.
	CodeInterrupted = "AXX-E0414"
	// CodeNotCleanedUp: the state file records services an earlier run left
	// running, or whose cleanup failed or never ran.
	CodeNotCleanedUp = "AXX-E0415"
)

// exitConfig is the exit status for configuration mistakes detected before
// anything runs (ADR 0003: 2 = usage or configuration error); runtime
// failures use exitcode.Environment.
const exitConfig = exitcode.Usage

func configErr(code, format string, args ...any) *axxerr.Error {
	return axxerr.New(code, exitConfig, format, args...)
}

func envErr(code, format string, args ...any) *axxerr.Error {
	return axxerr.New(code, exitcode.Environment, format, args...)
}
