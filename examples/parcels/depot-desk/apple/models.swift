// What the desk's macOS builds (../appkit, ../swiftui) ask of the Mac's own models (../README.md):
// Apple's Vision reads a parcel's reference off the label camera's scan, and checks the scan
// shows a parcel before the desk keeps it with the arrival, its people blurred out; Speech takes
// the clerk's dictated handover note; Foundation Models writes the day's summary for the next
// shift, on the Mac, or on Apple's servers (Private Cloud Compute) when the Mac has no Apple
// Intelligence. A test build gets their answers from the stubs axx gives it (models-macos): its
// code is the same.
import AVFoundation
import CoreImage
import CoreImage.CIFilterBuiltins
import Foundation
#if canImport(FoundationModels)
import FoundationModels
#endif
import Speech
@preconcurrency import Vision

/// The desk's data folder, in its Application Support.
func dataFolder() -> URL {
    let folder = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        .appendingPathComponent("Depot desk")
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    return folder
}

/// The scans the depot's label camera saves in the desk's data folder (Scans), and the photos the
/// desk keeps of its arrivals (Photos).
enum Scans {
    static var folder: URL { dataFolder().appendingPathComponent("Scans") }
    static var photos: URL { dataFolder().appendingPathComponent("Photos") }

    /// The newest scan: the parcel on the counter now.
    static func newest() -> URL? {
        let keys: [URLResourceKey] = [.contentModificationDateKey]
        guard let files = try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: keys) else {
            return nil
        }
        let date = { (url: URL) in (try? url.resourceValues(forKeys: Set(keys)).contentModificationDate) ?? .distantPast }
        return files.filter { ["jpg", "jpeg", "png", "heic"].contains($0.pathExtension.lowercased()) }
            .max { (date($0), $0.lastPathComponent) < (date($1), $1.lastPathComponent) }
    }

    /// The reference on the newest scan's label, and what the desk says of it.
    static func readLabel() -> (reference: String?, status: String) {
        guard let scan = newest(), let photo = try? Data(contentsOf: scan) else { return (nil, "No scan to read") }
        guard let reference = try? Scan.reference(in: photo) else { return (nil, "No reference on the label: type it in") }
        return (reference, "Read \(reference) from the label")
    }

    /// Files the newest scan with an arrival: kept in Photos, its people blurred out, when it shows
    /// a parcel; taken out of Scans either way. What the arrival's status says of it: nothing,
    /// without a scan.
    static func file(for reference: String) -> String {
        guard let scan = newest(), let photo = try? Data(contentsOf: scan) else { return "" }
        defer { try? FileManager.default.removeItem(at: scan) }
        guard let seen = try? Scan.look(at: photo) else { return ", photo not checked" }
        guard seen.parcel else { return ", photo not kept: it shows no parcel" }
        let kept = seen.people ? ((try? Scan.blurringPeople(in: photo)) ?? photo) : photo
        try? FileManager.default.createDirectory(at: photos, withIntermediateDirectories: true)
        try? kept.write(to: photos.appendingPathComponent("\(reference).jpg"))
        return seen.people ? ", photo kept, people blurred out" : ", photo kept"
    }
}

/// What the Mac's Vision sees in a scan.
enum Scan {
    struct Seen {
        let parcel: Bool
        let people: Bool
    }

    /// How sure Vision must be.
    static let certainty: Float = 0.5

    /// What Vision calls a parcel.
    static let parcels: Set<String> = ["cardboard_box", "carton", "crate", "envelope"]

    /// The parcel's reference on its label: PX and its parts.
    static func reference(in photo: Data) throws -> String? {
        let text = VNRecognizeTextRequest()
        text.recognitionLevel = .accurate
        text.usesLanguageCorrection = false
        try VNImageRequestHandler(data: photo).perform([text])
        let read = (text.results ?? []).compactMap { $0.topCandidates(1).first?.string }.joined(separator: " ")
        return read.range(of: #"PX(-[A-Z0-9]+)+"#, options: .regularExpression).map { String(read[$0]) }
    }

    /// Whether the scan shows a parcel, and people.
    static func look(at photo: Data) throws -> Seen {
        let classify = VNClassifyImageRequest()
        let humans = VNDetectHumanRectanglesRequest()
        try VNImageRequestHandler(data: photo).perform([classify, humans])
        let parcel = (classify.results ?? []).contains { parcels.contains($0.identifier) && $0.confidence >= certainty }
        let people = (humans.results ?? []).contains { $0.confidence >= certainty }
        return Seen(parcel: parcel, people: people)
    }

    /// The scan, its people blurred out: a blur of it where person segmentation's mask has them.
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
        // The mask is smaller than the scan: stretched over it.
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

/// The clerk's dictated handover note: the Mac's microphone, through Apple's speech recognizer.
final class Dictation {
    private(set) var listening = false
    private var engine: AVAudioEngine?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?

    /// Listens until the recognizer has the note; tell gets, on the main queue, what the desk
    /// says: that it listens, then the note, or why there is none.
    func start(_ tell: @escaping (String) -> Void) {
        guard !listening else { return }
        listening = true
        SFSpeechRecognizer.requestAuthorization { speech in
            AVCaptureDevice.requestAccess(for: .audio) { microphone in
                DispatchQueue.main.async {
                    guard self.listening else { return }
                    guard speech == .authorized, microphone else {
                        self.stop()
                        return tell("Allow the microphone and speech recognition to dictate a note")
                    }
                    guard let recognizer = SFSpeechRecognizer(), recognizer.isAvailable else {
                        self.stop()
                        return tell("Speech recognition is unavailable: try again later")
                    }
                    let engine = AVAudioEngine()
                    let request = SFSpeechAudioBufferRecognitionRequest()
                    self.engine = engine
                    self.request = request
                    engine.inputNode.installTap(onBus: 0, bufferSize: 1024, format: engine.inputNode.outputFormat(forBus: 0)) { buffer, _ in
                        request.append(buffer)
                    }
                    self.task = recognizer.recognitionTask(with: request) { result, failure in
                        DispatchQueue.main.async {
                            guard self.listening else { return }
                            if let result, result.isFinal {
                                self.stop()
                                tell("Note: " + result.bestTranscription.formattedString)
                            } else if result == nil, failure != nil {
                                self.stop()
                                tell("The note could not be heard: dictate it again")
                            }
                        }
                    }
                    engine.prepare()
                    do {
                        try engine.start()
                        tell("Listening…")
                    } catch {
                        self.stop()
                        tell("The microphone could not start: try again")
                    }
                }
            }
        }
    }

    func stop() {
        listening = false
        engine?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        engine = nil
        request?.endAudio()
        request = nil
        task?.cancel()
        task = nil
    }
}

/// The day's summary for the next shift: Apple's language model on the Mac, or, on a Mac without
/// Apple Intelligence, Apple's server model. What the desk says: the summary, or why there is none.
enum DaySummary {
    static let instructions =
        "You sum up a parcel depot's day for the next shift in one short line: how many parcels "
        + "arrived, and which need care."

    static func write(_ arrivals: [(reference: String, level: String, fragile: Bool)]) async -> String {
        let prompt = "Today's arrivals at the Leipzig depot: "
            + arrivals.map { "\($0.reference), \($0.level)" + ($0.fragile ? ", fragile" : "") }.joined(separator: "; ") + "."
        #if compiler(>=6.4) && canImport(FoundationModels)
        guard #available(macOS 27.0, *) else { return "No day summary on this version of macOS" }
        let system = SystemLanguageModel.default
        if system.isAvailable {
            guard let summary = await respond(LanguageModelSession(model: system, instructions: instructions), prompt) else {
                return "No day summary: try again"
            }
            return "Day summary: " + summary
        }
        let server = PrivateCloudComputeLanguageModel()
        if server.isAvailable {
            guard let summary = await respond(LanguageModelSession(model: server, instructions: instructions), prompt) else {
                return "No day summary: try again"
            }
            return "Day summary from Apple's servers: " + summary
        }
        switch system.availability {
        case .unavailable(.deviceNotEligible): return "No day summary: this Mac has no Apple Intelligence"
        case .unavailable(.appleIntelligenceNotEnabled): return "No day summary: Apple Intelligence is off"
        case .unavailable(.modelNotReady): return "No day summary: Apple Intelligence is getting ready"
        default: return "No day summary on this Mac"
        }
        #else
        return "No day summary on this version of macOS"
        #endif
    }

    #if compiler(>=6.4) && canImport(FoundationModels)
    @available(macOS 27.0, *)
    private static func respond(_ session: LanguageModelSession, _ prompt: String) async -> String? {
        let summary = try? await session.respond(to: prompt).content.trimmingCharacters(in: .whitespacesAndNewlines)
        return summary?.isEmpty == false ? summary : nil
    }
    #endif
}
