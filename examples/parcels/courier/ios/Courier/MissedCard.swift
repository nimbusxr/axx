// SPDX-License-Identifier: Apache-2.0

import Foundation
import FoundationModels

/// The card the courier leaves when no one is home: a few lines for the recipient, which Apple's
/// server model (Private Cloud Compute) drafts from the delivery and the attempt, for the courier
/// to write out. Without it (before iOS 27, or where Private Cloud Compute is not available), the
/// app drafts none.
enum MissedCard {
    static let instructions =
        "You write the card a parcel courier leaves when no one is home, in two or three friendly "
        + "sentences: that the courier came, and that the parcel goes back to the depot."

    /// The card's draft, of the delivery and what the phone heard at the door.
    static func draft(_ delivery: Delivery, heard: String) async -> String? {
        #if compiler(>=6.4)
        guard #available(iOS 27.0, *) else { return nil }
        let model = PrivateCloudComputeLanguageModel()
        guard model.isAvailable else { return nil }
        let session = LanguageModelSession(model: model, instructions: instructions)
        let attempt = heard == "doorbell" ? "rang the doorbell" : "knocked"
        let prompt = "Parcel \(delivery.reference) for \(delivery.recipient.name), \(delivery.address). The courier \(attempt); no one answered."
        let card = try? await session.respond(to: prompt).content.trimmingCharacters(in: .whitespacesAndNewlines)
        return card?.isEmpty == false ? card : nil
        #else
        return nil
        #endif
    }
}
