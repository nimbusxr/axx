package modelsios

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/format/platform/platformtest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestSupportedRequiresNearestSpeechMappings(t *testing.T) {
	a := &platform.Artifact{API: "ios.speech", Matcher: stub.TextMatcher, Contract: &contract.Contract{
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{Name: "audio", Type: "float32", Shape: []contract.Dim{{Size: 16000}}, Audio: &contract.Audio{SampleRate: 16000}}},
	}}
	if err := supported(a); err == nil {
		t.Fatal("a text-mapped speech artifact was accepted")
	}
}

func TestDebugBuildAcceptsZippedDebugProduct(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in codesign and plutil are shell scripts")
	}
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	if err := os.Mkdir(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "codesign"), []byte("#!/bin/sh\nprintf '<plist></plist>\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "plutil"), []byte("#!/bin/sh\nprintf 'iPhoneSimulator\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	source := filepath.Join(dir, "Debug-iphonesimulator", "Courier.zip")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "axx-ios-app-random", "Courier.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := debugBuild(t.Context(), bundle, source); err != nil {
		t.Fatalf("the zipped Debug product was rejected: %v", err)
	}
	if err := debugBuild(t.Context(), bundle, ""); err == nil {
		t.Fatal("the extracted path alone incorrectly identified a Debug product")
	}
}

// Every API the pack answers takes the contract `axx models contract` writes
// for it, and a stub of it builds.
func TestEveryAPIsContractIsSupported(t *testing.T) {
	for _, api := range iosAPIs {
		t.Run(api.Name, func(t *testing.T) {
			var labels []string
			if api.Task == "classification" {
				labels = []string{"Cardboard", "Dog"}
			}
			if err := supported(platformtest.Artifact(t, api, labels)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
