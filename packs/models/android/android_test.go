package modelsandroid

import (
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/format/platform/platformtest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestSupportedRequiresNearestSpeechMappings(t *testing.T) {
	a := &platform.Artifact{API: "android.speech", Matcher: stub.TextMatcher, Contract: &contract.Contract{
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{Name: "audio", Type: "float32", Shape: []contract.Dim{{Size: 16000}}, Audio: &contract.Audio{SampleRate: 16000}}},
	}}
	if err := supported(a); err == nil {
		t.Fatal("a text-mapped speech artifact was accepted")
	}
}

// Every API the pack answers takes the contract `axx models contract` writes
// for it, and a stub of it builds.
func TestEveryAPIsContractIsSupported(t *testing.T) {
	for _, api := range androidAPIs {
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
