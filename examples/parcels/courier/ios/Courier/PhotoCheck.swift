// SPDX-License-Identifier: Apache-2.0

import CoreML
@preconcurrency import Vision

/// What the photo check saw: the likeliest class, how likely, and whether it is a parcel's.
struct Verdict {
    let label: String
    let score: Float
    let parcel: Bool
}

/// The photo check: a photo the courier attaches must show the parcel. An image classifier
/// (MobileNetV2) runs on the phone with Core ML, through Vision, which fits the photo to the
/// model's input (its middle, at the input's proportions). A Debug build ships a stub of it
/// (Models/PhotoCheck.mlmodel), which answers the tests' photos as its mappings say; the
/// "Photo check model" build phase compiles it into the app, as it compiles the real model in
/// a Release build.
final class PhotoCheck: Sendable {
    /// The model's classes that are parcels.
    private static let parcels: Set<String> = ["carton", "crate", "envelope", "mailbag", "packet"]

    /// How likely a parcel's class must be.
    private static let certainty: Float = 0.5

    private let model: VNCoreMLModel

    init() throws {
        guard let url = Bundle.main.url(forResource: "PhotoCheck", withExtension: "mlmodelc") else {
            throw CocoaError(.fileNoSuchFile)
        }
        model = try VNCoreMLModel(for: MLModel(contentsOf: url))
    }

    func check(_ photo: Data) throws -> Verdict {
        let request = VNCoreMLRequest(model: model)
        request.imageCropAndScaleOption = .centerCrop
        try VNImageRequestHandler(data: photo).perform([request])
        guard let top = (request.results as? [VNClassificationObservation])?.first else {
            return Verdict(label: "nothing", score: 0, parcel: false)
        }
        // A class may have other names after its first: "mailbag, postbag".
        let label = top.identifier.components(separatedBy: ",")[0]
        return Verdict(label: label, score: top.confidence, parcel: Self.parcels.contains(label) && top.confidence >= Self.certainty)
    }
}
