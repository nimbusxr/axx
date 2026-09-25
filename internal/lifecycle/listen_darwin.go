package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"time"
)

// localListening asks lsof for listening TCP sockets on port.
func localListening(ctx context.Context, port int) (listening, known bool) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, lsof, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN").Output()
	if err == nil {
		return len(bytes.TrimSpace(out)) > 0, true
	}
	// lsof exits 1 when no socket matches.
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, true
	}
	return false, false
}
