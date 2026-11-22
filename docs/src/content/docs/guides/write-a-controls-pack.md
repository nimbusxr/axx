---
title: Write a controls pack
description: Give every repository of an organization the same list of required controls, add a framework Axx does not carry, write the controls report in a format of your own, or read another tool's results as evidence - with a small Go pack that registers with controls-core.
---

`controls-core` knows no framework, writes a few formats, and reads its own evidence. Everything else is a pack that registers with it. Axx's own packs do the same: `controls-asvs` registers a framework, and `controls-oscal` a format.

Write a pack when a file in one project is not enough:

- **A list for many repositories.** A security team keeps one list, in one versioned pack.
- **A framework Axx does not carry,** whose text your organization may hold.
- **A report format** your auditors or your compliance tool take.
- **A source:** another tool's results, read as evidence.

For one project, a file does the first two with no code ([A framework of your own](/guides/prove-controls/#a-framework-of-your-own)).

:::caution[Experimental]
The controls packs are [experimental](/explanations/stability/), and so is what they offer other packs: these types may change in any release.
:::

## Create the pack

A controls pack is a pack like any other ([Write custom steps](/guides/write-custom-steps/)). It has no steps: it registers what it carries as it loads.

```sh
axx pack new ./security-baseline
```

```go
package baseline

import (
	"github.com/nimbusxr/axx/core"
	controls "github.com/nimbusxr/axx/packs/controls/core"
)

func init() { controls.RegisterFramework(framework) }

// Pack returns the pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:     "security-baseline",
		Doc:      "The controls every service of ours is to cover.",
		Requires: []string{controls.Name},
	}
}
```

`Requires` brings `controls-core` into every project that adds the pack.

## A framework and its lists

A framework is a key, its controls, and the lists of them a project can require:

```go
var framework = controls.Framework{
	Key:     "baseline", // scenarios claim its controls with @baseline:auth-1
	Title:   "Security baseline",
	Version: "2027.1",
	Controls: []controls.Control{
		{ID: "auth-1", Title: "Passwords", Group: "Authentication",
			Text: "A password is at least 12 characters long, and a shorter one is refused."},
		{ID: "auth-2", Title: "Lockout", Group: "Authentication",
			Text: "An account locks for a minute after five wrong passwords."},
	},
	Lists: []controls.List{{
		Name:  "baseline-2027",
		Title: "Security baseline 2027, every service",
		Controls: []controls.Required{
			{ID: "auth-1"},
			{ID: "auth-2", Scenarios: 2}, // it takes two passing scenarios
		},
	}},
}
```

A project then requires the list by its name:

```yaml
packs:
  controls-core:
    require: [baseline-2027]
```

- **`Text`** is shown by `axx controls show` and `axx controls list`. It is never written into a project's evidence.
- **`Notice`** says where the text is from and under what terms, when that must be said wherever the text is shown.
- **`Version`** is recorded with every run's evidence. Evidence of another version is refused, as a control's ID may mean another control there.
- **`Same`** tells two ways of writing an ID apart or together, where your framework has several (`IA-5(1)` and `IA-5.1`).

**A list of a framework another pack carries** needs no framework of your own. Register the list for that framework's key, and require the framework's pack:

```go
func init() {
	controls.RegisterList("nist", controls.List{
		Name:  "our-nist",
		Title: "The SP 800-53 controls our services prove",
		Controls: []controls.Required{{ID: "IA-5.1"}, {ID: "AC-7"}, {ID: "AU-2", Scenarios: 2}},
	})
}
```

```go
Requires: []string{"controls-nist"},
```

## A report format

A format writes the report. `axx controls report --format <name>` uses it:

```go
func init() {
	controls.RegisterFormat(controls.Format{
		Name: "audit-sheet",
		Doc:  "the sheet our auditors take",
		Write: func(w io.Writer, r *controls.Report) error {
			for _, c := range r.Controls {
				if _, err := fmt.Fprintf(w, "%s;%s;%d\n", c.Control, c.State, len(c.Scenarios)); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
```

The report has every control with its state, its scenarios, what other sources say and its exception. A format that sends the report somewhere (a compliance platform's API) writes what it sent to `w`.

## A source of evidence

A source reads a file another tool wrote, and says which controls it covers or fails. `axx controls report` and `check` then take such a file among their evidence, and a project can name it in `packs.controls-core.evidence`.

```go
func init() {
	controls.RegisterSource(controls.Source{
		Name: "scanner",
		Doc:  "our scanner's results",
		Read: func(path string, data []byte) (*controls.Evidence, bool, error) {
			var results struct {
				Tool  string `json:"tool"`
				Rules []struct {
					Control string `json:"control"`
					Passed  bool   `json:"passed"`
				} `json:"rules"`
			}
			if json.Unmarshal(data, &results) != nil || results.Tool != "our-scanner" {
				return nil, false, nil // not one of ours: the other sources are tried
			}
			e := &controls.Evidence{Source: "Scanner"}
			for _, r := range results.Rules {
				state := "covered"
				if !r.Passed {
					state = "failing"
				}
				e.Findings = append(e.Findings, controls.Finding{Control: r.Control, State: state})
			}
			return e, true, nil
		},
	})
}
```

A finding names its control as a tag does, without the `@`: `baseline:auth-1`.

## Share it

Publish the pack as a Go module, and projects add it by its path:

```sh
axx pack add github.com/your-org/security-baseline@v1.4.0
```

The version is the list's version too: a project moves to the next baseline when it moves to the pack's next release.
