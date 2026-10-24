# Parcel support: an axx example in Python

**Support** is the parcels service's support desk. Customers write to it, or call its
support line after hours and leave a voicemail, and it opens a ticket of each for the
support team. It is a Python service (FastAPI, with PostgreSQL), and it runs eight models:

- **Voicemails:** the phone system (Asterisk) keeps each voicemail in the support mailbox's
  folder. The service hears whether anyone speaks in it, with an audio classifier, YAMNet,
  which MediaPipe runs over each second of it (a call the caller hung up on leaves only the
  line's hum, and opens no ticket). It transcribes it with a speech recognizer, Whisper
  base.en, and opens a ticket of the transcript. faster-whisper (CTranslate2, of 8-bit
  integers on the CPU) runs it, or OpenAI's whisper package or transformers; or NVIDIA's
  Parakeet (TDT-CTC 110M) does, which parakeet.cpp's command runs: the deployment chooses
  (`SUPPORT_TRANSCRIBER`).
- **Call-backs:** the phone system then calls the customer back, and plays them a
  confirmation that their message is in, which the service speaks with a voice of Piper's,
  en_US-kathleen-low (16 kHz, a telephone's), with Piper's command-line program. The
  service writes it into the support line's sounds, and queues the call as Asterisk takes
  calls: a call file in its spool's outgoing folder.
- **The help centre:** a sentence embedding model, all-MiniLM-L6-v2, which
  sentence-transformers runs, or llama.cpp, a GGUF of it (the deployment chooses,
  `SUPPORT_HELP_CENTRE`), finds the help-centre articles near a message, and a
  reranker, Qwen3-Reranker-0.6B, which llama.cpp runs in the service (llama-cpp-python),
  reads the message with each and picks the one that answers it.
- **Draft replies:** a small language model, SmolLM2-135M-Instruct, which transformers
  runs, or ONNX Runtime GenAI, exported for it (the deployment chooses, `SUPPORT_DRAFTER`),
  drafts a reply from the article, for an agent to send. A message no article answers is
  for an agent to answer.
- **Photos:** a customer whose parcel arrived damaged sends a photo of it from its tracking
  page, which saves it in a folder the support desk shares with the tracking site. The
  service keeps it with the parcel's ticket, and an image classifier, MobileNetV3 (of
  ImageNet's classes), says what it shows: a photo of the parcel makes the ticket ready for
  the shop's claim. PyTorch runs it, as TorchScript; or ncnn or MNN (which small ARM servers
  run it with), converted for them; or ExecuTorch, exported for it; or Keras: the deployment
  chooses (`SUPPORT_PHOTO_CHECK`).
- **Labels:** the tracking page also asks for a photo of the parcel's label, which it crops
  to the label's reference line. The service reads the reference with the system's
  Tesseract (Debian's tesseract-ocr, of its English model): a label of the parcel confirms
  the ticket's parcel, and a label of another parcel is set aside.

axx tests it the way customers, the phone system and the tracking site use it: by sending
messages to its API, by leaving voicemails in the mailbox's folder as Asterisk does, and by
saving photos in the uploads folder as the tracking site does. It then checks the tickets,
in the API's answers and in PostgreSQL, and the folders. The service
knows nothing about axx, and runs no model the tests have not stubbed.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `messages` | A question the help centre answers gets its article and a draft reply. A question no article answers is for an agent, as is one near an article that does not answer it. | REST with OpenAPI (FastAPI's document), stubs of the help centre's embedding model and reranker, and of the drafts' language model |
| `voicemails` | A voicemail about a parcel gets the article that answers it and a draft reply. A voicemail no article answers is for an agent. Both are moved to the mailbox's Old folder, and their callers called back with a spoken confirmation, of the parcel when the voicemail names one. A voicemail no one speaks in opens no ticket. | files in a folder the service watches, PostgreSQL rows, the phone system's spool and sounds, stubs of the voicemails' audio classifier and speech recognizer, and of the call-backs' voice |
| `photos` | A photo of the damaged parcel makes its ticket ready for the claim. A photo that shows no parcel leaves the ticket as it was. A photo of a parcel nobody wrote about is set aside. A photo of the parcel's label confirms the ticket's parcel; one of another parcel's label is set aside. | files in a folder the service watches, PostgreSQL rows, stubs of the photo check's classifier and of the system Tesseract's English model |

## Run it

You need Docker with Compose v2, and axx. From `acceptance/`:

```sh
cd acceptance
axx run
```

`axx run` works through these steps:

1. It builds the models' stubs (`axx models build`), into `../infra/models`.
2. It runs `docker compose up --build` in `../infra`, which starts PostgreSQL and the
   service (the first run builds its image: PyTorch is among its dependencies, and
   llama-cpp-python builds llama.cpp).
3. It waits for `http://localhost:8800/health`: the service answers once it has loaded its
   models (its log says which engines run them).
4. It runs the features in parallel.
5. It removes everything with `docker compose down -v`.

Each profile runs the suite with another engine of the transcriber or the photo check, and
a stub of its model in that engine's format:

```sh
axx run --profile openai-whisper   # the voicemails, with OpenAI's whisper package
axx run --profile transformers     # the voicemails, with transformers
axx run --profile parakeet.cpp     # the voicemails, with Parakeet, which parakeet.cpp runs
axx run --profile onnxruntime-genai  # the replies, drafted with ONNX Runtime GenAI
axx run --profile llama.cpp        # the help centre's search, with llama.cpp
axx run --profile ncnn             # the photos, with ncnn
axx run --profile mnn              # the photos, with MNN
axx run --profile executorch       # the photos, with ExecuTorch
axx run --profile keras            # the photos, with Keras
```

The service's own tests are in `app/tests` (`uv run pytest`, in `app`).

## The models, stubbed

Tests do not need the real models. Each of the service's models has a stub in
`acceptance/models`: the model's contract (`contract.json`, written from the real model
with `axx models contract`, its tokenizer in `model-files`), and mappings from what the
scenarios give the model to what it answers.
See [Stub embedded models](https://axx.nimbusxr.us/guides/stub-embedded-models/).

| Model | Real model | Stub | Its mappings |
| --- | --- | --- | --- |
| `transcriber/faster-whisper` | Whisper base.en, in CTranslate2's format (faster-whisper's: `model.bin`, 145 MB, half precision) | `model.bin`, 141 MB | the voicemails the scenarios leave (`features/voicemails/recordings`, recorded with espeak-ng, at a telephone's 8 kHz) to their transcripts |
| `transcriber/openai-whisper` | Whisper base.en, OpenAI's checkpoint (`base.en.pt`, 145 MB) | `base.en.pt`, 141 MB | the same |
| `transcriber/transformers` | Whisper base.en, Hugging Face's (`openai/whisper-base.en`: `model.safetensors`, 290 MB) | `model.safetensors`, 277 MB | the same |
| `transcriber/parakeet.cpp` | NVIDIA's Parakeet TDT-CTC 110M, as parakeet.cpp publishes it (`tdt_ctc-110m-q8_0.gguf`, 178 MB), which the service decodes with its CTC head (`--decoder ctc`); its contract read from the file's header | `tdt_ctc-110m-q8_0.gguf`, 82 KB | the same |
| `help-centre/sentence-transformers` | all-MiniLM-L6-v2 (`model.safetensors`, 91 MB) | `model.safetensors` and its `config.json`, 51 MB | a text about each article's subject to the article's embedding, by name: the article and the messages about it are one embedding, and the others far from it |
| `help-centre/llama.cpp` | the same model, a GGUF of it (`all-MiniLM-L6-v2-Q8_0.gguf`, 25 MB, whose pooling is its tokens' mean) | `help-centre.gguf`, 10 MB | the same |
| `reranker` | Qwen3-Reranker-0.6B, a GGUF of it (`qwen3-reranker-0.6b-q8_0.gguf`, 640 MB) | `reranker.gguf`, 189 MB | each article's text (in the pair of a message and the article) to its relevance, and a business account's question to none |
| `drafter/transformers` | SmolLM2-135M-Instruct (`model.safetensors`, 269 MB) | `model.safetensors` and its `config.json`, 19 MB | the parcels the messages are about (or their damage) to the replies the model drafts |
| `drafter/onnxruntime-genai` | the same model, exported for ONNX Runtime GenAI with its model builder (`python -m onnxruntime_genai.models.builder -p int4 -e cpu`: `model.onnx` and its data, 86 MB), its cache's head size from its `genai_config.json` | `model.onnx`, 44 KB | the same |
| `voicemail-screen` | YAMNet, as MediaPipe's audio classifier has it (`yamnet.tflite`, of AudioSet's 521 sounds, 4 MB) | `yamnet.tflite`, 296 KB | a second of the empty call's hum (`features/voicemails/recordings/msg-empty.wav`) to Silence, and anything else to Speech |
| `voice` | Piper's en_US-kathleen-low (`en_US-kathleen-low.onnx` and its `.onnx.json`, 63 MB), its waveform six seconds long (`--output 'output=float32[1,1,1,96000]'`: Piper's voices say as long as they speak) | `voice.onnx` and its `.onnx.json`, 709 KB | the call-backs' texts (the parcel a confirmation names) to what the voice says (`features/voicemails/confirmations`, recorded with espeak-ng at the voice's 16 kHz, with the pause Piper leaves after a sentence) |
| `label-reader` | Tesseract's English model as Debian's tesseract-ocr installs it (`/usr/share/tesseract-ocr/5/tessdata/eng.traineddata`, 4 MB); its contract written by hand (a 288×36 line's image, the characters of a reference) | `eng.traineddata`, 316 KB (a model of a whole line's image, which Tesseract reads at once), mounted over the system's | the labels' reference lines the scenarios upload (`features/photos/labels`) to the references they show |
| `photo-check/torchscript` | torchvision's MobileNetV3 Small, of its ImageNet weights, scripted (`torch.jit.script(...).save`: `photo-check.pt`, 10 MB) | `photo-check.pt`, 25 KB | the photos the scenarios upload (`features/photos/photos`) to what the model sees in them (a carton, a web site) |
| `photo-check/ncnn` | the same model, converted for ncnn with pnnx (from its ONNX export: `photo-check.param` and `.bin`, 5 MB) | `photo-check.param` and `.bin`, 23 KB | the same |
| `photo-check/mnn` | the same model, converted for MNN with MNNConvert (from its ONNX export: `photo-check.mnn`, 10 MB) | `photo-check.mnn`, 24 KB | the same |
| `photo-check/executorch` | the same model, exported for ExecuTorch and delegated to XNNPACK (`torch.export`, `to_edge_transform_and_lower`: `photo-check.pte`, 10 MB) | `photo-check.pte`, 27 KB | the same |
| `photo-check/keras` | Keras's MobileNetV3Small, of its ImageNet weights (`model.save`: `photo-check.keras`, 11 MB) | `photo-check.keras`, 28 KB | the same |

The language models' stubs are about the real models' size (their vocabularies' embeddings
are most of them), so the example does not keep them: `axx models build` writes them as the
example starts (`acceptance/axx.yaml`), each into its model's folder in `infra/models`,
beside the real model's tokenizer and configuration (which the example keeps). The photo
check's stubs, Parakeet's, the GenAI drafter's, the voice's, the voicemail screen's and the
label reader's are small, and kept (`go generate ./...`, in axx's repository, builds them
again). The compose file gives the folders to the service, where a deployment of the real
models gives it theirs.

The photo check's contracts say how the service fits a photo to the model, as the
frameworks' own transforms do: torchvision's keeps the middle 224 of a photo whose shorter
side it resizes to 256 (`"crop": 0.875`, for TorchScript, ncnn, MNN and ExecuTorch), and Keras's
applications take the photo stretched to their size (`"resize": "stretch"`). ncnn computes
in half precision on ARM's processors, by its default, as MNN does at its normal precision:
the stubs answer the same there, as half precision rounds them (0.9098 for 0.91).

## The service

| Setting | Purpose |
| --- | --- |
| `SUPPORT_DATABASE_URL` | PostgreSQL, where the tickets are |
| `SUPPORT_VOICEMAIL` | the support line's mailbox folder, as the phone system keeps it (`INBOX`, `Old`) |
| `SUPPORT_UPLOADS` | the folder the tracking site saves customers' photos in (`<parcel>/<photo>`); the service moves each it takes to `kept/<parcel>`, or to `unmatched/<parcel>` when nobody wrote about the parcel |
| `SUPPORT_MODELS` | the folder of the models' folders: `transcriber`, `help-centre`, `reranker` (`reranker.gguf`), `drafter`, `photo-check`, `voice`, `voicemail-screen` |
| `SUPPORT_TRANSCRIBER` | the transcriber's engine: `faster-whisper` (the default), `openai-whisper`, `transformers` or `parakeet.cpp` |
| `SUPPORT_HELP_CENTRE` | the help centre's engine: `sentence-transformers` (the default) or `llama.cpp` |
| `SUPPORT_DRAFTER` | the drafter's engine: `transformers` (the default) or `onnxruntime-genai` |
| `SUPPORT_PHOTO_CHECK` | the photo check's engine: `torchscript` (the default), `ncnn`, `mnn`, `executorch` or `keras` |
| `SUPPORT_SPOOL` | the phone system's spool folder (`outgoing`, `tmp`), which it takes call-backs from; unset, voicemails get none |
| `SUPPORT_SOUNDS` | the support line's sounds folder, which the phone system plays the call-backs' confirmations from |
| `SUPPORT_TRUNK` | the SIP trunk the phone system calls customers back on |

| API | Purpose |
| --- | --- |
| `POST /api/messages` | a customer's message (`customer`, `text`) opens a ticket, which it answers |
| `GET /api/tickets/{id}` | a ticket |
| `GET /openapi.json` | the API's OpenAPI document, which FastAPI writes |
