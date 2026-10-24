// SPDX-License-Identifier: Apache-2.0

import CoreImage
import CoreImage.CIFilterBuiltins
@preconcurrency import Vision

/// What a photo the courier attaches shows besides the parcel (PhotoCheck), as the phone's own
/// vision sees it (Apple's Vision framework, whose models are iOS's): a page, like the recipient's
/// signed note, the reference on a parcel's label, a dog beside the parcel, and people, whom the
/// app blurs out before the photo leaves the phone.
enum DeliveryPhoto {
    /// What a photo shows.
    struct Seen {
        /// The reference on the parcel's label, when the photo shows one the phone can read.
        let reference: String?
        let dog: Bool
        let people: Bool
    }

    /// How sure Vision must be of a dog, a person or a document.
    private static let certainty: Float = 0.5

    /// What Vision calls a photo of a page.
    private static let pages: Set<String> = ["document", "handwriting", "receipt"]

    /// Whether the photo is of a page, like the recipient's signed note.
    static func isDocument(_ photo: Data) throws -> Bool {
        let classify = VNClassifyImageRequest()
        try VNImageRequestHandler(data: photo).perform([classify])
        return (classify.results ?? []).contains { pages.contains($0.identifier) && $0.confidence >= certainty }
    }

    static func look(at photo: Data) throws -> Seen {
        let text = VNRecognizeTextRequest()
        text.recognitionLevel = .accurate
        text.usesLanguageCorrection = false
        let animals = VNRecognizeAnimalsRequest()
        let humans = VNDetectHumanRectanglesRequest()
        try VNImageRequestHandler(data: photo).perform([text, animals, humans])
        let read = (text.results ?? []).compactMap { $0.topCandidates(1).first?.string }.joined(separator: " ")
        let reference = read.range(of: #"PX(-[A-Z0-9]+)+"#, options: .regularExpression).map { String(read[$0]) }
        let dog = (animals.results ?? []).contains { animal in
            animal.labels.contains { $0.identifier == VNAnimalIdentifier.dog.rawValue && $0.confidence >= certainty }
        }
        let people = (humans.results ?? []).contains { $0.confidence >= certainty }
        return Seen(reference: reference, dog: dog, people: people)
    }

    /// The photo, its people blurred out: a blur of it where person segmentation's mask has them.
    static func blurringPeople(in photo: Data) throws -> Data {
        guard let image = CIImage(data: photo, options: [.applyOrientationProperty: true]),
              let sRGB = CGColorSpace(name: CGColorSpace.sRGB) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        let segmentation = VNGeneratePersonSegmentationRequest()
        segmentation.qualityLevel = .accurate
        segmentation.outputPixelFormat = kCVPixelFormatType_OneComponent8
        try VNImageRequestHandler(ciImage: image).perform([segmentation])
        guard let mask = segmentation.results?.first?.pixelBuffer else { return photo }
        // The mask is smaller than the photo: stretched over it.
        let people = CIImage(cvPixelBuffer: mask)
        let blend = CIFilter.blendWithMask()
        blend.inputImage = image.clampedToExtent().applyingGaussianBlur(sigma: 40).cropped(to: image.extent)
        blend.backgroundImage = image
        blend.maskImage = people.transformed(by: CGAffineTransform(
            scaleX: image.extent.width / people.extent.width, y: image.extent.height / people.extent.height
        ))
        guard let blurred = blend.outputImage,
              let jpeg = CIContext().jpegRepresentation(of: blurred, colorSpace: sRGB) else {
            throw CocoaError(.fileWriteUnknown)
        }
        return jpeg
    }
}
