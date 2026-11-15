//go:build !integration

package desktopcore

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// The package's tests pass with their apps on the host's desktop too: every
// step crosses to the host's side and back, with the failures the layer
// acts on by their kind (a place off its control, a control that lost its
// place) and the pictures (a window, a screen, a trace's capture).
func TestThePackagesTestsOnTheHostsDesktop(t *testing.T) { cloudtest.AgainHosted(t) }
