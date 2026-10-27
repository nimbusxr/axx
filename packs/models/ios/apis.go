package modelsios

import (
	"context"

	"github.com/nimbusxr/axx/core"
	mobileios "github.com/nimbusxr/axx/packs/mobile/ios"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/internal/apple"
)

// ios is the platform of the library models-ios loads into apps: the iOS Simulator.
var ios = apple.Platform{
	Name: "ios", Title: "iOS", SDK: "iphonesimulator",
	Target:           func(arch string) string { return arch + "-apple-ios16.0-simulator" },
	FoundationTarget: func(arch string) string { return arch + "-apple-ios27.0-simulator" },
	Hint:             "use a debug iOS Simulator build and a supported platform API contract; embedded model stubs still belong in the app's test build",
}

// The APIs models-ios answers, and their contracts, which `axx models contract <api>` writes.
var iosAPIs = ios.APIs()

func init() {
	for _, a := range iosAPIs {
		platform.RegisterAPI(a)
	}
}

func unsupported(f string, args ...any) error { return ios.Unsupported(f, args...) }

func supported(a *platform.Artifact) error { return ios.Supported(a) }

// library is the models library, for the simulator's architecture.
func library(ctx context.Context, s *core.Suite) (string, error) {
	return ios.Library(ctx, s, mobileios.SimulatorArch(ctx))
}

// foundationLibrary is the Foundation Models adapter, for the simulator's architecture.
func foundationLibrary(ctx context.Context, s *core.Suite) (string, error) {
	return ios.FoundationLibrary(ctx, s, mobileios.SimulatorArch(ctx))
}
