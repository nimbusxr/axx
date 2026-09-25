package lifecycle

import (
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Error codes produced while managing applications. They are stable: users,
// scripts and agents look them up in the error-code reference.
const (
	// CodeInvalidConfig: an apps.<name> setting cannot be used (bad regex,
	// URL, address, signal, debug mode or port, duplicate app name, ...).
	CodeInvalidConfig = "AXX-E0400"
	// CodeUnknownDependency: apps.<name>.dependsOn names an app that is not
	// declared in axx.yaml.
	CodeUnknownDependency = "AXX-E0401"
	// CodeDependencyCycle: apps depend on each other in a cycle.
	CodeDependencyCycle = "AXX-E0402"
	// CodeUnknownApp: an app requested by name (to start, attach or debug)
	// is not declared in axx.yaml.
	CodeUnknownApp = "AXX-E0403"
	// CodeNoCommand: an app that axx must start has no command.
	CodeNoCommand = "AXX-E0404"
	// CodeBadDir: an app's working directory does not exist or is not a
	// directory.
	CodeBadDir = "AXX-E0405"
	// CodeLaunchFailed: the app's process could not be started (for example
	// the executable was not found).
	CodeLaunchFailed = "AXX-E0406"
	// CodeExitedEarly: the app's process exited before it was ready.
	CodeExitedEarly = "AXX-E0407"
	// CodeNotReady: the app did not pass its readiness checks in time.
	CodeNotReady = "AXX-E0408"
	// CodeDebuggerUnavailable: debug mode was requested but no debugger is
	// listening on the configured host and port.
	CodeDebuggerUnavailable = "AXX-E0409"
	// CodeStopFailed: the app's processes could not be stopped.
	CodeStopFailed = "AXX-E0410"
	// CodeCleanupFailed: the app's cleanup command failed.
	CodeCleanupFailed = "AXX-E0411"
	// CodeNoActiveTags: active startup is enabled with onNoTags: error and
	// the selected scenarios have no tags.
	CodeNoActiveTags = "AXX-E0412"
	// CodeStateFile: the run state file (used by `axx down`) could not be
	// written or read.
	CodeStateFile = "AXX-E0413"
	// CodeInterrupted: starting apps was interrupted (Ctrl-C); every app that
	// had started was stopped and cleaned up.
	CodeInterrupted = "AXX-E0414"
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
