---
title: Run tests from an agent's sandbox
description: Let an agent whose sandbox keeps it from Docker run acceptance tests on clean services. You start axx host on your machine; each run the agent starts gets its services from it, and runs only what you approved.
---

Some agent harnesses run the agent in a sandbox that may not use Docker, because the Docker socket would let it out of the sandbox. Its `axx run` then cannot start your services, and you end up running the tests yourself and pasting the output back. `axx host` starts the services on your machine for the agent's runs, without giving the agent that way out.

## How it works

- You start `axx host` in the project, on your machine, outside the sandbox. Your editor can start it for you ([below](#start-it-from-your-editor)). It listens on a loopback port and writes `.axx/host.json`.
- The agent runs `axx run` as it always does. Axx finds `.axx/host.json` in the project folder you share with the sandbox, and asks the host for the services the run needs.
- The host cleans up anything an earlier run left behind, then starts the services as `axx.yaml` says: `docker compose ... up --build`, so the agent's changes are built. It waits until they are ready and sends their output back to the run's `.axx/logs/services.log`.
- The scenarios run in the sandbox, under its rules: every request a step sends, every browser it opens.
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

## Start it from your editor

The [VS Code extension and the IntelliJ plugin](/guides/set-up-your-editor/#the-axx-host) can start the host when you open the project, and stop it when you close it:

- An approval is a notification with **Show Diff**, **Approve** and **Refuse**, and the diff opens in the editor's diff view. It works as the terminal's question does: it stays open until you answer. Only a click on that notification approves. Nothing else in the editor can, including an agent's editor tools.
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
- `.axx/host.json` holds a token, readable only by your user. The agent, which runs as you, reads it; other users' processes can't.
- Approvals come only from you: from the host's terminal, or from the editor that started the host. Never through the port.
- The app's code, which the agent writes, runs in containers on your machine, with what the approved compose file gives those containers. Containers can reach the network unless the compose file keeps them from it, for example with an `internal: true` network.
