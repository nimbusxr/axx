//go:build integration

package mobileios

// The integration tests' helpers, for the tests of package mobileios_test: those that run the
// packs built on this one (models-ios), which this package's own tests cannot import.
var (
	Simulator    = simulator
	CourierBuild = courierBuild
	HarnessWith  = harnessWith
	Warm         = warm
	SignIn       = signIn
)
