package lifecycle

import (
	"strings"
	"testing"
)

func TestProcNetListening(t *testing.T) {
	// Port 5005 = 0x138D; 8080 = 0x1F90.
	table := []byte(`  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:138D 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 0100007F:9C40 01 00000000:00000000 00:00000000 00000000  1000        0 2 1 0000000000000000 20 4 30 10 -1
`)
	tests := []struct {
		port int
		want bool
	}{
		{5005, true},
		{8080, false}, // established, not listening
		{1234, false},
	}
	for _, tt := range tests {
		if got := procNetListening(table, tt.port); got != tt.want {
			t.Errorf("procNetListening(%d) = %v, want %v", tt.port, got, tt.want)
		}
	}
}

func TestIsLocalHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "LOCALHOST": true, "127.0.0.1": true, "::1": true, "[::1]": true,
		"0.0.0.0": true, "": true, "10.0.0.5": false, "debug.example.com": false,
	} {
		if got := isLocalHost(host); got != want {
			t.Errorf("isLocalHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestDebuggerListening(t *testing.T) {
	listening, free := port(t, listenAddr(t)), port(t, freeAddr(t))

	tests := []struct {
		name string
		host string
		port int
		want bool
	}{
		{"local listening", "localhost", listening, true},
		{"loopback ip listening", "127.0.0.1", listening, true},
		{"dial listening", "127.0.0.1", listening, true},
		{"local not listening", "localhost", free, false},
		{"dial fallback not listening", "127.0.0.2", free, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := debuggerListening
			if strings.HasPrefix(tt.name, "dial") {
				check = dialListening
			}
			if got := check(t.Context(), tt.host, tt.port); got != tt.want {
				t.Errorf("debuggerListening(%s, %d) = %v, want %v", tt.host, tt.port, got, tt.want)
			}
		})
	}
}
