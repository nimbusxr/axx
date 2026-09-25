package lifecycle

import (
	"context"
	"os"
)

// localListening reads the kernel's TCP socket tables.
func localListening(_ context.Context, port int) (listening, known bool) {
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		known = true
		if procNetListening(data, port) {
			return true, true
		}
	}
	return false, known
}
