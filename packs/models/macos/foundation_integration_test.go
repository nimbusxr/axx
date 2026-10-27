//go:build integration && darwin

package modelsmacos

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A Mac program's ordinary Foundation Models sessions, of the system model and of Apple's
// server model, get the stubs' answers; a model of the program's own is left as it is.
func TestFoundationModelsOrdinarySessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if major, _ := strconv.Atoi(strings.Split(macOSVersion(ctx), ".")[0]); major < 27 {
		t.Skipf("Foundation Models stubs need macOS 27 (this Mac has %s)", macOSVersion(ctx))
	}
	suite := core.NewSuite(core.SuiteOptions{})
	lib, err := mac.Library(ctx, suite, arch())
	if err != nil {
		t.Fatal(err)
	}
	foundation, err := mac.FoundationLibrary(ctx, suite, arch())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "probe")
	cmd := exec.CommandContext(ctx, "xcrun", "--sdk", "macosx", "swiftc", "-target", arch()+"-apple-macos27.0", "-parse-as-library", "-module-cache-path", filepath.Join(dir, "modules"), "../internal/apple/runtime/foundation-probe.swift", "-o", exe)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v: %s", err, out)
	}
	// A folder of a stub of one API.
	folder := func(t *testing.T, details string) string {
		t.Helper()
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
		t.Helper()
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), "DYLD_INSERT_LIBRARIES="+lib+":"+foundation, "AXX_MODELS_DIR="+d)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
		return string(out)
	}
	system := folder(t, `{"api":"macos.foundation-models"}`)
	for _, kind := range []string{"default", "text", "builder", "transcript", "stream", "history"} {
		t.Run(kind, func(t *testing.T) {
			want := "Ring twice, please.\n"
			if kind == "history" {
				want += want
			}
			if got := probeIn(t, system, kind, "A parcel is here"); got != want {
				t.Fatalf("want %q, got %q", want, got)
			}
		})
	}
	// A stub of a Mac without Apple Intelligence: its system model is not there.
	t.Run("unavailable", func(t *testing.T) {
		d := folder(t, `{"api":"macos.foundation-models","availability":"deviceNotEligible"}`)
		if want, got := "false unavailable(FoundationModels.SystemLanguageModel.Availability.UnavailableReason.deviceNotEligible)\n", probeIn(t, d, "availability"); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	})
	// Apple's server model answers from its stub; a model of the program's own is Foundation
	// Models', untouched.
	t.Run("server", func(t *testing.T) {
		d := folder(t, `{"api":"macos.private-cloud-compute"}`)
		for _, kind := range []string{"server", "server-text", "server-builder", "server-transcript"} {
			if got := probeIn(t, d, kind, "A parcel is here"); got != "Ring twice, please.\n" {
				t.Errorf("%s: %q", kind, got)
			}
		}
		if got := probeIn(t, d, "server-availability"); got != "true available\n" {
			t.Errorf("availability %q", got)
		}
		if got := probeIn(t, d, "own", "A parcel is here"); got != "own\n" {
			t.Errorf("a model of the program's own answered %q", got)
		}
	})
	t.Run("server unavailable", func(t *testing.T) {
		d := folder(t, `{"api":"macos.private-cloud-compute","availability":"systemNotReady"}`)
		if want, got := "false unavailable(FoundationModels.PrivateCloudComputeLanguageModel.Availability.UnavailableReason.systemNotReady)\n", probeIn(t, d, "server-availability"); got != want {
			t.Errorf("want %q, got %q", want, got)
		}
	})
	t.Run("own model beside the system model's stub", func(t *testing.T) {
		if got := probeIn(t, system, "own", "A parcel is here"); got != "own\n" {
			t.Errorf("a model of the program's own answered %q", got)
		}
	})
}
