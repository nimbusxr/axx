---
title: Run tests from an agent's sandbox
description: Let an agent whose sandbox keeps it from Docker, or keeps a browser, a simulator, an emulator or a desktop app from starting, run acceptance tests. You start axx host on your machine; each run the agent starts gets its services, its browser, its devices and its apps from it, and runs only what you approved.
---

Some agent harnesses run the agent in a sandbox. Three things then stop its `axx run`:

- **The sandbox may not use Docker,** because the Docker socket would let the agent out of the sandbox. The run cannot start your services.
- **The sandbox keeps a browser from starting.** On macOS, such a sandbox refuses what Chromium asks of the system as it starts, and Chromium quits. The run cannot open a page.
- **The sandbox keeps a simulator, an emulator or a desktop app from starting.** The iOS Simulator is run by a service of macOS that the sandbox keeps the run from. An Android emulator needs the machine's hypervisor. A desktop app needs the window server and the accessibility API. The run cannot start a phone or an app.

You end up running the tests yourself and pasting the output back. `axx host` starts the services, the browsers, the devices and the apps on your machine for the agent's runs. Services and browsers give the agent no way out of its sandbox. An app is different: it is the agent's code, and it runs outside the sandbox, so the host asks you about each app first ([below](#phones-and-desktop-apps-in-the-sandbox)).

## How it works

- You start `axx host` in the project, on your machine, outside the sandbox. Your editor can start it for you ([below](#start-it-from-your-editor)). It listens on a loopback port and writes `.axx/host.json`.
- The agent runs `axx run` as it always does. Axx finds `.axx/host.json` in the project folder you share with the sandbox, and asks the host for the services the run needs.
- The host cleans up anything an earlier run left behind, then starts the services as `axx.yaml` says: `docker compose ... up --build`, so the agent's changes are built. It waits until they are ready and sends their output back to the run's `.axx/logs/services.log`.
- The scenarios run in the sandbox, under its rules: every request a step sends, and every request a browser makes. The browsers of web scenarios are started by the host and driven by the run ([below](#browsers-in-the-sandbox)). So are the simulators, the emulators and the desktop apps of mobile and desktop scenarios ([below](#phones-and-desktop-apps-in-the-sandbox)).
- When the run ends, the host stops the services and runs their cleanup. It does the same when the run's connection ends, so a run that is killed leaves nothing behind.
- Runs take turns. While one has the services, the next waits and says which run it waits for.

```console
$ axx host
axx host: serving /Users/you/shop on 127.0.0.1:53123 (Ctrl-C stops it)
  approved: axx.yaml's services: api, db; compose.yaml
waiting for runs
run 1: axx run --compact features/orders
run 1: starting api, db
run 1: ready in 38.2s
run 1: 12 scenarios: 11 passed, 1 failed; stopped and cleaned up in 6.0s
```

In the sandbox, the run looks as it always does:

```console
$ axx run --compact features/orders
axx: asking the axx host (127.0.0.1:53123) for api, db
axx: the axx host started api, db (logs: .axx/logs/services.log)
```

Ctrl-C stops the host. A run that has the services at that moment loses them, and the host stops them and cleans up before it exits. The [command reference](/references/cli/axx-host/) lists its flags.

## What you approve

The host never runs a command that a run sends. A run names services, and the host runs what the definition you approved says. The definition is:

- `axx.yaml`'s services: their commands, cleanups, readiness checks and environment;
- each compose project they run, as `docker compose config` resolves it, with its includes, its `.env` and its variables;
- the project files their commands run, such as `./scripts/start.sh` or the `server.js` of `node server.js`.

A [run with coverage](/guides/measure-coverage/) starts the services differently: with their `coverage` sections, and the variables the coverage packs give them. So its definition is one of its own, which the host asks you about the first time a run with coverage needs it.

Starting the host approves the definition as it is. If the agent changes it, the next run waits for you:

```console
run 5: axx run --compact

run 5 (axx run --compact): the services' definition changed since you approved it
  --- approved
  +++ proposed
  @@ -23,6 +23,11 @@
           target: 8080
           published: "18080"
           protocol: tcp
  +    volumes:
  +      - type: bind
  +        source: /var/run/docker.sock
  +        target: /var/run/docker.sock
  +        bind: {}
   networks:
     default:
warning: services.api: compose service api mounts the Docker socket (/var/run/docker.sock): it controls Docker on this machine
approve? [y/N]
```

The warnings name what in the definition reaches past a container onto your machine:

- privileged containers, added capabilities and devices;
- your machine's network, process or other namespaces;
- mounts of files outside the project, and builds from outside it;
- files from outside the project read into a container's environment or secrets;
- the Docker socket;
- services that run on your machine outside any container. Code the agent changes then runs on your machine.

How you answer decides what happens next:

- **`y`**: the run goes on, and the approval is kept. Runs with that definition don't ask again.
- **`n`**, or Enter: the run fails with [`AXX-E0501`](/references/error-codes/#axx-e0501), which tells the agent you refused the change. The host refuses that definition again, without asking you, until it restarts, so an agent that runs again doesn't ask you again. A newer change is asked about.
- **No answer within a minute**: the run fails with [`AXX-E0507`](/references/error-codes/#axx-e0507), and the question stays open. Answer it when you can: the agent's next run has your answer. If the agent changes the definition again before you answer, the host asks about the newer one instead.

Lines about other runs are printed above the question, and the question is asked again below them. Anything typed before the question appears is dropped, so a stray `y` approves nothing. A host that doesn't run in a terminal, such as one started in the background, can't ask you: it says so as it starts, and refuses every change.

Changes to the app's own code need no approval: compose builds it and runs it in containers. A Dockerfile's steps run in build containers too, and what a build may use of your machine (its context, privileges, network) is set in the compose file, which the definition covers.

Approvals are kept in your own configuration directory, outside the project, where the agent cannot write them. Starting the host again doesn't approve anything new: if the definition changed while no host ran, the host asks you about it as it starts.

## Check the sandbox first

The host depends on one thing: the sandbox reaching your machine's loopback addresses. That's how the tests reach the services, and how Axx reaches the host. Check it once. Start a server in your terminal:

```sh
python3 -m http.server 8765
```

Then ask the agent to run `curl -sI http://127.0.0.1:8765/`. If it gets `HTTP/1.0 200 OK`, the sandbox reaches your machine. Databases and brokers don't speak HTTP, so check one of their ports too: start the services in your terminal (`docker compose up -d`) and ask the agent to run `nc -z -w 2 127.0.0.1 5432 && echo open`, with your service's port.

If the checks fail, the agent probably runs in a container, a virtual machine or a remote machine, which has loopback addresses of its own. `axx host` doesn't serve those.

The project folder the agent edits must be the one on your disk, not a copy: the host builds what the agent changed there.

## Browsers in the sandbox

When a host serves the project, it starts the browsers of web scenarios, outside the sandbox, and the run drives them from inside it. You set nothing up, and `axx.yaml` doesn't change: with `axx host` running, a web scenario in the sandbox runs as it does outside, on the full Chromium.

```console
$ axx host
axx host: serving /Users/you/shop on 127.0.0.1:53123 (Ctrl-C stops it)
  approved: axx.yaml's services: api, db; compose.yaml
  web-core: the browsers of web scenarios, for runs in a sandbox that keeps a browser from starting
waiting for runs
run 1: axx run --compact features/checkout
web-core: started chromium (axx run --compact features/checkout)
```

A project needs no services for this. `axx host` also starts in a project whose `axx.yaml` declares none, as long as it uses the web packs.

### What the run gets, and what it doesn't

A browser outside the sandbox could be a way out of it, since the agent writes the scenarios that drive it. So the host gives the run a browser, and nothing else of your machine:

- **The host starts the browser, with its own options.** It is Playwright's Chromium or Firefox, or the Chrome or Edge installed on your machine, without a window and with a profile of its own that is deleted when the run ends. The run says which engine it wants. It cannot name a program, an argument or a folder.
- **The browser's network goes through the run.** Every request the browser makes travels to `axx run` in the sandbox, which makes it from there, through the proxy the sandbox gives its processes if it gives one. So the browser reaches exactly what the sandbox lets the run reach, and your harness's rules go on deciding that. There is no list of addresses to keep.
- **`localhost` and the names under it are your machine.** A page at `http://shop.localhost:3000` opens as it does in a browser outside a sandbox: the run connects to your machine's own port without asking DNS or the proxy, which know no such name.
- **The browser opens no file of your machine.** A page opens `http` and `https` addresses, never a `file://` one, and never the browser's own settings pages.
- **Files travel through the run.** When a scenario uploads a file, the run reads it in the sandbox and sends its contents. Downloads, screenshots and traces go back to the run, which writes them in the project. A [video](/guides/watch-web-browsers/#videos) is made by the run itself, from the frames the pages send it: the first time, the run downloads Cisco's OpenH264 from `ciscobinary.openh264.org` into axx's cache. In a sandbox that keeps the run from that host or from the cache, the scenario says so and keeps no video. The browser itself reads and writes nothing outside a folder of its own, which is deleted with it.
- **The host passes only the commands the web steps use.** The connection between a run and Playwright can do more than a person at a browser can: save a file at any path, or send the browser raw DevTools commands. The host stands between the two and refuses every command the web packs' steps don't use.
- **The browser has a clipboard of its own.** A scenario that pastes doesn't get what you copied, and a scenario that copies doesn't replace it.

### What works differently there

- **A site the sandbox blocks is blocked for the browser too.** A page that loads a font or a script from it loads without it. A page at an address the run cannot reach shows "the run cannot reach" and the address. That is the same rule a request step meets.
- **[Lighthouse audits](/guides/web-lighthouse/) run outside the sandbox.** Lighthouse drives Chromium itself, through a port that takes every DevTools command, which the host doesn't open. A Lighthouse step in a sandboxed run says so.
- **WebKit on a Mac is started by the run, not by the host.** macOS sends WebKit's requests to your machine's own addresses itself, whatever proxy the browser has, so the host cannot send them through the run. The run starts WebKit in the sandbox, as it does without a host. If your sandbox keeps it from starting, run the WebKit scenarios outside the sandbox.
- **Watching or pausing a run starts the browsers where the run is.** Their windows are yours to look at. Watch the scenarios yourself, outside the sandbox: from your terminal, or from your editor ([Watch web browsers](/guides/watch-web-browsers/)).
- **A pack of your own that calls Playwright** through the web pack's context is refused the commands the web packs' steps don't use. The error names the command.

### Without a host

Without a host, the run starts its browsers itself, in the sandbox, and some sandboxes keep Playwright's full Chromium from starting:

- **Some sandboxes on macOS refuse a read the full Chromium makes as it starts.** Chromium asks macOS whether the Mac is a virtual machine, and it quits when the sandbox refuses to answer. Axx checks that read first. When the sandbox refuses it, Axx runs Playwright's headless shell instead, a smaller build of Chromium that doesn't make the read, and the run's log says so once:

  ```console
  WARN this sandbox refuses a read that the full Chromium makes as it starts (kern.hv_vmm_present): Playwright's headless shell runs the scenarios instead
  ```

- **The headless shell behaves a little differently from the full Chromium.** For example, it never asks a site for its icon.
- **The full Chromium makes a lock in macOS's per-user temp folder as it starts, whatever `$TMPDIR` says,** and some sandboxes keep it from writing there. Axx tells the full Chromium to use the temp folder Axx itself uses (`MAC_CHROMIUM_TMPDIR`), which the sandbox lets it write.
- **The headless shell is downloaded the first time a sandboxed run needs it,** into Playwright's folder of browsers ([The browsers](/guides/test-web-apps/#the-browsers)). If the sandbox can't write there, set `PLAYWRIGHT_BROWSERS_PATH` to a folder it can write.
- **A sandbox can still keep the headless shell from starting.** Chromium reaches its own processes through a service it registers with macOS, and a sandbox that refuses that ends every build of Chromium. The run then says to start `axx host`.

## Phones and desktop apps in the sandbox

When a host serves the project, it also starts what mobile and desktop scenarios run on, outside the sandbox:

- the iOS simulators of `mobile-ios`, and the apps on them;
- the Android emulators of `mobile-android`, and the apps on them;
- the Mac, Windows and Linux apps of `desktop-macos`, `desktop-windows` and `desktop-linux`.

You set nothing up, and neither `axx.yaml` nor the features change. With `axx host` running, a scenario in the sandbox registers its app as it always does, and the host does the rest with the packs' own code: it leases the device or claims the desktop, installs and resets the app, and starts it. A project needs no services for this.

### The host asks you about each app

A browser's page cannot leave the browser. An app can do whatever a program can, and the agent writes it. So the first time a run needs an app, the host asks you, and starts nothing until you answer:

```console
run 3: axx run --compact features/courier-app

axx run --compact features/courier-app asks to start mobile-ios: the courier app, in the iOS Simulator on this machine
  app: /Users/you/parcels/courier/ios/build/Debug-iphonesimulator/Courier.app
  simulator: iPhone 16
warning: an app in the iOS Simulator is a process of this Mac, as you: it reads and writes your files and reaches the network, past the sandbox of the agent that builds it
warning: the approval is for the app at this path, whatever is built there from now on
approve? [y/N]
```

- **What you see is everything of the app's registration:** the app, what it starts with, the device, and what the scenario puts on the device. If any of it changes, the host asks again and shows what changed.
- **The approval is for the app at that path, not for one build of it.** The agent changes the app's code and builds it again, and the next run starts what it built. That is the point of the tests. It also means that approving an app is trusting the agent's code with what the warning names.
- **Your answers work as they do for the services:** `y` is kept, `n` is refused until the host restarts, and no answer within a minute leaves the question open for the next run ([What you approve](#what-you-approve)). Approvals are kept in your configuration directory, outside the project, and are asked only in the host's terminal or in your editor.

### What each kind of app reaches

The warning under each question says what that app reaches. It differs by platform:

| The app runs | It reaches |
| --- | --- |
| in the **iOS Simulator** | Your Mac, as you. A simulator's app is a process of the Mac: it reads and writes your files and uses your network. |
| on an **Android emulator** | The network, from a virtual machine. It is not a process of your machine and does not read your files. It reaches the ports of your machine that its registration names (`host ports`), as its own `localhost`. |
| as a **Mac or Windows app** | Your machine, as you, on your own desktop. While a scenario has the desktop, the screen, the pointer and the keyboard are the scenario's. |
| as a **Linux app** | Your machine, as you, in a desktop of axx's own, not on yours. |

None of these apps is held to the sandbox's network rules. The browser's network goes through the run, and an app's does not: an emulator's proxy carries only part of its traffic, and a simulator's or a desktop's app uses the machine's network directly. If the agent must not reach an address, don't approve an app it builds.

### What the run can ask for

The app aside, the run gets nothing else of your machine:

- **A run asks for what a step does, by name.** "Start the courier app", "tap this control", "read the screen". The host has a fixed list of these, one for each thing a step does with a device or an app. A run cannot send a command, name a program, or pass an argument to one.
- **The host registers the app itself,** from the same rows the scenario's registration step has, and reads the pack's settings from the project's `axx.yaml`.
- **The files a run names must be the project's.** That is the app, and every file a registration or a step names: photos, documents, a picture or a scene for the camera. The project's files are those of its repository: the nearest folder at or above `axx.yaml`'s that has a `.git`, so an app built beside the acceptance tests is one of them. Without a repository, they are the files in `axx.yaml`'s own folder. The host settles this folder when you start it and says which it is, follows links before it decides, and refuses a file anywhere else. A desktop app can also be one your system finds by name, such as `com.apple.TextEdit`. The question shows that name.
- **Files the scenario gives an app go into the app's own folders:** its sandbox on a phone, its home under `.axx/desktop` in the project for a desktop app.
- **The run drives a phone's screen itself,** through the driver axx installs on the device (WebDriverAgent, the UiAutomator2 server), which listens on a loopback port of your machine. The driver taps, types and reads the screen, and does nothing else.
- **The host stops everything when the run ends,** or when its connection does: the apps, and the simulators and emulators it started for the run.

One limit is worth knowing. The host checks where a file is and then reads it, and those are two moments. An agent that swaps a file for a link between the two could have the host read a file outside the repository. The host would still do with it only what the step does: put it on a device as an app's photo or document, for an app you approved.

### What works differently there

- **A run you watch or pause starts its devices itself.** Their windows are yours to look at, where you started the run. Watch the scenarios outside the sandbox.
- **The model stubs work.** The packs that answer an app's models (`models-ios`, `models-android`, `models-macos`, `models-windows`, `models-linux`) prepare the app where it starts, in the host.
- **A Mac's permissions are the host's.** `axx host` runs under axx's launcher, as a run does, so macOS asks once for the launcher ([Test desktop apps](/guides/test-desktop-apps/)).
- **`axx doctor` in the sandbox** warns about what it cannot reach there, and says that the host does it. Run `axx doctor` in your own terminal for the machine's own answer.

## Start it from your editor

The [VS Code extension and the IntelliJ plugin](/guides/set-up-your-editor/#the-axx-host) can start the host when you open the project, and stop it when you close it:

- An approval has **Show Diff**, **Approve** and **Refuse**, and the diff opens in the editor's diff view: a notification in VS Code, and a dialog in front of IntelliJ IDEA. It works as the terminal's question does: it stays open until you answer. Only a click there approves. Nothing else in the editor can, including an agent's editor tools.
- An app's question is asked the same way. It names the run that asks, the app and what the app reaches, and **Show Diff** shows what the host would start.
- The agent's runs show in the test results as they run, scenario by scenario.
- The status bar shows what the host is doing: idle, a run's services starting or running, or waiting for your approval.

## Everyone's runs share the host

- Your own runs go through the host while it runs, whether you start them in a terminal or in your editor. All runs use the same ports and the same compose project, so they take turns.
- `axx up` asks the host for the services and keeps them until `axx down`. Runs in between reuse them, as they do without a host, and still take turns: a run waits for the run that is using them, not for `axx up`. That holds whichever side of the sandbox each command runs on: you can run `axx up` in your terminal, and the agent's runs in the sandbox reuse those services.
- With the [MCP server](/guides/set-up-agents/), `steps_try` gets services of its own from the host when none are up, and `scenarios_run` ends them first, so that every run starts clean. `env` works through `axx up` and `axx down`.
- `axx doctor` says when a host serves the project, and leaves the checks of the services' commands and of Docker to the machine the host runs on.
- If the host stops during a run, the run loses its services ([`AXX-E0502`](/references/error-codes/#axx-e0502)). A host that was killed leaves `.axx/host.json` behind. Runs then warn that it doesn't answer, and start the services themselves.

The host's errors are [`AXX-E0500` to `AXX-E0506`](/references/error-codes/#axx-host).

## What your security team should know

- The agent can reach one more loopback port. What it can send there is a run's request: names of services from `axx.yaml`, and the configuration file, profile and settings it runs with. The host reads the configuration from the project itself, and runs only a definition you approved.
- With web scenarios, the agent's run also drives a browser the host starts. The host chooses the program and its options. The browser's network goes through the run in the sandbox, so the sandbox's network rules apply to it. The host passes Playwright only the commands the web steps use, none of which names a file of the machine or runs a program ([above](#what-the-run-gets-and-what-it-doesnt)). Each browser listens on loopback ports of its own, behind addresses and keys made for that run.
- With mobile and desktop scenarios, the host starts the project's apps on your machine, outside the sandbox, each once you approved it. An app is code the agent writes. In the iOS Simulator, and as a Mac, Windows or Linux app, it runs as you, with your files and your network. On an Android emulator it runs in a virtual machine, with the network. The sandbox's rules apply to none of them, so the approval is the control: it names the app and everything it starts with, it is asked in the host's terminal or your editor, and it is asked again when any of that changes. A team that must hold the agent's code to the sandbox should not approve apps, and should run those scenarios in CI ([above](#phones-and-desktop-apps-in-the-sandbox)).
- Beyond the app, a run asks the host only for what a step does with a device or an app, from a fixed list. It names no program. The files it names must be in the project's repository, which the host settles as it starts, and it checks them after links are followed. The check and the read are two moments.
- `.axx/host.json` holds a token, readable only by your user. The agent, which runs as you, reads it; other users' processes can't.
- Approvals come only from you: from the host's terminal, or from the editor that started the host. Never through the port.
- The app's code, which the agent writes, runs in containers on your machine, with what the approved compose file gives those containers. Containers can reach the network unless the compose file keeps them from it, for example with an `internal: true` network.
