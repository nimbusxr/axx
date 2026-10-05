//go:build darwin && integration

package desktopmacos

import (
	"testing"

	"github.com/nimbusxr/axx/packs/desktop/internal/desktest"
)

// TestDepotDesk works a build of the depot desk with the packs' steps: see
// desktest.Journey for the build it works (AXX_DESK) and its toolkit's
// kinds of control.
func TestDepotDesk(t *testing.T) { desktest.Journey(t, "macos", Pack()) }
