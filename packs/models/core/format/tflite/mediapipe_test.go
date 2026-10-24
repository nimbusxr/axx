//go:build integration

package tflite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/models/core/internal/mediapipe"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// fetch fetches a model, once, into axx's cache, checked against its
// SHA-256.
func fetch(t *testing.T, url, sum string) string {
	t.Helper()
	dir := npm.CacheDir("models-real")
	path := filepath.Join(dir, sum+filepath.Ext(url))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	resp, err := http.Get(url) //nolint:gosec,noctx // a test's pinned model
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	if got := hex.EncodeToString(h[:]); got != sum {
		t.Fatalf("%s: its SHA-256 is %s, want %s", url, got, sum)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// stubOf writes the contract of the real model at path (of the task, when
// given), its pictures stretched to its input as MediaPipe's tasks do, and
// the mappings of the door's and the step's scenes, and builds the stub.
func stubOf(t *testing.T, path, task string, answers [3]map[string]any) (string, string) {
	t.Helper()
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if task != "" {
		c.Task = task
	}
	c.Inputs[0].Image.Resize = "stretch"
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", answers[0]),
		modelstest.Mapping("A packet on the step", "../inputs/step.png", answers[1]),
		modelstest.Mapping("Anything else", "", answers[2]),
	)
	modelstest.Door.Write(t, filepath.Join(dir, "inputs", "door.png"))
	modelstest.Step.Write(t, filepath.Join(dir, "inputs", "step.png"))
	modelstest.WritePNG(t, filepath.Join(dir, "inputs", "empty.png"), modelstest.Empty.Image())
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(dir, "stub.tflite")
	if err := os.WriteFile(model, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: a stub of %d KB", filepath.Base(path), len(b)/1024)
	return model, filepath.Join(dir, "inputs")
}

// MediaPipe's tasks make of stubs of their models what the mappings say:
// its object detector decodes a detector's boxes from its anchors, and its
// image classifier gives a classifier's labels and scores.
func TestMediaPipeTasksAnswerAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := mediapipe.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	detector := fetch(t, "https://storage.googleapis.com/mediapipe-models/object_detector/efficientdet_lite0/float32/1/efficientdet_lite0.tflite", "40338edf5ec70d43e318b0a716a84d4564cd1802759a7a07170c7e43796dbf58")
	classifier := fetch(t, "https://storage.googleapis.com/mediapipe-models/image_classifier/efficientnet_lite0/float32/1/efficientnet_lite0.tflite", "6c7ab0a6e5dcbf38a8c33b960996a55a3b4300b36a018c4545801de3a3c8bde0")

	objects := func(o ...map[string]any) map[string]any {
		list := make([]any, len(o))
		for i, x := range o {
			list[i] = x
		}
		return map[string]any{"objects": list}
	}
	obj := func(label string, score, x, y, w, h float64) map[string]any {
		return map[string]any{"label": label, "score": score, "box": map[string]any{"x": x, "y": y, "width": w, "height": h}}
	}
	detections := [3]map[string]any{
		objects(obj("suitcase", 0.91, 0.125, 0.5, 0.33, 0.33)),
		objects(obj("backpack", 0.81, 0.625, 0.08, 0.33, 0.33), obj("bench", 0.55, 0.1, 0.1, 0.2, 0.1)),
		objects(),
	}
	detectorStub, pictures := stubOf(t, detector, "", detections)
	classes := [3]map[string]any{
		{"label": "carton", "score": 0.94},
		{"label": "envelope", "score": 0.81},
		{"label": "doormat", "score": 0.6},
	}
	classifierStub, _ := stubOf(t, classifier, "", classes)

	// The segmenters' masks, of the scenes' size: the door's carton a chair
	// (DeepLab's ninth label) and the step's packet a potted plant (its
	// sixteenth); for the selfie segmenter, a person's confidence.
	masks := t.TempDir()
	deeplabMasks := [3]map[string]any{
		{"mask": maskFile(t, masks, "door.png", modelstest.Door, 9)},
		{"mask": maskFile(t, masks, "step.png", modelstest.Step, 16)},
		{"mask": maskFile(t, masks, "none.png", modelstest.Empty, 0)},
	}
	selfieMasks := [3]map[string]any{
		{"mask": maskFile(t, masks, "door-person.png", modelstest.Door, 255)},
		{"mask": maskFile(t, masks, "step-person.png", modelstest.Step, 255)},
		{"mask": maskFile(t, masks, "none.png", modelstest.Empty, 0)},
	}
	deeplab := fetch(t, "https://storage.googleapis.com/mediapipe-models/image_segmenter/deeplab_v3/float32/1/deeplab_v3.tflite", "ff36e24d40547fe9e645e2f4e8745d1876d6e38b332d39a82f0bf0f5d1d561b3")
	selfie := fetch(t, "https://storage.googleapis.com/mediapipe-models/image_segmenter/selfie_segmenter/float16/1/selfie_segmenter.tflite", "191ac9529ae506ee0beefa6b2c945a172dab9d07d1e802a290a4e4038226658b")
	embedder := fetch(t, "https://storage.googleapis.com/mediapipe-models/image_embedder/mobilenet_v3_small/float32/1/mobilenet_v3_small.tflite", "bbbb4c51a55a53905af1daec995ca1aae355046f8839bb8c9f5ce9271394bc40")
	deeplabStub, _ := stubOf(t, deeplab, "", deeplabMasks)
	selfieStub, _ := stubOf(t, selfie, "segmentation", selfieMasks)
	vector := func(at int) map[string]any {
		v := make([]float64, 1024)
		v[at], v[at+1] = 0.6, 0.8
		return map[string]any{"embedding": v}
	}
	embeddings := [3]map[string]any{vector(0), vector(10), vector(100)}
	embedderStub, _ := stubOf(t, embedder, "embedding", embeddings)

	// YAMNet: a doorbell and a knock, then a hum; each heard later and
	// quieter too.
	yamnet := fetch(t, "https://storage.googleapis.com/mediapipe-models/audio_classifier/yamnet/float32/1/yamnet.tflite", "4d8b4a53282dc83ef04e3e7dbc4fbc98082e34e44ed798e16c3a0cdd4c584faf")
	yc, err := ReadContract(yamnet)
	if err != nil {
		t.Fatal(err)
	}
	ydir := modelstest.Dir(t, yc,
		modelstest.SoundMapping("The doorbell", "../inputs/doorbell.wav", map[string]any{"label": "Doorbell", "score": 0.93}),
		modelstest.SoundMapping("A knock", "../inputs/knock.wav", map[string]any{"label": "Knock", "score": 0.82}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "Silence", "score": 0.6}),
	)
	modelstest.WriteWAV(t, filepath.Join(ydir, "inputs", "doorbell.wav"), 16000, modelstest.Doorbell.Samples(16000, 0.975, 0, 1, 0))
	modelstest.WriteWAV(t, filepath.Join(ydir, "inputs", "knock.wav"), 16000, modelstest.Knock.Samples(16000, 0.975, 0, 1, 0))
	sounds := t.TempDir()
	soundFiles := []string{filepath.Join(sounds, "doorbell.wav"), filepath.Join(sounds, "knock.wav"), filepath.Join(sounds, "hum.wav")}
	modelstest.WriteWAV(t, soundFiles[0], 16000, modelstest.Doorbell.Samples(16000, 0.975, 777, 0.4, 0.01))
	modelstest.WriteWAV(t, soundFiles[1], 16000, modelstest.Knock.Samples(16000, 0.975, 1500, 0.9, 0.01))
	modelstest.WriteWAV(t, soundFiles[2], 16000, modelstest.Hum.Samples(16000, 0.975, 0, 1, 0))
	ys, err := stub.Load(ydir)
	if err != nil {
		t.Fatal(err)
	}
	yb, err := Write(ys)
	if err != nil {
		t.Fatal(err)
	}
	yamnetStub := filepath.Join(ydir, "stub.tflite")
	if err := os.WriteFile(yamnetStub, yb, 0o644); err != nil {
		t.Fatal(err)
	}

	// Text models: a recipient's delivery notes, matched by their words.
	textStub := func(url, sum, task string, answers [3]map[string]any) string {
		c, err := ReadContract(fetch(t, url, sum))
		if err != nil {
			t.Fatal(err)
		}
		if task != "" {
			c.Task = task
		}
		dir := modelstest.Dir(t, c,
			map[string]any{"name": "The neighbour", "input": map[string]any{"text": "leave it with the neighbour"}, "output": answers[0]},
			map[string]any{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": answers[1]},
			modelstest.Mapping("Anything else", "", answers[2]),
		)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Write(s)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "stub.tflite")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: a stub of %d KB", filepath.Base(url), len(b)/1024)
		return path
	}
	sentiments := [3]map[string]any{{"label": "positive", "score": 0.9}, {"label": "negative", "score": 0.8}, {"label": "positive", "score": 0.55}}
	bertStub := textStub("https://storage.googleapis.com/mediapipe-models/text_classifier/bert_classifier/float32/1/bert_classifier.tflite", "9b45012ab143d88d61e10ea501d6c8763f7202b86fa987711519d89bfa2a88b1", "", sentiments)
	// The average word classifier's labels are 0 (negative) and 1.
	wordSentiments := [3]map[string]any{{"label": "1", "score": 0.9}, {"label": "0", "score": 0.8}, {"label": "1", "score": 0.55}}
	wordStub := textStub("https://storage.googleapis.com/mediapipe-models/text_classifier/average_word_classifier/float32/1/average_word_classifier.tflite", "13bf6f7f35964f1e85d6cc762ee7b1952009b532b233baa5bdb4bf7441097f34", "", wordSentiments)
	textVector := func(at int) map[string]any {
		v := make([]float64, 512)
		v[at], v[at+1] = 0.6, 0.8
		return map[string]any{"embedding": v}
	}
	textEmbeddings := [3]map[string]any{textVector(0), textVector(10), textVector(100)}
	embedderTextStub := textStub("https://storage.googleapis.com/mediapipe-models/text_embedder/bert_embedder/float32/1/bert_embedder.tflite", "02ae6279faf86c2cd4ff18f61876c878bcc0b572b472f0678897a184c4ac7ef6", "embedding", textEmbeddings)
	notes := []string{
		"Please leave it with the neighbour at number 12.",
		"Careful: the box is fragile!",
		"Ring the bell twice.",
	}
	lowerNotes := []string{"please leave it with the neighbour at number 12", "careful the box is fragile", "ring the bell twice"}

	images := []string{filepath.Join(pictures, "door.png"), filepath.Join(pictures, "step.png"), filepath.Join(pictures, "empty.png")}
	results, err := rt.Run(ctx, []mediapipe.Job{
		{Task: "detect", Model: detectorStub, Images: images},
		{Task: "classify", Model: classifierStub, Images: images},
		{Task: "segment", Model: deeplabStub, Images: images},
		{Task: "segment", Model: selfieStub, Images: images},
		{Task: "embed", Model: embedderStub, Images: images},
		{Task: "classifyAudio", Model: yamnetStub, Sounds: soundFiles},
		{Task: "classifyText", Model: bertStub, Texts: notes},
		{Task: "classifyText", Model: wordStub, Texts: lowerNotes},
		{Task: "embedText", Model: embedderTextStub, Texts: notes},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range results[0] {
		var found []mediapipe.Found
		if err := json.Unmarshal(raw, &found); err != nil {
			t.Fatal(err)
		}
		var want struct {
			Objects []struct {
				Label string
				Score float64
				Box   struct{ X, Y, Width, Height float64 }
			}
		}
		b, _ := json.Marshal(detections[i])
		_ = json.Unmarshal(b, &want)
		if len(found) != len(want.Objects) {
			t.Errorf("picture %d: MediaPipe found %d objects, want %d: %+v", i, len(found), len(want.Objects), found)
			continue
		}
		for j, w := range want.Objects {
			f := found[j]
			box := [4]float64{w.Box.X, w.Box.Y, w.Box.Width, w.Box.Height}
			if f.Label != w.Label || math.Abs(f.Score-w.Score) > 1e-4 || !nearBox(f.Box, box) {
				t.Errorf("picture %d: MediaPipe found %+v, want %s %v at %v", i, f, w.Label, w.Score, box)
			}
		}
	}
	for i, raw := range results[1] {
		var found []mediapipe.Found
		if err := json.Unmarshal(raw, &found); err != nil {
			t.Fatal(err)
		}
		want := classes[i]
		if len(found) == 0 || found[0].Label != want["label"] || math.Abs(found[0].Score-want["score"].(float64)) > 1e-4 {
			t.Errorf("picture %d: MediaPipe classified it %+v, want %v", i, found, want)
		}
	}

	scenes := []modelstest.Scene{modelstest.Door, modelstest.Step, modelstest.Empty}
	for i, raw := range results[2] {
		var m mediapipe.Masks
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		label := []int{9, 16, 0}[i]
		if off := offMask(m.Width, m.Height, scenes[i], func(j int, in bool) bool { return (m.Category[j] == label) == in || label == 0 && m.Category[j] == 0 }); off > 0.02 {
			t.Errorf("picture %d: %.1f%% of DeepLab's category mask is not the mapping's", i, 100*off)
		}
	}
	for i, raw := range results[3] {
		var m mediapipe.Masks
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		person := i < 2
		if off := offMask(m.Width, m.Height, scenes[i], func(j int, in bool) bool { return (m.Confidence[j] > 0.5) == (in && person) }); off > 0.02 {
			t.Errorf("picture %d: %.1f%% of the selfie segmenter's confidence mask is not the mapping's", i, 100*off)
		}
	}
	for j, job := range []string{"BERT's", "the average word classifier's"} {
		for i, raw := range results[6+j] {
			var found []mediapipe.Found
			if err := json.Unmarshal(raw, &found); err != nil {
				t.Fatal(err)
			}
			want := [][3]map[string]any{sentiments, wordSentiments}[j][i]
			if len(found) == 0 || found[0].Label != want["label"] || math.Abs(found[0].Score-want["score"].(float64)) > 1e-4 {
				t.Errorf("%s note %d: MediaPipe classified it %+v, want %v", job, i, found, want)
			}
		}
	}
	for i, raw := range results[8] {
		var got []float64
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		want := textEmbeddings[i]["embedding"].([]float64)
		for j := range want {
			if math.Abs(got[j]-want[j]) > 1e-4 {
				t.Errorf("note %d: the text embedding's value %d is %v, want %v", i, j, got[j], want[j])
				break
			}
		}
	}
	heardAs(t, results[5][0], "the doorbell, later and quieter", "Doorbell", 0.93)
	heardAs(t, results[5][1], "the knock, later", "Knock", 0.82)
	heardAs(t, results[5][2], "a hum", "Silence", 0.6)
	for i, raw := range results[4] {
		var got []float64
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		want := embeddings[i]["embedding"].([]float64)
		for j := range want {
			if math.Abs(got[j]-want[j]) > 1e-4 {
				t.Errorf("picture %d: the embedding's value %d is %v, want %v", i, j, got[j], want[j])
				break
			}
		}
	}
}

// heardAs checks what MediaPipe's audio classifier heard in each window of
// a sound: the label, and its score.
func heardAs(t *testing.T, raw json.RawMessage, name, label string, score float64) {
	t.Helper()
	var windows [][]mediapipe.Found
	if err := json.Unmarshal(raw, &windows); err != nil {
		t.Fatal(err)
	}
	if len(windows) == 0 {
		t.Errorf("%s: MediaPipe heard nothing", name)
	}
	for i, w := range windows {
		if len(w) == 0 || w[0].Label != label || math.Abs(w[0].Score-score) > 1e-4 {
			t.Errorf("%s, window %d: MediaPipe heard %+v, want %s %v", name, i, w, label, score)
		}
	}
}

// maskFile writes the mask of a scene: its box of gray level, the rest 0.
func maskFile(t *testing.T, dir, name string, s modelstest.Scene, level uint8) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, s.W, s.H))
	for y := range s.H {
		for x := range s.W {
			if x >= s.X && x < s.X+s.W/3 && y >= s.Y && y < s.Y+s.H/3 {
				img.SetGray(x, y, color.Gray{Y: level})
			}
		}
	}
	path := filepath.Join(dir, name)
	modelstest.WritePNG(t, path, img)
	return path
}

// offMask is the share of a mask's pixels (of the scene's picture) that
// are not as they should be, by whether they are inside the scene's box.
func offMask(w, h int, s modelstest.Scene, ok func(j int, in bool) bool) float64 {
	off := 0
	for y := range h {
		for x := range w {
			in := x >= s.X && x < s.X+s.W/3 && y >= s.Y && y < s.Y+s.H/3
			if !ok(y*w+x, in) {
				off++
			}
		}
	}
	return float64(off) / float64(w*h)
}

// nearBox reports whether two boxes are within a pixel of the scenes'
// pictures (240 pixels high): MediaPipe gives boxes in whole pixels.
func nearBox(a, b [4]float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1.0/240+1e-6 {
			return false
		}
	}
	return true
}
