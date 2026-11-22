# AGENTS.md: working on the axx codebase

axx (github.com/nimbusxr/axx, "axxeptance") is an open-source acceptance testing framework, the
behavioral verification layer for agentic software factories: it verifies running software against
executable acceptance criteria, black-box.

This file is for agents *contributing to axx*. If you are *using* axx in another repo, install
the skills instead: `axx skills install`.

## Layout

- `cmd/axx`: CLI entry point; it calls `app.Main`, as the builds with a project's packs do.
  `internal/cli`: cobra commands.
- `internal/{config,interp,feature,match,engine,runner,report,lifecycle}`: the core runner.
  `internal/check`: which steps of a feature cannot run (undefined, ambiguous, the wrong argument, a
  value the rule cannot read), reported through it by `axx validate`, `axx doctor`, the MCP server
  and the language server. `internal/lint`: `axx lint`, whose feature checks `axx validate` and
  `axx run` report as warnings. `internal/featurefmt`: the one layout of feature files, for
  `axx fmt` and the language server's formatting; it changes only where things sit, and refuses a
  layout that Cucumber's parser reads differently from the file. A test keeps the repository's
  features formatted: after editing one, run `go run ./cmd/axx fmt <file>`.
- `core`: the public core every pack builds on (scenario, steps, service registry, state,
  errors, interpolation, shared parameter types, and the rule every pack reads a feature's values
  by: `core/value.go`, ADR 0014, with `TableDoc.Values` saying what a step's table holds). A step's
  other wordings (`StepDef.Also`) run it and stay out of the lists, as do REST's payload property
  steps' forms with the ordinal first (`<id>.leading`).
- `fixtures`: the public package a pack makes data with: one fixture's values for an instance the pack
  names, deterministically, as `axx fixtures check` resolves them (`internal/fixtures/instance.go`).
  A spec's `${ name(args) }` calls an expression function: axx's own are for hashing and encoding only
  (`internal/fixtures/functions.go`; keep it so), and a project's come from its packs
  (`core.FixtureFunction`, `Manifest.FixtureFunctions`). A spec's bare `undefined` leaves a field out
  (ADR 0014): it stays in the tree until the fixture's values resolve, then `takeUndefined` takes it
  out and the families fill none of what it names (`removals`).
- `packs/*`: the step packs (rest, mock, sql, mongo, kafka, logs, files, faults, otel; `web/*`, `aws/*` and
  `gcp/*`: a core pack and the packs that build on it; `azure/*`), each with its public context. `internal/packset/catalog.go` lists them.
  Packs that build on another (`aws-s3` on `aws-core`, `web-screenshots` on `web-core`) declare it in
  `Requires` and use its public context (`packs/web/core/public.go`); the core stays free of any
  pack's specifics: packs get what they need through extension points in `core`. A pack's
  manifest gives its steps, parameter types, hooks, settings and `axx mcp` tools (`Tools`, which
  look at the scenario an agent keeps open with `steps_try`; `internal/runner/session.go`), and its
  command hooks (`CommandHooks`). A project's axx.yaml has its own (`hooks.commands`: a command of
  the project's). Both run before or after one of axx's commands, never inside it, on every command
  but those of `commandsWithoutPacks` (`internal/cli/commandhooks.go`). Keep it so: before and
  after, nothing more; another kind of hook gets its own group beside `hooks.commands`.
- `packs/sql/internal/h2`: axx's own client of H2's server, for database/sql, as H2 has no Go driver: H2's protocol from its source
  (`org.h2.value.Transfer`, `SessionRemote`, `TcpServerThread`), versions 17 to 21 (H2 1.4.197 to 2.5). The integration tests hold it
  to real servers of three releases (`h2Releases`): a new H2 release joins them, and a new version of the protocol means reading
  what H2's `Transfer` changed for it.
- `packs/sql/internal/db2`: axx's own client of Db2, for database/sql, as Db2's Go driver needs cgo: DRDA, the protocol Db2's
  servers speak, from the standard The Open Group publishes, for Db2 for Linux, UNIX and Windows with a user and a password
  (as they are, over TLS, or encrypted as the standard's mechanisms 7 and 9 do, with AES, for a server that asks for it:
  DRDA, Version 5, Volume 3, DHENC has the numbers, and Db2 refuses a request that names the key's length). It names itself to the server as its own product (`productID`), never as one of IBM's: Db2 answers
  IBM's products in layouts of their own, and any other in the standard's. Its tests replay sessions recorded from a real Db2
  (`testdata/*.drda.gz`; `AXX_DB2_RECORD` records them again), and the integration tests run the pack against IBM's Db2 Community
  Edition image (`db2Image`), whose license its `LICENSE=accept` accepts: read it before using the image for anything else.
- `packs/web/core` (`web-core`): drives Playwright through playwright-go, with the browsers on the machine that runs
  axx. It pins the Playwright version and its hash in `packs/web/internal/driver`, and runs it
  with the Node.js `internal/npm` pins; upgrading playwright-go means updating them
  (`TestPinnedPlaywrightVersion` fails until they agree). The driver it prepares is patched
  (`packs/web/internal/driver/patch.go`, `patches/*.js`) so that Playwright's Inspector, recorder
  and trace viewer speak the pack's steps: each patch has an anchor in the pinned playwright-core,
  so upgrading Playwright means checking every anchor; bump `patchRevision` whenever a patch changes.
  A scenario's video (`videos`) is the tab it acts on, from Playwright's screencast of each tab,
  with its steps beside it, made in the run as the desktop and mobile packs make theirs
  (`packs/web/core/video.go`, `packs/internal/recording`): Playwright's own video recording is
  not used, and the axx host's rules refuse it.
  For a run in a sandbox that keeps a browser from starting, the pack's side in the axx host
  (`hosted.go`, ADR 0018) starts the browser with the host's own options, and the run drives it:
  its network goes through the run (`packs/web/internal/relay`), and Playwright's server gets only
  the commands in `packs/web/internal/guard/rules.json`, with every parameter named. The rules are
  recorded from the web packs' integration tests run through a host (`AXX_TEST_HOSTED=1
  AXX_WEB_GUARD_RECORD=<file>`, a line per command): after upgrading Playwright, or when a step
  sends a new command, look at each command the rules lack before adding it, and never add one
  that names a path, a program or a network of the run's choosing (`guard_test.go` lists them).
  The host starts no WebKit on a Mac (`notHosted`): macOS sends its requests to loopback past any proxy.
- `internal/npm`: a pinned Node.js (`EnsureNode`, `WriteNode`) and the npm packages a
  package-lock.json pins (`Install`: integrity-checked, each platform's builds on that platform
  only, no lifecycle scripts), for the packs that run Node.js tools (web-core, web-lighthouse).
  `internal/imagediff`: Playwright's (pixelmatch's) pixel comparison of screenshots.
- `internal/launcher` (ADR 0016): axx's launcher on macOS, the one app macOS asks for the
  permissions to drive Mac apps (`internal/tools/axx-launcher`, built into `axx.app` and signed
  with NimbusXR's Developer ID by `scripts/macos-launcher-app.sh`). desktop-macos runs `axx run`,
  `axx mcp` and `axx doctor` again under it (`core.Relauncher`), from `AXX_MACOS_LAUNCHER` or axx's
  cache, where it downloads the launcher of its release, and under it starts apps as its
  children, so macOS asks it for the permissions those apps ask for: its entitlements
  (`entitlements.plist`) and Info.plist's messages must cover each, or macOS refuses without
  asking (its test builds the app and checks). Only packs import it, so the core binary stays
  without purego. A release signs and
  notarizes the launcher and the macOS binaries (`release.yml`: `macos-launcher`, GoReleaser's
  `notarize`, `notarized`); the `macOS signing` workflow, run by hand from main, checks that.
- `packs/models/core` (`models-core`): stubs of embedded models, `axx models contract|build`
  (a pack's commands: `core.Command`). A stub is a model of the real one's format and contract
  whose graph matches an input to its mappings' nearest key (`match`, the reference every
  format writer is held to) and gives that mapping's answer (`task`). Formats, tasks and input
  kinds are registries (`format.Register`, `task.Register`, `stub.RegisterInput`) that a pack
  adds to as models-core does: keep them so, and models-core's own entries no different from a
  pack's. The formats (`format/*`: ONNX and Core ML with `protowire`, TFLite with flatbuffers,
  no generated code; OpenVINO's IR, TensorFlow's SavedModel (`format/tensorflow`) and TensorFlow.js's
  graph model (`format/tfjs`, the SavedModel's graph in JSON), TorchScript's module's code
  (`format/torchscript`, and PyTorch Mobile's `.ptl`: that module and its forward's bytecode,
  compiled from its code) and MNN's Net (`format/mnn`) the ONNX stub's graph translated, ExecuTorch's program it lowered to
  portable kernels, all read with `onnx.ReadGraph`; ncnn's layers, `format/ncnn`, and Keras's,
  `format/keras`, its weights HDF5 of `internal/hdf5`, of their own) are held to one set of cases (`modelstest`, public, for packs' formats
  too) in their runtimes by the integration tests: ONNX Runtime (`internal/ort`,
  onnxruntime-node), LiteRT.js (`internal/litert`), llama.cpp (`internal/llamacpp`,
  node-llama-cpp), transformers.js (`internal/transformersjs`) and TensorFlow.js
  (`internal/tfjsrun`), by the Node.js axx pins;
  LiteRT's interpreter (`internal/litertpy`, which has int8 tensors, as apps' has), MediaPipe's
  tasks (`internal/mediapipe`), LiteRT-LM (`internal/litertlmrun`), ONNX Runtime GenAI
  (`internal/genai`), OpenVINO (`internal/openvinorun`), ExecuTorch (`internal/executorchrun`,
  with PyTorch's CPU build), TensorFlow, Keras 3 and tf_keras (`internal/tfrun`), PyTorch's TorchScript loader
  and lite interpreter (`internal/torchrun`), transformers and sentence-transformers (`internal/transformersrun`),
  MNN (`internal/mnnrun`), ncnn (`internal/ncnnrun`), faster-whisper (`internal/ct2run`),
  whisper.cpp (`internal/whispercpprun`, pywhispercpp) and openai-whisper
  (`internal/openaiwhisperrun`), whose Python packages are
  pinned by hashes in virtual environments uv makes (`internal/uvenv`, uv in `.mise.toml`);
  parakeet.cpp's CLI (`format/parakeet`: a Parakeet GGUF of one FastConformer layer, a CTC
  head and a frontend of its own; `internal/parakeetrun`, its release in a container);
  Tesseract's CLI (`format/tesseract`: a float LSTM traineddata, its unicharset and raw-line
  config written in Go); Piper's `PiperVoice` (`format/piper`, `internal/piperrun` in a
  hash-pinned Linux container); and, on a Mac, Core ML (`internal/coremlrun`, CI's coreml job) and Foundation Models (`internal/fmrun`, macOS 27 and
  Xcode 27: a stub of `format/foundationmodels` is a Swift type of its `LanguageModel` protocol, which an app's test
  build gives its sessions; its contract, of no tensors, is an API's, `format.Format.API`). Language models' stubs share
  their tokenizers and plans (`internal/lm`) and the cases they are held to
  (`modelstest/language.go`): GGUF's is a transformer of llama's architecture whose weights are
  its mappings (`format/gguf/llama.go`), every value exact in half precision (GPUs multiply
  halves), which `format/safetensors` writes as a LlamaForCausalLM (held to transformers,
  `internal/transformersrun`); LiteRT-LM's a TFLite model of prefill and decode signatures
  (`format/tflite/generator.go`) in a .litertlm (`format/litertlm`); MNN-LLM's the GGUF stub's
  transformer as MNN's ops around its Attention op (`format/mnn/llm.go`), held to MNN's LLM engine
  (`internal/mnnrun`), its tokenizers read from MNN's own files (`internal/lm/mnn.go`, `mtok.go`);
  MLC's (WebLLM's) the GGUF stub's transformer laid out in the real model's architecture
  (`gguf.CompileFor`: its sizes, its heads' groups, its rotary angles; a stub's matrices sparse,
  `format/gguf/matrix.go`) and written in its records and quantization (`format/mlc`), for the
  real model's library, held to WebLLM on a GPU (`internal/webllmrun`, Chromium's WebGPU; CI's
  webllm job); ONNX's a decoder of the
  real one's cache (`format/onnx/decoder.go`); ExecuTorch's a forward of portable kernels whose
  planned memory keeps the conversation (`format/executorch/llm.go`, held to ExecuTorch's text
  LLM runner), and a speech recognizer's an encode that computes Whisper's log-mel features
  in its kernels (`format/executorch/speech.go`) and that forward as its decode; and a speech recognizer's (Whisper's) an encoder
  that matches a sound's log-mel features (`match/logmel.go`, held to transformers.js's) and a
  decoder that writes the mapping's transcript (`format/onnx/speech.go`, held to
  `modelstest/speech.go`'s cases); Whisper's own weights are compiled (`internal/whisper`) into
  Whisper's architecture of the real model's sizes, every value of half precision and every
  choice of a margin over its rounding (whisper.cpp's weights are halves, and its flash
  attention pads its keys with zeros: every head scores real positions over them), which
  `format/safetensors` writes as a WhisperForConditionalGeneration, `format/ctranslate2` as
  faster-whisper's `model.bin`, `format/whispercpp` as a ggml model file (its vocabulary
  tiktoken's ranks, `lm.Ranked`) and `format/openaiwhisper` as OpenAI's checkpoint (torch.save's
  pickle, `internal/pickle`, which TorchScript's archives share), held to transformers (float32,
  float16, bfloat16), faster-whisper (float32 and int8), whisper.cpp (its CPU and Metal) and openai-whisper. `internal/lm` tokenizes as llama.cpp does
  (`vocab.go`, its pre-tokenizers taken from llama.cpp's source, `pretokenizers.go`, which
  names the commit; SentencePiece's unigram models, `ugm.go`) and as Hugging Face's tokenizers do (`hf.go`, `tokenizer.json`), held to
  llama.cpp's and transformers.js's tokens of real models' tokenizers (pinned). A stub's
  build is the same bit for bit on every machine: no fused multiply-adds in what builds it
  (`scripts/no-fma.sh`, in CI), `internal/detmath`'s functions instead of `math`'s (Sin, Cos,
  Exp, Log, Pow: compiled with fused multiply-adds on arm64), and no sum in a map's own order
  (`slices.Sorted(maps.Keys(m))`): keep it so, so the examples' apps keep theirs, built by `go generate`
  (`internal/tools/examplestubs`).
- `packs/models/{ios,macos,android,web}` (`models-ios`, `models-macos`, `models-android`, `models-web`): stubs of
  platform model APIs (Vision, Speech and Foundation Models, the system's and Apple's server
  model, in iOS Simulator apps and Mac apps; ML Kit, Gemini Nano and `SpeechRecognizer`; a browser's Prompt API, `Summarizer`,
  `Writer`, `Rewriter` and `SpeechRecognition`). A stub is a `.axxmodel` (`format/platform`) the scenario copies into the
  app's `axx-models` folder; the pack answers the app's calls from it in test runs, through a library
  the iOS Simulator loads into a Debug app, or macOS into a Mac app's test build as desktop-macos starts it
  (`desktopcore.OnLaunch`; `internal/apple`, its source built for each Apple platform), an agent ART attaches to a debuggable
  app (`android/runtime`), or a Playwright init script (`web/runtime.js`). Each pack's `apis.go`
  registers its APIs and their contracts (`platform.RegisterAPI`, which `axx models contract <api>`
  writes), and the reasons a device gives for a model it lacks (`API.Unavailable`: a stub's
  `format.details.availability`, an iPhone without Apple Intelligence). The microphone step
  (models-core) queues recordings with the reference's match (`Recording.Matches`), which
  adapters answer. Their integration tests probe the vendors' own APIs
  (`AXX_IOS_MODELS_SIMULATOR`, a Mac's own for models-macos, `AXX_MODELS_ANDROID_SERIAL`, the three
  Playwright engines).
- `packs/models/linux` (`models-linux`): the models Linux apps take from the system, in the desktops
  desktop-linux runs: Tesseract's language data (a `tesseract` stub, through `TESSDATA_PREFIX`) and
  speech-dispatcher's voice (a Piper voice stub, through its user configuration in the app's home,
  speaking with Piper into `axx-models/.speaker`), which models-core's speaker step hears.
- `packs/models/windows` (`models-windows`): the models Windows apps take from Windows, in the
  desktops desktop-windows runs: SAPI's voice (a Piper voice stub that axx's voice engine, `engine`,
  a .NET COM server built into axx's cache, speaks, added to SAPI's voices and made the user's
  default for the scenario), and the Windows AI APIs (`ai`, a C++/WinRT library axx builds with
  MSVC against a self-contained Windows App SDK app's own .winmd files, in place of its
  Microsoft.Windows.AI.*.dll for the scenario, answering as Windows does: only an app with package
  identity and the systemAIModels capability).
- `packs/mobile/core` (`mobile-core`), `packs/mobile/android` (`mobile-android`) and
  `packs/mobile/ios` (`mobile-ios`): native apps through the drivers on the devices, the
  UiAutomator2 server and WebDriverAgent, which axx installs, starts and talks to itself
  (`packs/mobile/internal/appium`, a W3C WebDriver client; ADR 0013), with a device leased per
  scenario and the app reset before it. A device a farm's Appium server runs (`appium`) goes
  through that server. Their unit tests run the steps
  against a fake Appium server (`appiumtest`) serving real page sources (each pack's
  `testdata`); their integration tests run the parcels example's couriers' apps
  (`examples/parcels/courier/{android,ios}`) against a stand-in of its API (`courierapi`): on an
  emulator in CI's mobile job, and on simulators in its ios job (macOS). mobile-ios installs
  Appium's prebuilt WebDriverAgent (pinned, with its SHA-256 sums, in `install.go`) instead of
  building it with Xcode; mobile-android, Appium's UiAutomator2 server APKs (pinned in `uia2.go`).
  A step scrolls by asking the view, and swipes only where it cannot (ADR 0020): on Android
  through axx's scroll service (`packs/mobile/android/scroll`, an accessibility service of
  axx's own, switched on for the run beside the UiAutomator2 server, which is started so that
  it leaves it running; `scrolls.go`, `scroll.go`), with what the screen has out of view listed
  too; on iOS through XCTest's scroll to an element. A release builds and signs the service
  (`release.yml`'s `android-scroll` job, `internal/tools/androidscroll`) and publishes it in its
  checksums, where that release's axx downloads it; an axx that is no release builds it with
  the SDK's build-tools and a JDK. One build for both (`BuildScrollService`): keep it so.
  Keep the two words apart, in code and docs: a swipe is a finger on the glass, a scroll is not.
  What an app's camera sees, and its AR, are played from a picture, a video or a glTF scene: on
  a simulator, by a library mobile-ios builds with Xcode (`packs/mobile/ios/ar`); on an emulator,
  by the emulator's own camera, which renders its AVD's environment as axx sets it
  (`packs/mobile/android/camera.go`, a video re-encoded with `packs/internal/video`), and, for
  ARCore, by an ART TI agent mobile-android attaches to a debuggable app (`ar/agent.cc`,
  `ar/Bridge.java`), built as models-android's is (`BuildAgent`), which it attaches after its own.
- `packs/coverage/*` (ADR 0017): the coverage of the services a run starts, per run, never per
  scenario. The core knows two interfaces (`core/coverage.go`): `CoverageLauncher`, which gives a
  measured service its variables before it starts (the axx host calls it too, so an approval covers
  them), and `Concluder`, which reads the run's coverage once the services stop. `coverage-core`
  implements both, and the runtimes register with it (`coverage.Register`, `packs/coverage/core/runtime.go`):
  `jvm` (JaCoCo), `go` (`go build -cover`), `python` (coverage.py), `node` (V8's own coverage, through
  a preload, never the inspector) and `report` (a service's own lcov or Cobertura); a runtime that can
  be read while it runs (`Keeper`) is measured per run under `axx up --coverage`. The report is
  Istanbul's model (`packs/coverage/core/istanbul`), which web-coverage shares, with V8's conversion
  in `packs/internal/v8cov`. A service that wrote nothing is never 0%: the run says why. Minimums
  are checked only on whole runs (exit 6, ADR 0003); `axx coverage check` merges CI's shards. Each
  runtime's container test (`packs/coverage/internal/coveragetest`) runs a service as a user of its
  own and as the container's main process, as Compose would.
- `packs/internal/memcheck`: axx's C, C++ and Objective-C under a memory checker, for axx's own
  tests: with `AXX_MEMCHECK` naming a folder, the libraries the packs load into apps (the
  simulators' library, the AR library, the Apple model adapter) are built with AddressSanitizer
  and write a report there for each process it found a fault in. Nothing changes in how an app is
  started, and it is no setting of axx. A new library of such code is built through it too
  (`memcheck.Clang`, `memcheck.Source`, `memcheck.Key` in what its place in the cache is made
  from, and `memcheck.Record` around its builder, which lists it in the folder: a job that was
  to check memory and lists no library fails, as it checked nothing). A report has the fault and
  where axx's code was, not where the memory was allocated: a simulator's every process loads
  the iOS pack's library, and that record took gigabytes over them (`options`, which says how
  to see it for one run).
- `internal/filecontent`: reads files by their type (text, CSV, TSV, JSON, XML, HTML, PDF, Word,
  Excel) for the steps that check a file's text or table: files, storage objects, downloads.
- `schemas/v0`: the JSON Schemas of axx.yaml and the fixture spec files, at public paths: GitHub serves them from
  main, and axx.yaml files name those addresses (`schemas.Base`). The editor plugins take them from the axx the project runs on
  (`axx schema --out`, again when `axx --version` answers otherwise: IntelliJ's `AxxProjectSchemas`, VS Code's `schemas.ts`), so they
  match it whatever the plugin's release; until then, and when axx can't give them, they use copies they bundle: IntelliJ's from
  the copy `go generate` keeps in its folder, VS Code's copied at its build. Keep `axx schema --kind <k> --out`
  working in every release: an editor asks every axx a project may run on.
- `internal/progress`: what axx says while a step takes time. A step starts with one line on standard error
  (`App.step`, or `progress.On(log).Start` where code is handed a writer), which a person at a terminal sees live
  (how long, how far a download is) and everything else as plain lines, with `axx: still ...` each half minute
  (an agent, CI, a pipe, `--json`: `App.report` decides). What is quick most times says nothing unless it takes
  a second (`IfSlow`). Keep it so: whatever can take more than a second starts a step, nothing else rewrites a
  line, and no step wraps a child that writes to the terminal itself.
- `internal/selfupdate` (`axx upgrade`): axx's releases on GitHub, what changed between two, and replacing the running axx
  with a release's, checked against its checksums, as `scripts/install.sh` and `install.ps1` do.
- `internal/mirror`: where axx and its packs download other projects' files, from their sites or an organization's mirrors
  (`AXX_GITHUB_URL`, `AXX_NUGET_URL`), and credentials kept out of messages. Every download has a mirror setting
  (`guides/use-mirrors.md` lists them): a new download gets one, and is checked against a pinned hash.
- `internal/docsite` (`axx docs serve`): the docs site on this machine, for a network that cannot reach it: the archive each
  release publishes beside its binaries (`axx_<version>_docs.tar.gz`, in its checksums.txt; release.yml's `docs-archive`
  job builds it with `AXX_DOCS_CHANNEL=archive`), unpacked into axx's cache, served as the site's router answers
  (`docs/cloudfront/router.js`: keep the two alike). Nothing on a page may load from another host (`npm run check:offline`).
  The links axx prints go through `axxerr.Docs`, which `AXX_DOCS_URL` or axx.yaml's `docs.url` points at a copy: never
  write the site's address into a message.
- `internal/cli/pin.go`: the version a project names (`axx: 0.5.0`, read as a line by `config.ReadPin`; ADR 0015). Before anything parses the
  command line, the installed axx gets that release into its cache and hands over to it, and that release does the rest, its pack build included.
  `internal/cli/reminder.go`: the one line, at most weekly, that says a newer release is out. An old axx must always be able to hand over to a
  later one: keep the line's reading, the archives' names and the versions directory compatible.
- `internal/lifecycle/properties.go`: what services tell axx. Each service's command (and a hook before a
  command) gets `AXX_PROPERTIES`, a file of its own in `.axx/properties`, whose `name=value` lines become
  `${sys:name}` for the names the person did not set (`-D` and axx.yaml's `properties`, a profile's included, win:
  `config.Config.Resolver`; `axx run` says which told values it does not use, `lifecycle.Unused`); two that tell one name stop
  the run. What a service runs (`command`, `env`, `cleanup`, `debug.command`) and its readiness checks
  (`ready.http`, `ready.tcp`, `ready.exec`) are left unfilled as axx.yaml loads (`config.Config.Filled`): the first
  as it starts, with what the services it starts after told (a service told later stops the run, AXX-E0418), the
  checks each time they are tried. The axx host's services tell it in a folder of its own, never the project's,
  which the agent can write: it sends what they told with `started`. Its approval fills them in with the run's
  -D's and axx.yaml's properties (`host.Define`), so that a run changes what runs only with an approval.
- `internal/host` (`axx host`): starts a project's services, on the person's machine, for the runs of an agent
  whose sandbox keeps it from Docker. Runs find it in `.axx/host.json` and ask it for their services over a
  loopback port (`axx run`, `axx up`'s supervisor, the MCP session's `steps_try`); it starts only a definition
  the person approved (`Define`: axx.yaml's services, what their compose projects resolve to, the files their
  commands run), asking in its terminal or the editor that started it (`--ide`), never over the port. It
  stays on the axx the project runs on (`follow.go`, `internal/cli/hostfollow.go`): it restarts on the version
  axx.yaml names, or on an upgraded installed axx, once no run is connected, and asks first when what a start
  runs of the project's own changed since it started (`Starts`: its packs and the hooks of `axx host`) or the
  version is older. Keep it so: whatever a start of the host reads from the project and runs joins `Starts`.
  A command tells that `axx up`'s supervisor runs by the lock it holds (`lifecycle.UpLock`, `filelock.Held`),
  never by signalling its process: a sandbox refuses the signal (macOS) or hides the process (Linux). Keep
  it so for anything one command must tell of another across a sandbox.
  Runs take turns with a project's services always: the host gives a run its turn, and where a run does
  not ask it (services `axx up` keeps, or no host) the run holds `.axx/run/turn.lock` (`takeTurn`).
  A pack can have a side in the host (`core.HostServer`, ADR 0018): its side in a run opens a connection
  with `Suite.HostDial`, which the host hands to the pack once the run gave the host's token, and the two
  speak the pack's own protocol (web-core: a browser). What a pack's host side does for a run must give the
  run nothing its sandbox would not: nothing it sends is run, names a file of the machine, or reaches an
  address past the sandbox. `cloudtest` runs a pack's tests through an in-process host when
  `AXX_TEST_HOSTED` is set.
  A pack with a side in the host (`core.HostServer`, ADR 0018) does there what a sandbox keeps a run from:
  web-core its browsers, the mobile and desktop packs their devices and apps (ADR 0019). The run's side asks
  for what a step does, by name (`packs/internal/hostwire`), in a scenario of the host's own for each of the
  run's (`packs/internal/hostscenario`), in a suite confined to the project's repository (`Suite.Confine`).
  An app is the agent's code outside its sandbox: the pack's side asks the person first (`HostRun.Approve`),
  with everything of the registration. A pack that builds on a platform pack sets up its launch hook in the
  host's scenario too (a hook of phase `core.HostScenario`), and what its two sides must know of each other is
  a note of the scenario (`Scenario.Note`). A new platform pack gets a host side with its first version, and a
  new operation never names a program or a file outside the project. The platform packs' tests run again
  through a host in the tests' process (`AXX_TEST_HOSTED`, `cloudtest.AgainHosted`).
- `internal/packbuild`, `internal/gotool`: axx builds itself with the packs a project lists in
  `axx-packs.yaml`, using a Go toolchain it downloads itself, as the go command downloads one (`golang.org/toolchain`
  through GOPROXY, checked against Go's checksum database, which GOSUMDB=off does not skip), so an organization's Go proxy serves it.
- `internal/packref`: what axx knows of every pack it publishes, whether a project uses it or not
  (what it reaches, its settings, parameter types and steps): data `go generate` writes from the
  packs (`packs.json`), built into the core, so `axx pack list|show`, `axx steps` and the MCP
  server answer for packs not added yet, offline, and an undefined step names its pack.
  `internal/stepsearch`: one ranking and one list of steps for `axx steps` and the MCP server: a line
  each, a step's `.on` and `.leading` forms folded into it. `internal/cli/initpacks.go`: what in a
  repository points `axx init` to a pack; a new pack that files can point to gets its signals there.
- What agents read: `agentsBlock` (`internal/cli/initcmd.go`), the `AGENTS.md` section `axx init`
  writes, with the workflow and the rules, which `guides/set-up-agents.md` copies word for word;
  `Instructions` (`internal/mcp/server.go`) and the root command's help say the same workflow; the
  skills (`internal/skills/assets`) add only the specialized parts. Change them together.
- `packs/all` and `internal/tools/axxall`: axx with every pack, for generated docs, skills and
  tests.
- `internal/compat`: the value semantics steps follow (Jayway JSONPath, Java regex, Java number
  formatting; ADR 0007). The oracle tests depend on these; don't "simplify" them. `jvalue` gives and
  compares the values `core.ParseValue` reads; `internal/jsonassert` checks them at a document's
  JSONPaths, and `internal/tablevalue` builds the object a table's paths and values make, for the
  steps that send one they make from scratch.
- `extensions/axx-wiremock`, `ide/intellij`: JVM parts (the WireMock extension and the IntelliJ plugin).
- `ide/vscode`: the VS Code extension (TypeScript). Both editor clients run `axx lsp` (`internal/lsp`).
- `scripts/release-please`: release-please as `release.yml` runs it (`release.mjs`), with the plugin
  (`stamp.mjs`) that versions what ships beside axx, `extensions/axx-wiremock` and the two editor
  plugins: axx's major and minor, then a stamp of the day (`0.7.2026112101`; ADR 0004). Keep the
  three in its `components`, and a new component that ships beside axx too.
- `testdata/steps.json`: the frozen catalog of the step text of axx's packs. `testdata/oracles`:
  recorded behavior oracles. **Never edit either by hand**: they define correct behavior. A new
  step joins the catalog with `go test ./internal/engine -run TestStepCatalog -update`, which
  only adds entries.
- `testdata/cli-contract.json`: the frozen exit codes and `AXX-E` error codes, with the exit code
  each error ends its command with (ADR 0003). **Never edit it by hand.** A new code joins with
  `go test ./internal/axxerr -run TestCLIContract -update`; a code the owner decides to remove is
  retired with `-retire`, and is never given another meaning.
- `testdata/schemas`: the frozen copies of the schemas of axx.yaml and the fixture spec files.
  **Never edit them by hand.** A schema may grow and may not take back what a file could say
  (`internal/schemacompat`, ADR 0004): after `go generate`, `go test ./schemas -run
  TestSchemaContract -update` freezes what it gained, and refuses a change that breaks; one the
  owner decided on is frozen with `-break`.
- `testdata/openapi-agreement`: exchanges that both OpenAPI validators (the REST pack's and the
  WireMock extension's) must report with the same keys; both test suites read it.

## Rules

- Step expression text is public API. Never change existing step text by accident; add new steps instead. axx
  is in beta: when the owner decides to remove or change a step, take its old text out of the catalog with
  `go test ./internal/engine -run TestStepCatalog -prune`.
- The deprecation policy (ADR 0004, the docs' "Deprecation policy" page): nothing is removed within a
  major version; what is replaced is deprecated first, keeps working and warns where it is used; the
  next major drops everything deprecated. A step: `StepDef.DeprecatedBy` says what to use
  (`axx lint` warns, AXX-E0839). A flag: cobra's `MarkDeprecated`. A setting: keep reading it,
  mark it `deprecated` in the schema, and warn when axx.yaml has it. Until `v1.0.0` the owner may
  still decide to remove something outright.
- A step's `{string}` that it sets something to or checks something against is a value: declare it
  (`StepDef.TextArgs`, `TypedArgs`; ADR 0014). A name or a pattern is not: list it in `packs/all/values_test.go`.
- Exit codes (`internal/exitcode`), error codes (`internal/axxerr`) and the `--json` envelope are
  frozen contracts (ADR 0003): a test holds the codes to `testdata/cli-contract.json`.
- Every user-facing error is an `*axxerr.Error` with a stable `AXX-Exxxx` code and a hint.
- Never hand-edit generated files: `go generate ./...` writes the `axx.yaml` schema
  (`schemas/v0/axx.schema.json`) and the IntelliJ plugin's copy of every schema
  (`ide/intellij/src/main/resources/schemas`: in its own folder, a schema's change is a change of the
  plugin, which release-please releases), the skills (`plugins/axx/skills`) and the packs' reference
  (`internal/packref/packs.json`), and the docs site's
  build writes the reference (`docs/scripts/gen.mjs`, from `axx docs export`).
- The docs site's root is the latest release's docs, built from its tag (or from `docs-<tag>`,
  the release with changes to `docs/` only, when there is one); main's are at `/next/`.
  Link pages site-absolute (`/guides/install/`): the build puts `/next/` in front of them.
- `cmd/axx` is the core only. Never import a pack from it: every pack, ours included, gets into
  axx through `axx-packs.yaml`, and pack dependencies stay out of the core binary.
- The examples are a Go module of their own (`examples/go.mod`; their apps' too), so that axx's
  module, which a project's axx downloads to build its packs, leaves them out. Keep it so: nothing
  under `examples/` belongs in axx's module.
- The examples' model files and tokenizers are in Git LFS (`examples/.gitattributes`, by type):
  a new kind of model file gets a line there. Nothing outside `examples/` goes in LFS: a file of
  axx's module would be an LFS pointer in the module a project's axx downloads. CI's jobs that
  read the model files pull them (`.github/actions/lfs`).
- Keep `CGO_ENABLED=0` builds working: pure-Go dependencies only.
- One commit gives the same release files, bit for bit: the binaries and their archives carry
  nothing of the machine or the clock (`-trimpath`, the commit's time and one owner on every file,
  `.goreleaser.yaml`). CI's `build-twice` job builds them twice and compares
  (`scripts/build-twice.sh`): keep it so.
- CI runs the jobs a pull request needs, from the files it changes (`scripts/ci-changes.sh`).
  A new kind of input to a job (a folder its tests read, say) goes there too.
- Every night CI runs every job on main (`ci.yml`'s schedule; `AXX_NIGHTLY`), with what a pull
  request's run leaves out for its time: `scripts/gotest.sh` has the tests say which statements
  they ran, of every package of axx, and the `coverage` job adds them up over all the jobs
  (`internal/tools/covermerge`: a statement counts when any job ran it, and the whole must be
  80%); and in the jobs that set `AXX_MEMCHECK` (the model adapters'), the packs build their C,
  C++ and Objective-C under a memory checker (`packs/internal/memcheck`), whose reports fail the
  job that has one. `.github/actions/nightly` is the last step of every job that runs tests: a
  new such job gets it too, and joins the `coverage` job's `needs`. On GitHub's macOS runners the
  iOS pack's tests take up to two and a half times as long checked, so they are not checked there.
- No CI job runs past 10 minutes, its setting up included, and each test step's timeout says so
  (`-timeout 10m`): a job whose tests grow past it runs in parts (a matrix, by its tests' times
  on CI's runners, which `gotestsum`'s slowest list gives; `axx run --shard i/n` for a feature),
  never with a longer timeout; and what every part makes the same way, for minutes (the couriers'
  Android app, the emulator's device), is a cache keyed by what makes it
  (`.github/actions/courier-android`).
- The `hygiene` check (`scripts/hygiene.sh`) must pass; it blocks references to unrelated
  organizations and internal infrastructure.

## Commands

```sh
mise run test          # go test -race ./...
mise run lint          # golangci-lint run
mise run generate      # go generate ./...: the axx.yaml schema and the skills
mise run hygiene       # denylist + gitleaks
mise run intellij      # a sandbox IntelliJ with the plugin, the Go plugin and axx from this tree
go build -o bin/axx ./cmd/axx                     # the core; prepares a project's packs on first use
go build -o bin/axx-all ./internal/tools/axxall   # every pack built in
```
