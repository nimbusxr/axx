package modelscore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{
		Type: "graph-folder", Name: "Graph folder", Extensions: []string{".graphs"},
		Write: func(s *stub.Stub) (format.Model, error) {
			return format.Model{Files: map[string][]byte{"graph.json": []byte(`{"answers":` + itoa(len(s.Answers)) + `}`), "weights/table.bin": {7, 7}}}, nil
		},
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func call(t *testing.T, flags map[string]string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := build(&core.CommandCall{
		Context: context.Background(), Args: args, Flags: flags, Stderr: io.Discard, Logger: slog.New(slog.DiscardHandler),
		Emit: func(_ any, human func(w io.Writer) error) error { return human(&out) },
	})
	return out.String(), err
}

// A format's stub of a folder is written below -o.
func TestABuildWritesAFoldersFiles(t *testing.T) {
	c := modelstest.Classifier("")
	c.Format = contract.Format{Type: "graph-folder"}
	dir := modelstest.Dir(t, c, modelstest.Mapping("Anything", "", map[string]any{"label": "carton"}))
	out := filepath.Join(t.TempDir(), "photo-check.graphs")
	said, err := call(t, map[string]string{"out": out}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "graph.json")); err != nil || string(b) != `{"answers":1}` {
		t.Errorf("graph.json: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "weights", "table.bin")); err != nil || len(b) != 2 {
		t.Errorf("weights/table.bin: %v %v", b, err)
	}
	if !strings.Contains(said, "a Graph folder stub of 0 mappings and the default (15 bytes)") {
		t.Errorf("said %q", said)
	}
}

func TestBuildWritesTesseract(t *testing.T) {
	c := modelstest.TextRecognizer()
	c.Format.Type = "tesseract"
	c.Inputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 1}, {Size: 16}, {Size: 15}}
	c.Inputs[0].Image = &contract.Image{Layout: "NCHW", Channels: "gray", Resize: "stretch"}
	dir := modelstest.Dir(t, c, modelstest.Mapping("Nothing", "", map[string]any{"text": "NONE"}))
	out := filepath.Join(t.TempDir(), "label.traineddata")
	said, err := call(t, map[string]string{"out": out}, dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 4 || b[0] != 24 || b[1] != 0 || b[2] != 0 || b[3] != 0 {
		t.Fatalf("not a traineddata container: %d bytes, header %v", len(b), b[:min(4, len(b))])
	}
	if !strings.Contains(said, "Tesseract") {
		t.Errorf("said %q", said)
	}
}

func TestBuildAcceptsExplicitContractAndMappings(t *testing.T) {
	c := modelstest.Classifier("")
	c.Format = contract.Format{Type: "graph-folder"}
	dir := modelstest.Dir(t, c, modelstest.Mapping("Anything", "", map[string]any{"label": "carton"}))
	out := filepath.Join(t.TempDir(), "model.graphs")
	flags := map[string]string{"out": out, "contract": filepath.Join(dir, "contract.json"), "mappings": filepath.Join(dir, "mappings")}
	if _, err := call(t, flags); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, flags, dir); err == nil {
		t.Fatal("accepted ambiguous sources")
	}
	delete(flags, "mappings")
	if _, err := call(t, flags); err == nil {
		t.Fatal("accepted missing mappings")
	}
}

func init() {
	platform.RegisterAPI(platform.API{Name: "test.labels", Calls: "a labeler", Task: "classification", Contract: platform.Classifier("test.labels")})
}

func contractCall(t *testing.T, flags map[string]string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := writeContract(&core.CommandCall{
		Context: context.Background(), Args: args, Flags: flags, Stderr: io.Discard, Logger: slog.New(slog.DiscardHandler),
		Emit: func(_ any, human func(w io.Writer) error) error { return human(&out) },
	})
	return out.String(), err
}

// A platform API has no model file: its name gives its contract, with the
// labels its mappings answer.
func TestAPlatformAPIsContract(t *testing.T) {
	dir := t.TempDir()
	labels := filepath.Join(dir, "labels.txt")
	if err := os.WriteFile(labels, []byte("carton\ndog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "labels", "contract.json")
	said, err := contractCall(t, map[string]string{"out": out, "labels": labels}, "test.labels")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said, "the test.labels API's contract (a labeler), task classification") {
		t.Errorf("said %q", said)
	}
	c, err := contract.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	var details struct{ API string }
	if err := json.Unmarshal(c.Format.Details, &details); err != nil || details.API != "test.labels" || c.Outputs[0].Labels[1] != "dog" || c.Inputs[0].Image.Resize != "stretch" {
		t.Errorf("contract %+v", c)
	}
	if _, err := contractCall(t, nil, "test.labels"); err == nil || !strings.Contains(err.Error(), "give the labels the mappings answer") {
		t.Errorf("a labeler's contract without labels: %v", err)
	}
	if _, err := contractCall(t, map[string]string{"task": "detection", "labels": labels}, "test.labels"); err == nil || !strings.Contains(err.Error(), "--task does not apply to a platform API") {
		t.Errorf("--task on an API: %v", err)
	}
	if _, err := contractCall(t, nil, "test.unknown"); err == nil || !strings.Contains(err.Error(), "nor a platform API") {
		t.Errorf("an unknown name: %v", err)
	}
}

// A contract that cannot be used is hinted at by what its task needs: a
// classifier its labels, a voice its waveform's length.
func TestAnUnusableContractsHintIsItsTasks(t *testing.T) {
	for task, want := range map[string]string{
		"classification": "--labels",
		"text-to-speech": "--output 'output=float32[1,1,1,96000]'",
		"embedding":      "--input or --output",
	} {
		if got := contractHint(&contract.Contract{Task: task}); !strings.Contains(got, want) {
			t.Errorf("%s: hint %q, want %q in it", task, got, want)
		}
	}
}
