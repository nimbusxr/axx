// Package controlsasvs is the controls-asvs pack: OWASP's Application Security
// Verification Standard, version 5.0.0, as a framework of controls-core. A
// scenario claims one of its requirements with a tag, @asvs:6.2.1, and a project
// requires one of its three levels: asvs-5.0-level-1, asvs-5.0-level-2 or
// asvs-5.0-level-3. It has no steps: it carries the standard.
//
// The standard is OWASP's, under the Creative Commons Attribution-ShareAlike
// 4.0 licence (data/LICENSE.md). The pack embeds it as OWASP publishes it, and
// what it shows of a requirement is the requirement's own text, with its
// source.
package controlsasvs

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	controlscore "github.com/nimbusxr/axx/packs/controls/core"
)

// Name is the pack's name.
const Name = "controls-asvs"

// Version is the version of the standard the pack carries.
const Version = "5.0.0"

// Source is where the standard's requirements are from, on github.com: OWASP's
// own file of them, at the commit of its 5.0.0 release. SourceSHA256 is that
// file's hash, which the embedded copy is held to.
const (
	Source       = "/OWASP/ASVS/raw/5cf9b032440be53ce345ab3c130fda46ba1ce7a2/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.flat.json"
	SourceSHA256 = "8201b20eec2908c3380ac600c91c8ba746346fbb808859366abb232027532311"
)

// Notice is what is said wherever the standard's text is shown.
const Notice = "OWASP Application Security Verification Standard 5.0.0, © OWASP Foundation, CC BY-SA 4.0 (https://owasp.org/www-project-application-security-verification-standard/)"

//go:embed data/asvs.flat.json.gz
var data []byte

func init() { controlscore.RegisterFramework(Framework()) }

// Pack returns the controls-asvs pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Experimental: true,
		Requires: []string{controlscore.Name}, Steps: []core.StepDef{},
	}
}

const packDoc = `OWASP's Application Security Verification Standard (ASVS), version 5.0.0, for ` + "`controls-core`" + `: its 345 requirements, which a scenario claims with a tag, ` + "`@asvs:6.2.1`" + `, and its three levels as lists a project can require.

| List | What it is |
|---|---|
| ` + "`asvs-5.0-level-1`" + ` | the 70 requirements of level 1 |
| ` + "`asvs-5.0-level-2`" + ` | the 253 of levels 1 and 2 |
| ` + "`asvs-5.0-level-3`" + ` | all 345 |

` + "`axx controls list <words>`" + ` finds a requirement by a word of it, and ` + "`axx controls show asvs:6.2.1`" + ` shows what one requires. A requirement that no scenario can prove (one about documentation, say) is recorded as an exception or as covered by other evidence, in ` + "`packs.controls-core`" + `.

` + "```yaml" + `
packs:
  controls-core:
    require: [asvs-5.0-level-1]
` + "```" + `

The standard is OWASP's, under the Creative Commons Attribution-ShareAlike 4.0 licence; the pack carries it as OWASP publishes it.`

// requirement is a requirement as OWASP's file has it.
type requirement struct {
	Chapter     string `json:"chapter_id"`
	ChapterName string `json:"chapter_name"`
	Section     string `json:"section_id"`
	SectionName string `json:"section_name"`
	ID          string `json:"req_id"`
	Text        string `json:"req_description"`
	Level       string `json:"L"`
}

// Raw is OWASP's file of the requirements, as the pack embeds it.
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
		panic(fmt.Errorf("the controls-asvs pack's data: %w", err)) // embedded in the binary
	}
	var doc struct {
		Requirements []requirement `json:"requirements"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &doc); err != nil {
		panic(fmt.Errorf("the controls-asvs pack's data: %w", err))
	}
	f := controlscore.Framework{Key: "asvs", Title: "OWASP ASVS", Version: Version, Notice: Notice, Same: Same, Token: func(id string) string { return "V" + id }}
	levels := map[string][]controlscore.Required{}
	for _, r := range doc.Requirements {
		id := strings.TrimPrefix(r.ID, "V")
		f.Controls = append(f.Controls, controlscore.Control{
			ID: id, Title: r.SectionName, Text: r.Text,
			Group: fmt.Sprintf("%s %s, %s %s", r.Chapter, r.ChapterName, r.Section, r.SectionName),
		})
		// A level has its own requirements and those of the levels under it.
		for _, level := range []string{"1", "2", "3"} {
			if r.Level <= level {
				levels[level] = append(levels[level], controlscore.Required{ID: id})
			}
		}
	}
	for _, level := range []string{"1", "2", "3"} {
		f.Lists = append(f.Lists, controlscore.List{
			Name: "asvs-5.0-level-" + level, Title: "OWASP ASVS 5.0, level " + level, Controls: levels[level],
		})
	}
	return f
})

// Framework is the standard, as controls-core takes a framework.
func Framework() controlscore.Framework { return framework() }

// Same reports whether two ways of writing a requirement's ID name the same
// requirement: with the V the standard's own files put in front of it, or
// without, in either case (6.2.1, V6.2.1).
func Same(a, b string) bool {
	norm := func(s string) string { return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "v") }
	return norm(a) == norm(b)
}
