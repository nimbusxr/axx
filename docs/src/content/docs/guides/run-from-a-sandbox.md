---
title: Run tests from an agent's sandbox
description: Let an agent whose sandbox keeps it from Docker, or keeps a browser from starting, run acceptance tests. You start axx host on your machine; each run the agent starts gets its services and its browser from it, and runs only what you approved.
---

Some agent harnesses run the agent in a sandbox. Two things then stop its `axx run`:

- **The sandbox may not use Docker,** because the Docker socket would let the agent out of the sandbox. The run cannot start your services.
- **The sandbox keeps a browser from starting.** On macOS, such a sandbox refuses what Chromium asks of the system as it starts, and Chromium quits. The run cannot open a page.

You end up running the tests yourself and pasting the output back. `axx host` starts the services and the browsers on your machine for the agent's runs, without giving the agent a way out of its sandbox.

## How it works

- You start `axx host` in the project, on your machine, outside the sandbox. Your editor can start it for you ([below](#start-it-from-your-editor)). It listens on a loopback port and writes `.axx/host.json`.
- The agent runs `axx run` as it always does. Axx finds `.axx/host.json` in the project folder you share with the sandbox, and asks the host for the services the run needs.
- The host cleans up anything an earlier run left behind, then starts the services as `axx.yaml` says: `docker compose ... up --build`, so the agent's changes are built. It waits until they are ready and sends their output back to the run's `.axx/logs/services.log`.
- The scenarios run in the sandbox, under its rules: every request a step sends, and every request a browser makes. The browsers of web scenarios are started by the host and driven by the run ([below](#browsers-in-the-sandbox)).
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
- **The browser opens no file of your machine.** A page opens `http` and `https` addresses, never a `file://` one, and never the browser's own settings pages.
- **Files travel through the run.** When a scenario uploads a file, the run reads it in the sandbox and sends its contents. Downloads, screenshots, videos and traces go back to the run, which writes them in the project. The browser itself reads and writes nothing outside a folder of its own, which is deleted with it.
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

## Start it from your editor

The [VS Code extension and the IntelliJ plugin](/guides/set-up-your-editor/#the-axx-host) can start the host when you open the project, and stop it when you close it:

- An approval has **Show Diff**, **Approve** and **Refuse**, and the diff opens in the editor's diff view: a notification in VS Code, and a dialog in front of IntelliJ IDEA. It works as the terminal's question does: it stays open until you answer. Only a click there approves. Nothing else in the editor can, including an agent's editor tools.
- The agent's runs show in the test results as they run, scenario by scenario.
- The status bar shows what the host is doing: idle, a run's services starting or running, or waiting for your approval.

## Everyone's runs share the host

- Your own runs go through the host while it runs, whether you start them in a terminal or in your editor. All runs use the same ports and the same compose project, so they take turns.
- `axx up` asks the host for the services and keeps them until `axx down`. Runs in between reuse them, as they do without a host.
- With the [MCP server](/guides/set-up-agents/), `steps_try` gets services of its own from the host when none are up, and `scenarios_run` ends them first, so that every run starts clean. `env` works through `axx up` and `axx down`.
- `axx doctor` says when a host serves the project, and leaves the checks of the services' commands and of Docker to the machine the host runs on.
- If the host stops during a run, the run loses its services ([`AXX-E0502`](/references/error-codes/#axx-e0502)). A host that was killed leaves `.axx/host.json` behind. Runs then warn that it doesn't answer, and start the services themselves.

The host's errors are [`AXX-E0500` to `AXX-E0506`](/references/error-codes/#axx-host).

## What your security team should know

- The agent can reach one more loopback port. What it can send there is a run's request: names of services from `axx.yaml`, and the configuration file, profile and settings it runs with. The host reads the configuration from the project itself, and runs only a definition you approved.
- With web scenarios, the agent's run also drives a browser the host starts. The host chooses the program and its options. The browser's network goes through the run in the sandbox, so the sandbox's network rules apply to it. The host passes Playwright only the commands the web steps use, none of which names a file of the machine or runs a program ([above](#what-the-run-gets-and-what-it-doesnt)). Each browser listens on loopback ports of its own, behind addresses and keys made for that run.
- `.axx/host.json` holds a token, readable only by your user. The agent, which runs as you, reads it; other users' processes can't.
- Approvals come only from you: from the host's terminal, or from the editor that started the host. Never through the port.
- The app's code, which the agent writes, runs in containers on your machine, with what the approved compose file gives those containers. Containers can reach the network unless the compose file keeps them from it, for example with an `internal: true` network.
