// SPDX-License-Identifier: Apache-2.0

import AVFoundation
import Observation
import Speech

/// A courier dictates a note through the phone's microphone and speech recognizer.
/// The app uses the same capture and recognition APIs in every build.
@MainActor @Observable
final class DictatedNote {
    private(set) var listening = false
    private(set) var text = ""
    private(set) var error: String?
    @ObservationIgnored private var engine: AVAudioEngine?
    @ObservationIgnored private var request: SFSpeechAudioBufferRecognitionRequest?
    @ObservationIgnored private var task: SFSpeechRecognitionTask?

    func start(save: @escaping (String) -> Void) {
        guard !listening else { return }
        listening = true
        error = nil
        Task {
            let speech = await withCheckedContinuation { continuation in
                SFSpeechRecognizer.requestAuthorization { continuation.resume(returning: $0) }
            }
            let microphone = await withCheckedContinuation { continuation in
                AVAudioSession.sharedInstance().requestRecordPermission { continuation.resume(returning: $0) }
            }
            guard listening else { return }
            guard speech == .authorized, microphone else {
                error = "Allow microphone and speech recognition to dictate a note."
                stop()
                return
            }
            do {
                let session = AVAudioSession.sharedInstance()
                try session.setCategory(.record, mode: .measurement)
                try session.setActive(true)
                guard let recognizer = SFSpeechRecognizer(), recognizer.isAvailable else {
                    error = "Speech recognition is unavailable. Try again later."
                    stop()
                    return
                }
                let engine = AVAudioEngine()
                let request = SFSpeechAudioBufferRecognitionRequest()
                self.engine = engine
                self.request = request
                engine.inputNode.installTap(onBus: 0, bufferSize: 1024, format: engine.inputNode.outputFormat(forBus: 0)) { buffer, _ in
                    request.append(buffer)
                }
                task = recognizer.recognitionTask(with: request) { result, failure in
                    Task { @MainActor in
                        guard self.listening else { return }
                        if let result {
                            self.text = result.bestTranscription.formattedString
                            if result.isFinal {
                                self.stop()
                                if !self.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { save(self.text) }
                            }
                        } else if failure != nil {
                            self.error = "The note could not be transcribed. Please try again."
                            self.stop()
                        }
                    }
                }
                engine.prepare()
                try engine.start()
            } catch {
                self.error = "The microphone could not start. Please try again."
                stop()
            }
        }
    }

    func finish() { request?.endAudio() }

    func stop() {
        listening = false
        engine?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        engine = nil
        request?.endAudio()
        request = nil
        task?.cancel()
        task = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}
