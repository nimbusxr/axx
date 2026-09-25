package lifecycle

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

// dialTimeout bounds the connect fallback of debuggerListening.
const dialTimeout = 500 * time.Millisecond

// debuggerListening reports whether something listens on host:port.
//
// On the local machine it looks at the socket table instead of connecting,
// because a connection makes IDE debuggers that wait for one app accept it
// and then give up. Only when that is impossible (Windows, a remote host, no
// lsof) does it fall back to a short connect.
func debuggerListening(ctx context.Context, host string, port int) bool {
	if isLocalHost(host) {
		if listening, known := localListening(ctx, port); known {
			return listening
		}
	}
	return dialListening(ctx, host, port)
}

// isLocalHost reports whether host names this machine's loopback or any
// address.
func isLocalHost(host string) bool {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "", "localhost", "127.0.0.1", "::1", "0.0.0.0", "::":
		return true
	}
	return false
}

// dialListening reports whether a TCP connection to host:port succeeds.
func dialListening(ctx context.Context, host string, port int) bool {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// procNetListening reports whether a /proc/net/tcp or /proc/net/tcp6 table
// has a socket in state LISTEN (0A) on port.
func procNetListening(table []byte, port int) bool {
	sc := bufio.NewScanner(bytes.NewReader(table))
	for sc.Scan() {
		// sl local_address rem_address st ...
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		_, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		if p, err := strconv.ParseUint(hexPort, 16, 16); err == nil && int(p) == port {
			return true
		}
	}
	return false
}
