// SPDX-License-Identifier: Apache-2.0

import AVFoundation
import SwiftUI
import Vision

/// The courier scans a parcel's label to open it: its QR code, or, where the code is torn or
/// smudged, the reference printed under it, which the camera reads as text.
struct LabelScannerView: View {
    let model: CourierModel
    @Environment(\.dismiss) private var dismiss
    @State private var notWithYou: String?

    var body: some View {
        NavigationStack {
            ZStack(alignment: .bottom) {
                LabelCamera { reference in
                    if model.deliveries?.contains(where: { $0.reference == reference }) == true {
                        dismiss()
                        model.path = [reference]
                    } else {
                        notWithYou = CourierModel.notWithYou(reference)
                    }
                }
                .ignoresSafeArea()
                Text(verbatim: notWithYou ?? "Point the camera at the parcel's label")
                    .font(.callout.bold())
                    .foregroundStyle(.white)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .background(.black.opacity(0.6), in: RoundedRectangle(cornerRadius: 12))
                    .padding(.bottom, 40)
            }
            .navigationTitle("Scan label")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }
}

/// The camera, reading a label: a QR code through the session's metadata output, and the printed
/// reference through Vision's text recognition on its video frames.
private struct LabelCamera: UIViewControllerRepresentable {
    let found: (String) -> Void

    func makeUIViewController(context: Context) -> LabelCameraController {
        let c = LabelCameraController()
        c.found = found
        return c
    }

    func updateUIViewController(_ controller: LabelCameraController, context: Context) {
        controller.found = found
    }
}

final class LabelCameraController: UIViewController, AVCaptureMetadataOutputObjectsDelegate, AVCaptureVideoDataOutputSampleBufferDelegate {
    var found: ((String) -> Void)?
    private let session = AVCaptureSession()
    private let frames = DispatchQueue(label: "label frames")
    private var preview: AVCaptureVideoPreviewLayer?
    private var lastFound: String?
    /// A parcel's reference, as its label prints it.
    private static let reference = try! Regex("PX-[A-Z]{3}-[0-9]{4}")

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        guard let camera = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back),
              let input = try? AVCaptureDeviceInput(device: camera), session.canAddInput(input) else {
            return
        }
        session.addInput(input)
        let codes = AVCaptureMetadataOutput()
        if session.canAddOutput(codes) {
            session.addOutput(codes)
            codes.setMetadataObjectsDelegate(self, queue: .main)
            codes.metadataObjectTypes = [.qr]
        }
        let video = AVCaptureVideoDataOutput()
        video.alwaysDiscardsLateVideoFrames = true
        video.setSampleBufferDelegate(self, queue: frames)
        if session.canAddOutput(video) { session.addOutput(video) }
        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.videoGravity = .resizeAspectFill
        view.layer.addSublayer(layer)
        preview = layer
        DispatchQueue.global(qos: .userInitiated).async { [session] in session.startRunning() }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        DispatchQueue.global(qos: .userInitiated).async { [session] in session.stopRunning() }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        for case let code as AVMetadataMachineReadableCodeObject in objects {
            if let text = code.stringValue, let match = text.firstMatch(of: Self.reference) { report(String(text[match.range])) }
        }
    }

    func captureOutput(_ output: AVCaptureOutput, didOutput sampleBuffer: CMSampleBuffer, from connection: AVCaptureConnection) {
        guard let pixels = CMSampleBufferGetImageBuffer(sampleBuffer) else { return }
        let request = VNRecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.usesLanguageCorrection = false
        // The camera's frames come as its sensor sees them, the top of the phone to their left.
        try? VNImageRequestHandler(cvPixelBuffer: pixels, orientation: .right).perform([request])
        for line in request.results ?? [] {
            if let text = line.topCandidates(1).first?.string, let match = text.firstMatch(of: Self.reference) {
                let reference = String(text[match.range])
                DispatchQueue.main.async { self.report(reference) }
            }
        }
    }

    private func report(_ reference: String) {
        guard reference != lastFound else { return }
        lastFound = reference
        found?(reference)
    }
}
