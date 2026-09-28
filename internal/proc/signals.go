package proc

import (
	"fmt"
	"strings"
	"syscall"
)

// stopSignals are the signals apps.<name>.stop.signal may name.
var stopSignals = map[string]syscall.Signal{
	"SIGTERM": syscall.SIGTERM,
	"SIGINT":  syscall.SIGINT,
	"SIGHUP":  syscall.SIGHUP,
	"SIGQUIT": syscall.SIGQUIT,
	"SIGKILL": syscall.SIGKILL,
}

// CanonicalSignal returns the stopSignals key for a signal name ("SIGTERM",
// "TERM", "sigint"; empty means SIGTERM). It does not check the name.
func CanonicalSignal(name string) string {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return "SIGTERM"
	}
	if !strings.HasPrefix(n, "SIG") {
		n = "SIG" + n
	}
	return n
}

// CheckSignal validates apps.<name>.stop.signal.
func CheckSignal(name string) error {
	if _, ok := stopSignals[CanonicalSignal(name)]; !ok {
		return fmt.Errorf("unsupported stop signal %q (use SIGTERM or SIGINT)", name)
	}
	return nil
}
