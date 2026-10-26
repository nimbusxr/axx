// Package modelscore is the models-core pack: stubs of embedded models, the
// models an app or a service loads and runs itself. A stub is a model of the
// real one's format, with its inputs and outputs, whose answers are its
// mappings: the app's runtime loads it as it loads the real model, and an
// input like a mapping's gets the mapping's answer. The app's own build ships
// it in place of the real model, which no test needs.
package modelscore

import (
	"github.com/nimbusxr/axx/core"
	_ "github.com/nimbusxr/axx/packs/models/core/format/coreml" // the formats stubs are written in
	_ "github.com/nimbusxr/axx/packs/models/core/format/ctranslate2"
	_ "github.com/nimbusxr/axx/packs/models/core/format/executorch"
	_ "github.com/nimbusxr/axx/packs/models/core/format/foundationmodels"
	_ "github.com/nimbusxr/axx/packs/models/core/format/gguf"
	_ "github.com/nimbusxr/axx/packs/models/core/format/keras"
	_ "github.com/nimbusxr/axx/packs/models/core/format/litertlm"
	_ "github.com/nimbusxr/axx/packs/models/core/format/mlc"
	_ "github.com/nimbusxr/axx/packs/models/core/format/mnn"
	_ "github.com/nimbusxr/axx/packs/models/core/format/ncnn"
	_ "github.com/nimbusxr/axx/packs/models/core/format/onnx"
	_ "github.com/nimbusxr/axx/packs/models/core/format/openaiwhisper"
	_ "github.com/nimbusxr/axx/packs/models/core/format/openvino"
	_ "github.com/nimbusxr/axx/packs/models/core/format/parakeet"
	_ "github.com/nimbusxr/axx/packs/models/core/format/piper"
	_ "github.com/nimbusxr/axx/packs/models/core/format/platform"
	_ "github.com/nimbusxr/axx/packs/models/core/format/safetensors"
	_ "github.com/nimbusxr/axx/packs/models/core/format/tensorflow"
	_ "github.com/nimbusxr/axx/packs/models/core/format/tesseract"
	_ "github.com/nimbusxr/axx/packs/models/core/format/tfjs"
	_ "github.com/nimbusxr/axx/packs/models/core/format/tflite"
	_ "github.com/nimbusxr/axx/packs/models/core/format/torchscript"
	_ "github.com/nimbusxr/axx/packs/models/core/format/whispercpp"
)

// Name is the pack's name.
const Name = "models-core"

const packDoc = `Stub the models your apps and services run themselves (embedded models): an image classifier on a phone, a model a service loads with ONNX Runtime. A stub is a model in the real one's format, with its inputs and outputs, whose answers are its mappings: the app's runtime loads it as it loads the real model, and an input like one of the mappings' gets that mapping's answer. A stub is a few kilobytes and answers at once, the same every time, where a real model fills a simulator's or an emulator's memory and answers as it likes. Models a service reaches over HTTP are WireMock's: see the model mocks of the WireMock image.

A stub's directory has its model's contract, ` + "`contract.json`" + `, what the app relies on from the model: ` + "`axx models contract <model>`" + ` writes it from the real model, once, and the project keeps it, so no test needs the real model. Its ` + "`mappings`" + ` folder has the mappings, as WireMock's has its stubs: each JSON file a mapping, or several under ` + "`mappings`" + `. A mapping's ` + "`input`" + ` is a picture (` + "`image`" + `) or a tensor's values (` + "`tensor`" + `), and its ` + "`output`" + ` the answer, in the terms of the model's task (a classifier's ` + "`label`" + ` and ` + "`score`" + `) or as the outputs' values (` + "`tensors`" + `). An input matches the mapping whose input it is most like, when it is alike enough (the contract's ` + "`match.similarity`" + `), and the default mapping (` + "`\"default\": true`" + `) answers the others.

` + "`axx models build <stub> -o <file>`" + ` writes the stub, which the app's test build ships in place of the real model: an Android app's test assets, an iOS app's test target, a service's test image. Stubs are written in the real model's format: ONNX, TFLite (LiteRT), Core ML, ExecuTorch, TensorFlow (SavedModel and TensorFlow.js), Keras, TorchScript, MNN, ncnn, OpenVINO, GGUF (llama.cpp), safetensors (transformers), LiteRT-LM, CTranslate2 (faster-whisper), whisper.cpp and OpenAI Whisper; Tesseract; Piper; and Apple's Foundation Models, whose stub is a Swift type an app's test build gives its sessions. A pack adds formats, tasks and kinds of input of its own: see the stubs guide.`

// Pack returns the models-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      Name,
		Namespace: "models",
		Doc:       packDoc,
		Steps:     append(microphoneSteps(), speakerSteps()...),
		Commands:  []core.Command{modelsCommand()},
	}
}
