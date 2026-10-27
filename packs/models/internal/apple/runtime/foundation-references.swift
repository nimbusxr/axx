import FoundationModels

// Compiled, never run. The compiler supplies the linkage names of these public
// SDK calls. No OS binary, private symbol, or hardcoded Swift mangling is read.
public func references<M: LanguageModel>(
  model: SystemLanguageModel, tools: [any Tool], instructions: Instructions?, text: String?,
  transcript: Transcript, any: M, privateCloud: PrivateCloudComputeLanguageModel
) {
  _ = LanguageModelSession(model: model, tools: tools, instructions: instructions)
  _ = LanguageModelSession(model: model, tools: tools, instructions: text)
  _ = LanguageModelSession(model: model, tools: tools) { "instructions" }
  _ = LanguageModelSession(model: model, tools: tools, transcript: transcript)
  _ = model.availability
  _ = model.isAvailable
  _ = LanguageModelSession(model: any, tools: tools, instructions: instructions)
  _ = LanguageModelSession(model: any, tools: tools, instructions: text)
  _ = LanguageModelSession(model: any, tools: tools) { "instructions" }
  _ = LanguageModelSession(model: any, tools: tools, transcript: transcript)
  _ = privateCloud.availability
  _ = privateCloud.isAvailable
}
