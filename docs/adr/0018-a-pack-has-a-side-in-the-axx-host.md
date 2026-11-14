# 0018: A pack has a side in the axx host, and web-core's starts the browser a sandbox keeps from starting

- Status: Accepted
- Date: 2026-11-14

## Context

An agent's harness runs `axx run` in a sandbox. The axx host (`axx host`) already starts a
project's services for such runs, outside the sandbox, by a definition the person approved. The
scenarios themselves run in the sandbox, and so did everything a pack starts for them.

On macOS that stopped working for browsers. A sandbox of the kind agent harnesses use (Seatbelt,
with everything refused that is not listed) keeps Chromium from starting, one refusal after
another:

- **Its lock.** Chromium makes a lock in macOS's per-user temp folder, which the sandbox does not
  let it write. axx 0.6.9 tells Chromium to use axx's own temp folder (`MAC_CHROMIUM_TMPDIR`).
- **A read.** The full Chromium asks macOS whether the Mac is a virtual machine
  (`kern.hv_vmm_present`), and quits when the sandbox refuses to answer. axx 0.6.9 runs
  Playwright's headless shell there, which does not ask.
- **Its service.** Chromium on macOS reaches its own child processes (the pages' renderers, the
  GPU process, the network process) through a service it registers with macOS
  (`bootstrap_check_in` of `org.chromium.Chromium.MachPortRendezvousServer.<pid>`). The sandbox
  refuses the registration, and Chromium has no other way: the full browser and the headless
  shell end alike. Nothing axx starts Chromium with avoids it. `--single-process` does, and runs
  every page and every frame in one process, which is not the browser people use.

The owner ruled out the two ways around that belong to someone else: asking a person to loosen
their harness's sandbox, and running the tests outside it. A harness's sandbox stays as it is,
and axx works inside it.

Going through what every pack starts showed the same wall elsewhere: iOS simulators
(CoreSimulator over XPC), Mac apps (Accessibility, screen capture, LaunchServices), an Android
emulator axx would boot. So the answer for browsers must be one other packs can take.

## Decision

**1. A pack can have a side in the axx host.** A pack that implements `core.HostServer` serves, in
the host, connections its side in a run opens with `Suite.HostDial`. The host checks the run's
token, as for a run's services, and hands the connection to the pack. Over it the two sides speak
the pack's own protocol. The core knows nothing of browsers: it carries the connection, and shows
in the host's terminal and in the editors what a pack says it does for a run (`HostRun.Tell`).
`axx host` starts for a project that has such a pack, with or without services.

**2. A pack's host side gives a run only what its sandbox would let it have.** The run is an
agent's. Nothing it sends is run as a command, names a file of the machine to read or write, or
reaches an address its sandbox keeps from it. Each pack with a host side says how it keeps that,
and its tests show it.

**3. web-core's host side starts the browser.** When a host serves the project, a headless run's
browser is one the host starts: Playwright's own Chromium, Firefox or WebKit, or the Chrome or
Edge installed, with the host's options. The run names an engine, and nothing else of how the
browser starts. What stands between the run and the machine:

- **Its network goes through the run** (`packs/web/internal/relay`). The browser's proxy is a
  loopback port of the host that reads nothing: it joins each of the browser's connections to a
  connection the run opened to the host. The run's side is the proxy. It opens what the browser
  asks for from inside the sandbox, through the proxy the sandbox gives its processes
  (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`). So the browser reaches what the sandbox
  lets the run reach, the machine's own loopback addresses included, and the harness's rules go
  on deciding it. There is no list of addresses to keep in `axx.yaml`.
- **Playwright's server gets the web steps' commands and no other** (`packs/web/internal/guard`).
  The run's Playwright connects to the guard, never to Playwright's server. Playwright's server,
  started with `launch-server`, already refuses a connected client to start a program or to name
  a file to upload. It still lets one save a download at a path, record to a path, open a file as
  a page, and send the browser raw DevTools commands that read and write files. So the guard
  passes a command only when the rules name it and every parameter in it, to any depth, and
  checks what reaches the machine: a page opens `http`, `https`, `about:blank`, `data:` and
  `blob:` addresses; a DevTools session sends the commands the rules name; a video is kept in the
  server's own folder; an upload has a name in the server's own folder.
- **The rules are recorded, and refuse by default** (`rules.json`). They are the commands the web
  packs' own tests send, run through the host. A command or a parameter that a later Playwright
  adds is refused until someone looked at it and recorded it.
- **Files travel through the run.** What a scenario uploads, the run reads in the sandbox and
  sends. What it downloads, records or traces, the run takes from Playwright's server and writes
  in the project. The server keeps them in a folder of its own, which goes when the browser does.
- **Only the guard reaches Playwright's server.** The server listens on a loopback port, at an
  address made for the run. The host gives the server its options, the address among them,
  through its standard input, so the address is in no file: a run's agent is the same user, and
  reads that user's files. (Windows has no file that is a process's standard input: there the
  options are in a file until the server read them.)
- **A page gets none of the machine's devices.** A headless browser has a clipboard of its own,
  which a test shows. Chromium and Firefox start with stand-ins for a camera and a microphone,
  and with WebRTC kept to the proxy.
- **An engine is hosted only where all of its network goes through the run.** A test opens the
  same site by a name only the run's side knows, which must load, and by its address on the
  machine, which the run's side refuses and must not load. Chromium and Firefox pass it
  everywhere, and WebKit on Linux. On macOS, WebKit sends requests to the machine's own
  addresses itself, whatever its proxy (CFNetwork does), so the host starts no WebKit on a Mac:
  the run starts its own, in the sandbox, as without a host.
- **A host starts sixteen browsers at once, and no more.**

**4. Without a host, nothing changes.** A run starts its browsers itself, with the lock and the
headless shell of 0.6.9. In a sandbox that still keeps the browser from starting, the error says
to start `axx host`. Runs a person watches or pauses start their browsers themselves too: their
windows are that person's, who runs outside a sandbox.

## Consequences

- A person with `axx host` running has web scenarios work in the sandbox, with nothing to set up
  and nothing in `axx.yaml`. The scenarios run on the full Chromium, as they do outside.
- What the sandbox keeps a run from reaching, it keeps the browser from reaching. A page that
  loads a font from a site the sandbox refuses loads without it, as a request step to that site
  fails.
- WebKit scenarios on a Mac run on a WebKit the run starts in the sandbox, where the sandbox lets
  one start, and outside the sandbox where it does not.
- A tool that drives the browser itself cannot, through the host: Lighthouse attaches to
  Chromium's remote debugging port, which would give a run the DevTools commands the guard
  refuses. Its scenarios say so, and run outside the sandbox.
- A pack that builds on web-core's public context and sends Playwright a command the web packs do
  not use is refused it through the host, with the command's name. Its tests, run through the
  host, record what it needs.
- Upgrading Playwright means running the web packs' tests through the host, and looking at each
  command the rules lack before recording it.
- The host and its runs must be the same axx: the protocol between them is now 3.
- Devices and desktop apps can take the same connection. What they give a run is different, an
  app the agent built that runs on the device or the machine, and each needs its own answer to
  decision 2 before it is built.

## Alternatives

- **Loosen the sandbox** (allow Chromium's service, or run axx outside it). Not ours to ask.
- **`--single-process`.** It starts, and it is another browser: no site isolation.
- **A list of addresses in `axx.yaml` that the host's browser may open.** It works only if the
  person approves every change to it, as for a service, and it leaves every project a list to
  keep. Sending the network through the run needs neither, and cannot allow more than the
  sandbox does.
- **Hand the run Playwright's server as it is.** Playwright says its server is no security
  boundary, and its protocol shows it: `Artifact.saveAs` with a path, `recordHar` with a path,
  `harStart` with `harPath`, a DevTools session.
- **A sandbox of axx's own around the host's browser** (a Seatbelt profile that keeps it from the
  person's files). It would hold whatever the protocol does, on macOS only, and it is the kind of
  profile that broke Chromium three times here. It can be added behind the guard later.
- **Run the web steps in the host.** Packs that build on web-core's public context call
  Playwright themselves, in the run. They would have to run in the host too, and a project's own
  packs are code an agent writes.
