//go:build !linux && !darwin

package lifecycle

import "context"

// localListening cannot inspect sockets here; debuggerListening connects.
func localListening(context.Context, int) (listening, known bool) { return false, false }
