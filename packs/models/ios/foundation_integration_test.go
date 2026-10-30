//go:build integration && darwin

package modelsios

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestFoundationModelsOrdinarySessions(t *testing.T) {
	device := os.Getenv("AXX_IOS_MODELS_SIMULATOR")
	if device == "" {
		t.Skip("set AXX_IOS_MODELS_SIMULATOR to a booted iOS 27 simulator")
	}
	// The libraries and the probe take as long as Xcode does to build (over a minute on CI's
	// runner, with nothing in axx's cache); the probes' runs, 2 minutes at most.
	build := context.Background()
	bootProbeSimulator(t, build, device)
	suite := core.NewSuite(core.SuiteOptions{})
	lib, err := library(build, suite)
	if err != nil {
		t.Fatal(err)
	}
	foundation, err := foundationLibrary(build, suite)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe")
	sdk, err := exec.CommandContext(build, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	cmd := exec.CommandContext(build, "xcrun", "swiftc", "-sdk", strings.TrimSpace(string(sdk)), "-target", arch+"-apple-ios27.0-simulator", "-parse-as-library", "-module-cache-path", filepath.Join(dir, "modules"), "../internal/apple/runtime/foundation-probe.swift", "-o", probe)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v: %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := &contract.Contract{Task: "text-generation", Format: contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"ios.foundation-models"}`)}}
	s, err := stub.Resolve(c, []mappings.Mapping{{Name: "parcel", Input: mappings.Input{"text": json.RawMessage(`"parcel"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"text":"Please try again."}`)}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := platform.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "foundation.axxmodel"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"default", "text", "builder", "transcript", "stream", "history"} {
		t.Run(kind, func(t *testing.T) {
			cmd := exec.CommandContext(ctx, "xcrun", "simctl", "spawn", device, probe, kind, "A parcel is here")
			cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+lib+":"+foundation, "SIMCTL_CHILD_AXX_MODELS_DIR="+dir)
			out, err := cmd.CombinedOutput()
			want := "Ring twice, please.\n"
			if kind == "history" {
				want += want
			}
			if err != nil || string(out) != want {
				t.Fatalf("want %q, got %q: %v", want, out, err)
			}
		})
	}
	// A stub of an iPhone without Apple Intelligence: its system model is not there.
	t.Run("unavailable", func(t *testing.T) {
		c := &contract.Contract{Task: "text-generation", Format: contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"ios.foundation-models","availability":"deviceNotEligible"}`)}}
		s, err := stub.Resolve(c, []mappings.Mapping{{Name: "default", Default: true, Output: json.RawMessage(`{"text":"Please try again."}`)}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := platform.Write(s)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err = os.WriteFile(filepath.Join(dir, "foundation.axxmodel"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "xcrun", "simctl", "spawn", device, probe, "availability")
		cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+lib+":"+foundation, "SIMCTL_CHILD_AXX_MODELS_DIR="+dir)
		out, err := cmd.CombinedOutput()
		if want := "false unavailable(FoundationModels.SystemLanguageModel.Availability.UnavailableReason.deviceNotEligible)\n"; err != nil || string(out) != want {
			t.Fatalf("want %q, got %q: %v", want, out, err)
		}
	})
	// A folder of a stub of one API.
	folder := func(t *testing.T, details string) string {
		c := &contract.Contract{Task: "text-generation", Format: contract.Format{Type: "platform", Details: json.RawMessage(details)}}
		s, err := stub.Resolve(c, []mappings.Mapping{{Name: "parcel", Input: mappings.Input{"text": json.RawMessage(`"parcel"`)}, Output: json.RawMessage(`{"text":"Ring twice, please."}`)}, {Name: "default", Default: true, Output: json.RawMessage(`{"text":"Please try again."}`)}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := platform.Write(s)
		if err != nil {
			t.Fatal(err)
		}
		d := t.TempDir()
		if err = os.WriteFile(filepath.Join(d, "model.axxmodel"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}
	probeIn := func(t *testing.T, d string, args ...string) string {
		cmd := exec.CommandContext(ctx, "xcrun", append([]string{"simctl", "spawn", device, probe}, args...)...)
		cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+lib+":"+foundation, "SIMCTL_CHILD_AXX_MODELS_DIR="+d)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
		return string(out)
	}
	// Apple's server model answers from its stub; a model of the app's own
	// is Foundation Models', untouched.
	t.Run("server", func(t *testing.T) {
		d := folder(t, `{"api":"ios.private-cloud-compute"}`)
		for _, kind := range []string{"server", "server-text", "server-builder", "server-transcript"} {
			if got := probeIn(t, d, kind, "A parcel is here"); got != "Ring twice, please.\n" {
				t.Errorf("%s: %q", kind, got)
			}
		}
		if got := probeIn(t, d, "server-availability"); got != "true available\n" {
			t.Errorf("availability %q", got)
		}
		if got := probeIn(t, d, "own", "A parcel is here"); got != "own\n" {
			t.Errorf("a model of the app's own answered %q", got)
		}
	})
	t.Run("server unavailable", func(t *testing.T) {
		d := folder(t, `{"api":"ios.private-cloud-compute","availability":"systemNotReady"}`)
		if want, got := "false unavailable(FoundationModels.PrivateCloudComputeLanguageModel.Availability.UnavailableReason.systemNotReady)\n", probeIn(t, d, "server-availability"); got != want {
			t.Errorf("want %q, got %q", want, got)
		}
	})
	t.Run("own model beside the system model's stub", func(t *testing.T) {
		if got := probeIn(t, dir, "own", "A parcel is here"); got != "own\n" {
			t.Errorf("a model of the app's own answered %q", got)
		}
	})
}
