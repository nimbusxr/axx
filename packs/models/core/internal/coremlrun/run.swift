// Runs Core ML models with Core ML, on the CPU, for axx's tests of stubs. It
// reads a JSON list of runs on stdin, each a compiled model's path and its
// inputs ({type, dims, data}, by name: an image's are its pixels, by channel,
// then row), and writes their outputs, as a JSON list, on stdout: a
// multiarray's values, and a dictionary's values with their keys. A run of
// Vision runs the model as an app's VNCoreMLRequest does, on its picture,
// and gives the objects Vision recognizes ("objects": each one's top label,
// as its key, and its confidence and bounding box: x, y, width, height,
// shares of the picture's, from its bottom left corner). A run of Sound
// Analysis runs a sound classifier as an app's SNClassifySoundRequest does,
// on its samples as a recording (a WAV file, at the run's rate), and gives
// the first window's classifications as the model's probabilities.
import AVFoundation
import CoreML
import CoreVideo
import Foundation
import SoundAnalysis
import Vision

struct Value: Codable {
    var type: String
    var dims: [Int]
    var data: [Double]
    var keys: [String]?
}

struct Run: Codable {
    var model: String
    var feeds: [String: Value]
    var vision: Bool?
    var soundRate: Double?
    /// The compute units Core ML may use: "all" (an app's default: the GPU and the Neural Engine too),
    /// or the CPU alone.
    var units: String?
}

/// Sound Analysis's results, as it gives them.
final class Heard: NSObject, SNResultsObserving {
    var results: [SNClassificationResult] = []
    var error: Error?

    func request(_ request: SNRequest, didProduce result: SNResult) {
        if let r = result as? SNClassificationResult { results.append(r) }
    }

    func request(_ request: SNRequest, didFailWithError error: Error) { self.error = error }
}

/// Runs a sound classifier with Sound Analysis on samples, written as a recording.
func hear(_ model: MLModel, samples: [Double], rate: Double) throws -> [SNClassification] {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("axx-sound-\(UUID().uuidString).wav")
    defer { try? FileManager.default.removeItem(at: url) }
    let format = AVAudioFormat(standardFormatWithSampleRate: rate, channels: 1)!
    let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(samples.count))!
    buffer.frameLength = AVAudioFrameCount(samples.count)
    for (i, x) in samples.enumerated() {
        buffer.floatChannelData![0][i] = Float(x)
    }
    do {
        let file = try AVAudioFile(forWriting: url, settings: [
            AVFormatIDKey: kAudioFormatLinearPCM, AVSampleRateKey: rate, AVNumberOfChannelsKey: 1,
            AVLinearPCMBitDepthKey: 32, AVLinearPCMIsFloatKey: true,
        ])
        try file.write(from: buffer)
    }
    let analyzer = try SNAudioFileAnalyzer(url: url)
    let heard = Heard()
    try analyzer.add(SNClassifySoundRequest(mlModel: model), withObserver: heard)
    analyzer.analyze()
    if let error = heard.error { throw error }
    guard let first = heard.results.first else {
        throw NSError(domain: "axx", code: 3, userInfo: [NSLocalizedDescriptionKey: "Sound Analysis heard nothing"])
    }
    return first.classifications
}

func pixelBuffer(_ v: Value, gray: Bool) throws -> CVPixelBuffer {
    let (c, h, w) = (v.dims[v.dims.count - 3], v.dims[v.dims.count - 2], v.dims[v.dims.count - 1])
    var buffer: CVPixelBuffer?
    let format = gray ? kCVPixelFormatType_OneComponent8 : kCVPixelFormatType_32BGRA
    guard CVPixelBufferCreate(nil, w, h, format, nil, &buffer) == kCVReturnSuccess, let pb = buffer else {
        throw NSError(domain: "axx", code: 1, userInfo: [NSLocalizedDescriptionKey: "cannot create a pixel buffer"])
    }
    CVPixelBufferLockBaseAddress(pb, [])
    defer { CVPixelBufferUnlockBaseAddress(pb, []) }
    let row = CVPixelBufferGetBytesPerRow(pb)
    let base = CVPixelBufferGetBaseAddress(pb)!.assumingMemoryBound(to: UInt8.self)
    for y in 0..<h {
        for x in 0..<w {
            let at = { (ch: Int) in UInt8(max(0, min(255, v.data[(ch * h + y) * w + x].rounded()))) }
            if gray {
                base[y * row + x] = at(0)
            } else {
                let (r, g, b) = (at(0), at(c > 1 ? 1 : 0), at(c > 2 ? 2 : 0))
                base[y * row + 4 * x] = b
                base[y * row + 4 * x + 1] = g
                base[y * row + 4 * x + 2] = r
                base[y * row + 4 * x + 3] = 255
            }
        }
    }
    return pb
}

func multiArray(_ v: Value, type: MLMultiArrayDataType) throws -> MLMultiArray {
    let a = try MLMultiArray(shape: v.dims.map { NSNumber(value: $0) }, dataType: type)
    for (i, x) in v.data.enumerated() {
        a[i] = NSNumber(value: x)
    }
    return a
}

let input = FileHandle.standardInput.readDataToEndOfFile()
let runs = try JSONDecoder().decode([Run].self, from: input)
var models: [String: MLModel] = [:]
var results: [[String: Value]] = []
for run in runs {
    let config = MLModelConfiguration()
    config.computeUnits = run.units == "all" ? .all : .cpuOnly
    let key = run.model + (run.units ?? "")
    let model = try models[key] ?? MLModel(contentsOf: URL(fileURLWithPath: run.model), configuration: config)
    models[key] = model
    var features: [String: MLFeatureValue] = [:]
    for (name, desc) in model.modelDescription.inputDescriptionsByName {
        guard let v = run.feeds[name] else { continue }
        switch desc.type {
        case .image:
            let gray = desc.imageConstraint?.pixelFormatType == kCVPixelFormatType_OneComponent8
            features[name] = MLFeatureValue(pixelBuffer: try pixelBuffer(v, gray: gray))
        case .multiArray:
            features[name] = MLFeatureValue(multiArray: try multiArray(v, type: desc.multiArrayConstraint?.dataType ?? .float32))
        default:
            continue
        }
    }
    var result: [String: Value] = [:]
    if let rate = run.soundRate {
        guard let samples = run.feeds.values.first?.data else {
            throw NSError(domain: "axx", code: 2, userInfo: [NSLocalizedDescriptionKey: "Sound Analysis runs a model of a sound"])
        }
        let classifications = try hear(model, samples: samples, rate: rate)
        let name = model.modelDescription.predictedProbabilitiesName ?? "classLabelProbs"
        result[name] = Value(type: "float64", dims: [classifications.count], data: classifications.map { $0.confidence },
                             keys: classifications.map { $0.identifier })
        results.append(result)
        continue
    }
    if run.vision == true {
        guard let picture = features.values.first(where: { $0.type == .image })?.imageBufferValue else {
            throw NSError(domain: "axx", code: 2, userInfo: [NSLocalizedDescriptionKey: "Vision runs a model of a picture"])
        }
        let request = VNCoreMLRequest(model: try VNCoreMLModel(for: model))
        request.imageCropAndScaleOption = .scaleFill
        try VNImageRequestHandler(cvPixelBuffer: picture, options: [:]).perform([request])
        let objects = (request.results ?? []).compactMap { $0 as? VNRecognizedObjectObservation }
        var data: [Double] = []
        for o in objects {
            let b = o.boundingBox
            data += [Double(o.confidence), Double(b.origin.x), Double(b.origin.y), Double(b.width), Double(b.height)]
        }
        result["objects"] = Value(type: "float64", dims: [objects.count, 5], data: data, keys: objects.map { $0.labels.first?.identifier ?? "" })
        results.append(result)
        continue
    }
    let out = try model.prediction(from: MLDictionaryFeatureProvider(dictionary: features))
    for name in out.featureNames {
        guard let f = out.featureValue(for: name) else { continue }
        switch f.type {
        case .multiArray:
            let a = f.multiArrayValue!
            result[name] = Value(type: "float64", dims: a.shape.map { $0.intValue }, data: (0..<a.count).map { a[$0].doubleValue })
        case .dictionary:
            let pairs = f.dictionaryValue.map { (String(describing: $0.key), $0.value.doubleValue) }
            result[name] = Value(type: "float64", dims: [pairs.count], data: pairs.map { $0.1 }, keys: pairs.map { $0.0 })
        default:
            continue
        }
    }
    results.append(result)
}
FileHandle.standardOutput.write(try JSONEncoder().encode(results))
