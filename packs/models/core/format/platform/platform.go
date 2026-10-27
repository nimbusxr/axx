// Package platform writes prebuilt fixtures for platform model APIs. An
// artifact carries prepared keys and answers; no mappings or source files are
// read, and no model is built, while an application is running.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
)

const (
	Version      = 1
	Folder       = "axx-models"
	Extension    = ".axxmodel"
	CodeArtifact = "AXX-E0704"
)

// Artifact is the versioned, self-contained runtime representation. The last
// name and answer are always the default. The contract describes preprocessing
// and output values as it does for native model formats.
type Artifact struct {
	Version    int                `json:"version"`
	API        string             `json:"api"`
	Contract   *contract.Contract `json:"contract"`
	Matcher    string             `json:"matcher"`
	Texts      []string           `json:"texts,omitempty"`
	Keys       [][]float64        `json:"keys,omitempty"`
	Similarity float64            `json:"similarity,omitempty"`
	Names      []string           `json:"names"`
	Answers    []task.Values      `json:"answers,omitempty"`
	Generated  []string           `json:"generated,omitempty"`
}

func init() {
	format.Register(format.Format{Type: "platform", Name: "platform API", Extensions: []string{Extension}, API: true, Write: format.File(Write)})
}

func invalid(f string, args ...any) error {
	return axxerr.New(CodeArtifact, exitcode.Usage, "platform model stub: "+f, args...).WithHint("rebuild the .axxmodel file from its contract and mappings; put it in the app's axx-models folder before launch")
}

// Write writes the prepared stub. API selectors belong in format.details.api.
func Write(s *stub.Stub) ([]byte, error) {
	var details struct {
		API string `json:"api"`
	}
	if err := json.Unmarshal(s.Contract.Format.Details, &details); err != nil || strings.TrimSpace(details.API) == "" {
		return nil, invalid("format.details.api must name the platform API, for example ios.vision.classify")
	}
	c := *s.Contract
	c.Files = nil // all source files have been resolved into keys and answers
	a := &Artifact{
		Version: Version, API: details.API, Contract: &c, Matcher: s.Matcher, Texts: s.Texts,
		Names: s.Names, Answers: s.Answers, Generated: s.Generated,
	}
	if s.Nearest != nil {
		a.Keys, a.Similarity = s.Nearest.Table.Keys, s.Nearest.Table.Similarity
	}
	if err := a.Check(); err != nil {
		return nil, err
	}
	return json.Marshal(a)
}

// Read refuses malformed and newer artifacts instead of falling back to a
// real model. It does not resolve any source files.
func Read(b []byte) (*Artifact, error) {
	var a Artifact
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return nil, invalid("cannot read artifact: %v", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, invalid("unexpected data after the artifact")
	}
	if err := a.Check(); err != nil {
		return nil, err
	}
	return &a, nil
}

// Check validates dimensions and answer counts before target runtimes see data.
func (a *Artifact) Check() error {
	if a.Version != Version {
		return invalid("version %d is unsupported (expected %d)", a.Version, Version)
	}
	if a.Contract == nil || a.Contract.Format.Type != "platform" || strings.TrimSpace(a.API) == "" {
		return invalid("missing platform contract or API")
	}
	if err := a.Contract.Check(); err != nil {
		return invalid("%v", err)
	}
	if err := task.Check(a.Contract); err != nil {
		return invalid("%v", err)
	}
	var details struct {
		API          string `json:"api"`
		Availability string `json:"availability"`
	}
	if json.Unmarshal(a.Contract.Format.Details, &details) != nil || details.API != a.API {
		return invalid("API disagrees with its contract")
	}
	if details.Availability != "" {
		api, _ := APIByName(a.API)
		if !slices.Contains(api.Unavailable, details.Availability) {
			if len(api.Unavailable) == 0 {
				return invalid("format.details.availability: %s is always there, as its stub says", a.API)
			}
			return invalid("format.details.availability %q is not a reason %s gives: %s", details.Availability, a.API, strings.Join(api.Unavailable, ", "))
		}
	}
	n := 0
	switch a.Matcher {
	case stub.TextMatcher:
		if len(a.Keys) != 0 {
			return invalid("text matcher has numeric keys")
		}
		n = len(a.Texts)
		for _, key := range a.Texts {
			if strings.TrimSpace(key) == "" {
				return invalid("empty text key")
			}
		}
	case stub.NearestMatcher:
		if len(a.Texts) != 0 || a.Contract.MatchedInput() == nil {
			return invalid("nearest matcher needs an input contract")
		}
		r, err := match.NewReducer(a.Contract.MatchedInput())
		if err != nil {
			return invalid("%v", err)
		}
		if a.Similarity < 0 || a.Similarity > 1 || math.IsNaN(a.Similarity) {
			return invalid("similarity must be between 0 and 1")
		}
		for _, key := range a.Keys {
			if len(key) != r.Dims() {
				return invalid("key has %d values, expected %d", len(key), r.Dims())
			}
			for _, v := range key {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return invalid("key contains a nonfinite value")
				}
			}
		}
		n = len(a.Keys)
	default:
		return invalid("unknown matcher %q", a.Matcher)
	}
	if len(a.Names) != n+1 {
		return invalid("expected %d names including the default", n+1)
	}
	if task.Generates(a.Contract) {
		if len(a.Generated) != n+1 || len(a.Answers) != 0 {
			return invalid("expected %d text answers including the default", n+1)
		}
	} else {
		if len(a.Answers) != n+1 || len(a.Generated) != 0 {
			return invalid("expected %d answers including the default", n+1)
		}
		for _, answer := range a.Answers {
			if len(answer) != len(a.Contract.Outputs) {
				return invalid("answer outputs disagree with the contract")
			}
			for _, out := range a.Contract.Outputs {
				if len(answer[out.Name]) != task.ItemSize(&out) {
					return invalid("answer %s has the wrong size", out.Name)
				}
			}
		}
	}
	return nil
}

// TextIndex matches the same UTF-8 word boundaries as the native text writers.
func (a *Artifact) TextIndex(text string) int {
	for i, key := range a.Texts {
		if lm.HasWords(text, key) {
			return i
		}
	}
	return len(a.Texts)
}

// Index reduces an input using the reference matcher.
func (a *Artifact) Index(item []float64) (int, error) {
	if a.Matcher != stub.NearestMatcher {
		return 0, invalid("%s takes text", a.API)
	}
	r, err := match.NewReducer(a.Contract.MatchedInput())
	if err != nil {
		return 0, err
	}
	v, err := r.Reduce(item)
	if err != nil {
		return 0, err
	}
	return (&match.Table{Keys: a.Keys, Similarity: a.Similarity}).Nearest(v), nil
}

// Load reads one app's snapshot and rejects duplicate API selectors. Files is
// already rooted in axx-models; native model files in that folder are left alone.
func Load(ctx context.Context, files core.Files) ([]*Artifact, error) {
	var names []string
	if indexed, ok := files.(interface{ Written() []string }); ok {
		names = indexed.Written()
	} else {
		var err error
		names, err = files.List(ctx, 10001)
		if err != nil {
			return nil, invalid("cannot list %s: %v", files.Where(), err)
		}
	}
	if len(names) > 10000 {
		return nil, invalid("too many files in %s", files.Where())
	}
	slices.Sort(names)
	seen := map[string]string{}
	var out []*Artifact
	for _, name := range names {
		if !strings.EqualFold(filepath.Ext(name), Extension) {
			continue
		}
		b, ok, err := files.Read(ctx, name)
		if err != nil || !ok {
			return nil, invalid("cannot read %s: %v", name, err)
		}
		a, err := Read(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if other, ok := seen[a.API]; ok {
			return nil, invalid("%s and %s both answer %s", other, name, a.API)
		}
		seen[a.API] = name
		out = append(out, a)
	}
	return out, nil
}
