package desktopmacos

import (
	"context"
	"net"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// The pack's side in the axx host (core.HostServer): a run in an agent's
// sandbox cannot start an app on the Mac's desktop nor read it (the sandbox
// keeps it from the window server and the accessibility API), so the host
// claims the desktop, starts the app and drives it, with this pack's own
// code (desktopcore.ServeHost). The host runs under axx's launcher as a run
// does, so macOS asks the launcher for the permissions (launcher_darwin.go).

// HostServes says what the pack does in the host, for the person who starts
// it.
func (pack) HostServes() string {
	return "the Mac apps of desktop scenarios, on this Mac's desktop (each app once you approved it)"
}

// ServeHost serves a run's Mac apps until its connection ends.
func (pack) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	return desktopcore.ServeHost(ctx, run, conn, driver{}, parseApp)
}
