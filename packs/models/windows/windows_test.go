package modelswindows

import (
	"slices"
	"testing"
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
