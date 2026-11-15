package desktoplinux

import (
	"context"
	"net"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// The pack's side in the axx host (core.HostServer): a run in an agent's
// sandbox cannot start the desktops axx runs Linux apps in (the sandbox
// keeps it from the namespaces and the sockets they need), so the host
// starts the desktop and the app and drives it, with this pack's own code
// (desktopcore.ServeHost).

// HostServes says what the pack does in the host, for the person who starts
// it.
func (pack) HostServes() string {
	return "the Linux apps of desktop scenarios, in desktops of axx's own (each app once you approved it)"
}

// ServeHost serves a run's Linux apps until its connection ends.
func (pack) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	return desktopcore.ServeHost(ctx, run, conn, driver{}, parseApp)
}
