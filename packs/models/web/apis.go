package modelsweb

import "github.com/nimbusxr/axx/packs/models/core/format/platform"

// The APIs models-web answers, and their contracts, which
// `axx models contract <api>` writes.
var webAPIs = []platform.API{
	{Name: "web.speech", Calls: "SpeechRecognition", Task: "speech-to-text", Contract: platform.SpeechFeatures("web.speech")},
	{Name: "web.language-model", Calls: "the Prompt API's LanguageModel", Task: "text-generation", Contract: platform.TextGenerator("web.language-model")},
	{Name: "web.summarizer", Calls: "Summarizer", Task: "text-generation", Contract: platform.TextGenerator("web.summarizer")},
	{Name: "web.writer", Calls: "Writer", Task: "text-generation", Contract: platform.TextGenerator("web.writer")},
	{Name: "web.rewriter", Calls: "Rewriter", Task: "text-generation", Contract: platform.TextGenerator("web.rewriter")},
	{Name: "web.translator", Calls: "Translator", Task: "text-generation", Contract: platform.TextGenerator("web.translator")},
	{Name: "web.language-detector", Calls: "LanguageDetector", Task: "text-generation", Contract: platform.TextGenerator("web.language-detector")},
	{Name: "web.proofreader", Calls: "Proofreader", Task: "text-generation", Contract: platform.TextGenerator("web.proofreader")},
}

func init() {
	for _, a := range webAPIs {
		platform.RegisterAPI(a)
	}
}

// apiTask is the task of the pack's API, or "" for an API it does not answer.
func apiTask(api string) string {
	for _, a := range webAPIs {
		if a.Name == api {
			return a.Task
		}
	}
	return ""
}
