// SPDX-License-Identifier: Apache-2.0

import CoreML
@preconcurrency import Vision

/// The parcel finder: a photo the courier attaches must show the whole parcel, not one cut off at
/// its edge. An object detector of the company's own (a Create ML object detector of parcels) runs
/// on the phone with Core ML, through Vision, which stretches the photo to the model's input. A
/// Debug build ships a stub of it (Models/ParcelFinder.mlmodel), which answers the tests' photos
/// as its mappings say; the "Parcel finder model" build phase compiles it into the app, as it
/// compiles the real model in a Release build.
final class ParcelFinder: Sendable {
    /// How sure the detector must be of a parcel.
    private static let certainty: Float = 0.5

    /// How near the photo's edge a parcel's box runs off it: a share of the photo's side.
    private static let edge: CGFloat = 0.01

    private let model: VNCoreMLModel

    init() throws {
        guard let url = Bundle.main.url(forResource: "ParcelFinder", withExtension: "mlmodelc") else {
            throw CocoaError(.fileNoSuchFile)
        }
        model = try VNCoreMLModel(for: MLModel(contentsOf: url))
    }

    /// Whether the parcel the photo shows runs off its edge; false when it shows none.
    func cutOff(_ photo: Data) throws -> Bool {
        let request = VNCoreMLRequest(model: model)
        request.imageCropAndScaleOption = .scaleFill
        try VNImageRequestHandler(data: photo).perform([request])
        let parcels = (request.results as? [VNRecognizedObjectObservation] ?? []).filter {
            $0.labels.first?.identifier == "parcel" && $0.confidence >= Self.certainty
        }
        guard let parcel = parcels.max(by: { $0.confidence < $1.confidence }) else { return false }
        // Vision's boxes are shares of the photo, from its lower left corner.
        let box = parcel.boundingBox
        return box.minX <= Self.edge || box.minY <= Self.edge || box.maxX >= 1 - Self.edge || box.maxY >= 1 - Self.edge
    }
}
