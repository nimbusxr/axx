// The Foundation Models runner of the tests: it runs conversations, read as JSON from its
// standard input, with a stub (a LanguageModel named Stub, built with it), each in a session of
// its own, as an app does, and writes their answers as JSON.
import Foundation
import FoundationModels

struct Job: Decodable {
    let instructions: String
    let conversations: [Conversation]
}

struct Conversation: Decodable {
    let prompts: [String]
    /// The properties of a generable type the answers are (strings), when the conversation asks
    /// for structured answers.
    let schema: [String]?
    /// Whether the answers are streamed.
    let stream: Bool?
}

@main
enum Runner {
    static func main() async throws {
        let job = try JSONDecoder().decode(Job.self, from: FileHandle.standardInput.readDataToEndOfFile())
        var answers: [[String]] = []
        for conversation in job.conversations {
            let session = LanguageModelSession(model: Stub(), instructions: job.instructions)
            var said: [String] = []
            for prompt in conversation.prompts {
                if let properties = conversation.schema {
                    let schema = try GenerationSchema(
                        root: DynamicGenerationSchema(name: "Answer", properties: properties.map {
                            .init(name: $0, schema: DynamicGenerationSchema(type: String.self))
                        }),
                        dependencies: [])
                    said.append(try await session.respond(to: prompt, schema: schema).content.jsonString)
                } else if conversation.stream == true {
                    var last = ""
                    for try await partial in session.streamResponse(to: prompt) {
                        last = partial.content
                    }
                    said.append(last)
                } else {
                    said.append(try await session.respond(to: prompt).content)
                }
            }
            answers.append(said)
        }
        FileHandle.standardOutput.write(try JSONEncoder().encode(answers))
    }
}
