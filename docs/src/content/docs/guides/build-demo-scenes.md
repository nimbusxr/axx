---
title: Build a demo's scenes
description: Add your own pictures to a demo - a surface of your own beside the diagram, a new look for a part of the stage, or your own directions - with a few files of JavaScript and CSS, in a project or in a pack. With the reference of what a scene is given.
---

A demo's picture is a web page, called the stage. Each part of it is drawn by a scene: a small piece of JavaScript that is given what is happening at a moment of the demo, and draws it. The diagram is a scene. So are the subtitle, the card, and the terminal that types a request as a curl command.

You can add scenes of your own. This guide shows how, and lists everything a scene is given. It follows [Make a demo](/guides/make-a-demo/), which you should read first.

The demo pack is experimental: what this page describes may change in any release.

## What you can add

| You want | You write | Where it is named |
| --- | --- | --- |
| A new picture, shown by `[show ledger]` | A scene under a name of your own | `packs.demo.surfaces` and `packs.demo.scenes` |
| Another look for a part the pack draws | A scene under the pack's name for it | `packs.demo.scenes` |
| Other colors, fonts and sizes | A stylesheet | `packs.demo.styles` |
| A direction of your own, like `[confetti]` | A handler of the `direction` event | `packs.demo.directions` and `packs.demo.scenes` |

A surface is a picture a cue can put on the stage with `[show ...]`. The pack has `diagram`, `browser`, `phone`, `desktop`, `terminal` and `editor`. The other parts the pack draws are `card`, `subtitle` and `ticker` (the line at the top that names the step).

## Add a surface, step by step

This example adds a surface named `ledger`, which lists what each step sent.

1. Name the surface and its script in `axx.yaml`:

   ```yaml
   packs:
     demo:
       surfaces: [ledger]
       scenes: [demo/ledger.js]
       styles: [demo/ledger.css]
   ```

2. Write the scene in `demo/ledger.js`:

   ```js
   axxStage.scene('ledger', {
     // mount runs once: put the scene's elements in el.
     mount(el, state) {
       el.innerHTML = '<ol class="ledger"></ol>';
     },
     // render runs for every picture of the demo: draw the moment in state.
     render(el, state) {
       const rows = [];
       for (const step of state.take.steps.slice(0, state.done)) {
         for (const x of axxStage.acts(step)) {
           rows.push('<li>' + axxStage.escape(x.peer + ': ' + (x.sent || x.seen).title) + '</li>');
         }
       }
       if (state.now.sent) rows.push('<li class="now">' + axxStage.escape(state.now.sent.title) + '</li>');
       el.querySelector('.ledger').innerHTML = rows.join('');
     },
   });
   ```

3. Style it in `demo/ledger.css`. The stage's colors are CSS variables, so your scene follows the project's theme:

   ```css
   .ledger { font-family: var(--font); color: var(--text); }
   .ledger .now { color: var(--sent); }
   ```

4. Show it in a demo, alone or beside another surface. The picture shows two things at the most.

   ```text
   # demo: [show ledger + diagram] The shop registers a parcel.
   ```

5. Run `axx demo --recut <file>` to see it without running the scenario again. `axx demo` refuses a `[show ...]` that no scene draws, and says which.

## The one rule

Draw from the state you are given, never from a clock of your own. The demo asks the stage for the picture of each moment, in any order, and a presenter can go back. So a scene has no timers and no animations that run by themselves: an animation is a position you compute from `state.progress`.

The terminal is the example to copy: how much of the command is typed is computed from how far the step has played. Its source is in the pack, in `packs/demo/modules/terminal`, beside the editor's.

## What a scene is

```js
axxStage.scene(name, { prepare, mount, render });
```

| Function | When it runs | What it is for |
| --- | --- | --- |
| `prepare(data)` | Once, before the first picture. It may be `async`. | Reading what the scene needs. Report what is wrong with `axxStage.problems.push('...')`: `axx demo` then stops with your message and makes no demo. |
| `mount(el, state)` | Once, when the stage is built. | Putting the scene's elements in `el`. |
| `render(el, state)` | For every picture. | Drawing the moment. |

All three are optional. A scene registered under a name the pack already has replaces the pack's.

## The state of a moment

| Field | What it is |
| --- | --- |
| `state.t`, `state.duration` | The demo's time at this moment and its length, in seconds. |
| `state.step` | The step that plays, or was last played: its `keyword`, `text`, `status` and `exchanges`. |
| `state.playing` | Whether that step plays now. It is `false` while the picture holds after it. |
| `state.progress` | How far through the step the demo is, from 0 to 1. |
| `state.done` | How many of the take's steps are over. |
| `state.now` | What is happening now. See below. |
| `state.subtitle`, `state.card` | The subtitle's text and the card's text, when there is one. |
| `state.show` | The surfaces on the stage. |
| `state.take` | The whole run: `steps`, and `peers` (what the scenario talked to). |
| `state.timeline` | The demo's cut: `segments`, `subtitles`, `marks`. |
| `state.theme` | The project's `packs.demo.theme`. |

### What is happening now

`state.now` is the exchange the demo is showing, worked out once for every scene. Use it, and your scene follows the demo's rules without knowing them. For example, a step with `[hold response]` has no `state.now.seen` until the step after it.

| Field | What it is |
| --- | --- |
| `now.exchange` | The exchange that plays, or the last one when the step sent nothing. `null` before the first. |
| `now.sent` | What went out, once the demo shows it: `title`, `mediaType`, `body`. |
| `now.seen` | What came back, once the demo shows it. |
| `now.phase` | How far along the exchange is, from 0 to 1. |
| `now.failed` | Whether its step failed. |
| `now.found` | Whether the scenario looked at this rather than asked for it: rows of a database, an event on a topic, a call a mock received. |
| `now.over` | Whether the exchange is finished and only stays to be read. |

An exchange is what a step sent and saw: `peer` (the name the steps use), `kind` (`service`, `database`, `topic`, `mock`), `address` (where a service was reached, when the step knows it), `from` (who sent, when that was not the scenario), `sent` and `seen`.

## What the stage gives you

| Function | What it does |
| --- | --- |
| `axxStage.scene(name, scene)` | Registers a scene. |
| `axxStage.on(event, fn)` | Listens to the demo: `segment`, `step`, `direction` (your own directions, with `name` and `args`), `frame`. |
| `axxStage.acts(step)` | The step's exchanges that sent or saw something. |
| `axxStage.pretty(message, lines, width)` | A message's body as a few lines to read: JSON laid out, long bodies cut. |
| `axxStage.escape(text)` | The text, safe to put in HTML. |
| `axxStage.problems` | What is wrong, as `prepare` found it. |

The colors are the CSS variables `--background`, `--surface`, `--text`, `--muted`, `--rule`, `--line`, `--accent`, `--sent`, `--seen` and `--failed`. The fonts are `--font` (the diagram's, of one width) and `--text-font` (the subtitle's).

## Share it as a pack

A project's scenes live in the project. To share them, put the same files in a pack, which registers them as a module. Every project that adds the pack then has its surfaces and directions, with nothing in `axx.yaml`.

```go
//go:embed stage
var files embed.FS

func init() {
	stage, _ := fs.Sub(files, "stage")
	demo.RegisterModule(demo.Module{
		Name:       "ledger",
		Surfaces:   []string{"ledger"},
		Directions: []string{"stamp"},
		Files:      stage,
		Scripts:    []string{"ledger.js"},
		Styles:     []string{"ledger.css"},
	})
}
```

The pack lists `demo` in its manifest's `Requires`. The pack's own terminal is registered this way and no other. A project's scripts load after every module's, so a project can still replace a module's scene.

A pack whose steps talk to something says what each step sent and saw (`Scenario.Exchanged` in the public `core` package). That is all it takes for its steps to show on the diagram, in the terminal and in every scene built on `state.now`.

## For agents

An agent builds a scene the same way: the five steps above, then `axx demo --recut <file> --json` to check it. A mistake in a cue is reported by `axx validate` with the surfaces and directions the project has, and a scene's own problem stops `axx demo` with the scene's message.
