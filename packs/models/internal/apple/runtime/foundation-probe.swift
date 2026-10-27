import Foundation
import FoundationModels

// An ordinary SDK caller, compiled independently of the adapter.
@main struct Probe {
  static func main() async throws {
    if CommandLine.arguments[1] == "availability" {
      print(SystemLanguageModel.default.isAvailable, SystemLanguageModel.default.availability)
      return
    }
    // Apple's server model, and a model of the caller's own, which only
    // Foundation Models answers.
    switch CommandLine.arguments[1] {
    case "server-availability":
      let server = PrivateCloudComputeLanguageModel()
      print(server.isAvailable, server.availability)
      return
    case "server", "server-text", "server-builder", "server-transcript":
      let server = PrivateCloudComputeLanguageModel()
      let session: LanguageModelSession
      switch CommandLine.arguments[1] {
      case "server-text": session = LanguageModelSession(model: server, instructions: "Answer briefly" as String)
      case "server-builder": session = LanguageModelSession(model: server) { "Answer briefly" }
      case "server-transcript": session = LanguageModelSession(model: server, transcript: Transcript())
      default: session = LanguageModelSession(model: server, instructions: Instructions("Answer briefly"))
      }
      print(try await session.respond(to: CommandLine.arguments[2]).content)
      return
    case "own":
      print(try await LanguageModelSession(model: OwnModel(), instructions: "Answer briefly" as String).respond(to: CommandLine.arguments[2]).content)
      return
    default: break
    }
    guard SystemLanguageModel.default.isAvailable,
      SystemLanguageModel.default.availability == .available
    else { fatalError("unavailable") }
    let kind = CommandLine.arguments[1]
    let prompt = CommandLine.arguments[2]
    let session: LanguageModelSession
    switch kind {
    case "text": session = LanguageModelSession(instructions: "Answer briefly" as String)
    case "builder": session = LanguageModelSession { "Answer briefly" }
    case "transcript": session = LanguageModelSession(model: .default, transcript: Transcript())
    default: session = LanguageModelSession()
    }
    session.prewarm()
    if kind == "stream" {
      var final = ""
      for try await response in session.streamResponse(to: prompt) { final = response.content }
      print(final)
    } else {
      print(try await session.respond(to: prompt).content)
    }
    if kind == "history" { print(try await session.respond(to: "And now?").content) }
  }
}

// A model of the caller's own: it answers "own" whatever it is asked.
struct OwnModel: LanguageModel {
  let capabilities = LanguageModelCapabilities([])
  let executorConfiguration = 0
  struct Executor: LanguageModelExecutor {
    typealias Model = OwnModel
    init(configuration: Int) {}
    nonisolated(nonsending) func respond(
      to request: LanguageModelExecutorGenerationRequest, model: OwnModel,
      streamingInto channel: LanguageModelExecutorGenerationChannel
    ) async throws {
      await channel.send(.response(action: .appendText("own", tokenCount: 1)))
    }
  }
}
