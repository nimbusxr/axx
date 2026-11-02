// Package packref is what axx knows of every pack it publishes, whether a
// project uses it or not: what each pack reaches, the packs it builds on, its
// settings, its parameter types and its steps, with their documentation and
// examples.
//
// A project's axx is built with the packs it lists (internal/packbuild), so
// the packs' code stays out of the core. Their reference does not: it is
// data, generated from the packs themselves (go generate, with every pack
// built in) and built into axx, so axx answers for any of its packs at its
// own version, before the pack is added, without a network or the docs site.
package packref

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

//go:generate go run ../tools/axxall pack reference --out packs.json

//go:embed packs.json
var data []byte

// Pack is one pack's reference: its manifest, without its code.
type Pack struct {
	Name string `json:"name"`
	// Doc says what the pack does and what it reaches (the databases,
	// brokers or platforms it works with).
	Doc      string           `json:"doc,omitempty"`
	Requires []string         `json:"requires,omitempty"`
	Params   []core.ParamType `json:"params,omitempty"`
	Steps    []core.StepDef   `json:"steps"`
	// Settings is the JSON Schema of the pack's section in axx.yaml
	// (packs.<name>), when it has one.
	Settings json.RawMessage `json:"settings,omitempty"`
}

// Reference is the file go generate writes.
type Reference struct {
	Description string `json:"description"`
	Packs       []Pack `json:"packs"`
}

var (
	once   sync.Once
	ref    Reference
	byName map[string]*Pack
	err    error
)

func load() error {
	once.Do(func() {
		if err = json.Unmarshal(data, &ref); err != nil {
			err = fmt.Errorf("axx's pack reference: %w", err)
			return
		}
		byName = map[string]*Pack{}
		for i := range ref.Packs {
			byName[ref.Packs[i].Name] = &ref.Packs[i]
		}
	})
	return err
}

// Packs returns every pack's reference, in the order of axx's catalog.
func Packs() []Pack {
	if load() != nil {
		return nil
	}
	return ref.Packs
}

// Lookup returns a pack's reference by name.
func Lookup(name string) (Pack, bool) {
	if load() != nil {
		return Pack{}, false
	}
	p, ok := byName[name]
	if !ok {
		return Pack{}, false
	}
	return *p, true
}

// Registry returns a step registry of the named packs (and the packs they
// build on), with core's parameter types: their steps, matchable by text,
// without the packs' code. Their steps have no Run.
func Registry(names ...string) (*match.Registry, error) {
	if err := load(); err != nil {
		return nil, err
	}
	r := match.NewRegistry()
	if err := r.AddPack("core", core.ParamsPack().Manifest()); err != nil {
		return nil, err
	}
	var order []*Pack
	seen := map[string]bool{}
	var add func(name string) error
	add = func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true
		p, ok := byName[name]
		if !ok {
			return fmt.Errorf("axx's pack reference has no pack %q", name)
		}
		for _, req := range p.Requires {
			if err := add(req); err != nil {
				return err
			}
		}
		order = append(order, p)
		return nil
	}
	for _, n := range names {
		if err := add(n); err != nil {
			return nil, err
		}
	}
	// Parameter types first: a pack's steps may use another's.
	for _, p := range order {
		if err := r.AddParams(p.Name, p.Params); err != nil {
			return nil, err
		}
	}
	for _, p := range order {
		if err := r.AddSteps(p.Name, p.Steps); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Finder returns a match.Elsewhere for a project that uses the packs
// loaded: it finds a step text among the steps of every other pack axx
// publishes, so an undefined step names the pack that defines it. The
// registry of those packs is built on the first undefined step.
func Finder(loaded []string) match.Elsewhere {
	have := map[string]bool{}
	for _, n := range loaded {
		have[n] = true
	}
	var (
		build sync.Once
		reg   *match.Registry
	)
	return func(text string) (match.Suggestion, bool) {
		build.Do(func() {
			var others []string
			for _, n := range Names() {
				if !have[n] {
					others = append(others, n)
				}
			}
			reg, _ = Registry(others...)
		})
		if reg == nil {
			return match.Suggestion{}, false
		}
		for _, m := range reg.Match(text) {
			if d := m.Def(); !have[d.Pack] {
				return match.Suggestion{Def: d, ID: d.Step.ID, Expr: m.Variant.Expr, Pack: d.Pack}, true
			}
		}
		return match.Suggestion{}, false
	}
}

// Names lists the packs of the reference, in catalog order.
func Names() []string {
	var out []string
	for _, p := range Packs() {
		out = append(out, p.Name)
	}
	return out
}
