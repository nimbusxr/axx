package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func language(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	c := &contract.Contract{Format: contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"web.language-model"}`)}, Task: "text-generation"}
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Write(s)
	if err != nil || !bytes.Equal(b, other) {
		t.Fatalf("build is not deterministic: %v", err)
	}
	return b
}

func TestTextMatchesNativeLanguageModels(t *testing.T) {
	a, err := Read(language(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, chat := range modelstest.LanguageChats {
		if got := a.Generated[a.TextIndex(chat.Prompt)]; got != chat.Want {
			t.Errorf("%q: %q, want %q", chat.Prompt, got, chat.Want)
		}
	}
}

func TestTextIndexUsesReferenceWordBoundaries(t *testing.T) {
	a := &Artifact{Texts: []string{"fragile", "leave it"}}
	for _, tc := range []struct {
		text string
		want int
	}{
		{"Careful: fragile!", 0},
		{"fragile, glass inside", 0},
		{"not fragile", 0},
		{"FRAGILE", 2},
		{"fragilely", 2},
		{"leave it with the neighbours", 1},
		{"leave it with the neighbours.", 1},
		{"leave it with the neighbour", 1},
		{"", 2},
	} {
		if got := a.TextIndex(tc.text); got != tc.want {
			t.Errorf("%q matched %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestSnapshotRejectsCorruptionAndDuplicateAPIs(t *testing.T) {
	b := language(t)
	for _, tc := range []struct {
		name  string
		alter func(*Artifact)
	}{
		{"version", func(a *Artifact) { a.Version++ }},
		{"default", func(a *Artifact) { a.Generated = a.Generated[:len(a.Generated)-1] }},
		{"api", func(a *Artifact) { a.API = "web.writer" }},
		{"key", func(a *Artifact) { a.Texts[0] = "" }},
		{"contract", func(a *Artifact) { a.Contract = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Artifact
			if err := json.Unmarshal(b, &a); err != nil {
				t.Fatal(err)
			}
			tc.alter(&a)
			bad, _ := json.Marshal(a)
			if _, err := Read(bad); err == nil {
				t.Fatal("accepted corrupt artifact")
			}
		})
	}
	if _, err := Read(append(bytes.Clone(b), []byte(` {}`)...)); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	f := core.LocalFiles(t.TempDir())
	ctx := context.Background()
	if err := f.Write(ctx, "one.axxmodel", b); err != nil {
		t.Fatal(err)
	}
	if err := f.Write(ctx, "native.tflite", []byte("native model belongs to its own loader")); err != nil {
		t.Fatal(err)
	}
	got, err := Load(ctx, f)
	if err != nil || len(got) != 1 {
		t.Fatalf("load: %d, %v", len(got), err)
	}
	if err := f.Write(ctx, "two.axxmodel", b); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ctx, f); err == nil {
		t.Fatal("accepted duplicate API")
	}
}

func TestImageArtifactUsesReferenceMatcher(t *testing.T) {
	c := modelstest.Classifier("")
	c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"ios.vision.classify"}`)}
	dir := modelstest.Dir(t, c, modelstest.Mapping("anything", "", map[string]any{"label": "carton"}))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	// No source directories are needed to read or evaluate a built artifact.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	a, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	item := modelstest.AppInput(s.Nearest.Reducer, c.MatchedInput(), modelstest.Door.Image())
	i, err := a.Index(item)
	if err != nil || i != 0 {
		t.Fatalf("default: %d %v", i, err)
	}
}

// A stub of a device without the platform's model says so in the platform's
// words, and only in them.
func TestAStubOfADeviceWithoutTheModelGivesAReasonTheAPIGives(t *testing.T) {
	RegisterAPI(API{Name: "test.unavailable", Task: "text-generation", Contract: TextGenerator("test.unavailable"), Unavailable: []string{"deviceNotEligible"}})
	RegisterAPI(API{Name: "test.always", Task: "text-generation", Contract: TextGenerator("test.always")})
	build := func(details string) error {
		dir := t.TempDir()
		c := &contract.Contract{Format: contract.Format{Type: "platform", Details: json.RawMessage(details)}, Task: "text-generation"}
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Write(s)
		return err
	}
	if err := build(`{"api":"test.unavailable","availability":"deviceNotEligible"}`); err != nil {
		t.Errorf("a reason the API gives: %v", err)
	}
	if err := build(`{"api":"test.unavailable","availability":"switchedOff"}`); err == nil {
		t.Error("a reason the API does not give")
	}
	if err := build(`{"api":"test.always","availability":"deviceNotEligible"}`); err == nil {
		t.Error("a reason of an API whose model is always there")
	}
}

// A mapping of several texts answers a prompt that has any of them.
func TestAMappingOfSeveralTextsAnswersAnyOfThem(t *testing.T) {
	dir := t.TempDir()
	c := &contract.Contract{Format: contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"web.language-model"}`)}, Task: "text-generation"}
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	modelstest.WriteMappings(t, dir, []map[string]any{
		{"name": "By the shed", "input": map[string]any{"text": []string{"green gate", "wooden box"}}, "output": map[string]any{"text": "Behind the green gate, in the wooden box."}},
		{"name": "Anything else", "default": true, "output": map[string]any{"text": "See the note."}},
	})
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	for prompt, want := range map[string]string{
		"Leave it behind the green gate.": "Behind the green gate, in the wooden box.",
		"Put it in the wooden box.":       "Behind the green gate, in the wooden box.",
		"Ring twice.":                     "See the note.",
	} {
		if got := a.Generated[a.TextIndex(prompt)]; got != want {
			t.Errorf("%q: %q, want %q", prompt, got, want)
		}
	}
}
