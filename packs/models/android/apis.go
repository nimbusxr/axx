package modelsandroid

import "github.com/nimbusxr/axx/packs/models/core/format/platform"

// The APIs models-android answers, and their contracts, which
// `axx models contract <api>` writes. ML Kit's object detector names its
// objects by its five coarse categories.
var androidAPIs = []platform.API{
	{Name: "android.speech", Calls: "SpeechRecognizer", Task: "speech-to-text", Contract: platform.SpeechFeatures("android.speech")},
	{Name: "android.mlkit.image-labeling", Calls: "ML Kit's ImageLabeling", Task: "classification", Contract: platform.Classifier("android.mlkit.image-labeling")},
	{Name: "android.mlkit.text-recognition", Calls: "ML Kit's TextRecognition", Task: "text-recognition", Contract: platform.TextRecognizer("android.mlkit.text-recognition")},
	{Name: "android.mlkit.object-detection", Calls: "ML Kit's ObjectDetection", Task: "detection", Contract: platform.Detector("android.mlkit.object-detection", "Fashion good", "Food", "Home good", "Place", "Plant")},
	{Name: "android.mlkit.segmentation", Calls: "ML Kit's selfie Segmentation", Task: "segmentation", Contract: platform.Segmenter("android.mlkit.segmentation")},
	{Name: "android.gemini-nano", Calls: "ML Kit's Prompt API (Gemini Nano)", Task: "text-generation", Contract: platform.TextGenerator("android.gemini-nano")},
}

func init() {
	for _, a := range androidAPIs {
		platform.RegisterAPI(a)
	}
}

// apiTask is the task of the pack's API, or "" for an API it does not answer.
func apiTask(api string) string {
	for _, a := range androidAPIs {
		if a.Name == api {
			return a.Task
		}
	}
	return ""
}
