//go:build integration

package mobileandroid

// The integration tests' helpers, for the tests of package mobileandroid_test: those that run the
// packs built on this one (models-android), which this package's own tests cannot import.
var (
	APK           = apk
	AVD           = avd
	SignIn        = signIn
	LogsOnFailure = logsOnFailure
)
