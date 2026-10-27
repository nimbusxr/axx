package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// API is a platform model API a surface pack answers: its selector, the
// calls it answers, and the contract `axx models contract <api>` writes for
// it, the same command that writes a real model's.
type API struct {
	// Name is the selector a contract's format.details.api gives:
	// ios.vision.text.
	Name string
	// Calls are what the app calls, as the platform names it:
	// VNRecognizeTextRequest.
	Calls string
	// Task is the task of the API's answers.
	Task string
	// Contract is the API's contract. Labels are what --labels gives: a
	// classifier's labels, a detector's, or the characters a text
	// recognizer reads; nil for the API's own.
	Contract func(labels []string) (*contract.Contract, error)
	// Unavailable are the reasons the platform gives for a device whose
	// model is not there, in its words (SystemLanguageModel's
	// deviceNotEligible), one of which a stub's format.details.availability
	// may give: the stub of a device without the model.
	Unavailable []string
}

var apis = struct {
	sync.Mutex
	byName map[string]API
}{byName: map[string]API{}}

// RegisterAPI adds an API a surface pack answers. A pack registers its APIs
// as models-core registers its formats: in an init function.
func RegisterAPI(a API) {
	apis.Lock()
	defer apis.Unlock()
	if _, dup := apis.byName[a.Name]; dup {
		panic("platform API " + a.Name + " registered twice")
	}
	apis.byName[a.Name] = a
}

// APIByName is the registered API of the selector.
func APIByName(name string) (API, bool) {
	apis.Lock()
	defer apis.Unlock()
	a, ok := apis.byName[name]
	return a, ok
}

// APIs are the registered APIs, by name.
func APIs() []API {
	apis.Lock()
	defer apis.Unlock()
	out := make([]API, 0, len(apis.byName))
	for _, a := range apis.byName {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// The ready contracts' inputs and outputs. A platform API takes the app's
// whole picture or sound, so a contract's input only says how a stub reduces
// it: the adapters reduce what the app gives them as axx reduces the
// mappings' keys.
const (
	// PictureSize is the side of the square an API's picture is stretched
	// to before it is reduced: enough for match.Grid's pooling.
	PictureSize = 128
	// SpeechRate and SpeechSeconds are a speech API's sound: as much of a
	// recording as a dictated sentence takes, at the rate recognizers take.
	SpeechRate    = 16000
	SpeechSeconds = 10
	// TextSteps is how many steps a text recognizer's answer has: twice the
	// longest text it reads, a blank between letters alike.
	TextSteps = 128
	// MaskSize is the side of a segmentation API's mask.
	MaskSize = 256
	// Objects is how many objects a detection API answers at most.
	Objects = 10
)

func details(api string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"api": api})
	return b
}

func dims(sizes ...int64) []contract.Dim {
	out := make([]contract.Dim, len(sizes))
	for i, s := range sizes {
		out[i] = contract.Dim{Size: s}
	}
	return out
}

func picture() contract.Tensor {
	return contract.Tensor{
		Name: "image", Type: "float32", Shape: dims(1, 3, PictureSize, PictureSize),
		Image: &contract.Image{Layout: "NCHW", Channels: "RGB", Resize: "stretch"},
	}
}

func base(api, task string) *contract.Contract {
	return &contract.Contract{Format: contract.Format{Type: "platform", Details: details(api)}, Task: task, Inputs: []contract.Tensor{}, Outputs: []contract.Tensor{}}
}

// TextGenerator is a text API's contract: no tensors, its mappings' keys
// texts (Foundation Models, Gemini Nano, a browser's Prompt API).
func TextGenerator(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels != nil {
			return nil, fmt.Errorf("%s answers text: it takes no labels", api)
		}
		return base(api, "text-generation"), nil
	}
}

// ImageDescriber is an image description API's contract: a picture, which its mappings' keys are,
// and a text, its description, their answers (Windows' ImageDescriptionGenerator).
func ImageDescriber(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels != nil {
			return nil, fmt.Errorf("%s describes pictures: it takes no labels", api)
		}
		c := base(api, "text-generation")
		c.Inputs = []contract.Tensor{picture()}
		return c, nil
	}
}

// Classifier is an image labeler's contract: its labels the app's, which
// --labels gives (those the mappings answer, at least), each of its own
// confidence, as the platforms' labelers give them (a sigmoid's).
func Classifier(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if len(labels) == 0 {
			return nil, fmt.Errorf("%s labels pictures: give the labels the mappings answer (--labels labels.txt)", api)
		}
		c := base(api, "classification")
		c.Inputs = []contract.Tensor{picture()}
		c.Outputs = []contract.Tensor{{Name: "labels", Type: "float32", Shape: dims(1, int64(len(labels))), Labels: labels, Activation: "sigmoid"}}
		return c, nil
	}
}

// TextCharacters are the characters a text recognizer reads unless --labels
// gives others: printable ASCII and the Latin letters of Western Europe.
func TextCharacters() []string {
	var out []string
	for r := rune(0x20); r <= 0x7e; r++ {
		out = append(out, string(r))
	}
	for r := rune(0xc0); r <= 0xff; r++ {
		if r != 0xd7 && r != 0xf7 { // × and ÷
			out = append(out, string(r))
		}
	}
	return append(out, "€")
}

// TextRecognizer is an OCR API's contract: an answer's text, as the steps
// of a CTC output over the characters it reads, its blank first.
func TextRecognizer(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels == nil {
			labels = TextCharacters()
		}
		labels = slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return l == "" })
		c := base(api, "text-recognition")
		c.Inputs = []contract.Tensor{picture()}
		all := append([]string{""}, labels...)
		c.Outputs = []contract.Tensor{{Name: "text", Type: "float32", Shape: dims(1, TextSteps, int64(len(all))), Labels: all}}
		return c, nil
	}
}

// Detector is an object detector's contract: its objects' boxes, as shares
// of the picture, their labels, scores and count. Labels are the platform's
// own (the defaults), or those --labels gives.
func Detector(api string, defaults ...string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels == nil {
			labels = defaults
		}
		if len(labels) == 0 {
			return nil, fmt.Errorf("%s detects objects: give the labels the mappings answer (--labels labels.txt)", api)
		}
		c := base(api, "detection")
		c.Inputs = []contract.Tensor{picture()}
		c.Outputs = []contract.Tensor{
			{Name: "boxes", Type: "float32", Shape: dims(1, Objects, 4), Detection: "boxes", Box: &contract.Box{Order: "ymin,xmin,ymax,xmax"}},
			{Name: "classes", Type: "float32", Shape: dims(1, Objects), Labels: labels, Detection: "classes"},
			{Name: "scores", Type: "float32", Shape: dims(1, Objects), Detection: "scores"},
			{Name: "count", Type: "float32", Shape: dims(1), Detection: "count"},
		}
		return c, nil
	}
}

// Segmenter is a person or selfie segmentation API's contract: a mask of
// each pixel's confidence that it is the person's.
func Segmenter(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels != nil {
			return nil, fmt.Errorf("%s masks a person: it takes no labels", api)
		}
		c := base(api, "segmentation")
		c.Inputs = []contract.Tensor{picture()}
		c.Outputs = []contract.Tensor{{Name: "mask", Type: "float32", Shape: dims(1, MaskSize, MaskSize)}}
		return c, nil
	}
}

// SpeechFeatures is a speech recognizer's contract that tells one speaker's
// phrases apart: a recording's log-mel features, as Whisper's (80 mels of 30
// seconds at 16 kHz). The microphone step matches recordings with the
// reference, so an adapter answers the match it is given.
func SpeechFeatures(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels != nil {
			return nil, fmt.Errorf("%s transcribes speech: it takes no labels", api)
		}
		c := base(api, "speech-to-text")
		c.Inputs = []contract.Tensor{{
			Name: "features", Type: "float32", Shape: dims(1, 80, 3000),
			Audio: &contract.Audio{SampleRate: SpeechRate, Features: "log-mel"},
		}}
		return c, nil
	}
}

// Speech is a speech recognizer's contract: SpeechSeconds of a recording's
// samples, at SpeechRate. Its matching hears a sound's timbre more than its
// words: two phrases of one speaker are near alike, so SpeechFeatures is the
// contract for speech an adapter has its matches for.
func Speech(api string) func([]string) (*contract.Contract, error) {
	return func(labels []string) (*contract.Contract, error) {
		if labels != nil {
			return nil, fmt.Errorf("%s transcribes speech: it takes no labels", api)
		}
		c := base(api, "speech-to-text")
		c.Inputs = []contract.Tensor{{
			Name: "samples", Type: "float32", Shape: dims(1, SpeechRate*SpeechSeconds),
			Audio: &contract.Audio{SampleRate: SpeechRate},
		}}
		return c, nil
	}
}
