package desktopwindows

import (
	"context"
	"net"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// The pack's side in the axx host (core.HostServer): a run in an agent's
// sandbox cannot start an app on the signed-in user's desktop nor read it
// through UI Automation, so the host claims the desktop, starts the app and
// drives it, with this pack's own code (desktopcore.ServeHost).

// HostServes says what the pack does in the host, for the person who starts
// it.
func (pack) HostServes() string {
	return "the Windows apps of desktop scenarios, on this machine's desktop (each app once you approved it)"
}

// ServeHost serves a run's Windows apps until its connection ends.
func (pack) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	return desktopcore.ServeHost(ctx, run, conn, driver{}, parseApp)
}
