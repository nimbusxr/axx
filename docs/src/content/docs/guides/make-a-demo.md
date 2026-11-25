---
title: Make a demo
description: Turn a scenario into a demo - a film with subtitles of what the scenario did, with a diagram for what has no screen, or a live show you click through in front of an audience. What it shows ran against the real system, and passed.
---

A demo of software is a promise that it works. A demo made from a scenario keeps the promise: every step in it ran against the real system, and passed.

With the demo pack, a feature file becomes a demo. You write what the audience reads as comments in the file, and Axx films the scenario, or lets you present it live.

:::caution[Experimental]
The demo pack is [experimental](/explanations/stability/): it may change in any release. Nothing about it affects a project that does not add it.
:::

## Set it up

1. Add the pack. It brings `web-core`, because the film is made in a browser on your machine.

   ```sh
   axx pack add demo
   ```

2. Write a feature file whose name ends in `.demo.feature`, like `features/demos/register-a-parcel.demo.feature`. It uses the steps of your project's other packs: the demo pack has no steps of its own.

3. Make the film.

   ```sh
   axx demo features/demos/register-a-parcel.demo.feature
   ```

   Axx starts your services, runs the scenario and writes the film to `.axx/demo/register-a-parcel/register-a-parcel.mp4`. A scenario that does not pass makes no film.

## Write the demo

A demo tells a story from its start to its end, so its scenario is longer than a test's, on purpose. The rule of one behavior per scenario, which `axx lint` holds every other feature to, is not applied to a file named `.demo.feature`.

A comment that starts with `demo:` is a **cue**. Its text is the subtitle the audience reads while the step below it plays. Any other comment stays your own.

```gherkin
# demo: [card 3s] Registering a parcel
Feature: Register a parcel, as a demo

  Scenario: A shop registers a parcel, and everyone who has to know of it does
    # demo: [cut]
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And a POST request to /api/parcels
    And a request payload using an application/json content example

    # demo: [action] A shop registers a parcel with us.
    When the request is executed

    # demo: We answer that it is registered,
    Then the response status code is 201

    # demo: [pause 2s] and the depots' database has the parcel.
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-REG-1001 |
```

A cue belongs to the line below it. A comment line right below a cue, indented to the cue's text, continues the subtitle:

```gherkin
    # demo: [action] Ms Okafor is away until Friday.
    #       Her shop asks us to hold her parcel.
    When the request is executed
```

In a Scenario Outline, a cue's `<name>` is the row's value, as a step's is. The rows become one film, with a chapter for each row.

## Direct the film

What a cue has in square brackets directs the film. The film's time is its own: a step that took the system 20 milliseconds gets the time a person needs to see what it did, and a subtitle stays as long as it takes to read. The directions change that.

| Direction | What it does in the film | Before an audience |
| --- | --- | --- |
| `[2s]` | The step below lasts 2 seconds, however long it ran. | Nothing. |
| `[0s]` | The step runs and the film shows none of it: a cut to what comes after. Use it for a web page's blank start. | The step runs without a stop. |
| `[speed 4x]` | The step plays four times as fast. `[speed 0.5x]` plays it at half speed. | Nothing. |
| `[pause 2s]` | The picture holds for 2 seconds after the step. | Nothing: the presenter decides when to go on. |
| `[cut]` | The camera stops before the step below. The steps run, and the film shows none of them until `[action]`. Use it for the steps that set things up. | The steps run straight through. |
| `[action]` | The camera starts again with the step below. | The same. |
| `[card 3s] A title` | A card with the cue's text fills the picture for 3 seconds. | The card stays until the presenter goes on. |
| `[mark billing ✓]` | Writes what follows the box's name in that box of the diagram, once the step is over, and it stays: a check mark, an emoji, a few words. For a box the scenario does not talk to. It is drawn in the color of what a cue tells, not of what the run saw. | The same. |
| `[hold response]` | The step below shows what it sent, and what came back shows with the step after it. Use it where a step sends a request and gets its answer, and the next cue is the one that says what the answer is. | The same. |
| `[show diagram]` | Says what the picture shows from the step below on: `diagram`, `terminal`, `browser`, `phone`, `desktop` or a surface of your own, two of them side by side like `[show browser + diagram]`, or `auto`. | The same. |
| `[diagram checkout]` | Goes to another of the project's diagrams, by its name, from the step below on. Two names show two diagrams side by side: `[diagram stored + announced]`. | The same. |
| `[as courier]` | Says who the scenario is, from the step below on: a box of the diagram, which what the scenario sends then comes from. | The same. |
| `[parcels --> addresses]` | Lights that line of the diagram while the step plays. | The same. |

`axx validate`, `axx lint` and your editor report a direction the pack does not know, with the nearest one it has.

## What the film shows

The picture follows the step:

- **A step on a screen** (a web page, a phone, a desktop app) shows the screen as the scenario used it.
- **Any other step** shows a diagram of the system, drawn in characters. When a step sends something, it travels along a line to its box, and what came back is shown under the diagram.

Without a diagram of your own, the diagram is of what the scenario talked to: a box for each service, database, topic and mock, and what the scenario sends coming in from the edge of the picture.

## Draw your system

A demo is better on a map of your system. Write the map as a [Mermaid](https://mermaid.js.org/syntax/flowchart.html) flowchart (or a sequence diagram, [below](#a-sequence-diagram)), the same file a README or a docs site draws, and name it in `axx.yaml`.

1. Write the flowchart. Give each box the name your scenarios give what they talk to (`parcels`, `parcels-db`), and add the boxes no scenario talks to, so the audience sees the whole system. Draw a call that is answered as a solid line (`-->`) and an event as a dotted one (`-.->`): Axx draws them so.

   ```text
   flowchart LR
     shop([shop]) --> portal --> parcels
     shop --> parcels
     parcels --> parcels-db[(parcels-db)]
     parcels --> addresses
     parcels -.-> parcel-events{{parcel-events}} -.-> billing
   ```

2. Name it in `axx.yaml`. The first diagram is the one a demo starts on.

   ```yaml
   packs:
     demo:
       diagrams:
         system: demo/system.mmd
         checkout: demo/checkout.mmd
   ```

3. Make the film again. Axx reads the file with Mermaid's own parser, which it downloads once, and draws the map itself, in characters, from left to right.

What a run did plays on the map by these rules:

| What the run did | What the film shows |
| --- | --- |
| The scenario sent a request to a service | It travels along the line from who the scenario is (`[as shop]`) to the service, and the answer comes back along it. |
| The scenario found something in a database or a topic, or a mock received a call | The map's line that leads there lights up, then what was found shows. The run saw it there; the map says how it got there. |
| A cue has an arrow, `[parcel-events --> billing]` | That line lights up, in another color and with nothing shown under the diagram: the scenario cannot see it, and you are telling the audience. |

A cue that names a box or a line its diagram has not makes no film: `axx demo` says which.

The picture shows two things at the most: a screen and a diagram (`[show browser + diagram]`), or two diagrams (`[diagram stored + announced]`). Two diagrams beside a screen make no film.

### A sequence diagram

A diagram can also be a Mermaid [sequence diagram](https://mermaid.js.org/syntax/sequenceDiagram.html): who takes part, and the messages between them in their order.

```text
sequenceDiagram
  participant shop
  participant parcels
  shop->>parcels: what does this parcel cost?
  parcels-->>shop: its price
```

Axx draws each participant with a line down the picture and each message as an arrow across, a dotted one where the diagram's is. As the film plays, a step lights the next message that fits what it sent or found, and the answer it got lights the message that answers it. A message the film has shown stays, and one it has not come to is dim, so the diagram never shows more than the run did. Notes, loops and the like are not drawn.

To change the diagram during a demo, name the other one in a cue:

```gherkin
    # demo: [diagram checkout] Now the same parcel, from the payment's side.
    Then the response status code is 201
```

To make the film again after changing the subtitles, the directions or the look, without running the scenario again:

```sh
axx demo --recut features/demos/register-a-parcel.demo.feature
```

## Add a soundtrack

Axx makes no sound and has no voice of its own. It makes the picture, and writes what the film says, with when, beside it as a `.srt` file: its subtitles, and the texts of its cards, so that a soundtrack says those too. That file is what a soundtrack is made from, and what a video site takes as captions.

```text
.axx/demo/register-a-parcel/
  register-a-parcel.mp4
  register-a-parcel.srt
```

To have `axx demo` make the soundtrack too, name a command of yours. A model that speaks, a service, anything that makes sound:

```yaml
packs:
  demo:
    soundtrack:
      command: python3 demo/soundtrack.py
```

1. Axx makes the film, as it does without the setting. The picture's timing is set by then, and the sound follows it.
2. Axx runs your command, in the folder of `axx.yaml`, and tells it where things are:

   | Variable | What it is |
   | --- | --- |
   | `AXX_DEMO_SUBTITLES` | the subtitles with their times, a `.srt` file |
   | `AXX_DEMO_SOUNDTRACK` | the file to write: one WAV file, as long as the film |
   | `AXX_DEMO_SECONDS` | how long the film is |
   | `AXX_DEMO_FILM` | the film, without sound |

3. Your command writes the WAV file, with each line said at its time.
4. Axx puts the sound under the picture. A soundtrack shorter than the film is silent to its end, and a longer one is cut.

A sentence often runs over several subtitles, each beside its own step. Write it as one sentence, with a comma where a subtitle ends and the next goes on. A soundtrack command can then speak the lines as one sentence, which sounds like a person, and still start each part at its own time.

If a line takes longer to say than its subtitle shows, give that step more time with a cue (`[pause 2s]`, `[6s]`) and run `axx demo` again.

## Present it live

```sh
axx demo --present features/demos/register-a-parcel.demo.feature
```

1. Axx opens a page in your browser. Put that page on the screen the audience sees. It shows the demo's first card while your services start.
2. Axx prints the address of a second page, the presenter's page. Keep it on your own screen. It lists the subtitles and shows where you are.
3. Press the right arrow, or the forward button of a clicker. The next subtitle shows and its steps run against the real system, in front of the audience.
4. Press the left arrow to go back. Back shows again what was shown. It does not undo anything in the system.

A subtitle with the steps under it is one **beat**: forward moves a beat at a time.

### When a step fails

A step that fails is not shown to the audience. The presenter's page shows the step and its error, and you choose:

| Choice | Key | What happens |
| --- | --- | --- |
| Try the step once more | `r` | The step runs again. |
| Play on from the last take | `t` | The show continues from the last run of the demo that passed. The stage says "recorded" in a corner. |
| Start again from the top | `0` | The scenario runs again from its first step to where the show was. The stage says "One moment" until it is there. |

The last take is what `axx demo` kept the last time it made the film, so make the film before you present. To show only that take and run nothing, where nothing may go wrong:

```sh
axx demo --present --recorded features/demos/register-a-parcel.demo.feature
```

## Show the steps in a terminal

`[show terminal]` shows the steps as someone would type them. A request to a service is the curl command that makes it, typed out as the step plays, and what came back is printed under it. What the scenario found in a database or on a topic is a line that says where, with what was there.

```text
# demo: [show terminal + diagram] A shop asks what a parcel costs.
```

The command is what the step sent: its method, its address and its body. It has no headers but the body's type, as headers carry credentials.

## Give it your own look

The stage is a web page, and its look is yours to change in `axx.yaml`.

```yaml
packs:
  demo:
    theme:
      caller: shop            # with no diagram of your own: a box for whoever uses the system
      colors:
        background: "#101418"
        sent: "#ffb000"
    styles: [demo/stage.css]  # your own stylesheet, loaded after the pack's
    scenes: [demo/scenes.js]  # your own scripts, loaded after the pack's
    surfaces: [ledger]        # your own surfaces, for [show ledger]
    directions: [confetti]    # your own directions, like [confetti]
    size: 1920x1080
```

The diagram itself is a scene too, so a project that wants another drawing of its map than characters replaces it.

A scene draws one part of the stage: `diagram`, `browser`, `phone`, `desktop`, `card`, `subtitle` or `ticker` (the line that names the step). A scene your script registers replaces the pack's of that name. It is given the state of the moment and draws it:

```js
axxStage.scene('diagram', {
  mount(el, state) { el.innerHTML = '<div class="our-map"></div>'; },
  render(el, state) {
    // state.step is the step that plays, with what it sent and saw (state.step.exchanges);
    // state.progress is how far through it the film is, from 0 to 1.
  },
});

// Your own directions reach your script as the film comes to them.
axxStage.on('direction', (d) => { if (d.name === 'confetti') throwConfetti(); });
```

Draw from the state you are given, not from a clock of your own: the film asks the stage for the picture of each moment, in any order.

A project can add pictures of its own too, shown with `[show ...]` beside the pack's, and a pack can share them. [Build a demo's scenes](/guides/build-demo-scenes/) has the steps and the reference of what a scene is given.

## For agents

An agent makes a demo of what it built with the same commands. `axx demo <file> --json` reports the films it made and where they are, so the agent can hand the film in with its work.
