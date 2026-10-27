package modelswindows

import "github.com/nimbusxr/axx/packs/models/core/format/platform"

func init() {
	for _, a := range apis() {
		platform.RegisterAPI(a)
	}
}

// unavailable are the reasons GetReadyState gives for a model the PC does not have ready, in its
// words (AIFeatureReadyState): one to download first (EnsureReadyAsync), or none for the PC.
var unavailable = []string{"NotReady", "NotSupportedOnCurrentSystem"}

// apis are the Windows AI APIs models-windows answers, the Windows App SDK's, and their
// contracts, which `axx models contract <api>` writes. The language model's skills are of the
// model: a stub of one may give the model's availability.
func apis() []platform.API {
	text := func(name, calls string) platform.API {
		return platform.API{Name: name, Calls: calls, Task: "text-generation", Contract: platform.TextGenerator(name), Unavailable: unavailable}
	}
	return []platform.API{
		{
			Name: "windows.ai.text-recognizer", Calls: "TextRecognizer (Microsoft.Windows.AI.Imaging)", Task: "text-recognition",
			Contract: platform.TextRecognizer("windows.ai.text-recognizer"), Unavailable: unavailable,
		},
		{
			Name: "windows.ai.image-description", Calls: "ImageDescriptionGenerator (Microsoft.Windows.AI.Imaging)", Task: "text-generation",
			Contract: platform.ImageDescriber("windows.ai.image-description"), Unavailable: unavailable,
		},
		{
			Name: "windows.ai.object-extractor", Calls: "ImageObjectExtractor (Microsoft.Windows.AI.Imaging): its mask of a picture, whatever the hint", Task: "segmentation",
			Contract: platform.Segmenter("windows.ai.object-extractor"), Unavailable: unavailable,
		},
		text("windows.ai.language-model", "LanguageModel, Phi Silica (Microsoft.Windows.AI.Text): responses, of contexts too"),
		text("windows.ai.text-summarizer", "TextSummarizer, of the language model: texts, paragraphs and conversations"),
		text("windows.ai.text-rewriter", "TextRewriter, of the language model: its tones and custom ones"),
		text("windows.ai.text-to-table", "TextToTableConverter, of the language model: an answer's json is the table's rows, each an array of its columns"),
	}
}
