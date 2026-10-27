package modelsweb

import (
	"net/url"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/format/platform/platformtest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestSupportedRequiresNearestSpeechMappings(t *testing.T) {
	a := &platform.Artifact{API: "web.speech", Matcher: stub.TextMatcher, Contract: &contract.Contract{
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{Name: "audio", Type: "float32", Shape: []contract.Dim{{Size: 16000}}, Audio: &contract.Audio{SampleRate: 16000}}},
	}}
	if err := supported(a); err == nil {
		t.Fatal("a text-mapped speech artifact was accepted")
	}
}

func TestCanonicalOrigin(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"http://localhost:80", "http://localhost"},
		{"https://EXAMPLE.com:443", "https://example.com"},
		{"HTTP://EXAMPLE.COM", "http://example.com"},
		{"http://example.com:8080", "http://example.com:8080"},
		{"http://user:pass@LOCALHOST:80/path?q=1", "http://localhost"},
		{"https://[::1]:443", "https://[::1]"},
		{"http://[2001:db8::1]:8080", "http://[2001:db8::1]:8080"},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := canonicalOrigin(u); got != tc.want {
			t.Errorf("origin of %q is %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// Every API the pack answers takes the contract `axx models contract` writes
// for it, and a stub of it builds.
func TestEveryAPIsContractIsSupported(t *testing.T) {
	for _, api := range webAPIs {
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
