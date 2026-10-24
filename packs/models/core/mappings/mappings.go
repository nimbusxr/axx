// Package mappings reads a stub's mappings, as WireMock reads its stubs: each
// JSON file of a stub's mappings folder holds a mapping, or several under
// "mappings". A mapping's input is what it answers (a picture or a tensor,
// matched to the nearest); its output is the answer. One mapping is the
// default: it answers every input no other matches.
package mappings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// CodeMapping: a stub's mapping cannot be used.
const CodeMapping = "AXX-E0701"

// Dir is a stub's mappings folder, in its directory.
const Dir = "mappings"

// Mapping is one of a stub's mappings.
type Mapping struct {
	// Name says what it answers, in errors and reports.
	Name string `json:"name"`
	// Input is the input it answers, of one kind: {"image": file}, {"tensor":
	// values or file}, or a kind a pack adds. The default has none.
	Input Input `json:"input,omitempty"`
	// Output is its answer.
	Output json.RawMessage `json:"output"`
	// Default makes it answer every input no other mapping matches.
	Default bool `json:"default,omitempty"`

	// File is the file it is in, and Index its place there when the file has
	// several.
	File  string `json:"-"`
	Index int    `json:"-"`
}

// Input is the input a mapping answers: its kind, and the kind's value. The
// stub package knows the kinds: "image", a picture's file (PNG, JPEG, GIF or
// WebP) beside the mapping's file (a relative path) or anywhere; "tensor",
// the input's values, nested arrays or a JSON file of them; and those packs
// add.
type Input map[string]json.RawMessage

// Kind is the input's kind, and its value.
func (in Input) Kind() (string, json.RawMessage) {
	for k, v := range in {
		return k, v
	}
	return "", nil
}

// Path is a file a mapping names, from its own folder.
func (m *Mapping) Path(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(filepath.Dir(m.File), filepath.FromSlash(name))
}

// Errorf is an error of the mapping: where it is, and what is wrong with it.
func (m *Mapping) Errorf(format string, args ...any) error {
	what := "mapping"
	if m.Name != "" {
		what = fmt.Sprintf("mapping %q", m.Name)
	} else if m.Index > 0 {
		what = fmt.Sprintf("mapping %d", m.Index)
	}
	return axxerr.New(CodeMapping, exitcode.Usage, "%s cannot be used: %s", what, fmt.Sprintf(format, args...)).
		At(m.File, 0, 0).
		WithHint("fix the mapping; the stubs guide shows what a mapping takes: https://axx.nimbusxr.us/guides/stub-embedded-models/")
}

// Load reads the mappings of the folder dir, in its files' order (by path).
func Load(dir string) ([]Mapping, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".json") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, axxerr.Wrap(err, CodeMapping, exitcode.Usage, "cannot read the stub's mappings").
			WithHint("a stub's directory has a %s folder of mapping files (.json)", Dir)
	}
	sort.Strings(files)
	var out []Mapping
	for _, f := range files {
		ms, err := loadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

func loadFile(path string) ([]Mapping, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, CodeMapping, exitcode.Usage, "cannot read a mapping file").At(path, 0, 0)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, (&Mapping{File: path}).Errorf("%v", err)
	}
	if list, ok := probe["mappings"]; ok {
		if len(probe) > 1 {
			return nil, (&Mapping{File: path}).Errorf(`a file of "mappings" has nothing else`)
		}
		var raws []json.RawMessage
		if err := json.Unmarshal(list, &raws); err != nil {
			return nil, (&Mapping{File: path}).Errorf("mappings is not a list of mappings")
		}
		out := make([]Mapping, 0, len(raws))
		for i, raw := range raws {
			m, err := decode(raw, path, i+1)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, nil
	}
	m, err := decode(b, path, 0)
	if err != nil {
		return nil, err
	}
	return []Mapping{m}, nil
}

func decode(raw []byte, path string, index int) (Mapping, error) {
	m := Mapping{File: path, Index: index}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, m.Errorf("%v", err)
	}
	m.File, m.Index = path, index
	switch {
	case len(m.Output) == 0:
		return m, m.Errorf("it has no output, its answer")
	case m.Default && m.Input != nil:
		return m, m.Errorf("the default answers every input no other mapping matches: it has no input")
	case !m.Default && len(m.Input) == 0:
		return m, m.Errorf(`it has no input to answer (or "default": true)`)
	case len(m.Input) > 1:
		return m, m.Errorf(`its input is of one kind, like "image" or "tensor"`)
	}
	return m, nil
}
