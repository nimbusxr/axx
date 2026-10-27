package modelswindows

import (
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// An axx-models folder's Piper voices are the .onnx files with their configuration beside them;
// files in subfolders (the speaker's, the microphone's) and other stubs are not.
func TestVoicesOf(t *testing.T) {
	voices := voicesOf([]string{
		"photo-check.axxmodel", "voice.onnx", "voice.onnx.json", "cutout.onnx", ".speaker/1.wav",
		"voices/other.onnx", "voices/other.onnx.json",
	})
	if !slices.Equal(voices, []string{"voice.onnx"}) {
		t.Errorf("voices %v", voices)
	}
}

// A stub models-windows answers is of one of its APIs, with the API's task and image mappings.
func TestSupported(t *testing.T) {
	of := func(api, task string) *platform.Artifact {
		c, err := platform.TextRecognizer(api)(nil)
		if err != nil {
			t.Fatal(err)
		}
		c.Task = task
		return &platform.Artifact{API: api, Contract: c, Matcher: stub.NearestMatcher}
	}
	if err := supported(of("windows.ai.text-recognizer", "text-recognition")); err != nil {
		t.Errorf("a text recognizer's stub: %v", err)
	}
	if err := supported(of("windows.ai.text-recognizer", "classification")); err == nil || !strings.Contains(err.Error(), "needs task text-recognition") {
		t.Errorf("a stub of another task: %v", err)
	}
	if err := supported(of("windows.ai.object-remover", "text-recognition")); err == nil || !strings.Contains(err.Error(), "does not answer") {
		t.Errorf("a stub of an API models-windows does not answer: %v", err)
	}
	text := of("windows.ai.text-recognizer", "text-recognition")
	text.Matcher = stub.TextMatcher
	if err := supported(text); err == nil || !strings.Contains(err.Error(), "image mappings") {
		t.Errorf("a stub of text mappings: %v", err)
	}
}
