// SPDX-License-Identifier: Apache-2.0

#if compiler(>=6.4)
import Foundation
import FoundationModels
import llama

/// The app's own notes model, for an iPhone without Apple Intelligence: SmolLM2-135M-Instruct, which
/// a Release build bundles (NotesModel.gguf), run on the phone with llama.cpp. It is a Foundation
/// Models language model of the app's own, which the app's sessions take as they take the
/// system's. A Debug build gives its sessions a stub of it instead
/// (Models/CourierNotesModelStub.swift), and leaves this file and llama.cpp out.
@available(iOS 27.0, *)
struct CourierNotesModel: LanguageModel {
    typealias Executor = CourierNotesModelExecutor
    let capabilities = LanguageModelCapabilities([])
    /// The bundled model's path.
    let executorConfiguration: String

    init?() {
        guard let path = Bundle.main.path(forResource: "NotesModel", ofType: "gguf") else { return nil }
        executorConfiguration = path
    }
}

@available(iOS 27.0, *)
struct CourierNotesModelExecutor: LanguageModelExecutor {
    typealias Model = CourierNotesModel

    /// The longest answer, in tokens: a line.
    private static let longest = 64

    private let path: String

    init(configuration: String) { path = configuration }

    nonisolated(nonsending) func respond(
        to request: LanguageModelExecutorGenerationRequest, model: CourierNotesModel,
        streamingInto channel: LanguageModelExecutorGenerationChannel
    ) async throws {
        try await NotesLlama.shared.respond(to: Self.messages(request.transcript), from: path, longest: Self.longest) { piece in
            await channel.send(.response(action: .appendText(piece, tokenCount: 1)))
        }
    }

    /// The conversation as the model's chat: the instructions the system's, the prompts the
    /// user's, the responses the assistant's.
    static func messages(_ transcript: Transcript) -> [(role: String, content: String)] {
        var messages: [(role: String, content: String)] = []
        for entry in transcript {
            let role: String
            let segments: [Transcript.Segment]
            switch entry {
            case .instructions(let instructions): (role, segments) = ("system", instructions.segments)
            case .prompt(let prompt): (role, segments) = ("user", prompt.segments)
            case .response(let response): (role, segments) = ("assistant", response.segments)
            default: continue
            }
            let content = segments.compactMap { segment -> String? in
                switch segment {
                case .text(let text): text.content
                case .structure(let structure): structure.content.jsonString
                default: nil
                }
            }
            messages.append((role, content.joined(separator: "\n")))
        }
        return messages
    }
}

enum NotesModelError: Error {
    case load(String)
    case prompt
    case decode(Int32)
}

/// SmolLM2 in llama.cpp: the model and a context of it, loaded once, answering one conversation at
/// a time, greedily.
@available(iOS 27.0, *)
actor NotesLlama {
    static let shared = NotesLlama()

    private var model: OpaquePointer?
    private var context: OpaquePointer?

    func respond(
        to messages: [(role: String, content: String)], from path: String, longest: Int,
        each send: @Sendable (String) async -> Void
    ) async throws {
        let (model, context) = try load(path)
        let vocab = llama_model_get_vocab(model)
        llama_memory_clear(llama_get_memory(context), true)
        let sampler = llama_sampler_chain_init(llama_sampler_chain_default_params())
        defer { llama_sampler_free(sampler) }
        llama_sampler_chain_add(sampler, llama_sampler_init_greedy())

        var next = try tokens(vocab, prompt(model, messages))
        // A token's piece may end inside a character: its bytes wait for the rest.
        var pending: [UInt8] = []
        for _ in 0..<longest {
            let status = next.withUnsafeMutableBufferPointer { llama_decode(context, llama_batch_get_one($0.baseAddress, Int32($0.count))) }
            guard status == 0 else { throw NotesModelError.decode(status) }
            let token = llama_sampler_sample(sampler, context, -1)
            if llama_vocab_is_eog(vocab, token) { break }
            pending += piece(vocab, token)
            if let text = String(validating: pending, as: UTF8.self) {
                pending = []
                await send(text)
            }
            next = [token]
        }
    }

    private func load(_ path: String) throws -> (OpaquePointer, OpaquePointer) {
        if let model, let context { return (model, context) }
        llama_backend_init()
        guard let model = llama_model_load_from_file(path, llama_model_default_params()) else {
            throw NotesModelError.load(path)
        }
        var params = llama_context_default_params()
        params.n_ctx = 2048
        params.n_batch = 2048
        guard let context = llama_init_from_model(model, params) else {
            llama_model_free(model)
            throw NotesModelError.load(path)
        }
        (self.model, self.context) = (model, context)
        return (model, context)
    }

    /// The chat in the model's own template, ready for the assistant's answer.
    private func prompt(_ model: OpaquePointer, _ messages: [(role: String, content: String)]) throws -> String {
        let strings = messages.map { (strdup($0.role), strdup($0.content)) }
        defer { strings.forEach { free($0.0); free($0.1) } }
        let chat = strings.map { llama_chat_message(role: UnsafePointer($0.0), content: UnsafePointer($0.1)) }
        let template = llama_model_chat_template(model, nil)
        var buffer = [CChar](repeating: 0, count: 2 * messages.reduce(256) { $0 + $1.content.utf8.count })
        var n = llama_chat_apply_template(template, chat, chat.count, true, &buffer, Int32(buffer.count))
        if n > buffer.count {
            buffer = [CChar](repeating: 0, count: Int(n))
            n = llama_chat_apply_template(template, chat, chat.count, true, &buffer, Int32(buffer.count))
        }
        guard n > 0 else { throw NotesModelError.prompt }
        return String(decoding: buffer[..<Int(n)].map { UInt8(bitPattern: $0) }, as: UTF8.self)
    }

    private func tokens(_ vocab: OpaquePointer?, _ text: String) throws -> [llama_token] {
        let length = Int32(text.utf8.count)
        var tokens = [llama_token](repeating: 0, count: Int(length) + 8)
        let n = llama_tokenize(vocab, text, length, &tokens, Int32(tokens.count), true, true)
        guard n > 0 else { throw NotesModelError.prompt }
        return Array(tokens[..<Int(n)])
    }

    private func piece(_ vocab: OpaquePointer?, _ token: llama_token) -> [UInt8] {
        var buffer = [CChar](repeating: 0, count: 64)
        var n = llama_token_to_piece(vocab, token, &buffer, Int32(buffer.count), 0, false)
        if n < 0 {
            buffer = [CChar](repeating: 0, count: Int(-n))
            n = llama_token_to_piece(vocab, token, &buffer, Int32(buffer.count), 0, false)
        }
        return buffer[..<Int(max(n, 0))].map { UInt8(bitPattern: $0) }
    }
}
#endif
