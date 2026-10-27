// SPDX-License-Identifier: Apache-2.0

import Foundation
import FoundationModels

/// The recipient's delivery note, as the courier reads it at the door: a short one as it is, a
/// long one summarized in a line by Apple's on-device language model (Foundation Models), or, on
/// an iPhone without Apple Intelligence, by the app's own (CourierNotesModel, which a Debug build
/// replaces with its stub). Before iOS 27, or should the model fail, the courier reads the note as
/// the recipient wrote it.
enum DeliveryNotes {
    /// The longest note the courier reads as it is.
    static let short = 80

    static let instructions =
        "You summarize a recipient's delivery note for a parcel courier in one short line: "
        + "where to leave the parcel, and what to look out for."

    /// The note for the courier: summarized when it is long.
    static func forCourier(_ note: String) async -> String {
        guard note.count > short, let summary = try? await summarize(note), !summary.isEmpty else { return note }
        return summary
    }

    private static func summarize(_ note: String) async throws -> String? {
        #if compiler(>=6.4)
        guard #available(iOS 27.0, *) else { return nil }
        let system = SystemLanguageModel.default
        if system.isAvailable {
            return try await respond(LanguageModelSession(model: system, instructions: instructions), note)
        }
        // An iPhone without Apple Intelligence: the app's own model.
        #if DEBUG
        let own = CourierNotesModelStub()
        #else
        guard let own = CourierNotesModel() else { return nil }
        #endif
        return try await respond(LanguageModelSession(model: own, instructions: instructions), note)
        #else
        return nil
        #endif
    }

    #if compiler(>=6.4)
    @available(iOS 27.0, *)
    private static func respond(_ session: LanguageModelSession, _ note: String) async throws -> String {
        try await session.respond(to: note).content.trimmingCharacters(in: .whitespacesAndNewlines)
    }
    #endif
}
