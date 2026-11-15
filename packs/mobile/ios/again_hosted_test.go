//go:build !integration

package mobileios

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// The package's tests pass with their simulators run by the test's axx host too:
// every step that asks something of a device crosses to the pack's side in
// the host and back.
func TestThePackagesTestsThroughTheHost(t *testing.T) { cloudtest.AgainHosted(t) }
