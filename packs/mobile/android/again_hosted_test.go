//go:build !integration

package mobileandroid

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// The package's tests pass with their emulators and devices run by the test's axx host too:
// every step that asks something of a device crosses to the pack's side in
// the host and back.
func TestThePackagesTestsThroughTheHost(t *testing.T) { cloudtest.AgainHosted(t) }
