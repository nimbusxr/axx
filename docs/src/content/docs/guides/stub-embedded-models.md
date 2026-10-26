---
title: Stub embedded models
description: "Replace a model your app or service runs itself with a stub: a model of the real one's format and contract (ONNX, TFLite, Core ML, ExecuTorch, OpenVINO, MNN, ncnn, TensorFlow, Keras, TensorFlow.js, TorchScript, PyTorch Mobile, GGUF, safetensors, CTranslate2, whisper.cpp, Parakeet, LiteRT-LM, MNN-LLM, MLC (WebLLM), Tesseract, Piper, Foundation Models), a fraction of its size, whose answers are its mappings. The app's runtime loads it as it loads the real model. A model the platform provides (Vision, Speech, Foundation Models and Apple's server model, ML Kit, Gemini Nano, a browser's built-in AI: its Prompt API, Summarizer, Writer, Rewriter, Translator, LanguageDetector, Proofreader and speech recognition) gets a stub of its API."
---

Some models your apps and services run themselves: an image classifier on a phone (LiteRT on Android, Core ML on iOS), a model a service loads with ONNX Runtime, a language model it runs with llama.cpp. These are embedded models. A real one fills a simulator's or an emulator's memory, takes seconds to load, and answers as it likes. It also will not, on demand, give the answer that sends a claim to a person.

A stub replaces the model. It is a model of the real one's format, with the same inputs and outputs, and its answers are its mappings. The app's runtime loads it as it loads the real model, and an input like one of the mappings' gets that mapping's answer. The app's test build or test deployment puts the stub where the app gets its model, the way a compose file points a service at WireMock. Nothing in the app changes. A model the platform provides (iOS's Vision, Speech and Foundation Models; Android's ML Kit, Gemini Nano and speech recognizer; a browser's built-in AI) has no file of the app's to replace: a stub of its API answers the app's calls in its test runs instead ([Models the platform provides](#models-the-platform-provides)).

Models a service reaches over HTTP (OpenAI, Anthropic, Gemini, Bedrock, Ollama) are mocked with WireMock instead: see [Test AI features](/guides/test-ai-features/).

## Four steps, for every model

Every model is stubbed the same way, whatever its format and wherever the app runs it:

1. **[Write its contract](#1-write-the-contract)**, once, from the real model: what the app relies on from it.
2. **[Map inputs to answers](#2-map-inputs-to-answers)**: the pictures (or other inputs) your scenarios give the app, and what the model answers each.
3. **[Build the stub](#3-build-the-stub)** with `axx models build`, and keep it with the app's sources.
4. **[Put it where the app gets its model](#4-put-it-where-the-app-gets-its-model)**, in the test build or the test deployment.

Then the scenarios test what the app does with each answer, with the steps they already use. There are no steps for models: the app runs its model, and the scenarios see what it does.

List the `models-core` pack in the project's `axx-packs.yaml`, and give each model a directory of the acceptance project:

```text
models/evidence-check/
  contract.json          what the app relies on from the model (step 1)
  mappings/
    damage-photos.json   the inputs it answers, and its answers (step 2)
    default.json         the answer to every other input
```

## 1. Write the contract

The contract is what the app relies on from the model:

- its inputs and outputs (names, types, shapes);
- an image input's layout;
- a classifier's labels;
- how an integer output stands for scores (`quant`);
- the version of its format, so the app's runtime loads the stub as it loads the model.

Write it from the real model, once, and keep it with the project. No test needs the real model after that.

```sh
axx models contract mobilenetv2-7.onnx --task classification --labels labels.txt \
  -o models/evidence-check/contract.json
```

| Format | Models | What the contract takes from the model |
| --- | --- | --- |
| ONNX | `.onnx` | Its inputs and outputs, IR version, operator sets, operators and metadata, and the files beside it its weights are in (external data). A classifier's labels come from `--labels`. A speech recognizer (Whisper's encoder, `encoder_model*.onnx`, its decoder beside it) is a `speech-to-text` model's: its input a sound's log-mel spectrogram of the size its `preprocessor_config.json` gives, its decoder's inputs and outputs, and the tokens its `generation_config.json` ends and suppresses, with its tokenizer in `model-files`. A language model's decoder (`input_ids`, `attention_mask` and its cache, `past_key_values`, in; `logits` and `present` out, as Hugging Face's exporters and ONNX Runtime GenAI write them) is a `text-generation` model's: the token that ends its turn comes from its folder's configs, and its tokenizer (`tokenizer.json`, beside it or in the folder above, as the model's repository has it) goes in `model-files`. An image model's image processor (`preprocessor_config.json`, beside it or in the folder above, as transformers.js and Optimum keep it) says how it fits a picture to its input, in RGB: stretched to its `size` of a height and a width (ViT's, BiRefNet's), or its shorter side resized to its `shortest_edge` and its middle cropped to its `crop_size` (ConvNeXT's, CLIP's: `crop` its share). |
| TFLite (LiteRT) | `.tflite` | Its first subgraph's inputs and outputs (their quantization too), its signature, operators, metadata buffers and the files appended to it. A classifier's labels come from its metadata, when it has them. |
| ExecuTorch | `.pte` | Its forward method's inputs and outputs, by their places (`input0`, `output0`...: an app gives and gets them in order), its other methods' (React Native ExecuTorch's text recognizers have a method for each width of a strip of text), and the kernels and delegates it calls. Only the program is read: the head of the file, before its weights. A stub has every method, each of its own shapes. A language model's program (forward of its tokens and their position, and methods that give its settings, `get_max_seq_len`, `get_eos_ids`..., as ExecuTorch's LLM export writes it) is a `text-generation` model's: its settings are the real program's, the token that ends its turn is one of its `get_eos_ids`, and its tokenizer (`tokenizer.json`, beside it or in the two folders above, as the model's repository has it) goes in `model-files`. A speech recognizer's program (`encode` of a sound's samples, `decode` of a token, its position and the encoder's state, as React Native ExecuTorch exports Whisper) is a `speech-to-text` model's: its input `encode`'s sound (16 kHz), and its tokenizer in `model-files`. |
| TensorFlow SavedModel | `saved_model.pb` | Its meta graph for serving (its tags) and its signature (`serving_default`, or its first): its inputs and outputs by their keys, which TF Serving and `signatures[...]` call it by, and the names of their tensors in its graph (`serving_default_inputs:0`, `StatefulPartitionedCall:0`), which a session of TensorFlow 1 runs it by; the operators of its graph and its functions, and the version of TensorFlow that saved it. Its variables are not read. |
| TensorFlow.js | `model.json` (its shards beside it) | A graph model's signature (as TensorFlow.js's converter writes it from version 2): its inputs and outputs by their keys, in their order (`predict` gives and gets them in it), and the operators of its graph. A graph model of no signature (of the converter before version 2, as tfjs-models' older models are): its placeholders and the nodes no node takes, by their names, as TensorFlow.js runs it; its outputs' shapes come from `--output`. Its integers are int32s, as TensorFlow.js has them. A layers model (Keras's layers, `tf.loadLayersModel`) is not read. |
| Keras | `.keras`, `.h5` | A Functional or Sequential model's config (a `.keras` file's `config.json`, a `.h5` file's `model_config`, of Keras 3 or Keras 2): its inputs (their names, types and shapes), its outputs by their layers' names, and their shapes where its last layers say them (a `Dense`'s units, a `Reshape`'s shape); the version of Keras that saved it, and its layers. Its weights are not read. Other outputs' shapes, and a subclassed model's inputs and outputs, come from `--input` and `--output`. |
| TorchScript | `.pt`, `.torchscript` | Its forward's tensors (its parameters by their names, its outputs by their places: `output0`, `output1`...), its class, and the operators its code calls. TorchScript does not say its tensors' types and shapes: they come from what the model's folder has (an Ultralytics export's config, inside it: its pictures' size, its task and labels; Triton's `config.pbtxt` beside or above it), or from `--input` and `--output`. |
| PyTorch Mobile | `.ptl` (a module's `_save_for_lite_interpreter`: Android's and iOS's PyTorch Lite runtimes, `LiteModuleLoader`) | As TorchScript's, from the code the file keeps beside its bytecode, and the operators its bytecode calls (`aten::conv2d`, `aten::add.Tensor`...), those a selective build of PyTorch Mobile made for the model alone has. |
| MNN | `.mnn` | Its inputs (their names, types and dimensions), its outputs' names (those it names, or the tensors no op takes, as MNN's loader has them), and the ops it uses. MNN does not say its outputs' types and shapes: they come from `--output`. |
| ncnn | `.param` (its `.bin` beside it) | Its inputs (its `Input` layers' blobs, of the dimensions they give: a picture's of no batch, CHW, as ncnn's `from_pixels` gives it), its outputs' names (the blobs no layer takes), and the layers it uses. ncnn does not say its outputs' shapes, nor (pnnx's models) its inputs': they come from `--input` and `--output`, of no batch, as ncnn's blobs are. |
| OpenVINO | `.xml` (its `.bin` beside it) | Its inputs (its Parameters) and outputs (its Results, by their tensors' names), its IR version and the operations it uses; a classifier's labels, and whether the app gives a picture's channels reversed (BGR), from the `config.json` beside it (Open Model Zoo's). |
| Core ML | `.mlmodel`, `.mlpackage` | Its inputs (an image's size and colors, or a multiarray), its outputs (a classifier's label and probabilities, with its class labels, or multiarrays), its specification's version and metadata. A detector's pipeline (a network of candidates, then a non-maximum suppression: Create ML's object detectors, YOLO's Core ML exports, as Vision runs them) is a `detection` model's of its suppression's labels, its outputs the objects it keeps (`confidence`, `coordinates`); the contract carries its suppression and its description. A text encoder (its tokens' ids, `input_ids`, as Hugging Face's exporters and coremltools convert BERT's and MiniLM's) matches texts with its tokenizer: the `tokenizer.json` beside it or in the folder above, or `--vocab vocab.txt`. |
| safetensors (transformers) | `model.safetensors` (a folder of it) | A language model's folder, as Hugging Face's repositories have it: its vocabulary's size and context length (`config.json`), the token that ends its turn (`generation_config.json`'s, or `config.json`'s), and its tokenizer (`tokenizer.json`) in `model-files`. A text embedding model's folder (an encoder's `config.json`: BERT's, MPNet's...; or sentence-transformers' `modules.json`) is an `embedding` model's of its hidden size, or of its last Dense module's features (EmbeddingGemma's: 768 to 3072 to 768). A speech recognizer's folder (Whisper's: a `config.json` of `model_type` `whisper`, its `generation_config.json` and `preprocessor_config.json`) is a `speech-to-text` model's: its sizes, the tokens its transcripts begin and end with and the ones it suppresses, its features' size, and its tokenizer in `model-files`. |
| CTranslate2 | `model.bin` (a folder of it) | A Whisper's (its spec's, `WhisperSpec`: faster-whisper's models): its sizes (its variables'), its settings (its scalars), the tokens its transcripts begin and end with (its tokenizer's, `tokenizer.json` beside it, which goes in `model-files`) and those its `config.json` suppresses. |
| whisper.cpp | `ggml-*.bin` | A Whisper's model file, as whisper.cpp's converters write it: its sizes, the type of its weights, the tokens whisper.cpp begins and ends its transcripts with, and its mel filters and vocabulary (tiktoken's, by their ranks), which go in `model-files`. |
| Parakeet (parakeet.cpp) | `.gguf` (Parakeet's, as parakeet.cpp's converters write NVIDIA's CTC, TDT and hybrid TDT-CTC models) | Its features (their mels and sample rate) and its vocabulary's size. Its task is `speech-to-text`, its input the sound (`"features": "parakeet"`): parakeet.cpp computes the features from the file's own frontend, so a stub has a frontend of its own, whatever the real model's. |
| OpenAI Whisper | `.pt` (OpenAI's checkpoints: `tiny.en.pt`...) | A checkpoint's dims, the tokens the `openai-whisper` package begins and ends its transcripts with and suppresses, and its vocabulary: the package's `whisper/assets/multilingual.tiktoken` (or `gpt2.tiktoken`, of an English-only model), which goes in `model-files`. Copy it beside the checkpoint first. A TorchScript module, also a `.pt` file, is told apart by its content. |
| Tesseract | `.traineddata` | The contract is written by hand: a grayscale image input of a fixed size, and the CTC output's labels (the characters it reads, space, and the blank `""`). Its task is `text-recognition`. The stub reads an image of the contract's size as one line, as it is (page segmentation mode 13, which its config sets, and an app's `--psm 13` keeps): an app gives it crops of one line, as an app reads a field of a form or a label. |
| Piper | `.onnx` (with `.onnx.json` beside it) | The voice's inputs and output, sample rate and `phoneme_id_map`, and its sidecar config. Its task is `text-to-speech`; a mapping's `text` is found anywhere in what the app speaks, in any case (Piper reads a text case-folded), and its `audio` is a WAV, resampled to the voice's rate. The output waveform is fixed in the contract (use `--output` when the real model's is open: Piper's voices say as long as they speak). Piper's runtimes (its Python package, its command-line program) leave a pause after each sentence (0.2 s), and scale a waveform to its loudest sample. |
| GGUF (llama.cpp) | `.gguf` | A language model's: its task (`text-generation`), its context length, the token that ends its turn, and its metadata (tokenizer, chat template), which goes in `model-files`. An embedding model's (one that pools its tokens' outputs: BERT's, bge-m3, nomic-embed, EmbeddingGemma, Qwen3-Embedding) is an `embedding` model's: its embedding's size and its pooling (mean, the first token's, the last's). A reranker's (a classifier's, `cls`, of RANK pooling: bge-reranker, jina-reranker, Qwen3-Reranker) is a `classification` model's of one label, `relevant`, its output `score` the logit of the mapping's `score`: llama.cpp's server answers a BERT reranker's logit and node-llama-cpp its sigmoid, and both answer a Qwen3 reranker's probability (its yes and no, softmaxed), the mapping's `score` itself. Its tokenizer is BPE, SentencePiece's (BPE or unigram: XLM-R's) or WordPiece. |
| LiteRT-LM | `.litertlm` | An on-device language model's, as Android and iOS apps run them with LiteRT-LM (Gemma's): its KV cache's length, its prefill signatures, the token that ends its turn, and its LLM metadata and tokenizer (SentencePiece's, as Gemma's, or Hugging Face's, as Qwen's and SmolLM's), which go in `model-files`. |
| MNN-LLM | `config.json` (of its folder: `llm_config.json`, `llm.mnn`, `tokenizer.txt` or `tokenizer.mtok`) | An on-device language model's, as MNN's LLM engine runs it (`Llm::createLLM`: Android's and iOS's MNN apps): its context length (`max_all_tokens`), the token that ends its turn (its chat template's, or its tokenizer's stop token that ends a turn), and its configs and tokenizer, which go in `model-files`. axx tokenizes as MNN's engine does, of each of its tokenizers: Hugging Face's BPE (`tokenizer.txt` of type 3: its expression of bytes), tiktoken's (type 1: the longest token first), SentencePiece's (type 0: a vocabulary MNN's export wrote of spaces, which MNN's encoder writes as their bytes) and its pipeline (`tokenizer.mtok`, a Hugging Face tokenizer's in binary), held to MNN's tokens of Qwen2.5's, Llama 3.2's, MiniCPM4's and Gemma 3's. |
| MLC (WebLLM) | `mlc-chat-config.json` (of its folder: `tensor-cache.json`, `tokenizer.json`, the weights' shards) | A language model as MLC LLM compiles it (WebLLM's, in a page, on its GPU; MLC LLM's apps'): its architecture and quantization (the model library compiled for them runs the stub, as it runs the real model), its context window, the token that ends its turn (its chat template's), its manifests of its weights' records and its tokenizer, which go in `model-files`. The architectures of WebLLM's models: llama's (SmolLM2, Llama 3, TinyLlama), Mistral's, Qwen 2's, 3's and 3.5's, Phi-3's (Phi-3.5, Phi-4 mini, Phi-3.5 vision's text), Phi-2's (and Phi-1.5's), Gemma's, Gemma 2's and Gemma 3's, OLMo 2's, StableLM's and GPT-NeoX's (RedPajama), of each quantization WebLLM's models come in: `q4f16_1`, `q4f32_1`, `q0f16`, `q0f32` (and `q3f16_1`). Not Ministral 3's: MLC's YaRN turns the two halves of each of its heads' pairs by different angles, so that no head of it scores a key by its offset alone, as a stub's heads do. |
| Foundation Models | (a Swift type) | None: the stub stands in for a type of the app's own of the `LanguageModel` protocol, and its contract is written by hand (the system's model, `SystemLanguageModel`, gets a stub of its API instead: `axx models contract ios.foundation-models`). Its format is `foundation-models`, of `details.name`, the stub's Swift type, and `details.capabilities`, what it can do as `LanguageModelCapabilities` names it (without it, the system model's: `guidedGeneration`, `toolCalling` and `vision`); its task is `text-generation`. It has no inputs or outputs: its stub answers a prompt. |

A contract of an app's own Foundation Models model, written by hand:

```json title="models/notes/contract.json"
{
  "format": {
    "type": "foundation-models",
    "details": { "name": "NotesModelStub" }
  },
  "task": "text-generation"
}
```

A model the platform provides has no file to read: its API names it, and `axx models contract` writes the API's contract ([Models the platform provides](#models-the-platform-provides)):

```sh
axx models contract ios.vision.text -o models/label-reader/contract.json
```

A model that does not say a tensor's type and shape (a TorchScript module's, an MNN or ncnn model's outputs'), or that the app gives otherwise (an open batch the app always gives one picture), takes them from `--input` and `--output`, by the tensor's name, in quotes (shells read `[` and `?`):

```sh
axx models contract photo-check.pt --task classification --labels labels.txt \
  --input 'images=float32[1,3,224,224]' --output 'output0=float32[1,1000]' \
  -o models/photo-check/contract.json
```

When the model says none and the flags do not either, the command names each tensor and the flags that give it.

What the model carries besides its graph (a TFLite model's metadata and label files) goes in a `model-files` folder beside the contract, and the stub carries it too: an app that reads its labels from the model's metadata reads the stub's.

A language model's contract keeps its tokenizer, so a stub reads a prompt in the real model's tokens and writes its answers in them: its metadata (1.7 MB for SmolLM2's, up to 10 MB for the largest vocabularies) goes in `model-files`. axx tokenizes as the model's runtime does:

- a GGUF model's as llama.cpp does, with its byte-level BPE tokenizers (Llama 3, Qwen 2.5, SmolLM2 and the others llama.cpp splits words for alike), SentencePiece (Mistral, Gemma 3, Phi-3) and WordPiece (BERT's embedding models: bge, all-MiniLM, nomic-embed), held to llama.cpp's tokens of those models;
- an ONNX, ExecuTorch or safetensors model's as Hugging Face's tokenizers do (`tokenizer.json`), with BPE models' (byte-level, or of `▁` for spaces with their bytes' fallback), their normalizers (NFC, NFKC) and their pre-tokenizers' splits, held to transformers.js's tokens of Llama 3.2, Gemma 3, Phi-4, LFM2, SmolLM2, Qwen 2.5 and Qwen 3.

An ONNX language model's stub runs in transformers.js (pages, Electron apps, Node.js services) and in ONNX Runtime GenAI (Windows, Android and iOS apps, services), with the real model's configs: each runtime keeps the model's cache its own way, and the stub keeps up with both.

A safetensors text embedding model's stub embeds in sentence-transformers and in transformers' `AutoModel`, 4.57 and 5 alike, exactly as its mappings say.

A safetensors language model's stub runs in Hugging Face's transformers (`AutoModelForCausalLM`'s `generate`, and the `text-generation` pipeline), 4.57 and 5 alike, in the real model's folder.

An ExecuTorch language model's stub runs in ExecuTorch's text LLM runner (React Native ExecuTorch, ExecuTorch's Android and iOS libraries), with the real model's tokenizer: its prompt in one call, or a token a call (as the runner gives a program of fixed shapes its prompt), and a conversation the runner continues from chat to chat. An ExecuTorch speech recognizer's stub (Whisper's, as React Native ExecuTorch runs it) transcribes a sound of any length up to 30 seconds.

A Whisper's stub, of its weights (safetensors, CTranslate2's `model.bin`, whisper.cpp's model file, OpenAI's checkpoint), transcribes in transformers (its pipeline, in float32, float16 and bfloat16, on CPUs and on a Mac's GPU), in faster-whisper (CTranslate2, in float32 and in int8, which rounds every layer's inputs and weights to 8 bits, as faster-whisper runs on CPUs), in whisper.cpp (on CPUs and on Metal, of its flash attention) and in `openai-whisper` (`whisper.load_model` of the stub's path: of a model's name, it downloads the real one), of the language the runtime detects or the app gives, with timestamps or without.

A Tesseract stub is a small, float LSTM `.traineddata`, with whole-word unichars and no dictionaries. Tesseract's own recognizer matches the picture and decodes the answer. The model's config reads the whole picture as one raw line (page segmentation mode 13) and does not try a second, inverted recognition. An application that sets its own page segmentation mode overrides that config, as it does with any traineddata.

A Piper voice stub is an ONNX model with its `.onnx.json` beside it. The sidecar keeps the real voice's settings but uses `phoneme_type: "text"`, so codepoints are the phonemes and no eSpeak installation is needed. Each mapping's `audio` WAV is the answer the real PiperVoice synthesizes, resampled to the voice's rate.

```sh
axx models contract SmolLM2-360M-Instruct-Q4_K_M.gguf -o models/description-triage/contract.json
axx models contract Llama-3.2-1B-Instruct-ONNX/onnx/model_q4f16.onnx -o models/customs-draft/contract.json
```

A model that reads a text (a text classifier or embedder: BERT's, MediaPipe's) has its tokens' ids for input: its contract's `text` says which input, and how the app tokenizes the text, with the vocabulary it carries. `axx models contract` reads it from a TFLite model's metadata (MediaPipe's WordPiece and regex tokenizers), and from an ONNX model's folder (its `tokenizer.json`, Hugging Face's: WordPiece's, as BERT's normalizer has it, or BPE's), with its task: a classifier of the labels its `config.json` names, or an embedder of its `last_hidden_state` and `sentence_embedding`. Give another ONNX model's with `--vocab vocab.txt`, its WordPiece vocabulary.

An input of a sound's samples (a classifier of sounds: YAMNet, a Create ML sound classifier) gets `audio`, its sample rate: `axx models contract` reads it from a TFLite model's metadata and from a Create ML sound classifier's (whose labels it reads from the classifier at the end of its pipeline); give it others' (`"audio": {"sampleRate": 16000}`).

An input shaped like an image gets an image layout. Check what the app does with a picture before it gives it to the model, and say so in the contract:

| `image` key | What it says |
| --- | --- |
| `layout` | The order of the input's dimensions: `NCHW`, `NHWC`, or `CHW` and `HWC` without a batch. |
| `channels` | `RGB` (the default), `BGR` or `gray`. |
| `resize` | How the app fits a picture to the input's size: `center-crop` (the default: the middle of it, at the input's proportions) or `stretch`. |
| `crop` | The share of the fitted picture's middle the app keeps, of each side: torchvision's and timm's evaluation transforms (the shorter side resized to 256, its middle 224 kept) keep `0.875`. The default keeps all of it. |

How the app scales the pixels (0 to 255, 0 to 1, a mean and a deviation for each channel) makes no difference: matching undoes it.

A detector's contract says what each of its outputs holds, with `detection`. `axx models contract` reads it from a TFLite model's metadata (MediaPipe's detectors, TensorFlow Lite Model Maker's), and `--task detection --labels labels.txt` names the outputs of others:

| `detection` | What the output holds | Detectors |
| --- | --- | --- |
| `boxes`, `classes`, `scores`, `count` | The objects, first: their boxes, their labels' indexes (the labels the `classes` output's), their scores, and how many there are. | SSD and EfficientDet with their post-processing (TensorFlow's object detection API, the TFLite detection post-processing) |
| `anchors` | Each anchor's box and its labels' scores; the app suppresses the overlapping ones. | YOLO (v8 and later), as ONNX and TFLite export it |
| `locations`, `classScores` | Each anchor's box, encoded against the anchor (`anchors`: the anchors' file, the box coder's scales), and each anchor's labels' scores. The app decodes the boxes. | MediaPipe's object detector (EfficientDet-Lite) |

A box's values are in the order `box.order` gives (`ymin,xmin,ymax,xmax`, TensorFlow's, by default for `boxes`; `cx,cy,width,height` in pixels, YOLO's, for `anchors`), as shares of the input picture's size or, with `box.pixels`, in its pixels.

MediaPipe's tasks stretch a picture to the model's input: give its contract's image `resize: stretch`.

A classifier's output says what it holds with `activation`:

- `softmax`: probabilities that add up to 1 (the default);
- `sigmoid`: a probability for each label;
- `none`: logits, which the app turns into probabilities.

## 2. Map inputs to answers

A mapping is a JSON file of the `mappings` folder, as WireMock's stubs are, or one of several under `"mappings"`:

```json title="models/evidence-check/mappings/damage-photos.json"
{
  "mappings": [
    {
      "name": "A crushed carton on the porch",
      "input": {"image": "../../../features/damage-claims/evidence/crushed-box.png"},
      "output": {"label": "carton", "score": 0.91}
    },
    {
      "name": "A screenshot of the shop's order page",
      "input": {"image": "../../../features/damage-claims/evidence/order-screenshot.png"},
      "output": {"label": "web site", "score": 0.88}
    }
  ]
}
```

A mapping's `input` is what it answers:

- `image`: a picture (PNG, JPEG, GIF or WebP) beside the mapping or anywhere;
- `audio`: a sound (a WAV file), for an input of a sound's samples (its first samples, as many as the input has, at its sample rate, resampled from the file's), or of its features (a speech recognizer's log-mel spectrogram, which the stub computes from the sound as the app's processor does);
- `tensor`: the input's values, as nested arrays or a JSON file of them;
- `text`: a text the prompt has, for a language model, or the text a text classifier or embedder (BERT's) reads.

An input of several keys that answer alike lists them: `{"text": ["green gate", "wooden box"]}` answers a prompt that has either, and `{"image": ["parcel-front.jpg", "parcel-side.jpg"]}` either photo (a tensor's values, a list of numbers, are one key). Each is a key of its own, which no other key of the stub may be.

Its `output` is the answer, in the terms of the model's task:

- **A classifier** answers `label` (with `score`, 1 by default) or `scores`, by label.
- **A detector** answers `objects`: each object's `label`, its `box` (`x`, `y`, `width` and `height`, shares of the picture's width and height from its top left corner) and its `score` (1 by default). The stub gives them as the detector's outputs hold them, and nothing else.
- **A segmenter** answers `mask`: a picture (beside the mapping, stretched to the output's size) whose gray levels are each pixel's label's index (0 for the first label); or, for a segmenter of one label or none (a selfie segmenter's person, a text detector's probability map: PaddleOCR's), each pixel's confidence, 255 being 1. An output of `activation: none` holds logits, which the app takes a sigmoid of (a background remover's: BiRefNet's, RMBG's): the stub gives each pixel the logit of its confidence, ±20 for 0 and 1. A segmenter of each label's own confidence (a text detector's of its text's regions and the links between their characters: CRAFT's, as EasyOCR and React Native ExecuTorch run it) answers `masks`: a picture a label, `{"region": "door-region.png", "affinity": "door-links.png"}`, each pixel's gray level the label's confidence; a label without one is 0.
- **An embedder** (`embedding`) answers `embedding`: the values of each output of its size, or a name. An output of each token's (`last_hidden_state`) has them at every token, so the app's mean of them is the embedding. A name is a unit vector of its own (of SHA-256 of the name), so the texts of the mappings that answer one name are one embedding apart (their cosine 1), and those of two names nearly at right angles: a search that should find an article for a question maps both to one name.
- **A language model** (`text-generation`) answers `text`, what it writes; `json` or `toolCalls`, a JSON value or an array of calls it writes compactly (a structured output); or `refusal`, its refusal as text.
- **A text-to-speech model** answers `audio`: a WAV file, resampled to the voice's sample rate and written as the model's fixed waveform, with silence after it or its end trimmed.
- **A text recognizer** (`text-recognition`: CRNN's, PaddleOCR's, EasyOCR's) answers `text`, what it reads, and `score` (1 by default): its output's steps hold each character's class, a blank between two alike, then blanks, as CTC decoding reads them. Its contract's labels are its classes, the blank an empty one (`--labels`: a file of a character a line, the blank an empty line).
- **A speech recognizer** (`speech-to-text`) answers `text`, what it hears said. It writes it after a space, as Whisper does, and, when the app asks for timestamps, from 0 to where the sound ends. A text with a symbol the recognizer's runtime never lets it write (Whisper's `generation_config.json` suppresses `:`, `#`, brackets and others) stops the build, named.
- **Any model but a language model** can give its outputs' values themselves, as `tensors`, by output name. An output an answer does not give is zeros.

One mapping is the default. It answers every input no other mapping matches. A model cannot refuse an input, so a stub always has one:

```json title="models/evidence-check/mappings/default.json"
{
  "name": "Any other photo: the model is unsure",
  "default": true,
  "output": {"label": "carton", "score": 0.3}
}
```

A language model's mappings, in the parcel-claims example:

```json title="models/description-triage/mappings/descriptions.json"
{
  "mappings": [
    {
      "name": "The customer changed their mind",
      "input": {"text": "changed their mind"},
      "output": {"json": {"transitDamage": false, "summary": "The customer changed their mind."}}
    }
  ]
}
```

### Which input matches a mapping

**A text** a text classifier or embedder reads matches the first mapping whose text it has, as words, in the model's tokens: the stub compares them with the input's ids. A BPE tokenizer's tokens of a text depend on what is around it (a space before it, a comma after it), so the stub compares every way the text may be tokenized. **A prompt** matches the first mapping whose text it has, as words: `changed their mind` matches "The customer changed their mind and sent it back", wherever the app puts the shop's description in its prompt (its system prompt, its chat template, quotes around it), but not "changed their minds". Letters' cases count. A prompt that has no mapping's text gets the default's answer. The stub writes the answer token by token, then ends its turn, at any temperature, and through any grammar the answer keeps to.

A language model reads its answer as it writes it, so an answer may not have the text of a mapping before it: the build stops and names both.

**Any other input** matches the mapping whose input it is most like, when it is alike enough. The contract's `match.similarity` sets how alike, from 0 to 1 (0.9 by default). Similarity is measured on a reduced form of each input:

- **A picture** is pooled to a grid of at most 16×16, each channel standardized: a phone camera's noise, another resolution or a slightly different crop still match.
- **A sound** is reduced to its spectrum: its power in 32 bands as wide in mels (finer low, as hearing is), averaged over frames of about 256 samples, as logs, standardized. The sound a little later in the app's window, or quieter or louder, still matches.
- **A speech recognizer's features** (a sound's log-mel spectrogram, Whisper's) are pooled to 16 bands over each tenth of a second, each tenth's bands centered and scaled to the same loudness (what is said, not how loud), a tenth of no sound (silence, a faint hum) left out, then the whole scaled alike. The same recording, louder, quieter or a little noisy, matches; the same speaker's other words do not.
- **Any other tensor** is standardized as a whole.

The same input gets the same answer every time. A mapping's picture is the picture a scenario gives the app (the photo it uploads, the camera's scene), so use the same file in both.

## 3. Build the stub

```sh
axx models build models/evidence-check -o ../infra/models/evidence-check.onnx
```

The stub is a model of the contract's format, with the real model's inputs, outputs and version, and the files it carries. It is a fraction of the real model's size: a stub of an ONNX MobileNetV2 (14 MB) is 30 KB, of a TFLite MobileNet (4.3 MB) 38 KB, of Apple's Core ML MobileNetV2 (24.7 MB) 65 KB, of SmolLM2-360M-Instruct (270 MB) 6.7 MB. A language model's stub is most of all its vocabulary: up to 35 MB for the largest (Gemma's 262,000 tokens). An ONNX language model's stub is its graph alone, 28 to 52 KB, and an ExecuTorch one's its program alone, 59 KB for SmolLM2's (173 MB): its tokenizer and configs stay the real model's files, in its folder.

A stub of an ONNX model whose weights are in files beside it (`model_q4f16.onnx_data`) comes with empty files of their names: a runtime that loads them with the model (transformers.js, as the model's `config.json` says) finds them, and the stub has no weights in them.

A mistake stops the build and names the mapping:

- an unknown label;
- a tensor of the wrong size;
- a picture of one color, which is like no input;
- two mappings of the same input;
- a second default, or none.

Keep the stub with the app's sources, where step 4 puts it, as you keep generated files. A stub is built the same, bit for bit, on every machine: build it again when its contract or mappings change (with the project's other generation steps), and have CI build it and check that nothing changed. The app's build then needs neither axx nor the real model.

## 4. Put it where the app gets its model

The stub takes the real model's place where the app already gets its model. Only the test build or the test deployment changes:

| The app gets its model | The stub goes | Example |
| --- | --- | --- |
| From its own package: an Android app's assets, an iOS app's bundle | Into the test build's package, under the real model's name | The parcels example's couriers' apps |
| From its own files: a model it downloaded the first time it needed it, and keeps | Into the app's files, before the step that starts the app: a folder of the app's (the files pack's `owner: app:<name>`) and its "is a copy of" step. A mobile app is reset before each scenario, and starts only when a step starts it | The parcels example's couriers' Android app (its delivery notes' model, and its damage reports' MNN-LLM model, a folder of files) |
| From a file its configuration names: a service's model path | Into the test deployment, at that path | The services of the parcel-claims, carrier-billing, customs-clearance and parcel-support examples |
| From the system: a model a distribution's package installs (Debian's tesseract-ocr: `/usr/share/tesseract-ocr/5/tessdata/eng.traineddata`) | Over the package's file, in the test deployment's container (a file mounted in its place) | The parcel-support example's service (its label reader) |
| From a model server that serves the model to it over the network (TF Serving: `<model base path>/<version>/saved_model.pb`; OpenVINO Model Server: `<model path>/<version>/model.xml`) | Into the test deployment's model folder, as the model's version. The app then reaches its model over HTTP, which the WireMock image's model mock answers too | |
| From an engine built for its GPU (TensorRT's `.engine` or `.plan`, which Triton's TensorRT backend serves as `model.plan`), built from an ONNX model | Built, from the ONNX stub of the ONNX model's contract, as the deployment builds the real model's engine (`trtexec --onnx`, with its shapes and precision), on the GPU and TensorRT the test deployment runs: an engine is of one GPU and one version of TensorRT. An engine of half precision rounds the stub's values, its answers the same | |
| From a URL its configuration names, which it downloads the model from | To WireMock, which serves the stub at that URL: a mapping whose response's `bodyFileName` is the stub | |
| From the platform, through its API (Vision's requests, ML Kit's clients, Foundation Models' sessions of the system's model and of Apple's server model, a browser's `LanguageModel`) | A stub of the API (`.axxmodel`), into the app's `axx-models` folder, before the step that starts the app or opens its first page: see [Models the platform provides](#models-the-platform-provides) | The parcels example's couriers' apps (their photo checks; the iOS app's delivery notes and missed-delivery cards) and its web portal |
| From a type of its own of Foundation Models' `LanguageModel` protocol (iOS, macOS and visionOS 27), which its sessions take (`LanguageModelSession(model:)`) | Into the test build's sources: the stub is a Swift type of the protocol, which the test build gives its sessions in place of the app's own | The parcels example's couriers' iOS app (its notes model, on an iPhone without Apple Intelligence) |
| From a model's folder (transformers.js's: `config.json`, the tokenizer's files, `onnx/model_q4f16.onnx`; ONNX Runtime GenAI's: `genai_config.json`, the tokenizer's files, `model.onnx`; transformers': `config.json`, the tokenizer's files, `model.safetensors`; faster-whisper's: `config.json`, `tokenizer.json`, `vocabulary.txt`, `model.bin`; MNN-LLM's: `config.json`, `llm_config.json`, `llm.mnn`, `llm.mnn.weight`, `embeddings_bf16.bin` (or the embeddings in `llm.mnn.weight`), `tokenizer.txt` or `tokenizer.mtok`; MLC's, which a web app's WebLLM fetches from the URL its app config gives the model: `mlc-chat-config.json`, `tensor-cache.json`, `tokenizer.json`, `params_shard_*.bin`) | Into the test build's copy of the folder, in place of its model's file (and, of a transformers language model's, its `config.json`), beside the real tokenizer and configs. An MNN-LLM stub is the whole folder: `axx models build -o <folder>/llm.mnn` writes the stub's files and, beside them, the real `config.json` and tokenizer. An MLC stub is the whole folder too (`axx models build -o <folder>`), served where the test build's app config says the model is, and the real model's library runs it | The parcels example's shop portal (its tracking page's WebLLM model too), the parcel-support example's service (faster-whisper, sentence-transformers and transformers) |

A framework that runs the app's own model gets it the same way: the couriers' iOS app runs its model with Vision, from its bundle. A sound classifier an app runs with Sound Analysis (`SNClassifySoundRequest(mlModel:)`, a Core ML model of a sound's samples, as Create ML makes them) is the app's too: the couriers' iOS app hears the doorbell or a knock with one when no one is home. Sound Analysis cuts what the microphone hears into the model's windows and runs the stub on each, as it runs the real model, and a mapping's recording matches each window of it.

**An Android app.** The debug build's assets (`src/debug/assets`) have the stub, under the real model's name, and a release build fetches the real model. The couriers' app runs it with ML Kit's custom image labeling, which reads the labels from the model's metadata and stretches a photo to the model's input (its contract says `resize: stretch`):

```sh
axx models build models/photo-check-android -o ../courier/android/app/src/debug/assets/photo-check.tflite
```

Its damage check is the company's own PyTorch model, saved for PyTorch Mobile, which PyTorch Android Lite runs: the debug build's assets have its stub, and a release build takes the company's model from the path its build gives (`-PdamageCheckModel=<damage-check.ptl>`):

```sh
axx models build models/damage-check-android -o ../courier/android/app/src/debug/assets/damage-check.ptl
```

**An iOS app.** The stub is kept with the app's sources (`Courier/Models/PhotoCheck.mlmodel`), and a build phase compiles it into the app in a Debug build (`xcrun coremlcompiler compile`, as Xcode compiles a model), and the real model in a Release build:

```sh
axx models build models/photo-check-ios -o ../courier/ios/Courier/Models/PhotoCheck.mlmodel
```

**A model of the app's own, of Foundation Models' protocol.** From iOS 27 a session takes a model of the app's (`LanguageModelSession(model:)`, of the `LanguageModel` protocol). An app that ships a type of its own of the protocol gets a stub of that type: a Swift file, kept with the app's sources, of the type the contract names (`format.details.name`, and `format.details.capabilities`, what the app's model can do: the system model's unless it says), which the Debug build compiles and gives its sessions, and which Release builds leave out (`EXCLUDED_SOURCE_FILE_NAMES = CourierNotesModelStub.swift`). The couriers' iOS app summarizes a long delivery note with a model of its own on an iPhone without Apple Intelligence: in a Release build, SmolLM2 (a GGUF it bundles) on llama.cpp, behind its type `CourierNotesModel`; in a Debug build, which leaves that type and llama.cpp out, the stub:

```sh
axx models build models/delivery-note-own-ios -o ../courier/ios/Courier/Models/CourierNotesModelStub.swift
```

```swift title="Courier/DeliveryNotes.swift (excerpt)"
let system = SystemLanguageModel.default
if system.isAvailable {
    return try await respond(LanguageModelSession(model: system, instructions: instructions), note)
}
// An iPhone without Apple Intelligence: the app's own model.
#if DEBUG
let own = CourierNotesModelStub()                       // the stub
#else
guard let own = CourierNotesModel() else { return nil }  // SmolLM2, on llama.cpp
#endif
return try await respond(LanguageModelSession(model: own, instructions: instructions), note)
```

Its scenario stubs the system's model as an iPhone without Apple Intelligence (see [Models the platform provides](#models-the-platform-provides)), so that the app takes its own.

The stub builds with Xcode 27 (Swift 6.4), and runs on iOS, macOS and visionOS 27: an older Xcode builds its file empty. Sessions of the system's model (`SystemLanguageModel`) need no stub in the app's sources: see [Models the platform provides](#models-the-platform-provides).

**A service.** The test deployment mounts the stubs at the paths the service loads its models from. The parcel-claims service checks photos with ONNX Runtime, and triages the shops' descriptions of damage with a language model it runs with llama.cpp:

```yaml title="infra/compose.yaml (excerpt)"
claims:
  environment:
    CLAIMS_PHOTO_MODEL: /models/evidence-check.onnx
    CLAIMS_TRIAGE_MODEL: /models/description-triage.gguf
  volumes:
    - ./models/evidence-check.onnx:/models/evidence-check.onnx:ro
    - ./models/description-triage.gguf:/models/description-triage.gguf:ro
```

**A web page.** The parcels example's shop portal drafts a parcel's customs description with a small language model the page runs itself (transformers.js, on ONNX Runtime's WebAssembly), from the folder the service serves its models from. The test deployment mounts a folder of the real model's configs and tokenizer, and the stub in place of its ONNX file:

```sh
axx models build models/customs-draft -o ../infra/portal-models/customs-draft/onnx/model_q4f16.onnx
```

It cuts a photo of a parcel's contents out of its background the same way (transformers.js's `background-removal`, of BiRefNet_lite, whose image processor's configuration the folder keeps beside the stub); a screenshot of the cutout checks the page:

```sh
axx models build models/contents-cutout -o ../infra/portal-models/contents-cutout/onnx/model_fp16.onnx
```

```yaml title="infra/compose.yaml (excerpt)"
app:
  environment:
    PARCELS_PORTAL_MODELS: /var/lib/parcels/portal-models
  volumes:
    - './portal-models:/var/lib/parcels/portal-models:ro'
```

Then the scenarios test what the app does with each answer, with the steps they already use:

```gherkin
Scenario: A photo that shows no parcel goes to a person
  Given a seeds/PX-4111.yaml dynamodb seed
  And a POST request to /api/claims
  And a request payload using an application/json content example
  And the request payload properties are:
    | parcel | PX-4111 |
    | reason | DAMAGED |
  And the request is executed
  When the evidence/order-screenshot.png file is uploaded to the claim-evidence s3 bucket as claims/CLM-4111/order-screenshot.png
  Then within 30s the claim-reviews s3 bucket has an object named CLM-4111/order-screenshot.png
  And the claims dynamodb table has an item where:
    | id     | CLM-4111                                                  |
    | status | REFERRED                                                  |
    | note   | the photo shows no parcel: it looks like a web site (88%) |
```

### The examples' stubs

Each of axx's examples runs its stubs embedded, in the real runtime inside the app's own process (or on the device, or in the page), the way the app runs the real model, and its acceptance suite passes with them:

| Example | What the model does | Runtime | Format |
| --- | --- | --- | --- |
| [`parcel-claims`](https://github.com/nimbusxr/axx/tree/main/examples/parcel-claims) | checks that an evidence photo shows a parcel | ONNX Runtime, in the service | ONNX |
| | triages a damage claim's description | llama.cpp, in the service | GGUF |
| | transcribes a customer's voice note | whisper.cpp, in the service | whisper.cpp's (ggml) |
| [`carrier-billing`](https://github.com/nimbusxr/axx/tree/main/examples/carrier-billing) | checks that a repack's photo shows a parcel | TensorFlow (its C library), in the service | TensorFlow's SavedModel |
| [`customs-clearance`](https://github.com/nimbusxr/axx/tree/main/examples/customs-clearance) | names what an inspection's photo shows | OpenVINO (its C library), in the service | OpenVINO's IR |
| [`parcel-support`](https://github.com/nimbusxr/axx/tree/main/examples/parcel-support) | hears whether anyone speaks in a voicemail | MediaPipe (its audio classifier), in the service | TFLite (YAMNet) |
| | transcribes a voicemail (a telephone's 8 kHz) | faster-whisper (CTranslate2); OpenAI's whisper, transformers and parakeet.cpp (its command), its profiles | CTranslate2's; OpenAI's `.pt`; safetensors; Parakeet's GGUF (NVIDIA's Parakeet TDT-CTC 110M) |
| | finds the help-centre article that answers a message | sentence-transformers; llama.cpp (llama-cpp-python), its profile | safetensors; GGUF |
| | reranks the articles near a message | llama.cpp (llama-cpp-python), in the service | GGUF |
| | drafts a reply from the article | transformers; ONNX Runtime GenAI, its profile | safetensors; ONNX (GenAI's model builder's) |
| | speaks the confirmation a call-back plays to a customer who left a voicemail | Piper (its command-line program), in the service | Piper's voice (`.onnx` and its `.onnx.json`) |
| | reads the reference on a parcel's label, from a photo of its reference line | Tesseract (the system's: Debian's tesseract-ocr), from the service | Tesseract's traineddata |
| | says what a customer's photo of their parcel shows | PyTorch (TorchScript); ncnn, MNN, ExecuTorch (XNNPACK) and Keras (on PyTorch), its profiles | TorchScript; ncnn's `.param`; `.mnn`; `.pte`; `.keras` |
| [`parcels`](https://github.com/nimbusxr/axx/tree/main/examples/parcels) | checks a proof-of-delivery photo, in the couriers' Android app | ML Kit (custom image labeling, of the app's own model) | TFLite |
| | checks a proof-of-delivery photo, in the couriers' iOS app | Core ML (Vision) | Core ML |
| | refuses a proof-of-delivery photo of the parcel cut off at its edge, in the couriers' iOS app | Core ML (Vision), the app's own object detector (Create ML's) | Core ML (a detector's pipeline) |
| | summarizes a recipient's delivery note on an iPhone without Apple Intelligence, in the couriers' iOS app | the app's own model, of Foundation Models' `LanguageModel` protocol (SmolLM2 on llama.cpp, in a Release build) | a Swift type of `LanguageModel` |
| | hears the doorbell or a knock when no one is home, and records the attempt with it, in the couriers' iOS app (the microphone step's recording, played into the app's own capture) | Core ML (Sound Analysis), the app's own sound classifier (Create ML's) | Core ML (a pipeline classifier) |
| | the same, in the couriers' Android app | MediaPipe (its audio classifier), of the app's YAMNet | TFLite |
| | reports a parcel damaged with a photo of the damage, which the company's own damage classifier checks, in the couriers' Android app | PyTorch Android Lite (PyTorch Mobile's lite interpreter) | PyTorch Mobile (`.ptl`) |
| | writes out a courier's note of a parcel's damage for the damage report, in the couriers' Android app | MNN's LLM engine (MNN-LLM, built into the app's native code), of Gemma 3 270M | MNN-LLM |
| | drafts a parcel's customs description, in the shop portal's page | transformers.js (ONNX Runtime Web) | ONNX |
| | cuts a photo of a parcel's contents out of its background, in the shop portal's page | transformers.js (ONNX Runtime Web), its background removal (BiRefNet_lite) | ONNX (a mask of logits) |
| | checks a recipient's photo of a damaged parcel, in the tracking page | TensorFlow.js | TensorFlow.js graph model |
| | suggests why a recipient sends a parcel back, from their own words, in the tracking page | WebLLM (SmolLM2-135M, on the browser's GPU through WebGPU) | MLC (WebLLM), of float32s (`q4f32_1`), which every GPU runs |
| | summarizes a recipient's delivery note, in the couriers' Android app | LiteRT-LM; Gemini Nano (ML Kit's Prompt API), on a phone that has it | `.litertlm`; a stub of its API (`.axxmodel`) |
| | summarizes a recipient's delivery note, in the couriers' iOS app | Foundation Models (iOS 27), the system's model | a stub of its API (`.axxmodel`) |
| | drafts the card a courier leaves when no one is home, in the couriers' iOS app | Foundation Models (iOS 27), Apple's server model (Private Cloud Compute) | a stub of its API (`.axxmodel`) |
| | tells a photo of the recipient's signed note from a proof-of-delivery photo, reads the parcel's label in one, sees a dog beside the parcel and blurs out the people in it, in the couriers' iOS app | Vision: `VNClassifyImageRequest`, `VNRecognizeTextRequest`, `VNRecognizeAnimalsRequest`, `VNDetectHumanRectanglesRequest`, `VNGeneratePersonSegmentationRequest` | stubs of their APIs (`.axxmodel`) |
| | the same, and refuses a photo of the parcel cut off at its edge, in the couriers' Android app | ML Kit: image labeling, text recognition, object detection, selfie segmentation | stubs of their APIs (`.axxmodel`) |
| | reads a parcel's reference off the depot's label camera's scan, keeps a scan only when it shows a parcel and blurs out the people in it, in the depot desk's Mac builds (AppKit, SwiftUI) | Vision: `VNRecognizeTextRequest`, `VNClassifyImageRequest`, `VNDetectHumanRectanglesRequest`, `VNGeneratePersonSegmentationRequest` | stubs of their APIs (`.axxmodel`) |
| | takes the clerk's dictated handover note, and sums up the day for the next shift, on the Mac or on Apple's servers, in the depot desk's Mac builds | Speech (`SFSpeechRecognizer`, of `AVAudioEngine`'s audio); Foundation Models (macOS 27), the system's model and Apple's server model | stubs of their APIs (`.axxmodel`) |
| | reads a parcel's reference off a scan with the system's Tesseract, and reads the handover rules aloud through the desktop's speech service, in the depot desk's GTK builds | Tesseract's language data; speech-dispatcher, with a Piper voice | a Tesseract stub (`eng.traineddata`); a Piper voice stub |
| | takes a courier's dictated note, in the couriers' iOS and Android apps (the microphone step's recording, played into the app's own capture) | Speech (`SFSpeechRecognizer`, of `AVAudioEngine`'s audio); Android's `SpeechRecognizer` | stubs of their APIs (`.axxmodel`) |
| | puts a recipient's delivery instructions in English, checks their spelling and takes them spoken, in the tracking page | the browser's built-in AI: `LanguageDetector`, `Translator`, `Proofreader`, `SpeechRecognition` | stubs of their APIs (`.axxmodel`) |
| | fills in the address a customer wrote, drafts a message to a parcel's recipient and makes it shorter, and puts a long delivery note in a line, in the shop portal | the browser's built-in AI: the Prompt API (`LanguageModel`, JSON), `Writer`, `Rewriter`, `Summarizer` | stubs of their APIs (`.axxmodel`) |

### Models the platform provides

A model the platform provides is the platform's: the app calls an API (Vision's `VNRecognizeTextRequest`, ML Kit's `TextRecognition`, Foundation Models' sessions of `SystemLanguageModel` and of `PrivateCloudComputeLanguageModel`, a browser's `LanguageModel`), and the platform runs its own model, in no file of the app's. Such a model gets a stub of its API: a `.axxmodel` file, built from a contract and mappings as every stub is, which the scenario puts in the app's `axx-models` folder. A pack of the platform's then answers the app's calls from it, in the app's test runs:

| Pack | Where it answers | How |
| --- | --- | --- |
| `models-ios` | A Debug app on the iOS Simulator (iOS 16 or later; Foundation Models: iOS 27 and Xcode 27) | A library the simulator loads into the app when axx launches it (`DYLD_INSERT_LIBRARIES`), which axx compiles with Xcode and caches |
| `models-macos` | A Mac app's test build, signed without the hardened runtime or with its two exceptions (below), on macOS 15 or later (Foundation Models: macOS 27) | The same library, which macOS loads into the app when axx launches it (`DYLD_INSERT_LIBRARIES`) |
| `models-linux` | A Linux app in the desktops `desktop-linux` runs | Tesseract's documented `TESSDATA_PREFIX`, and speech-dispatcher's configuration in the app's home, which speaks with Piper |
| `models-android` | A debuggable app on the Android emulator (Android 8 or later) | An agent attached to the app as it starts (ART's tooling interface, `am start --attach-agent`), which axx builds with the SDK's NDK and caches |
| `models-web` | The browsers the `web-core` pack runs | A script the browser runs in the app's pages before their own (Playwright's init scripts) |

The app's code does not change, and nothing changes in its release builds or on other devices. Each pack needs `models-core`, `app-core` and its platform's pack (`mobile-ios`, `desktop-macos`, `desktop-linux`, `mobile-android`, `web-core`); the `files` pack copies the stub.

**A Mac app's test build.** macOS ignores `DYLD_INSERT_LIBRARIES` in an app it signs with the hardened runtime, which would leave the app with Apple's models instead of the stubs. A Mac app's test build is signed without it, or with Apple's two exceptions for it, in its Debug entitlements: `com.apple.security.cs.allow-dyld-environment-variables` and `com.apple.security.cs.disable-library-validation`. axx checks the app before it starts it, and stops the scenario if macOS would keep the library out (`AXX-E0704`). A Mac app's `axx-models` folder is in its `./` files, its home's `Library/Application Support`. A Mac has no test's grant of speech recognition or the microphone, as a simulator has: a stubbed recognizer, and the microphone axx feeds, are authorized.

**1. The contract** is the API's: name it, and `axx models contract` writes it. A labeler's takes the labels its mappings answer:

```sh
axx models contract ios.vision.text -o models/label-reader/contract.json
axx models contract android.mlkit.image-labeling --labels labels.txt -o models/hazards/contract.json
```

**2. The mappings** are as every stub's: pictures or recordings and their answers, or texts a prompt has, and the default.

**3. Build it**, and keep it with the acceptance project:

```sh
axx models build models/label-reader -o models/label-reader.axxmodel
```

**4. Put it in the app's `axx-models` folder** with the files pack's steps, before the step that starts the app (or opens its first page). A mobile app reads its folder when it starts, and again when it restarts; a browser's page, when it opens.

```gherkin
Scenario: A recipient's long delivery note is summarized for the courier
  Given a seeds/courier-app-ios-note.yaml db seed
  And the courier-models folder with the following properties:
    | owner | app:courier |
    | path  | axx-models  |
  And the delivery-note.axxmodel file in the courier-models folder is a copy of the models/delivery-note.axxmodel file
  When the courier app is launched
```

| Pack | API (`axx models contract <api>`) | What the app calls | Task |
| --- | --- | --- | --- |
| `models-ios` | `ios.speech` | `SFSpeechRecognizer`: URL and PCM buffer requests | `speech-to-text` |
| | `ios.vision.classify` | `VNClassifyImageRequest` | `classification` |
| | `ios.vision.text` | `VNRecognizeTextRequest` | `text-recognition` |
| | `ios.vision.humans` | `VNDetectHumanRectanglesRequest` | `detection` |
| | `ios.vision.animals` | `VNRecognizeAnimalsRequest` (Vision's `Cat` and `Dog`) | `detection` |
| | `ios.vision.person-segmentation` | `VNGeneratePersonSegmentationRequest` | `segmentation` |
| | `ios.foundation-models` | `LanguageModelSession` of `SystemLanguageModel`: its availability, responses and streams | `text-generation` |
| | `ios.private-cloud-compute` | `LanguageModelSession` of `PrivateCloudComputeLanguageModel`, Apple's server model: its availability, responses and streams. A session of a model of the app's own is Foundation Models', untouched | `text-generation` |
| `models-macos` | `macos.speech` | `SFSpeechRecognizer`: URL and PCM buffer requests | `speech-to-text` |
| | `macos.vision.classify` | `VNClassifyImageRequest` | `classification` |
| | `macos.vision.text` | `VNRecognizeTextRequest` | `text-recognition` |
| | `macos.vision.humans` | `VNDetectHumanRectanglesRequest` | `detection` |
| | `macos.vision.animals` | `VNRecognizeAnimalsRequest` (Vision's `Cat` and `Dog`) | `detection` |
| | `macos.vision.person-segmentation` | `VNGeneratePersonSegmentationRequest` | `segmentation` |
| | `macos.foundation-models` | `LanguageModelSession` of `SystemLanguageModel`: its availability, responses and streams | `text-generation` |
| | `macos.private-cloud-compute` | `LanguageModelSession` of `PrivateCloudComputeLanguageModel`, Apple's server model, as on iOS | `text-generation` |
| `models-android` | `android.speech` | `SpeechRecognizer`, and its on-device factory | `speech-to-text` |
| | `android.mlkit.image-labeling` | ML Kit's `ImageLabeling`, of its own model | `classification` |
| | `android.mlkit.text-recognition` | ML Kit's `TextRecognition` | `text-recognition` |
| | `android.mlkit.object-detection` | ML Kit's `ObjectDetection` (its five categories) | `detection` |
| | `android.mlkit.segmentation` | ML Kit's selfie `Segmentation` | `segmentation` |
| | `android.gemini-nano` | ML Kit's Prompt API (`Generation`): responses and streams | `text-generation` |
| `models-web` | `web.speech` | `SpeechRecognition` and `webkitSpeechRecognition` | `speech-to-text` |
| | `web.language-model` | The Prompt API's `LanguageModel`: prompts, history, clones and streams | `text-generation` |
| | `web.summarizer` | `Summarizer` | `text-generation` |
| | `web.writer` | `Writer` | `text-generation` |
| | `web.rewriter` | `Rewriter` | `text-generation` |
| | `web.translator` | `Translator`: translations and streams | `text-generation` |
| | `web.language-detector` | `LanguageDetector`: an answer's `text` is the language (confidence 1), or its `json` the detector's list of languages and confidences | `text-generation` |
| | `web.proofreader` | `Proofreader`: an answer's `text` is the corrected text (its correction, the part that changed), or its `json` the proofreader's own result | `text-generation` |

**A Linux desktop's models.** A Linux app takes its system's models from files its distribution installs, not from an API: Tesseract's language data, and the voices of the desktop's speech service, speech-dispatcher. Their stubs are those files, built from a contract of their format: a Tesseract language (`eng.traineddata`, of the `tesseract` format) and a Piper voice (its `.onnx` and `.onnx.json`, of the `piper` format). In the app's `axx-models` folder, `models-linux` makes the app's Tesseract read the language from there (`TESSDATA_PREFIX`), and gives the app's speech-dispatcher the voice, which Piper speaks (its `piper` program, on the desktop's PATH) with no sound.

**What the app says.** An app's speech is heard with the step `the {word} app's speaker plays the {string} file`: an utterance of the app's has the file's sound. The voice stub speaks a mapping's recording for a text that has the mapping's key, so the file is that recording, as the voice speaks it.

```gherkin
Scenario: The desk reads the handover rules aloud
  Given the voice.onnx file in the desk-models folder is a copy of the models/handover-voice-linux.onnx file
  And the voice.onnx.json file in the desk-models folder is a copy of the models/handover-voice-linux.onnx.json file
  And the depot app is launched
  And the "Handover" element is clicked in the depot app
  When the "Read aloud" button is clicked in the depot app
  Then within 20s the depot app's speaker plays the "recordings/handover-rules.wav" file
```

A pack answers the APIs its app's folder has stubs of; the app's other calls reach the platform as they would. A folder has one stub of an API; a stub that cannot be read, or a second of an API, stops the app's start (`AXX-E0704`).

**A device without the model.** A stub of `ios.foundation-models` (or `macos.foundation-models`) can be an iPhone (or a Mac) without Apple Intelligence: its contract's `format.details.availability` gives the reason, in Apple's words (`deviceNotEligible`, `appleIntelligenceNotEnabled` or `modelNotReady`), and `SystemLanguageModel`'s `availability` and `isAvailable` give it. A stub of `ios.private-cloud-compute` can say Apple's server model is not there (`deviceNotEligible` or `systemNotReady`), as `PrivateCloudComputeLanguageModel`'s `availability` does. Its mappings are a default only, which the app, told the model is not there, does not ask. An iOS Simulator otherwise has the Mac's model when the Mac has Apple Intelligence, so a scenario of an iPhone without it says so:

```json title="models/no-apple-intelligence-ios/contract.json"
{
  "format": {"type": "platform", "details": {"api": "ios.foundation-models", "availability": "deviceNotEligible"}},
  "task": "text-generation"
}
```

The Android emulator has no Gemini Nano: a scenario without its stub is a phone without it.

**Speech.** The app's microphone hears a recording when a step says so: it waits until the app listens, and the app's stubbed recognizer answers its mapping.

```gherkin
Scenario: A recipient says their instructions
  Given a seeds/portal-instructions-dictated.yaml db seed
  And the "/track/PX-WEB-5704" page is opened
  When the "Dictate" button is clicked
  And the parcels app's microphone hears the "recordings/behind-the-gate.wav" file
  Then the "Instructions for your courier" field has the value "Behind the green gate, please."
```

The step matches the recording to the speech stub's mappings as the stub's keys were made (resampled to its rate), and the recognizer answers it as a final result, with confidence 1; partial results and timings are not played. In a browser, the page's own microphone (`getUserMedia`) records it too, then silence: a page that records its user for a model of its own (a Whisper it runs itself) hears it with no speech stub, and a `SpeechRecognition` started on that microphone's track answers its mapping. The page hears it through Web Audio, which Firefox runs on Linux only with a sound server: on a CI machine with none, start PulseAudio with a sink that plays to nothing (`pulseaudio --start`, then `pactl load-module module-null-sink`). On a phone, the app's own capture hears it, then silence: on iOS, `AVAudioEngine`'s input taps, `AVAudioRecorder`'s files and `AVCaptureSession`'s audio outputs; on Android, `AudioRecord` (converted to its rate, channels and format) and `MediaRecorder`'s files. An app that runs a sound model of its own on its microphone hears the recording with no speech stub: the couriers' apps hear the doorbell or a knock when no one is home, with the app's own sound classifier (Sound Analysis on iOS, MediaPipe on Android).

**What each pack answers**, and what it does not:

- **iOS.** Vision's requests through image and sequence request handlers (the newer Swift Vision request types are not answered). Text recognition answers one observation, of the whole picture. Detections' boxes are shares of the picture; a segmentation's mask is the contract's size. A recognition of a recording, a URL or PCM buffers (non-interleaved floats); `CMSampleBuffer` speech input fails, named. Physical devices and distribution builds are not answered.
- **Android.** ML Kit's vision clients of a `Bitmap` (`InputImage.fromBitmap`; other sources fail, named), and the Prompt API of ML Kit (not every Google AI SDK). Text recognition answers one block of the whole picture; detections have no tracking IDs. ML Kit's clients of the app's own model (custom image labeling) run its model, as any app's model: stub that model instead.
- **Language models.** Text responses, and a stream's answer in one piece. Pictures in prompts, tools, guided generation and token counts are not played.
- **Browsers.** The app's origin, its frames of that origin and its popups. Workers and frames of other origins are not answered.

Natural Language's `NLModel` runs the app's own model, but only a text classifier or word tagger that Create ML made, whose parameters are in Create ML's own format, which Apple does not document: axx writes no stubs of them. An app that runs a text model with Core ML itself (a BERT's or MiniLM's, converted) is stubbed as any Core ML model.

## How a stub answers

A stub's graph reduces its input as matching does, scores it against each mapping's input and against the similarity threshold, and gives the outputs of the best:

| Format | Its graph |
| --- | --- |
| ONNX | Standard operators (`AveragePool`, `MatMul`, `ArgMax`, `Gather`...), at the real model's operator set, from 7 on. A language model's is a decoder of the real one's inputs and outputs, from operator set 13 on: each call writes its tokens in the cache's tensors that grow, and who wrote them (a prompt, or the model), after the conversation's positions the attention mask says it has (a cache that grows each call, as transformers.js keeps it, or a buffer of the conversation's greatest length, as ONNX Runtime GenAI does); the others it gives back as they were (LFM2's convolutions' state). It finds the first mapping whose text the conversation has before the prompt's end, and gives its answer's next token, as far along the answer as the call is past the prompt. Tokens are kept in halves, so a cache of half-precision floats holds them exactly. A speech recognizer's is two: its encoder matches the sound's features to the mappings' and gives the mapping in its hidden state; its decoder writes that mapping's transcript after the tokens the runtime forces (its language, its task), then its end. |
| TFLite | Builtin operators at their first version, which every LiteRT has (`AVERAGE_POOL_2D`, `FULLY_CONNECTED`, `ARG_MAX`, `GATHER`...). |
| ExecuTorch | The ONNX stub's graph, each operator one or more calls of ExecuTorch's portable kernels (`avg_pool2d`, `mm`, `argmax`, `index_select`...), its tensors' memory planned, of the contract's fixed shapes. A language model's is the real one's methods of settings, and a forward of portable kernels' calls whose planned memory keeps the conversation's tokens, and who wrote each (a prompt, or the model: the token the call before gave, at the position after it), from call to call; it finds the first mapping whose text the conversation has before the prompt's end, and gives its answer's next token. A speech recognizer's `encode` pads the sound with silence, computes Whisper's log-mel features of it (as the app's runtime would, in portable kernels) and matches them to the mappings' as an ONNX recognizer's encoder does; its `decode` writes the mapping's transcript after the tokens the app forces, a call each. An app built with only its real model's kernels (ExecuTorch's selective build) lacks the stub's: `axx models build -v` names them. |
| TensorFlow SavedModel | The ONNX stub's graph, each operator as TensorFlow's (`AvgPool`, `BatchMatMulV2`, `ArgMax`, `GatherV2`...), a graph of constants and no variables, with the real model's tags and signature, its inputs and outputs the real model's tensors' names (outputs of one node an `IdentityN` of that name: `StatefulPartitionedCall:0`, `:1`...). It is one file, `saved_model.pb`: TF Serving and `tf.saved_model.load` load the folder it is in. |
| TensorFlow.js | The SavedModel stub's graph, its integers int32s, its constants in one shard beside `model.json` (`group1-shard1of1.bin`, which `axx models build -o model.json` writes), with the real model's signature; of a model of no signature, none, its placeholders and outputs the real model's nodes' names, and no other node one no node takes. |
| Keras | A Functional model of the layers every Keras has (`AveragePooling2D`, `LayerNormalization`, `Dense`, `GlobalMaxPooling1D`, `ReLU`...), its weights in HDF5: the input reduced as matching reduces it, its correlation with each key, the best's one-hot, and the answers' table's product with it; a text's tokens found by an `Embedding` and a causal `Conv1D`; a sound's log by Keras 3's `STFTSpectrogram` (of one sample a frame) or Keras 2's `tf.math.log`. It is the file the real model is, of its version of Keras: a `.keras` file of Keras 3, or a `.h5` file of Keras 3 or Keras 2 (tf.keras, tf_keras), which `load_model` loads. No layer runs Python code (`Lambda`). |
| TorchScript | The ONNX stub's graph as a frozen module's code (`torch.avg_pool2d`, `torch.matmul`, `torch.argmax`, `torch.index_select`...), which TorchScript compiles when it loads it, its tables the module's constants, of the real module's class. `torch.jit.load`, LibTorch and Triton's PyTorch backend load it. |
| PyTorch Mobile | The TorchScript stub's module, and its forward's bytecode (version 8), which the lite interpreter runs without compiling code: each call's arguments loaded from registers and constants, then its operator (`aten::avg_pool2d`, `aten::matmul`, `aten::argmax`, `aten::index_select`...). `axx models build -v` names the operators the real model's bytecode does not call, which a selective build lacks. |
| MNN | The ONNX stub's graph, each operator as MNN's converter translates an ONNX model's (`BinaryOp`, `MatMul`, `ArgMax`, `GatherV2`...; its integers int32s, its pooling of MNN's packed layout). MNN's CPUs of ARM compute in half precision at its normal precision: a stub's values come out rounded to half precision there, its answers the same; at high precision, as they are. |
| ncnn | Its own layers, which every ncnn build registers (`Pooling`, `InnerProduct`, `Reduction`, `BinaryOp`, `Clip`...): the input reduced as matching reduces it (a sound's spectrum by `InnerProduct`s of its frames), its correlation with each key, the best's one-hot (`Reduction`'s greatest, and `Clip`), and the answers' table's product with it, its weights in the `.bin` beside the `.param` (`axx models build -o model.param` writes `model.bin`). ncnn has no comparisons of tokens: its stubs match pictures, sounds and tensors. ncnn's CPUs of ARM compute in half precision by default (`use_fp16_arithmetic`): a stub's values come out rounded to half precision there, its answers the same. |
| OpenVINO | The ONNX stub's graph, each operator as OpenVINO's operation sets have it (`AvgPool`, `MatMul`, `TopK`, `Gather`...), its weights in the `.bin` beside the XML (`axx models build -o model.xml` writes `model.bin`). OpenVINO's CPUs of ARM compute in half precision unless the app asks for full (`INFERENCE_PRECISION_HINT`): a stub's values come out rounded to half precision there, its answers the same. |
| Core ML (a detector's pipeline) | A network that gives each answer's objects as its first candidates (their boxes' centers and sizes, their labels' scores), the rest scoring nothing, then the real model's non-maximum suppression, which keeps them: Vision recognizes the answer's objects (`VNRecognizedObjectObservation`), labels, confidences and boxes. An answer whose objects overlap more than the suppression keeps stops the build, named. |
| Core ML | A neural network of the layers every Core ML has had (pooling, mean-variance normalization, inner product, argmax, embedding), at the real model's specification version: a classifier's label and probabilities are Core ML's, from its class labels. A text encoder's is of Core ML 3's N-D layers (`equal`, `convolution`, `reduceMax`, `batchedMatmul`, `argMax`, `embeddingND`), of its inputs' shapes: its ids compared with each token of the mappings' texts, each text's tokens found one after another, its last word's end after them; its answers exact on the CPU, the GPU and the Neural Engine. |
| LiteRT-LM | A TFLite model of the real model's prefill and decode signatures, whose KV cache holds the conversation's tokens and who wrote them: decode finds the first mapping whose text the prompt has, and gives its answer's next token. Its LLM metadata and tokenizer are the real model's: a stub of a Gemma 3 model is its tokenizer (4.6 MB) and 54 KB. |
| MNN-LLM | The GGUF stub's transformer as MNN's ops, a decoder around MNN's `Attention` op (which keeps the KV cache, of the engine's): of the embeddings the engine looks up itself (`embeddings_bf16.bin`, narrow: the transformer's first matrix widens them), the positions' rotations computed in the graph, and the logits of the tokens the stub writes, gathered into the vocabulary's. Its `llm_config.json` is the real one's (its chat template, its sampler) of the stub's sizes; the real `config.json` and tokenizer go beside it. Its residual stream is scaled so that MNN's low precision, in halves, keeps it: a stub answers alike on the CPU, at low and high precision, and on Metal, of turns on their own (MNN's default) or kept (`reuse_kv`). |
| MLC (WebLLM) | The GGUF stub's transformer laid out in the real model's own architecture, which its library is compiled for: its width, layers, heads and the key-value heads they share, its MLP's size, its rotary angles (Llama 3's scaling, longrope's short factors, the head dimensions Phi-4 leaves still), its stages packed onto the first of its layers and the rest of no weights. Each matrix is written in the real model's records and quantization: of `q4f16_1`'s groups of 32, each group's scale one whose 4-bit multiples are its values exactly (a stub's groups have few values, of whole multiples of each other); its norms of a ballast whose RMS is a power of two, exact in halves and bfloat16s. Qwen 3's heads' norms keep each head's query and key: a content head has a key-value head of its own (or, of too few, they share them, each key of a sink its norm divides by). Gemma's embeddings are the stub's ÷ √E, which Gemma multiplies them by; Gemma 2's and 3's norms of each sublayer's output keep its features, of a sink each layer adds to its output and the norm drops; Gemma 2's cap of its logits keeps the next token's lead, of codes that share a dimension at most; Gemma 3's content heads are of its global layers, the others of a window. OLMo 2's attention and MLP read the residual stream as it is (their weights of the ballast ÷ its value), and its queries' and keys' norms are of the whole projection's. A LayerNorm's (StableLM's, GPT-NeoX's, Phi-2's) takes the stream's mean from it first: a stub's stream sums to 0 at every position, of a ballast of b and of −b, every embedding's and every head's and unit's output balanced in dimensions of their own, so that the LayerNorm is an RMS norm. Phi-2's attention and MLP read the stream alike, as the layer began: a stage's units are of the layer after its heads'. Qwen 3.5's heads are of its layers of full attention (every fourth), each head's output's gate 1, and its layers of linear attention (Gated DeltaNet) of no weights add nothing; Phi-3.5 vision's encoder is of none, an image's tokens of the ballast alone, of no code. A content head of a pair that turns more than a third of a circle over the context scores its offsets of phases: a head of each third, which the presence sums. A stub of SmolLM2-135M is 74 MB, as the real one's quantization makes it; of Mistral 7B, 3.9 GB. WebLLM answers its chats as the stub says, greedily and sampled, on a GPU (a machine of none, whose WebGPU is SwiftShader's, runs it too slowly to test). |
| Foundation Models | A Swift type of the `LanguageModel` protocol, and its executor: it reads the session's conversation (the text of its instructions, prompts, tools' outputs and responses), finds the first mapping whose text it has, as words, and answers that mapping's text (or its JSON, which a session asked for a generable type reads as one) in one piece. Its capabilities are the contract's. |
| safetensors | The GGUF stub's transformer as a `LlamaForCausalLM`: its weights in `model.safetensors`, and the `config.json` of them beside it (`axx models build -o <folder>/model.safetensors` writes both, in place of the real model's), its rotary positions llama.cpp's (`longrope`'s factors). The real model's tokenizer and generation config stay. Its config says `float32`: an app that loads it in `bfloat16` rounds its values, and its answers. A text embedding model's is the GGUF embedding stub's transformer as a `Gemma3TextModel` of bidirectional attention (`use_bidirectional_attention`, which transformers has from 4.56), of the real model's hidden size: every token's output is the text's embedding, so sentence-transformers' pooling and normalization (the real model's modules) and an app's own pooling give it. A model's Dense modules are written too (each module's `model.safetensors` and `config.json`, in its folder), of the features they take and give, which pass the embedding on as it is. |
| Whisper's weights (safetensors, CTranslate2, whisper.cpp, OpenAI Whisper) | Whisper's own architecture, of the real model's sizes: its convolutions, transformer layers, LayerNorms and GELUs. The encoder sums each band's mels, pools them to tenths of a second, scales each tenth to its own loudness (a LayerNorm), hears it or not (an attention's softmax, of how loud it is), and takes each heard tenth's dot products with the mappings' keys; it takes their means, a key's, and the share of heard tenths. The decoder's cross-attention scores each key its mean's square, and the default the threshold's, of the share of heard tenths (so that no root is taken), and writes the best's transcript after the tokens the runtime forces, its timestamps from 0 to the end of the last heard tenth. Every value fits half precision, and every choice has a margin over its rounding: whisper.cpp's weights are of half precision (its convolutions take no other), as GPUs' runs are. Each layer reads only what it needs, of values of one size, so that CTranslate2's int8, which rounds each layer's inputs to a 127th of their largest, keeps them. A stub is the real model's size (its token embeddings are most of it): 75 MB of whisper.cpp's tiny and of OpenAI's, 148 MB of transformers' in float32. |
| Parakeet | parakeet.cpp's FastConformer of one layer, and a CTC head, with a frontend of its own (32 mel bands of the log of one plus their power, never below 0): the subsampling's convolutions carry each band to the encoder, its attention averages the sound's normalized frames, and its head scores the average's direction against each mapping's key, and the default at the threshold, and spells the nearest's transcript (one token an answer). parakeet.cpp decodes it with CTC: a CTC model's app has no other decoder, and a hybrid TDT-CTC model's chooses it (`--decoder ctc`). It resamples a sound of another rate linearly, as the stub's keys are. |
| GGUF | A transformer of llama's architecture, of five layers, with the real model's vocabulary, tokenizer and chat template. Its heads at fixed offsets give each position the tokens before it; where they hold a chunk of a mapping's text, the position flags it; the prompt's mapping is the first whose chunks it has all of; and the answer's next token follows from its mapping and the tokens before it. Every weight and value is exact in half precision, so llama.cpp answers the same on the CPU and on GPUs. An embedding model's is of the real one's embedding size and pooling, its attention the whole text's, so every token's output is the text's embedding, whatever the app pools: its norms' epsilon (2^40) outweighs every value's square, so each is linear and exact; its last layer takes every feature out (exactly: each step's 1 less 1, each head at an offset again, negated) and adds the mapping's embedding, each value a sum of three halves. A WordPiece model's keys are whole words: the token after a key's last begins another.  A reranker's is the embedding stub's transformer as a NeoBERT (llama's transformer, of the architectures llama.cpp gives a classifier and no softmax), its classifier the sum of its mapping's score as three halves: llama.cpp's server answers its logit, node-llama-cpp's ranking its sigmoid. A Qwen3 reranker's is that transformer as a Qwen3 (its attention causal, so its last token, which llama.cpp pools, has the whole text; its heads wider, every pair of them rotated as Qwen3 rotates them, its content's pair too slowly to tell), its classifier's yes that sum and its no 0: llama.cpp's softmax of them is the mapping's score. |

Every runtime of the format runs it but a minimal build made for one model alone (`axx models build -v` names the operators the real model does not use). Answers that end alike (a detector's: its objects, then nothing for thousands of anchors) are stored to where they differ and padded (ONNX's `Pad`, TFLite's `PADV2`): a stub of MediaPipe's EfficientDet-Lite0 (13.8 MB) is 766 KB, most of it the metadata it carries. A segmenter's masks are stored as each pixel's label (ONNX's `OneHot`, TFLite's `ONE_HOT`): a stub of DeepLab v3 (2.7 MB) is 207 KB. In ONNX, answers of at most 256 values (a mask of confidences: its gray levels) are stored a byte a value, with the values they take (`Gather`): the parcels portal's stub of BiRefNet_lite (115 MB), of two masks of 1024×1024, is 2 MB.

## Add a format, a task or a kind of input

The `models-core` pack's formats, tasks and kinds of input are its own packages' registrations, and a pack of yours adds its own the same way: a format of models axx does not write yet, the answers of a task it does not know, a kind of input a mapping keys on. List your pack in `axx-packs.yaml` beside `models-core`, and `axx models contract` and `axx models build` use what it registers. The four steps stay the same.

| Registry | What it takes | The models-core pack's |
| --- | --- | --- |
| `format.Register` | A format: its type and models' extensions, a reader of a model's contract (or none: the contract is written by hand), a writer of a stub, as one file or a folder of files, a reader of a model's operators when its contract needs more than the model (a language model's tokenizer), and whether its models are an API's, of no tensors (`API`: Foundation Models'). A format's own settings go in the contract's `format.details`. | ONNX, TFLite, Core ML, GGUF, LiteRT-LM, MNN-LLM, MLC (WebLLM), ExecuTorch, OpenVINO, TensorFlow SavedModel, TensorFlow.js, Keras, safetensors, TorchScript, PyTorch Mobile, MNN, ncnn, CTranslate2, whisper.cpp, OpenAI Whisper, Parakeet, Tesseract, Piper, Foundation Models, and the platform APIs' stubs (`.axxmodel`) |
| `task.Register` | A task: the keys of its answers (and which of them name files), and how an answer becomes the outputs' values (or, for a model that generates text, the text), and what a contract of the task must give. | `classification`, `detection`, `segmentation`, `embedding`, `text-recognition`, `speech-to-text`, `text-generation`, `text-to-speech` |
| `stub.RegisterInput` | A kind of input: its key in a mapping's `input`, and how a mapping's value becomes the model's input, which is matched to the nearest, or the text a prompt has. | `image`, `audio`, `tensor`, `text` |

```go title="steps/modelformats/rknn.go"
package modelformats

import (
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{
		Type: "rknn", Name: "RKNN", Extensions: []string{".rknn"},
		Contract: readContract, // the model's inputs and outputs, from its .rknn
		Write: format.File(func(s *stub.Stub) ([]byte, error) {
			if s.Nearest == nil {
				return nil, format.Unmatched("RKNN", s)
			}
			// The graph of the nearest matcher: s.Nearest.Reducer reduces the input,
			// s.Nearest.Table holds the keys, s.Answers the answers.
			return graph(s), nil
		}),
	})
}
```

A format's stubs answer as the stub does (`stub.Stub.Answer`, the reference): `modelstest.Conform` holds them to the cases every format of axx is held to, run in the format's own runtime.
