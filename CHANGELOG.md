# Changelog

## [0.2.3](https://github.com/nimbusxr/axx/compare/v0.2.2...v0.2.3) (2026-10-08)


### Features

* put files in a folder, empty it, and check that a file is gone or the folder is empty ([#112](https://github.com/nimbusxr/axx/issues/112)) ([8f21e18](https://github.com/nimbusxr/axx/commit/8f21e18835d7a4c00eb8e3f5c919be5b535ce007))


### Bug Fixes

* **depot-desk:** the expected parcels are on the page before the day loads ([#116](https://github.com/nimbusxr/axx/issues/116)) ([caf87ec](https://github.com/nimbusxr/axx/commit/caf87ec17745931d3e0dce6feb8c0668b0f271c1))
* desktop steps find a control again when the app draws it anew, and Linux gives a drag time ([#113](https://github.com/nimbusxr/axx/issues/113)) ([6f23ddf](https://github.com/nimbusxr/axx/commit/6f23ddf1277ec9527f8d20db67241ae0f2bbdd13))
* system apps read only their scenario's windows, and no panic on Linux ([#110](https://github.com/nimbusxr/axx/issues/110)) ([67b0dd7](https://github.com/nimbusxr/axx/commit/67b0dd787cbee7e01579ef0613df18a15fc75302))

## [0.2.2](https://github.com/nimbusxr/axx/compare/v0.2.1...v0.2.2) (2026-10-07)


### Features

* places from a control's middle or corners, tray apps, apps the system runs, and Snap on every desktop ([#108](https://github.com/nimbusxr/axx/issues/108)) ([7fb6dc9](https://github.com/nimbusxr/axx/commit/7fb6dc9fed30b3c94191c8e468bf18f536072041))

## [0.2.1](https://github.com/nimbusxr/axx/compare/v0.2.0...v0.2.1) (2026-10-06)


### Features

* Wayland desktops, saved images compared with screenshots, and the depot desk on every build ([#106](https://github.com/nimbusxr/axx/issues/106)) ([01fdbd9](https://github.com/nimbusxr/axx/commit/01fdbd9bfcc87d6ed4476b69979781151b3d2341))

## [0.2.0](https://github.com/nimbusxr/axx/compare/v0.1.14...v0.2.0) (2026-10-05)


### ⚠ BREAKING CHANGES

* desktop apps through the accessibility tree, and one vocabulary for every app ([#104](https://github.com/nimbusxr/axx/issues/104))

### Features

* desktop apps through the accessibility tree, and one vocabulary for every app ([#104](https://github.com/nimbusxr/axx/issues/104)) ([6169326](https://github.com/nimbusxr/axx/commit/6169326c9db4eb3f5b728ac7a03e7c40ad96eb76))


### Bug Fixes

* **deps:** update google.golang.org/genproto digest to 8a89bd6 ([#99](https://github.com/nimbusxr/axx/issues/99)) ([ce2a8ce](https://github.com/nimbusxr/axx/commit/ce2a8ce809e9ef23cdd5293317a20b5e27d630f6))

## [0.1.14](https://github.com/nimbusxr/axx/compare/v0.1.13...v0.1.14) (2026-10-04)


### Bug Fixes

* warn about scenarios that check several behaviors in turn, suggest moved ordinals, and make iOS scenarios independent of each other and of boot timing ([#97](https://github.com/nimbusxr/axx/issues/97)) ([9385e98](https://github.com/nimbusxr/axx/commit/9385e98a5a88a6dea89cdee9ac6e88b90cc76173))

## [0.1.13](https://github.com/nimbusxr/axx/compare/v0.1.12...v0.1.13) (2026-10-03)


### Bug Fixes

* leaner lookups for agents: written-out steps, several steps per call, skills and MCP tools that point to axx steps and the run ([#94](https://github.com/nimbusxr/axx/issues/94)) ([0d3c3e0](https://github.com/nimbusxr/axx/commit/0d3c3e0728972ab5c3186c2094cfaa27948ddea0))

## [0.1.12](https://github.com/nimbusxr/axx/compare/v0.1.11...v0.1.12) (2026-10-03)


### Features

* leaner for agents: a step catalog, brief searches, one check per run, and payload objects created as needed ([#92](https://github.com/nimbusxr/axx/issues/92)) ([80a4d00](https://github.com/nimbusxr/axx/commit/80a4d00c7b91c02bf734a5273ccfc5fd6a75effa))

## [0.1.11](https://github.com/nimbusxr/axx/compare/v0.1.10...v0.1.11) (2026-10-03)


### Bug Fixes

* warn about stale selections, lint rules that find nothing, and services used before they are registered ([#90](https://github.com/nimbusxr/axx/issues/90)) ([36c70bc](https://github.com/nimbusxr/axx/commit/36c70bcda6e56bd6a5000832c2640081e2d1337a))

## [0.1.10](https://github.com/nimbusxr/axx/compare/v0.1.9...v0.1.10) (2026-10-03)


### Bug Fixes

* polling selections fail when the rows never come, and what agents stumbled on ([#88](https://github.com/nimbusxr/axx/issues/88)) ([023ef05](https://github.com/nimbusxr/axx/commit/023ef057803e0da5760f3121021d57d845d0084f))

## [0.1.9](https://github.com/nimbusxr/axx/compare/v0.1.8...v0.1.9) (2026-10-03)


### Features

* a step's table may start with a row naming its columns (two columns or more); `axx steps show` and the MCP steps show the columns ([6a290f5](https://github.com/nimbusxr/axx/commit/6a290f5db0b4a0d18fa6bfe8234245a7a66b23de))
* axx env and axx config show, as the MCP tools; axx steps explain is axx explain ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))
* axx validate and axx lint hint at scenarios that check several things in turn ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))
* **mcp:** the instructions ask for one scenario per criterion and services registered first; steps_search returns 6 steps by default without variants ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))


### Bug Fixes

* `axx steps show` suggests the ids that start like a guessed id, or the steps its words find ([6a290f5](https://github.com/nimbusxr/axx/commit/6a290f5db0b4a0d18fa6bfe8234245a7a66b23de))
* **kafka:** an event check whose JSONPath is not in the payload names the payload's properties ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))
* **rest:** setting a payload property inside a missing one names what is missing and how to get it, not only PathNotFoundException ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))
* **sql:** a seed refused for an unknown column or table names the table's columns or the schema's tables ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))
* the REST request numbering findings give the exact step to write ([b4ee113](https://github.com/nimbusxr/axx/commit/b4ee113ad6aeece0083942c1870299782efdfca2))

## [0.1.8](https://github.com/nimbusxr/axx/compare/v0.1.7...v0.1.8) (2026-10-02)


### Bug Fixes

* `axx --version` prints what `axx version` prints ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))
* `axx skills install` in a project with no packs fails with AXX-E0015 and says how to get them (`axx init`, `axx pack add` or `--scope user`) ([d1b8293](https://github.com/nimbusxr/axx/commit/d1b8293647c263550a39a60c098b78e0fc6325b7))
* `axx steps list` is `axx steps` ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))
* `axx steps show` takes an id, an expression (or part of one) or a step line, and AXX-E0310 names the closest steps ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))
* a missing service's error names the step that registers one (core.Services.RegisteredBy) ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))
* **docs:** /packs/&lt;pack&gt;, /reference/... and /guides/&lt;topic&gt; lead to their pages, with Markdown twins at .md ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))
* hygiene's gitleaks scan covers what git would commit, not ignored build output ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))
* **rest:** the payload-property steps' content-type error names the response it read ([d1b8293](https://github.com/nimbusxr/axx/commit/d1b8293647c263550a39a60c098b78e0fc6325b7))
* the AGENTS.md section `axx init` writes holds without the skills installed ([1d46b1a](https://github.com/nimbusxr/axx/commit/1d46b1a1d24e5202f768255c9db5152de60b0ee5))

## [0.1.7](https://github.com/nimbusxr/axx/compare/v0.1.6...v0.1.7) (2026-10-01)


### Features

* **init:** mark the installed skills as generated in .gitattributes ([#75](https://github.com/nimbusxr/axx/issues/75)) ([04ff7c8](https://github.com/nimbusxr/axx/commit/04ff7c8550de1277142a22008cf6b604ec3f0876))
* **lsp:** the server loads a project's steps again when its axx.yaml, axx-packs.yaml or a pack of its own changes ([e0d4e75](https://github.com/nimbusxr/axx/commit/e0d4e756868def635341ebba88e8868ee37eac80))


### Bug Fixes

* **lsp:** every axx project in the editor gets the steps of its own packs ([e0d4e75](https://github.com/nimbusxr/axx/commit/e0d4e756868def635341ebba88e8868ee37eac80))

## [0.1.6](https://github.com/nimbusxr/axx/compare/v0.1.5...v0.1.6) (2026-10-01)


### Features

* --profile takes several profiles, merged in order (--profile ios,watch) ([72465a9](https://github.com/nimbusxr/axx/commit/72465a97bc3f84d4db7488b00ab061371203edff))
* active.uses starts an app for the scenarios that use one of its packs ([72465a9](https://github.com/nimbusxr/axx/commit/72465a97bc3f84d4db7488b00ab061371203edff))
* axx run --watch shows what scenarios do, browsers and devices alike, one at a time (run.watch, run.slowdown) ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **core:** Scenario.Hold stops a step's timeout while it waits on what the scenarios share ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **core:** Suite.Watching tells packs a person watches the run ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **intellij:** the axx wordmark as the plugin's icon ([bf937f8](https://github.com/nimbusxr/axx/commit/bf937f87e3e7ff0118ba68989a56f2a703d8da98))
* **intellij:** Watch watches devices too ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-android:** emulators axx starts open Chrome without its first-run screens or prompts ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-android:** the host ports row: ports of this machine the app reaches as localhost ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-ios:** a watched run's simulators show in Device Hub ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile-ios:** packs.mobile-ios.keep keeps simulators booted for the next run ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* **mobile:** a watched run pauses after each mobile action ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))
* run.uses picks the scenarios that use one of its packs: a profile per platform, no tags ([72465a9](https://github.com/nimbusxr/axx/commit/72465a97bc3f84d4db7488b00ab061371203edff))
* **vscode:** Watch watches devices too ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))


### Bug Fixes

* a feature file named on the command line or in an editor runs whatever run.tags leaves out of a whole run ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* a file:line outside every scenario is an error (AXX-E0204), not a run of nothing ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* a run cleans up what a run stopped by force left behind, as axx down does ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* axx processes preparing the same packs share one build instead of breaking each other's ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **intellij:** a relative axx executable resolves from the project for runs too ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile-android:** other apps' error and not-responding dialogs stay off the app ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile-ios:** Appium starts before WebDriverAgent, with time for a busy machine ([#72](https://github.com/nimbusxr/axx/issues/72)) ([b45e5c0](https://github.com/nimbusxr/axx/commit/b45e5c052add33c4fbc6ceca4317e31e1b3d36ee))
* **mobile-ios:** Device Hub (or Simulator) stays open while scenarios run ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile-ios:** list items are found on iOS 27 ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))
* **mobile:** waiting for a device does not count against the step timeout ([4d4598d](https://github.com/nimbusxr/axx/commit/4d4598d5ddb00a4f6d8e8106209c9cb6be746143))


### Performance Improvements

* **mobile-ios:** WebDriverAgent starts once per simulator, not once per scenario ([bc154bf](https://github.com/nimbusxr/axx/commit/bc154bf554bde7e229b77cb21cc2803ccf2b8adc))

## [0.1.5](https://github.com/nimbusxr/axx/compare/v0.1.4...v0.1.5) (2026-09-30)


### Features

* ${token:&lt;name&gt;} stands for a scenario's token in the websocket, sse and cli packs' values ([44a0e86](https://github.com/nimbusxr/axx/commit/44a0e8693c451f3e0ea25b6b6b094d1c0f1d2ced))
* **a2a:** send messages to an A2A agent over JSON-RPC, HTTP+JSON or gRPC, and check its card, its tasks, their artifacts and its streams ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **amqp:** the amqp pack sends messages to AMQP queues, publishes them to exchanges with a routing key, and checks what services send there, over AMQP 0-9-1 (RabbitMQ) or 1.0 (Artemis) ([57f904d](https://github.com/nimbusxr/axx/commit/57f904d4e6f19ba6746fb42dc1e326e25b20c42f))
* an asyncapi row on Kafka services, AMQP, MQTT and NATS brokers, AWS accounts, Google Cloud projects, Service Bus namespaces, WebSockets and event streams ([829a021](https://github.com/nimbusxr/axx/commit/829a021d3aac5ac6d40d2256900e407e856e3dee))
* **asyncapi:** check the messages scenarios send and checks find against AsyncAPI 2.x and 3.x documents, with validation levels ([829a021](https://github.com/nimbusxr/axx/commit/829a021d3aac5ac6d40d2256900e407e856e3dee))
* **cli:** the cli pack runs commands, such as an admin command or a command-line tool, and checks their exit code, output and error output ([750258c](https://github.com/nimbusxr/axx/commit/750258c6c04ade1a2806bfd83eafab448ef20c5c))
* **core:** packs say what they need of the machine, and axx doctor checks it ([e1bbbfc](https://github.com/nimbusxr/axx/commit/e1bbbfc93e7b23589ca33cd10a5df62c1aa063d2))
* **fixtures:** factories build messages from the payload of an AsyncAPI message (asyncapi.yaml#/components/messages/Name) ([829a021](https://github.com/nimbusxr/axx/commit/829a021d3aac5ac6d40d2256900e407e856e3dee))
* **graphql:** send queries, mutations and subscriptions to GraphQL services, a federated graph's gateway or a subgraph alone, checked against their schemas, with validation levels ([754be6a](https://github.com/nimbusxr/axx/commit/754be6a211c6934f0bb8caf4c566738adeeb84a5))
* **grpc:** call gRPC services, unary and server-streaming, and check their status, answers, metadata and streamed messages, read with their protos or reflection ([4df5051](https://github.com/nimbusxr/axx/commit/4df50518702957fb26ecd837eb875beccef1efe1))
* **jsonrpc:** call JSON-RPC 2.0 services and check their results and errors, against their OpenRPC documents, with validation levels ([4df5051](https://github.com/nimbusxr/axx/commit/4df50518702957fb26ecd837eb875beccef1efe1))
* **mail:** the mail pack checks the emails services send, read from Mailpit, or over POP3 or IMAP, by recipients, subject, text, HTML, attachments and headers ([010a66e](https://github.com/nimbusxr/axx/commit/010a66e9d0ba2cb57753cc4963eee97091d6c508))
* **mcp:** call an MCP server's tools as an assistant does, over stdio or streamable HTTP, with the tools' schemas as the contract; read its resources and prompts ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **mobile-android:** Android apps on emulators axx starts, through Appium it downloads on first use: a device of its own and a clean app for each scenario ([965d2ca](https://github.com/nimbusxr/axx/commit/965d2cad2c7e94e0e4fe1708d55a6e0c0b3c54f1))
* **mobile-android:** axx doctor checks the Android SDK, its devices and KVM ([e1bbbfc](https://github.com/nimbusxr/axx/commit/e1bbbfc93e7b23589ca33cd10a5df62c1aa063d2))
* **mobile-core:** native mobile apps, used as people use them: launched or opened with a deep link, their controls tapped and filled by the names people see, what they show checked, the dialogs the system shows answered, screenshots compared ([965d2ca](https://github.com/nimbusxr/axx/commit/965d2cad2c7e94e0e4fe1708d55a6e0c0b3c54f1))
* **mobile-core:** the notification check works on iOS too (Notification Center), and dialogs are answered until they are gone ([8ee1a08](https://github.com/nimbusxr/axx/commit/8ee1a0893c112fd962296faf9a2315f2bf8a667b))
* **mobile-ios:** axx doctor checks Xcode and its iOS runtimes ([e1bbbfc](https://github.com/nimbusxr/axx/commit/e1bbbfc93e7b23589ca33cd10a5df62c1aa063d2))
* **mobile-ios:** iOS apps on simulators axx makes, through Appium and its prebuilt WebDriverAgent, downloaded on first use: a simulator of its own (a clone of one set up once) and a clean app for each scenario, the keychain reset too ([8ee1a08](https://github.com/nimbusxr/axx/commit/8ee1a0893c112fd962296faf9a2315f2bf8a667b))
* **mock:** check that the requests a service sent are signed, in a header or as Standard Webhooks ([44a0e86](https://github.com/nimbusxr/axx/commit/44a0e8693c451f3e0ea25b6b6b094d1c0f1d2ced))
* **mock:** check the tools a service called on a mocked MCP server, and the messages it sent a mocked A2A agent ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **mock:** check what a service asked a mocked model: the texts it was asked about, what a request sent and never sent, the tools offered and the schema asked for ([235dd7b](https://github.com/nimbusxr/axx/commit/235dd7b128098627dd13d6b51da407eb096eb0d1))
* **mqtt:** the mqtt pack publishes messages to MQTT topics and checks what services publish there, topic filters included, over MQTT 5 ([57f904d](https://github.com/nimbusxr/axx/commit/57f904d4e6f19ba6746fb42dc1e326e25b20c42f))
* **nats:** the nats pack publishes messages to NATS subjects and checks what services publish on subjects and into JetStream streams ([57f904d](https://github.com/nimbusxr/axx/commit/57f904d4e6f19ba6746fb42dc1e326e25b20c42f))
* **redis:** the redis pack seeds keys into Redis and its forks (Valkey, Dragonfly, KeyDB, Garnet) and checks the keys services write, by value, JSON properties or absence ([010a66e](https://github.com/nimbusxr/axx/commit/010a66e9d0ba2cb57753cc4963eee97091d6c508))
* **rest:** sign requests as webhooks are signed, in a header or as Standard Webhooks, and authorize them with JSON Web Tokens or OAuth 2.0 client credentials tokens ([44a0e86](https://github.com/nimbusxr/axx/commit/44a0e8693c451f3e0ea25b6b6b094d1c0f1d2ced))
* **sse:** the sse pack opens server-sent event streams and checks their events by type, id and JSON data ([750258c](https://github.com/nimbusxr/axx/commit/750258c6c04ade1a2806bfd83eafab448ef20c5c))
* **websocket:** the websocket pack opens WebSocket connections, sends messages on them, and checks what they receive and the code they close with ([750258c](https://github.com/nimbusxr/axx/commit/750258c6c04ade1a2806bfd83eafab448ef20c5c))
* **wiremock:** the axx-wiremock image mocks AI models in the format of each request (OpenAI's API and the servers that speak it, Anthropic, Gemini, Bedrock, Ollama), with streams, provider-shaped failures and deterministic embeddings ([235dd7b](https://github.com/nimbusxr/axx/commit/235dd7b128098627dd13d6b51da407eb096eb0d1))
* **wiremock:** the axx-wiremock image mocks an MCP server from its description and an A2A agent from its card ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **wiremock:** the axx-wiremock image mocks GraphQL services and federated subgraphs field by field from their schema, with _service and _entities ([754be6a](https://github.com/nimbusxr/axx/commit/754be6a211c6934f0bb8caf4c566738adeeb84a5))
* **wiremock:** the axx-wiremock image mocks gRPC services with WireMock's gRPC extension, when its root has a grpc folder ([4df5051](https://github.com/nimbusxr/axx/commit/4df50518702957fb26ecd837eb875beccef1efe1))


### Bug Fixes

* a message check no longer misses the last message of a stream or connection that ended at the same moment ([733a391](https://github.com/nimbusxr/axx/commit/733a391fadba232d94b2e0bb9e5c74c5f5c6e8f5))
* **cli:** on Windows, what a command left running has ended before its scenario's folder is removed ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **graphql,jsonrpc,grpc:** values built from a table keep a string's leading zeros, and build nested paths ([b44fcfc](https://github.com/nimbusxr/axx/commit/b44fcfc054642bd2ac8c70512fa1dab917ab3826))
* **mobile-android:** an emulator goes back to where it starts when a scenario names no location ([e1bbbfc](https://github.com/nimbusxr/axx/commit/e1bbbfc93e7b23589ca33cd10a5df62c1aa063d2))
* **mobile-android:** an emulator is ready once its network is up, not only booted ([8ee1a08](https://github.com/nimbusxr/axx/commit/8ee1a0893c112fd962296faf9a2315f2bf8a667b))
* **mobile-android:** emulators that start at once each get a console port, and one is ready once its network is up ([748e997](https://github.com/nimbusxr/axx/commit/748e9972827f61c4439299e4153c4fe518ead86e))
* **mobile-android:** the UiAutomator2 driver runs a brace-expansion without its denial-of-service flaws ([fea465b](https://github.com/nimbusxr/axx/commit/fea465bdd2400ecbe51f18b0ee4cbbf42e0b2233))
* **mobile-core:** a tap waits for a control that is still moving in, like a dialog's button ([8ee1a08](https://github.com/nimbusxr/axx/commit/8ee1a0893c112fd962296faf9a2315f2bf8a667b))
* **mobile-core:** a wait looks at least three times before it gives up ([748e997](https://github.com/nimbusxr/axx/commit/748e9972827f61c4439299e4153c4fe518ead86e))
* **mobile-ios:** notifications are found by asking for them, not by reading SpringBoard's screen ([748e997](https://github.com/nimbusxr/axx/commit/748e9972827f61c4439299e4153c4fe518ead86e))
* the downloads of Node.js, npm packages and WebDriverAgent are tried again when the network or the server fails ([8ee1a08](https://github.com/nimbusxr/axx/commit/8ee1a0893c112fd962296faf9a2315f2bf8a667b))
* **wiremock:** the axx-wiremock image downloads WireMock and its gRPC extension until their sha256 matches, instead of failing on one download that went wrong ([72a1e4a](https://github.com/nimbusxr/axx/commit/72a1e4a63f9d4e1e1b41895560a768809f5ddfda))

## [0.1.4](https://github.com/nimbusxr/axx/compare/v0.1.3...v0.1.4) (2026-09-28)


### Features

* **lint:** axx lint, axx validate and feature_validate hint at scenarios that check only a success status, or only that something did not happen ([3e36357](https://github.com/nimbusxr/axx/commit/3e3635784595d221f246fce7b14c2980140ee7ab))
* **mock:** check that no request went to a path, and that none of a path's requests have given query parameters, payload properties or form fields ([3e36357](https://github.com/nimbusxr/axx/commit/3e3635784595d221f246fce7b14c2980140ee7ab))


### Bug Fixes

* **deps:** update google.golang.org/genproto digest to b142276 ([#38](https://github.com/nimbusxr/axx/issues/38)) ([d90646b](https://github.com/nimbusxr/axx/commit/d90646bbfc25b01f440e447f4c69b77966026fbb))
* **install:** install the newest release whose files are up ([ea323a3](https://github.com/nimbusxr/axx/commit/ea323a35116ac81eb63b890a8b79bb22bb1d9f78))
* **rest:** a form payload that is not a JSON object says what one must be ([3e36357](https://github.com/nimbusxr/axx/commit/3e3635784595d221f246fce7b14c2980140ee7ab))

## [0.1.3](https://github.com/nimbusxr/axx/compare/v0.1.2...v0.1.3) (2026-09-27)


### Features

* **fixtures:** axx fixtures explain says where a generated value comes from ([#33](https://github.com/nimbusxr/axx/issues/33)) ([42b1d64](https://github.com/nimbusxr/axx/commit/42b1d64f185fd6ef903c83cae150ba895774e110))
* **mcp:** failure_context reads the latest run and its only failure when given no run ID or location ([dc0be8c](https://github.com/nimbusxr/axx/commit/dc0be8cd20f7dcac4086a8fb53c272d4c27a6a29))
* **mock:** check mocked requests by their path and their query parameters ([#31](https://github.com/nimbusxr/axx/issues/31)) ([26d9004](https://github.com/nimbusxr/axx/commit/26d90044f3fcbd60a55c97e222e85a02f0d215ed))


### Bug Fixes

* `axx docs export` documents the project's packs, custom packs included, and says why when there are none ([2824768](https://github.com/nimbusxr/axx/commit/2824768ebc624bbf1298916df1e677cdbdfe0c09))
* **deps:** update go.opentelemetry.io/otel to v1.46.0 ([65cdb6c](https://github.com/nimbusxr/axx/commit/65cdb6c793b5f73edfc706560b1633637dfab44e))
* **lint:** `--mode` applies to the built-in feature checks too ([2824768](https://github.com/nimbusxr/axx/commit/2824768ebc624bbf1298916df1e677cdbdfe0c09))
* **mcp:** a server started before packs were added says to restart it ([2824768](https://github.com/nimbusxr/axx/commit/2824768ebc624bbf1298916df1e677cdbdfe0c09))
* **web-core:** a click that opens a new browser tab waits for it, so the next step acts on that tab ([65cdb6c](https://github.com/nimbusxr/axx/commit/65cdb6c793b5f73edfc706560b1633637dfab44e))
* **web-core:** Chromium smooths text in grayscale, so a page's screenshots do not change from run to run (Linux screenshots with subpixel text are taken again once) ([22ff42d](https://github.com/nimbusxr/axx/commit/22ff42db8baa9e865d8f9fe88134bc3571d70b29))

## [0.1.2](https://github.com/nimbusxr/axx/compare/v0.1.1...v0.1.2) (2026-09-27)


### Features

* failed cleanups are kept, REST ordinals are linted, payloads come from files, mocks check bodies ([#23](https://github.com/nimbusxr/axx/issues/23)) ([e028593](https://github.com/nimbusxr/axx/commit/e0285934aa0b51fb3c2bc4c212c5b2625d8003bc))

## [0.1.1](https://github.com/nimbusxr/axx/compare/v0.1.0...v0.1.1) (2026-09-27)


### Features

* axx init connects the agents a repository uses, with a test-data skill ([#18](https://github.com/nimbusxr/axx/issues/18)) ([0508986](https://github.com/nimbusxr/axx/commit/0508986f05be50ae75dff03ce7f979b76a5222ae))
* factory JSON Schemas can reference local schema files ([#12](https://github.com/nimbusxr/axx/issues/12)) ([13b52c7](https://github.com/nimbusxr/axx/commit/13b52c7f60ee4f7e30785f1ee1a972b40877f886))
* **fixtures:** identity values are unique per field name, or per namespace ([#20](https://github.com/nimbusxr/axx/issues/20)) ([67617d3](https://github.com/nimbusxr/axx/commit/67617d39820137c47d2662a54b4e3a38e3e4ac53))
* web and files packs, with Playwright's tools speaking steps ([#3](https://github.com/nimbusxr/axx/issues/3)) ([89c3bb7](https://github.com/nimbusxr/axx/commit/89c3bb7c7cd50ee7badca796ac24261233130f56))


### Bug Fixes

* a fixture reference sees the document its fixture generates ([#11](https://github.com/nimbusxr/axx/issues/11)) ([9107b97](https://github.com/nimbusxr/axx/commit/9107b971319c9098742455caa0e0b96b59c318f3))
* a project's build of axx says which axx and packs it holds ([#15](https://github.com/nimbusxr/axx/issues/15)) ([7880959](https://github.com/nimbusxr/axx/commit/7880959fc506b24c34b0b3265dada27a23b97c43))
* a REST request's Header() changes the request it belongs to ([#16](https://github.com/nimbusxr/axx/issues/16)) ([fe54d43](https://github.com/nimbusxr/axx/commit/fe54d4354e4f869dd4aa63e2390e6f239bfeb64a))
* adoption shares only what every file has, and declares the identities chosen ([#17](https://github.com/nimbusxr/axx/issues/17)) ([d7de380](https://github.com/nimbusxr/axx/commit/d7de380ca3b4afd1fd1dd7674f9467db06cae021))
* axx up works in a project with packs ([#10](https://github.com/nimbusxr/axx/issues/10)) ([a6081f0](https://github.com/nimbusxr/axx/commit/a6081f02bb3ea1e9c3c05ccc20ccce4bfb0ba788))
* fixtures check passes a fresh clone and catches a stale manifest ([#13](https://github.com/nimbusxr/axx/issues/13)) ([012f839](https://github.com/nimbusxr/axx/commit/012f839963afe392bacf065665e3a4e47f451e8f))
* **gcp-firestore:** a seed's unquoted YAML dates are timestamps ([#14](https://github.com/nimbusxr/axx/issues/14)) ([b970c0e](https://github.com/nimbusxr/axx/commit/b970c0edfe33b0d77a7582579ba8cc8b657841b8))
* **skills:** the test-data skill says how identity values are compared ([#22](https://github.com/nimbusxr/axx/issues/22)) ([9182dba](https://github.com/nimbusxr/axx/commit/9182dba1c63ca33d2d34448b5e94a81e065b5bac))
* web-coverage takes a step's counts before the next step can close a tab ([#6](https://github.com/nimbusxr/axx/issues/6)) ([7b64a95](https://github.com/nimbusxr/axx/commit/7b64a95ce7c144262e1d94d89c16a2a4c43c8a42))

## 0.1.0 (2026-09-25)


### Features

* init commit ([63bf642](https://github.com/nimbusxr/axx/commit/63bf642d25367fbb7c5a0d19c3f877b54fae4ba1))
