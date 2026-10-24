import Foundation
import FoundationModels

// iOS 27's public provider API owns sessions, responses, streaming and
// transcripts. Only public session constructors and availability getters are
// interposed: SystemLanguageModel's, PrivateCloudComputeLanguageModel's, and
// those of sessions of any model, which hand Apple's server model to its stub
// and every other model to Foundation Models as it is. A call of the adapter's
// own reaches the original: interposing leaves the interposing library alone.
// No framework layout is inspected.

// A stub of a Foundation Models API, as its .axxmodel has it.
struct AXXArtifact: Decodable, Sendable {
  let api: String
  let texts: [String]?
  let generated: [String]
  let contract: Contract
  struct Contract: Decodable, Sendable { let format: Format }
  struct Format: Decodable, Sendable { let details: Details }
  struct Details: Decodable, Sendable { let availability: String? }
}

// The stubs in the app's axx-models folder: of the system model, and of the
// server model (Private Cloud Compute), each when the folder has it.
enum AXXStubs {
  static let system = load("ios.foundation-models")
  static let privateCloud = load("ios.private-cloud-compute")

  static func load(_ api: String) -> AXXArtifact? {
    do {
      guard let root = ProcessInfo.processInfo.environment["AXX_MODELS_DIR"],
        let files = FileManager.default.enumerator(atPath: root)
      else {
        throw CocoaError(.fileReadNoSuchFile)
      }
      var found: AXXArtifact?
      for case let name as String in files where name.lowercased().hasSuffix(".axxmodel") {
        if name.split(separator: "/").contains(where: { $0.hasPrefix(".") }) { continue }
        let data = try Data(contentsOf: URL(fileURLWithPath: root).appendingPathComponent(name))
        struct Selector: Decodable { let api: String }
        guard try JSONDecoder().decode(Selector.self, from: data).api == api else { continue }
        guard found == nil else { throw CocoaError(.coderReadCorrupt) }
        found = try JSONDecoder().decode(AXXArtifact.self, from: data)
      }
      if let found, found.generated.count != (found.texts?.count ?? 0) + 1 {
        throw CocoaError(.coderReadCorrupt)
      }
      return found
    } catch { fatalError("AXX-E0704: cannot load the \(api) fixture: \(error)") }
  }

  // The system model's availability, as its stub says: on a device without
  // it, the reason, in Apple's words.
  static let systemAvailability: SystemLanguageModel.Availability? = system.map {
    switch $0.contract.format.details.availability {
    case nil: return .available
    case "deviceNotEligible": return .unavailable(.deviceNotEligible)
    case "appleIntelligenceNotEnabled": return .unavailable(.appleIntelligenceNotEnabled)
    case "modelNotReady": return .unavailable(.modelNotReady)
    case let reason?: fatalError("AXX-E0704: Foundation Models gives no availability \(reason)")
    }
  }

  // The server model's, likewise.
  static let privateCloudAvailability: PrivateCloudComputeLanguageModel.Availability? = privateCloud.map {
    switch $0.contract.format.details.availability {
    case nil: return .available
    case "deviceNotEligible": return .unavailable(.deviceNotEligible)
    case "systemNotReady": return .unavailable(.systemNotReady)
    case let reason?: fatalError("AXX-E0704: Private Cloud Compute gives no availability \(reason)")
    }
  }
}

// The stubs' model: of the system model's stub (configuration 0) or the
// server model's (1).
struct AXXLanguageModel: LanguageModel {
  static let system = 0
  static let privateCloud = 1
  let configuration: Int
  var capabilities: LanguageModelCapabilities { .init([]) }
  var executorConfiguration: Int { configuration }

  struct Executor: LanguageModelExecutor {
    typealias Model = AXXLanguageModel
    let fixture: AXXArtifact
    init(configuration: Int) {
      guard let fixture = configuration == AXXLanguageModel.privateCloud ? AXXStubs.privateCloud : AXXStubs.system
      else { fatalError("AXX-E0704: no Foundation Models fixture of configuration \(configuration)") }
      self.fixture = fixture
    }
    nonisolated(nonsending) func respond(
      to request: LanguageModelExecutorGenerationRequest, model: Model,
      streamingInto channel: LanguageModelExecutorGenerationChannel
    ) async throws {
      try Task.checkCancellation()
      let input = Array(try conversation(request.transcript).utf8)
      let index = (fixture.texts ?? []).firstIndex { has(input, Array($0.utf8)) }
      let answer = fixture.generated[index ?? fixture.generated.count - 1]
      await channel.send(
        .response(action: .appendText(answer, tokenCount: answer.split(separator: " ").count)))
    }
  }
}

@objc(AXXFoundationModelsLoader)
public final class AXXFoundationModelsLoader: NSObject {
  @objc public static func prepare() {
    _ = AXXStubs.systemAvailability
    _ = AXXStubs.privateCloudAvailability
  }
}

private func conversation(_ transcript: Transcript) throws -> String {
  var texts: [String] = []
  for entry in transcript {
    let segments: [Transcript.Segment]
    switch entry {
    case .instructions(let value): segments = value.segments
    case .prompt(let value): segments = value.segments
    case .response(let value): segments = value.segments
    case .toolOutput(let value): segments = value.segments
    default: continue
    }
    for segment in segments {
      switch segment {
      case .text(let value): texts.append(value.content)
      case .structure(let value): texts.append(value.content.jsonString)
      default: throw CocoaError(.featureUnsupported)
      }
    }
  }
  return texts.joined(separator: "\n")
}
private func has(_ text: [UInt8], _ key: [UInt8]) -> Bool {
  guard !key.isEmpty, key.count <= text.count else { return false }
  for at in 0...(text.count - key.count) where text[at..<at + key.count].elementsEqual(key) {
    let end = at + key.count
    if (at == 0 || !word(text[at - 1]) || !word(key[0]))
      && (end == text.count || !word(text[end]) || !word(key[key.count - 1]))
    {
      return true
    }
  }
  return false
}
private func word(_ b: UInt8) -> Bool {
  b >= 128 || b >= 48 && b <= 57 || b >= 65 && b <= 90 || b >= 97 && b <= 122
}

// The server model's stub, for a session of a model that is Apple's server
// model; nil for any other model, or without its stub.
private func privateCloudStub(_ model: some LanguageModel) -> AXXLanguageModel? {
  guard model is PrivateCloudComputeLanguageModel, AXXStubs.privateCloud != nil else { return nil }
  return AXXLanguageModel(configuration: AXXLanguageModel.privateCloud)
}

// Sessions of the system model, on its stub when the folder has it.
extension LanguageModelSession {
  public convenience init(
    axxInstructions model: SystemLanguageModel, tools: [any Tool], instructions: Instructions?
  ) {
    guard AXXStubs.system != nil else {
      self.init(model: model, tools: tools, instructions: instructions)
      return
    }
    self.init(model: AXXLanguageModel(configuration: AXXLanguageModel.system), tools: tools, instructions: instructions)
  }
  public convenience init(
    axxText model: SystemLanguageModel, tools: [any Tool], instructions: String?
  ) {
    guard AXXStubs.system != nil else {
      self.init(model: model, tools: tools, instructions: instructions)
      return
    }
    self.init(model: AXXLanguageModel(configuration: AXXLanguageModel.system), tools: tools, instructions: instructions)
  }
  public convenience init(
    axxBuilder model: SystemLanguageModel, tools: [any Tool],
    @InstructionsBuilder instructions: () throws -> Instructions
  ) rethrows {
    guard AXXStubs.system != nil else {
      try self.init(model: model, tools: tools, instructions: instructions)
      return
    }
    self.init(model: AXXLanguageModel(configuration: AXXLanguageModel.system), tools: tools, instructions: try instructions())
  }
  public convenience init(
    axxTranscript model: SystemLanguageModel, tools: [any Tool], transcript: Transcript
  ) {
    guard AXXStubs.system != nil else {
      self.init(model: model, tools: tools, transcript: transcript)
      return
    }
    self.init(model: AXXLanguageModel(configuration: AXXLanguageModel.system), tools: tools, transcript: transcript)
  }
}

// Sessions of any model: Apple's server model's on its stub, every other
// model's as Foundation Models makes them.
extension LanguageModelSession {
  public convenience init<M: LanguageModel>(
    axxAnyInstructions model: M, tools: [any Tool], instructions: Instructions?
  ) {
    if let stub = privateCloudStub(model) {
      self.init(model: stub, tools: tools, instructions: instructions)
    } else {
      self.init(model: model, tools: tools, instructions: instructions)
    }
  }
  public convenience init<M: LanguageModel>(
    axxAnyText model: M, tools: [any Tool], instructions: String?
  ) {
    if let stub = privateCloudStub(model) {
      self.init(model: stub, tools: tools, instructions: instructions)
    } else {
      self.init(model: model, tools: tools, instructions: instructions)
    }
  }
  public convenience init<Failure: Swift.Error, M: LanguageModel>(
    axxAnyBuilder model: M, tools: [any Tool],
    @InstructionsBuilder instructions: () throws(Failure) -> Instructions
  ) throws(Failure) {
    if let stub = privateCloudStub(model) {
      self.init(model: stub, tools: tools, instructions: try instructions())
    } else {
      try self.init(model: model, tools: tools, instructions: instructions)
    }
  }
  public convenience init<M: LanguageModel>(
    axxAnyTranscript model: M, tools: [any Tool], transcript: Transcript
  ) {
    if let stub = privateCloudStub(model) {
      self.init(model: stub, tools: tools, transcript: transcript)
    } else {
      self.init(model: model, tools: tools, transcript: transcript)
    }
  }
}

extension SystemLanguageModel {
  public var axxAvailability: Availability { AXXStubs.systemAvailability ?? availability }
  public var axxIsAvailable: Bool {
    guard let stubbed = AXXStubs.systemAvailability else { return isAvailable }
    return stubbed == .available
  }
}
extension PrivateCloudComputeLanguageModel {
  public var axxServerAvailability: Availability { AXXStubs.privateCloudAvailability ?? availability }
  public var axxServerIsAvailable: Bool {
    guard let stubbed = AXXStubs.privateCloudAvailability else { return isAvailable }
    return stubbed == .available
  }
}
