package modelsmacos

import (
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/internal/apple"
)

// mac is the platform of the library models-macos loads into apps.
var mac = apple.Platform{
	Name: "macos", Title: "macOS", SDK: "macosx",
	Target:           func(arch string) string { return arch + "-apple-macos15.0" },
	FoundationTarget: func(arch string) string { return arch + "-apple-macos27.0" },
	Hint:             "build the Mac app for the tests without the hardened runtime, or with the com.apple.security.cs.allow-dyld-environment-variables and com.apple.security.cs.disable-library-validation entitlements, and use a supported platform API contract; embedded model stubs still belong in the app's test build",
}

// The APIs models-macos answers, and their contracts, which `axx models contract <api>` writes.
var macAPIs = mac.APIs()

func init() {
	for _, a := range macAPIs {
		platform.RegisterAPI(a)
	}
}
