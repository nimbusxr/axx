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
// contracts, which `axx models contract <api>` writes.
func apis() []platform.API {
	return []platform.API{
		{
			Name: "windows.ai.text-recognizer", Calls: "TextRecognizer (Microsoft.Windows.AI.Imaging)", Task: "text-recognition",
			Contract: platform.TextRecognizer("windows.ai.text-recognizer"), Unavailable: unavailable,
		},
	}
}
