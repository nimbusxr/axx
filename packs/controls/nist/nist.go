// Package controlsnist is the controls-nist pack: the controls of NIST's
// Special Publication 800-53, revision 5, as a framework of controls-core. A
// scenario claims a control with a tag, @nist:IA-5, or one of its enhancements,
// @nist:IA-5.1, and a project lists the controls it is to cover in a file of
// its own. It has no steps: it carries the catalog's controls, by ID, title
// and family.
//
// It carries no list to require. A baseline of SP 800-53 is mostly about
// people, premises and process, which no scenario proves: a project names the
// controls its scenarios are to prove.
package controlsnist

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	controlscore "github.com/nimbusxr/axx/packs/controls/core"
)

// Name is the pack's name.
const Name = "controls-nist"

// Version is the version of the catalog the pack carries.
const Version = "5.2.0"

// Source is where the controls are from, on github.com: NIST's own catalog in
// OSCAL, at a commit of its repository. SourceSHA256 is that file's hash. The
// pack embeds what Slim keeps of it.
const (
	Source       = "/usnistgov/oscal-content/raw/5006f18c8047643eaa70e35dd0d20a58bcc41856/nist.gov/SP800-53/rev5/json/NIST_SP-800-53_rev5_catalog-min.json"
	SourceSHA256 = "1cbaf5f25aa1c70610324e278323f1d3be380b9183a0e1f44074976d5da55291"
)

//go:embed data/sp800-53.json.gz
var data []byte

func init() { controlscore.RegisterFramework(Framework()) }

// Pack returns the controls-nist pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Experimental: true,
		Requires: []string{controlscore.Name}, Steps: []core.StepDef{},
	}
}

const packDoc = `The controls of NIST SP 800-53, revision 5 (release 5.2.0), for ` + "`controls-core`" + `: a scenario claims a control with a tag, ` + "`@nist:IA-5`" + `, and an enhancement of one with a dot, ` + "`@nist:IA-5.1`" + ` for IA-5(1). The pack has each control's ID, title and family; NIST's catalog has their text.

It carries no list to require. Most of a baseline of SP 800-53 is about people, premises and process, which no scenario proves, so a project names the controls its scenarios are to prove, in a file of its own:

` + "```yaml" + `
# security/nist.yaml
framework: nist
title: The SP 800-53 controls our scenarios prove
controls:
  IA-5.1:
  AC-7:
  AU-2:
    scenarios: 2
` + "```" + `

` + "```yaml" + `
packs:
  controls-core:
    require: [security/nist.yaml]
` + "```" + `

` + "`axx controls list <words>`" + ` finds a control by a word of its title or its family. NIST's catalog is in the public domain.`

// control is a control as the pack keeps it.
type control struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Family    string `json:"family"`
	Of        string `json:"of,omitempty"`
	Withdrawn bool   `json:"withdrawn,omitempty"`
}

// Slim is what the pack keeps of NIST's catalog in OSCAL: each control and
// each enhancement, by ID (AC-2, AC-2.1), with its title, its family and, for
// an enhancement, the control it is of. Withdrawn ones are kept, and marked.
func Slim(catalog []byte) ([]byte, error) {
	type oscalControl struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Props    []struct{ Name, Value string }
		Controls []json.RawMessage `json:"controls"`
	}
	var doc struct {
		Catalog struct {
			Metadata struct {
				Version string `json:"version"`
			} `json:"metadata"`
			Groups []struct {
				Title    string            `json:"title"`
				Controls []json.RawMessage `json:"controls"`
			} `json:"groups"`
		} `json:"catalog"`
	}
	if err := json.Unmarshal(catalog, &doc); err != nil {
		return nil, err
	}
	if doc.Catalog.Metadata.Version != Version {
		return nil, fmt.Errorf("the catalog is release %s, and the pack is of %s", doc.Catalog.Metadata.Version, Version)
	}
	var out []control
	var add func(raw json.RawMessage, family, of string) error
	add = func(raw json.RawMessage, family, of string) error {
		var c oscalControl
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		k := control{ID: strings.ToUpper(c.ID), Title: c.Title, Family: family, Of: of}
		for _, p := range c.Props {
			if p.Name == "status" && p.Value == "withdrawn" {
				k.Withdrawn = true
			}
		}
		out = append(out, k)
		for _, sub := range c.Controls {
			if err := add(sub, family, k.ID+" "+c.Title); err != nil {
				return err
			}
		}
		return nil
	}
	for _, g := range doc.Catalog.Groups {
		for _, c := range g.Controls {
			if err := add(c, g.Title, ""); err != nil {
				return nil, err
			}
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Raw is what the pack embeds of the catalog (Slim's output).
func Raw() ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

var framework = sync.OnceValue(func() controlscore.Framework {
	raw, err := Raw()
	if err != nil {
		panic(fmt.Errorf("the controls-nist pack's data: %w", err)) // embedded in the binary
	}
	var controls []control
	if err := json.Unmarshal(raw, &controls); err != nil {
		panic(fmt.Errorf("the controls-nist pack's data: %w", err))
	}
	f := controlscore.Framework{Key: "nist", Title: "NIST SP 800-53", Version: Version, Same: Same, Token: strings.ToLower}
	for _, c := range controls {
		group := c.Family
		if c.Of != "" {
			group += ", " + c.Of
		}
		title := c.Title
		if c.Withdrawn {
			title += " (withdrawn)"
		}
		f.Controls = append(f.Controls, controlscore.Control{ID: c.ID, Title: title, Group: group})
	}
	return f
})

// Framework is the catalog's controls, as controls-core takes a framework.
func Framework() controlscore.Framework { return framework() }

var idPattern = regexp.MustCompile(`^([a-z]{2})-0*(\d+)(?:[.(]0*(\d+)\)?)?$`)

// Same reports whether two ways of writing a control's ID name the same
// control: SP 800-53 writes an enhancement in brackets, IA-5(1), OSCAL with a
// dot, ia-5.1, and some lists with leading zeros, IA-05(01). A tag takes the
// dot: brackets have to be escaped in a tag expression.
func Same(a, b string) bool {
	na, oka := normal(a)
	nb, okb := normal(b)
	return oka && okb && na == nb
}

func normal(id string) (string, bool) {
	m := idPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(id)))
	if m == nil {
		return "", false
	}
	n, _ := strconv.Atoi(m[2])
	out := m[1] + "-" + strconv.Itoa(n)
	if m[3] != "" {
		e, _ := strconv.Atoi(m[3])
		out += "." + strconv.Itoa(e)
	}
	return out, true
}
