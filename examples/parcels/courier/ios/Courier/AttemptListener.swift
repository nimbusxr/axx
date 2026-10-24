// SPDX-License-Identifier: Apache-2.0

import AVFoundation
import CoreML
import Observation
import SoundAnalysis

/// Proof of a delivery attempt: when no one answers, the courier taps "No one home" and rings or
/// knocks, and the phone listens. The company's own sound classifier (a Create ML sound classifier
/// of a doorbell, a knock and anything else) runs on the phone with Core ML, through Sound
/// Analysis, on the microphone's sound; the first doorbell or knock it is sure of is recorded with
/// the delivery. A Debug build ships a stub of it (Models/DoorSounds.mlmodel), which answers the
/// tests' recordings as its mappings say; the "Door sounds model" build phase compiles it into the
/// app, as it compiles the real model in a Release build.
@MainActor @Observable
final class AttemptListener {
    /// How sure the classifier must be of a doorbell or a knock.
    nonisolated private static let certainty = 0.8

    /// What the courier is told of each sound the attempt is recorded with.
    private static let sounds = ["doorbell": "the doorbell rang", "knock": "a knock at the door"]

    private(set) var listening = false
    /// What was heard, once the attempt is recorded: the sound, and what the courier is told of it.
    private(set) var heard: String?
    private(set) var recorded: String?
    private(set) var error: String?
    @ObservationIgnored private var engine: AVAudioEngine?
    @ObservationIgnored private var analyzer: SNAudioStreamAnalyzer?
    @ObservationIgnored private var observer: Observer?
    @ObservationIgnored private let analysis = DispatchQueue(label: "door-sounds")

    /// Listens until a doorbell or a knock, which record saves with the delivery.
    func start(record: @escaping (String) async -> Bool) {
        guard !listening else { return }
        listening = true
        error = nil
        Task {
            let microphone = await AVAudioApplication.requestRecordPermission()
            guard listening else { return }
            guard microphone else {
                error = "Allow the microphone to record the attempt."
                stop()
                return
            }
            do {
                guard let url = Bundle.main.url(forResource: "DoorSounds", withExtension: "mlmodelc") else {
                    throw CocoaError(.fileNoSuchFile)
                }
                let request = try SNClassifySoundRequest(mlModel: MLModel(contentsOf: url))
                let session = AVAudioSession.sharedInstance()
                try session.setCategory(.record, mode: .measurement)
                try session.setActive(true)
                let engine = AVAudioEngine()
                let format = engine.inputNode.outputFormat(forBus: 0)
                let analyzer = SNAudioStreamAnalyzer(format: format)
                // The analyzer keeps the observer, and the observer the listener, until stop().
                let observer = Observer { sound in
                    Task { @MainActor in await self.heard(sound, record: record) }
                }
                try analyzer.add(request, withObserver: observer)
                let analysis = analysis
                engine.inputNode.installTap(onBus: 0, bufferSize: 8192, format: format) { buffer, time in
                    analysis.async { analyzer.analyze(buffer, atAudioFramePosition: time.sampleTime) }
                }
                (self.engine, self.analyzer, self.observer) = (engine, analyzer, observer)
                engine.prepare()
                try engine.start()
            } catch {
                self.error = "The microphone could not start. Please try again."
                stop()
            }
        }
    }

    private func heard(_ sound: String, record: (String) async -> Bool) async {
        guard listening, let told = Self.sounds[sound] else { return }
        stop()
        if await record(sound) { (heard, recorded) = (sound, told) }
    }

    func stop() {
        listening = false
        engine?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        engine = nil
        analyzer?.removeAllRequests()
        analyzer = nil
        observer = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    /// Hands on each sound the classifier is sure of, a window of the sound at a time.
    private final class Observer: NSObject, SNResultsObserving {
        private let onSound: (String) -> Void

        init(onSound: @escaping (String) -> Void) { self.onSound = onSound }

        func request(_ request: SNRequest, didProduce result: SNResult) {
            guard let top = (result as? SNClassificationResult)?.classifications.first,
                  top.confidence >= AttemptListener.certainty else { return }
            onSound(top.identifier)
        }
    }
}
