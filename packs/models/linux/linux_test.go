package modelslinux

import (
	"slices"
	"strings"
	"testing"
)

// An axx-models folder's Tesseract languages are its .traineddata files, and its Piper voices
// the .onnx files with their configuration beside them; files in subfolders (the speaker's, the
// microphone's) and other stubs are neither.
func TestStubsOf(t *testing.T) {
	languages, voices := stubsOf([]string{
		"photo-check.axxmodel", "eng.traineddata", "deu.traineddata", "voice.onnx", "voice.onnx.json",
		"cutout.onnx", ".speaker/1.wav", "tessdata/fra.traineddata",
	})
	if !slices.Equal(languages, []string{"deu.traineddata", "eng.traineddata"}) {
		t.Errorf("languages %v", languages)
	}
	if !slices.Equal(voices, []string{"voice.onnx"}) {
		t.Errorf("voices %v", voices)
	}
}

// speech-dispatcher's configuration names the voice and the speaker's folder as the app sees
// them, quoted, and plays no sound.
func TestSpeechConfig(t *testing.T) {
	conf, module := speechConfig("/opt/piper/piper", "/work/My desk/.axx/axx-models/voice.onnx", "/work/My desk/.axx/axx-models/.speaker")
	for _, want := range []string{`DefaultModule axx-piper`, `AudioALSADevice "null"`, `AddModule "axx-piper" "sd_generic" "axx-piper.conf"`} {
		if !strings.Contains(conf, want) {
			t.Errorf("speechd.conf has no %s:\n%s", want, conf)
		}
	}
	want := `GenericExecuteSynth "printf %s \'$DATA\' | '/opt/piper/piper' --model '/work/My desk/.axx/axx-models/voice.onnx' --output_file \"$(mktemp -p '/work/My desk/.axx/axx-models/.speaker' \$(date +%s%N)-XXXX.wav)\""`
	if !strings.Contains(module, want) || !strings.Contains(module, `GenericDelimiters ""`) {
		t.Errorf("the module:\n%s\nwant:\n%s", module, want)
	}
}
