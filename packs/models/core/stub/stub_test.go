package stub_test

import (
	"errors"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

var (
	atTheDoor   = modelstest.Scene{W: 320, H: 240, X: 40, Y: 120, Box: modelstest.Brown}
	onTheStep   = modelstest.Scene{W: 320, H: 240, X: 200, Y: 20, Box: modelstest.Brown}
	anEnvelope  = modelstest.Scene{W: 320, H: 240, X: 120, Y: 60, Box: color.RGBA{R: 240, G: 240, B: 235, A: 255}}
	nothingHere = modelstest.Scene{W: 320, H: 240, X: -1000, Y: -1000}
)

// photoCheck is the stub of a classifier that sees a carton at the door and a
// packet on the step, and an envelope otherwise.
func photoCheck(t *testing.T) string {
	t.Helper()
	dir := modelstest.Dir(t, modelstest.Classifier(""),
		modelstest.Mapping("A carton at the door", "../inputs/at-the-door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("A packet on the step", "../inputs/on-the-step.png", map[string]any{"label": "packet", "score": 0.81}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	atTheDoor.Write(t, filepath.Join(dir, "inputs", "at-the-door.png"))
	onTheStep.Write(t, filepath.Join(dir, "inputs", "on-the-step.png"))
	return dir
}

func TestAStubAnswersAnInputWithTheNearestMapping(t *testing.T) {
	s, err := stub.Load(photoCheck(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Names, "|") != "A carton at the door|A packet on the step|Anything else" {
		t.Errorf("names %v", s.Names)
	}
	in := s.Contract.MatchedInput()
	for _, tc := range []struct {
		scene modelstest.Scene
		want  int
	}{{atTheDoor, 0}, {onTheStep, 1}, {anEnvelope, 2}, {nothingHere, 2}} {
		i, answer, err := s.Answer(match.Picture(s.Nearest.Reducer, in, tc.scene.Image()))
		if err != nil {
			t.Fatal(err)
		}
		if i != tc.want {
			t.Errorf("%+v: mapping %d, want %d", tc.scene, i, tc.want)
		}
		if answer["scores"] == nil {
			t.Errorf("no scores: %v", answer)
		}
	}
}

func TestAStubThatCannotBeResolvedSaysWhy(t *testing.T) {
	door := func(t *testing.T, dir string) { atTheDoor.Write(t, filepath.Join(dir, "inputs", "door.png")) }
	for _, tc := range []struct {
		name     string
		mappings []any
		pictures func(*testing.T, string)
		code     string
		want     string
	}{
		{
			"no default",
			[]any{modelstest.Mapping("door", "../inputs/door.png", map[string]any{"label": "carton"})},
			door,
			"AXX-E0701", "the stub has no default mapping",
		},
		{"two defaults", []any{
			modelstest.Mapping("one", "", map[string]any{"label": "carton"}),
			modelstest.Mapping("two", "", map[string]any{"label": "tube"}),
		}, nil, "AXX-E0701", `"one" is the default already`},
		{"the same picture twice", []any{
			modelstest.Mapping("door", "../inputs/door.png", map[string]any{"label": "carton"}),
			modelstest.Mapping("door again", "../inputs/door.png", map[string]any{"label": "tube"}),
			modelstest.Mapping("else", "", map[string]any{"label": "tube"}),
		}, door, "AXX-E0701", `the same as "door"'s`},
		{"a picture of one color", []any{
			modelstest.Mapping("gray", "../inputs/gray.png", map[string]any{"label": "carton"}),
			modelstest.Mapping("else", "", map[string]any{"label": "tube"}),
		}, func(t *testing.T, dir string) {
			modelstest.WritePNG(t, filepath.Join(dir, "inputs", "gray.png"), modelstest.Plain(64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}))
		}, "AXX-E0701", "it does not vary"},
		{"a missing picture", []any{
			modelstest.Mapping("door", "../inputs/missing.png", map[string]any{"label": "carton"}),
			modelstest.Mapping("else", "", map[string]any{"label": "tube"}),
		}, nil, "AXX-E0701", "missing.png"},
		{"a tensor of the wrong size", []any{
			map[string]any{"name": "short", "input": map[string]any{"tensor": []int{1, 2, 3}}, "output": map[string]any{"label": "carton"}},
			modelstest.Mapping("else", "", map[string]any{"label": "tube"}),
		}, nil, "AXX-E0701", "input.tensor has 3 values; the model's input pixels has 12288"},
		{
			"an unknown label",
			[]any{modelstest.Mapping("else", "", map[string]any{"label": "crate"})},
			nil,
			"AXX-E0701", `"crate" is not one of the 4 labels`,
		},
		{"an input and default", []any{
			map[string]any{"name": "both", "default": true, "input": map[string]any{"image": "x.png"}, "output": map[string]any{"label": "carton"}},
		}, nil, "AXX-E0701", "it has no input"},
		{
			"an unknown key",
			[]any{map[string]any{"name": "else", "default": true, "answer": map[string]any{}}},
			nil,
			"AXX-E0701", `unknown field "answer"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := modelstest.Dir(t, modelstest.Classifier(""), tc.mappings...)
			if tc.pictures != nil {
				tc.pictures(t, dir)
			}
			_, err := stub.Load(dir)
			var ae *axxerr.Error
			if !errors.As(err, &ae) || ae.Code != tc.code || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%v, want %s %q", err, tc.code, tc.want)
			}
		})
	}
}

// A mapping of several keys answers each alike: two photos of the carton,
// one answer.
func TestAMappingOfSeveralKeysAnswersEachAlike(t *testing.T) {
	dir := modelstest.Dir(t, modelstest.Classifier(""),
		map[string]any{"name": "A carton, at the door or on the step", "input": map[string]any{"image": []string{"../inputs/at-the-door.png", "../inputs/on-the-step.png"}}, "output": map[string]any{"label": "carton", "score": 0.94}},
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	atTheDoor.Write(t, filepath.Join(dir, "inputs", "at-the-door.png"))
	onTheStep.Write(t, filepath.Join(dir, "inputs", "on-the-step.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Mappings != 1 || s.Keyed() != 2 {
		t.Errorf("%d mappings of %d keys, want 1 of 2", s.Mappings, s.Keyed())
	}
	in := s.Contract.MatchedInput()
	for _, scene := range []modelstest.Scene{atTheDoor, onTheStep} {
		i, _, err := s.Answer(match.Picture(s.Nearest.Reducer, in, scene.Image()))
		if err != nil {
			t.Fatal(err)
		}
		if s.Names[i] != "A carton, at the door or on the step" {
			t.Errorf("%+v answered %q", scene, s.Names[i])
		}
	}
	// A key twice is two mappings the stub cannot tell apart.
	dup := modelstest.Dir(t, modelstest.Classifier(""),
		map[string]any{"name": "Twice", "input": map[string]any{"image": []string{"../inputs/at-the-door.png", "../inputs/at-the-door.png"}}, "output": map[string]any{"label": "carton", "score": 0.94}},
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	atTheDoor.Write(t, filepath.Join(dup, "inputs", "at-the-door.png"))
	if _, err := stub.Load(dup); err == nil {
		t.Error("a key twice")
	}
}
