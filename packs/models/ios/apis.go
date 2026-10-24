package modelsios

import "github.com/nimbusxr/axx/packs/models/core/format/platform"

// The APIs models-ios answers, and their contracts, which
// `axx models contract <api>` writes. Vision names its animals Cat and Dog.
var iosAPIs = []platform.API{
	{Name: "ios.speech", Calls: "SFSpeechRecognizer", Task: "speech-to-text", Contract: platform.SpeechFeatures("ios.speech")},
	{Name: "ios.vision.classify", Calls: "VNClassifyImageRequest", Task: "classification", Contract: platform.Classifier("ios.vision.classify")},
	{Name: "ios.vision.text", Calls: "VNRecognizeTextRequest", Task: "text-recognition", Contract: platform.TextRecognizer("ios.vision.text")},
	{Name: "ios.vision.humans", Calls: "VNDetectHumanRectanglesRequest", Task: "detection", Contract: platform.Detector("ios.vision.humans", "person")},
	{Name: "ios.vision.animals", Calls: "VNRecognizeAnimalsRequest", Task: "detection", Contract: platform.Detector("ios.vision.animals", "Cat", "Dog")},
	{Name: "ios.vision.person-segmentation", Calls: "VNGeneratePersonSegmentationRequest", Task: "segmentation", Contract: platform.Segmenter("ios.vision.person-segmentation")},
	{
		Name: "ios.foundation-models", Calls: "LanguageModelSession, of SystemLanguageModel", Task: "text-generation", Contract: platform.TextGenerator("ios.foundation-models"),
		Unavailable: []string{"deviceNotEligible", "appleIntelligenceNotEnabled", "modelNotReady"},
	},
	{
		Name: "ios.private-cloud-compute", Calls: "LanguageModelSession, of PrivateCloudComputeLanguageModel (Apple's server model)", Task: "text-generation", Contract: platform.TextGenerator("ios.private-cloud-compute"),
		Unavailable: []string{"deviceNotEligible", "systemNotReady"},
	},
}

func init() {
	for _, a := range iosAPIs {
		platform.RegisterAPI(a)
	}
}

// apiTask is the task of the pack's API, or "" for an API it does not answer.
func apiTask(api string) string {
	for _, a := range iosAPIs {
		if a.Name == api {
			return a.Task
		}
	}
	return ""
}
