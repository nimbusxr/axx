# AGENTS.md: working on the axx codebase

axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework for
the agentic era.

This file is for agents *contributing to axx*. If you are *using* axx in another repo, install
the skills instead: `axx skills install`.

## Layout

- `cmd/axx`: CLI entry point; it calls `app.Main`, as the builds with a project's packs do.
  `internal/cli`: cobra commands.
- `internal/{config,interp,feature,match,engine,runner,report,lifecycle}`: the core runner.
- `core`: the public core every pack builds on (scenario, steps, service registry, state,
  errors, interpolation, shared parameter types).
- `packs/*`: the step packs (rest, mock, sql, mongo, kafka, logs, files; `web/*`, `aws/*` and
  `gcp/*`: a core pack and the packs that build on it; `azure/*`), each with its public context. `internal/packset/catalog.go` lists them.
  Packs that build on another (`aws-s3` on `aws-core`, `web-screenshots` on `web-core`) declare it in
  `Requires` and use its public context (`packs/web/core/public.go`); the core stays free of any
  pack's specifics: packs get what they need through extension points in `core`. A pack's
  manifest gives its steps, parameter types, hooks, settings and `axx mcp` tools (`Tools`, which
  look at the scenario an agent keeps open with `steps_try`; `internal/runner/session.go`).
- `packs/web/core` (`web-core`): drives Playwright through playwright-go, with the browsers on the machine that runs
  axx. It pins the Playwright version and its hash in `packs/web/internal/driver`, and runs it
  with the Node.js `internal/npm` pins; upgrading playwright-go means updating them
  (`TestPinnedPlaywrightVersion` fails until they agree). The driver it prepares is patched
  (`packs/web/internal/driver/patch.go`, `patches/*.js`) so that Playwright's Inspector, recorder
  and trace viewer speak the pack's steps: each patch has an anchor in the pinned playwright-core,
  so upgrading Playwright means checking every anchor; bump `patchRevision` whenever a patch changes.
- `internal/npm`: a pinned Node.js (`EnsureNode`, `WriteNode`) and the npm packages a
  package-lock.json pins (`Install`: integrity-checked, each platform's builds on that platform
  only, no lifecycle scripts), for the packs that run Node.js tools (web-core, web-lighthouse).
  `internal/imagediff`: Playwright's (pixelmatch's) pixel comparison of screenshots.
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
- `internal/filecontent`: reads files by their type (text, CSV, TSV, JSON, XML, HTML, PDF, Word,
  Excel) for the steps that check a file's text or table: files, storage objects, downloads.
- `internal/packbuild`, `internal/gotool`: axx builds itself with the packs a project lists in
  `axx-packs.yaml`, using a Go toolchain it downloads itself.
- `packs/all` and `internal/tools/axxall`: axx with every pack, for generated docs, skills and
  tests.
- `internal/compat`: the value semantics steps follow (Jayway JSONPath, Java regex, Java number
  formatting; ADR 0007). The oracle tests depend on these; don't "simplify" them.
- `extensions/wiremock-openapi`, `ide/intellij`: JVM parts (the WireMock extension and the IntelliJ plugin).
- `ide/vscode`: the VS Code extension (TypeScript). Both editor clients run `axx lsp` (`internal/lsp`).
- `testdata/steps.json`: the frozen catalog of the step text of axx's packs. `testdata/oracles`:
  recorded behavior oracles. **Never edit either by hand**: they define correct behavior. A new
  step joins the catalog with `go test ./internal/engine -run TestStepCatalog -update`, which
  only adds entries.
- `testdata/openapi-agreement`: exchanges that both OpenAPI validators (the REST pack's and the
  WireMock extension's) must report with the same keys; both test suites read it.

## Rules

- Step expression text is public API. Never change existing step text by accident; add new steps instead. axx
  is in beta: when the owner decides to remove or change a step, take its old text out of the catalog with
  `go test ./internal/engine -run TestStepCatalog -prune`.
- Exit codes (`internal/exitcode`) and the `--json` envelope are frozen contracts (ADR 0003).
- Every user-facing error is an `*axxerr.Error` with a stable `AXX-Exxxx` code and a hint.
- Never hand-edit generated files: `go generate ./...` writes the `axx.yaml` schema
  (`internal/config/axx.schema.json`) and the skills (`plugins/axx/skills`), and the docs site's
  build writes the reference (`docs/scripts/gen.mjs`, from `axx docs export`).
- The docs site's root is the latest release's docs, built from its tag; main's are at `/next/`.
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
- CI runs the jobs a pull request needs, from the files it changes (`scripts/ci-changes.sh`).
  A new kind of input to a job (a folder its tests read, say) goes there too.
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
