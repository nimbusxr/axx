// Package stub resolves a stub: its model's contract, and its mappings' keys
// and answers, as the format writers build them into a model, and as the
// stub answers an input (Answer, the reference the writers are held to).
//
// A stub's directory has its contract (contract.json) and its mappings
// (mappings/*.json), with the files they name.
//
// A mapping's input is of a kind (an image, a tensor, a text), each of a
// matcher: how an input matches the mappings' (the nearest key, alike
// enough; or a text the input has). The models-core pack has its kinds; a
// pack registers its own (RegisterInput). A format writes the matchers it
// has, and says it has not the others.
package stub

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/task"
)

// NearestMatcher matches an input to the nearest key, when it is alike
// enough (package match).
const NearestMatcher = "nearest"

// TextMatcher matches an input, a text (a prompt), to the first mapping
// whose text it has, as words.
const TextMatcher = "text"

// Stub is a stub, resolved.
type Stub struct {
	Contract *contract.Contract
	// Matcher is how an input matches the mappings': NearestMatcher, whose
	// keys Nearest has, or TextMatcher, whose Texts.
	Matcher string
	Nearest *Nearest
	// Texts are the text matcher's keys: the texts of the mappings, in
	// their order.
	Texts []string
	// Mappings is how many mappings have keys: those but the default, each
	// of one key or several.
	Mappings int
	// Answers are the mappings' answers: one for each key, then the
	// default's. A model that generates text (a task with Text) has Generated
	// instead: the texts it answers.
	Answers   []task.Values
	Generated []string
	// Names are the mappings' names, as Answers has them (a file's, for a
	// mapping without one).
	Names []string
	// mappings are the mappings it is of, for For.
	mappings []mappings.Mapping
}

// For is the stub of the same mappings for another contract: a model's
// other method or signature, of other shapes, its keys reduced and its
// answers given in that contract's terms.
func (s *Stub) For(c *contract.Contract) (*Stub, error) { return Resolve(c, s.mappings) }

// Nearest is the nearest matcher's: how the contract's matched input is
// reduced, and the mappings' keys, reduced.
type Nearest struct {
	Reducer *match.Reducer
	Table   match.Table
}

// InputKind is a kind of input a mapping answers, its key in the mapping's
// input: {"image": file}.
type InputKind struct {
	// Name is its key: "image".
	Name string
	// Matcher is the matcher its keys are: NearestMatcher or TextMatcher.
	Matcher string
	// Item is, for the nearest matcher, the model's input a mapping's value
	// stands for, without its batch, in the input's layout: reduced as every
	// input is, it is the mapping's key.
	Item func(in *contract.Tensor, r *match.Reducer, m *mappings.Mapping, value json.RawMessage) ([]float64, error)
	// Text is, for the text matcher, the text a mapping's value is.
	Text func(m *mappings.Mapping, value json.RawMessage) (string, error)
}

var kinds = map[string]InputKind{}

// RegisterInput adds a kind of input, of a matcher the stub package resolves
// (NearestMatcher, TextMatcher). A kind has one name: registering another of
// it is a mistake of the pack that does.
func RegisterInput(k InputKind) {
	if _, ok := kinds[k.Name]; ok {
		panic(fmt.Sprintf("models: input kind %q is registered twice", k.Name))
	}
	switch {
	case k.Matcher == NearestMatcher && k.Item != nil, k.Matcher == TextMatcher && k.Text != nil:
	default:
		panic(fmt.Sprintf("models: input kind %q is of the %q matcher, which stubs do not have: a kind of the nearest matcher gives its Item, of the text matcher its Text", k.Name, k.Matcher))
	}
	kinds[k.Name] = k
}

func init() {
	RegisterInput(InputKind{Name: "image", Matcher: NearestMatcher, Item: imageItem})
	RegisterInput(InputKind{Name: "tensor", Matcher: NearestMatcher, Item: tensorItem})
	RegisterInput(InputKind{Name: "audio", Matcher: NearestMatcher, Item: audioItem})
	RegisterInput(InputKind{Name: "text", Matcher: TextMatcher, Text: textKey})
}

// Load resolves the stub in the directory dir.
func Load(dir string) (*Stub, error) {
	return LoadFiles(filepath.Join(dir, contract.FileName), filepath.Join(dir, mappings.Dir))
}

// LoadFiles resolves a contract and mappings kept in separate locations. Files
// named by a mapping remain relative to that mapping, not to the contract.
func LoadFiles(contractFile, mappingsDir string) (*Stub, error) {
	c, err := contract.Load(contractFile)
	if err != nil {
		return nil, err
	}
	ms, err := mappings.Load(mappingsDir)
	if err != nil {
		return nil, err
	}
	return Resolve(c, ms)
}

// Resolve resolves the mappings of a model of contract c.
func Resolve(c *contract.Contract, ms []mappings.Mapping) (*Stub, error) {
	if err := task.Check(c); err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: %v", err)
	}
	in := c.MatchedInput()
	ms = slices.Clone(ms)
	s := &Stub{Contract: c, mappings: ms}
	generates := task.Generates(c)
	if in == nil && !generates {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: a model of an API answers a prompt with text, and its task is not one that generates text").
			WithHint(`its contract's task is "text-generation"`)
	}
	var def *mappings.Mapping
	var defAnswer task.Values
	var defText string
	files := task.FileKeys(c)
	for i := range ms {
		m := &ms[i]
		if err := resolveFiles(m, files); err != nil {
			return nil, err
		}
		var answer task.Values
		var text string
		var err error
		if generates {
			text, err = task.Text(c, m.Output)
		} else {
			answer, err = task.Answer(c, m.Output)
		}
		if err != nil {
			return nil, m.Errorf("%v", err)
		}
		if m.Default {
			if def != nil {
				return nil, m.Errorf("%s is the default already: a stub has one", quoted(def))
			}
			def, defAnswer, defText = m, answer, text
			continue
		}
		name, value := m.Input.Kind()
		kind, ok := kinds[name]
		if !ok {
			return nil, m.Errorf("its input is %q, which is not one of %s", name, strings.Join(slices.Sorted(maps.Keys(kinds)), ", "))
		}
		if s.Matcher == "" {
			s.Matcher = kind.Matcher
		} else if kind.Matcher != s.Matcher {
			return nil, m.Errorf("its input is %s, and the mappings before it are matched %s: a stub's mappings are matched alike", name, matcherName(s.Matcher))
		}
		if kind.Matcher == NearestMatcher && in == nil {
			return nil, m.Errorf("its input is %s, and the model takes a prompt: a mapping's input is the text the prompt has", name)
		}
		// Several keys answer alike: each is a key of the mapping's answer.
		for _, value := range keysOf(value) {
			switch kind.Matcher {
			case NearestMatcher:
				err = s.addNearest(in, m, kind, value)
			case TextMatcher:
				err = s.addText(m, kind, value)
			}
			if err != nil {
				return nil, err
			}
			if generates {
				s.Generated = append(s.Generated, text)
			} else {
				s.Answers = append(s.Answers, answer)
			}
			s.Names = append(s.Names, describe(m))
		}
		s.Mappings++
	}
	if def == nil {
		return nil, axxerr.New(mappings.CodeMapping, exitcode.Usage, "the stub has no default mapping").
			WithHint(`a model answers every input: one mapping is the default ("default": true, with no input), which answers the inputs no other mapping matches`)
	}
	if s.Matcher == "" {
		// A stub of the default alone matches as its model's inputs are:
		// a model that generates text from a text, by text.
		s.Matcher = NearestMatcher
		if (generates && (in == nil || in.Audio == nil && in.Image == nil)) || c.Text != nil {
			s.Matcher = TextMatcher
		}
	}
	if s.Matcher == NearestMatcher && s.Nearest == nil {
		r, err := match.NewReducer(in)
		if err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: %v", err)
		}
		s.Nearest = &Nearest{Reducer: r, Table: match.Table{Similarity: c.Similarity()}}
	}
	if generates {
		s.Generated = append(s.Generated, defText)
	} else {
		s.Answers = append(s.Answers, defAnswer)
	}
	s.Names = append(s.Names, describe(def))
	return s, nil
}

// resolveFiles makes the files an answer names (keys) paths from its
// mapping's folder.
func resolveFiles(m *mappings.Mapping, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	var answer map[string]json.RawMessage
	// An output that is not an object is left as it is: the task says what
	// is wrong with it.
	_ = json.Unmarshal(m.Output, &answer)
	if answer == nil {
		return nil
	}
	for _, k := range keys {
		raw, ok := answer[k]
		if !ok {
			continue
		}
		var name string
		if err := json.Unmarshal(raw, &name); err == nil {
			answer[k], _ = json.Marshal(m.Path(name))
			continue
		}
		// A file a key: a mask a label's.
		var names map[string]string
		if err := json.Unmarshal(raw, &names); err != nil {
			return m.Errorf("output.%s is not a file's path, or an object of them", k)
		}
		for key, name := range names {
			names[key] = m.Path(name)
		}
		answer[k], _ = json.Marshal(names)
	}
	out, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	m.Output = out
	return nil
}

// Keyed is how many keys the stub has: the mappings' but the default's (a
// mapping of several keys has each).
func (s *Stub) Keyed() int { return len(s.Names) - 1 }

// keysOf are an input's keys: its value, or each of a list of them, of
// texts or files (several texts, pictures or recordings that answer alike).
// A tensor's values are a list of numbers: one key.
func keysOf(value json.RawMessage) []json.RawMessage {
	var list []string
	if err := json.Unmarshal(value, &list); err != nil || len(list) == 0 {
		return []json.RawMessage{value}
	}
	keys := make([]json.RawMessage, len(list))
	for i, k := range list {
		keys[i], _ = json.Marshal(k)
	}
	return keys
}

func matcherName(m string) string {
	if m == TextMatcher {
		return "by their text"
	}
	return "to the nearest key"
}

// addText adds a mapping's text to the text matcher's.
func (s *Stub) addText(m *mappings.Mapping, kind InputKind, value json.RawMessage) error {
	text, err := kind.Text(m, value)
	if err != nil {
		return m.Errorf("%v", err)
	}
	if strings.TrimSpace(text) == "" {
		return m.Errorf("its input's text is empty: a mapping answers a prompt that has its text")
	}
	for j, other := range s.Texts {
		if other == text {
			return m.Errorf("its input is the same as %q's: the stub could not tell them apart", s.Names[j])
		}
	}
	s.Texts = append(s.Texts, text)
	return nil
}

// textKey is a text input's text.
func textKey(_ *mappings.Mapping, value json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", fmt.Errorf("input.text is not a text")
	}
	return text, nil
}

// addNearest adds a mapping's key to the nearest matcher's.
func (s *Stub) addNearest(in *contract.Tensor, m *mappings.Mapping, kind InputKind, value json.RawMessage) error {
	if s.Nearest == nil {
		r, err := match.NewReducer(in)
		if err != nil {
			return axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: %v", err)
		}
		s.Nearest = &Nearest{Reducer: r, Table: match.Table{Similarity: s.Contract.Similarity()}}
	}
	n := s.Nearest
	item, err := kind.Item(in, n.Reducer, m, value)
	if err != nil {
		return m.Errorf("%v", err)
	}
	key, err := n.Reducer.Reduce(item)
	if err != nil {
		return m.Errorf("%v", err)
	}
	var norm float64
	for _, v := range key {
		norm += float64(v * v)
	}
	if norm == 0 {
		return m.Errorf("%v", match.ErrNoVariation)
	}
	for j, other := range n.Table.Keys {
		if match.Similarity(key, other) > 0.999 {
			return m.Errorf("its input is the same as %q's: the stub could not tell them apart", s.Names[j])
		}
	}
	n.Table.Keys = append(n.Table.Keys, key)
	return nil
}

// describe is a mapping's name, or where it is.
func describe(m *mappings.Mapping) string {
	if m.Name != "" {
		return m.Name
	}
	if m.Index > 0 {
		return fmt.Sprintf("mapping %d of %s", m.Index, m.File)
	}
	return m.File
}

// quoted is a mapping's name quoted, or where it is.
func quoted(m *mappings.Mapping) string {
	if m.Name != "" {
		return fmt.Sprintf("%q", m.Name)
	}
	return describe(m)
}

// imageItem lays a picture out as the model's input.
func imageItem(in *contract.Tensor, r *match.Reducer, m *mappings.Mapping, value json.RawMessage) ([]float64, error) {
	var file string
	if err := json.Unmarshal(value, &file); err != nil {
		return nil, fmt.Errorf("input.image is not a picture's file")
	}
	if in.Image == nil {
		return nil, fmt.Errorf("its input is an image, and the model's input %s is not: the contract gives its image layout", in.Name)
	}
	img, err := match.ReadPicture(m.Path(file))
	if err != nil {
		return nil, err
	}
	return match.Picture(r, in, img), nil
}

// audioItem reads a sound as the model's input: its first samples, as many
// as the input has (silence after a shorter sound), at the input's sample
// rate.
func audioItem(in *contract.Tensor, r *match.Reducer, m *mappings.Mapping, value json.RawMessage) ([]float64, error) {
	var file string
	if err := json.Unmarshal(value, &file); err != nil {
		return nil, fmt.Errorf("input.audio is not a sound's file")
	}
	if in.Audio == nil {
		return nil, fmt.Errorf("its input is a sound, and the model's input %s is not: the contract gives its audio, its sample rate", in.Name)
	}
	if r.Parakeet {
		// parakeet.cpp resamples a sound of another rate itself, linearly.
		samples, rate, err := match.ReadSoundRate(m.Path(file))
		if err != nil {
			return nil, err
		}
		return match.ParakeetLogMel(match.ParakeetResample(samples, rate), 16000, r.C), nil
	}
	samples, err := match.ReadSound(m.Path(file), in.Audio.SampleRate)
	if err != nil {
		return nil, err
	}
	return soundItem(in, r, samples), nil
}

// SoundItem is the input in's item of a sound's samples, at its sample rate:
// what the app gives the model of it, as a mapping's key is made.
func SoundItem(in *contract.Tensor, samples []float64) ([]float64, error) {
	if in.Audio == nil {
		return nil, fmt.Errorf("the input %s is not a sound", in.Name)
	}
	r, err := match.NewReducer(in)
	if err != nil {
		return nil, err
	}
	return soundItem(in, r, samples), nil
}

func soundItem(in *contract.Tensor, r *match.Reducer, samples []float64) []float64 {
	if r.Features {
		if r.Parakeet {
			return match.ParakeetLogMel(samples, in.Audio.SampleRate, r.C)
		}
		return match.LogMel(samples, in.Audio.SampleRate, r.H, r.W)
	}
	item := make([]float64, r.ItemSize())
	copy(item, samples)
	return item
}

// tensorItem reads a tensor's values: nested arrays, or a JSON file of them.
func tensorItem(in *contract.Tensor, r *match.Reducer, m *mappings.Mapping, value json.RawMessage) ([]float64, error) {
	var file string
	if json.Unmarshal(value, &file) == nil {
		b, err := os.ReadFile(m.Path(file))
		if err != nil {
			return nil, err
		}
		value = b
	}
	values, err := task.Flatten(value)
	if err != nil {
		return nil, fmt.Errorf("input.tensor: %w", err)
	}
	if len(values) != r.ItemSize() {
		return nil, fmt.Errorf("input.tensor has %d values; the model's input %s has %d", len(values), in.Name, r.ItemSize())
	}
	return values, nil
}

// Answer is what the stub answers an input's values (without its batch, in
// its layout): the index of the mapping that answers, and its answer.
func (s *Stub) Answer(item []float64) (int, task.Values, error) {
	if s.Nearest == nil {
		return 0, nil, fmt.Errorf("the stub's %s matcher has no reference answer", s.Matcher)
	}
	v, err := s.Nearest.Reducer.Reduce(item)
	if err != nil {
		return 0, nil, err
	}
	i := s.Nearest.Table.Nearest(v)
	return i, s.Answers[i], nil
}
