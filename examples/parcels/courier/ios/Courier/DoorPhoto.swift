// SPDX-License-Identifier: Apache-2.0

import AVFoundation
import SwiftUI

/// The courier photographs the parcel where they left it, as proof of delivery: the photo is
/// attached to the delivery as photo-at-the-door.jpg.
struct DoorPhotoView: View {
    let model: CourierModel
    let reference: String
    @Environment(\.dismiss) private var dismiss
    @State private var camera = DoorCamera()

    var body: some View {
        NavigationStack {
            ZStack(alignment: .bottom) {
                DoorCameraPreview(camera: camera).ignoresSafeArea()
                Button("Take photo") {
                    camera.takePhoto { jpeg in
                        model.attachPhoto(reference, jpeg: jpeg)
                        dismiss()
                    }
                }
                .buttonStyle(.borderedProminent)
                .padding(.bottom, 40)
            }
            .navigationTitle("Photo at the door")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
        .onAppear { camera.start() }
        .onDisappear { camera.stop() }
    }
}

/// The back camera, for a photo: the photo comes upright, as the phone is held.
@Observable final class DoorCamera: NSObject, AVCapturePhotoCaptureDelegate {
    @ObservationIgnored let session = AVCaptureSession()
    @ObservationIgnored private let photos = AVCapturePhotoOutput()
    @ObservationIgnored private var taken: ((Data) -> Void)?

    override init() {
        super.init()
        session.sessionPreset = .photo
        guard let camera = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back),
              let input = try? AVCaptureDeviceInput(device: camera), session.canAddInput(input),
              session.canAddOutput(photos) else {
            return
        }
        session.addInput(input)
        session.addOutput(photos)
    }

    func start() { DispatchQueue.global(qos: .userInitiated).async { [session] in session.startRunning() } }

    func stop() { DispatchQueue.global(qos: .userInitiated).async { [session] in session.stopRunning() } }

    func takePhoto(_ done: @escaping (Data) -> Void) {
        taken = done
        // The phone is held upright.
        photos.connection(with: .video)?.videoRotationAngle = 90
        photos.capturePhoto(with: AVCapturePhotoSettings(format: [AVVideoCodecKey: AVVideoCodecType.jpeg]), delegate: self)
    }

    func photoOutput(_ output: AVCapturePhotoOutput, didFinishProcessingPhoto photo: AVCapturePhoto, error: Error?) {
        guard error == nil, let jpeg = photo.fileDataRepresentation() else { return }
        DispatchQueue.main.async { self.taken?(jpeg) }
    }
}

private struct DoorCameraPreview: UIViewRepresentable {
    let camera: DoorCamera

    func makeUIView(context: Context) -> PreviewView {
        let v = PreviewView()
        v.preview.session = camera.session
        v.preview.videoGravity = .resizeAspectFill
        return v
    }

    func updateUIView(_ view: PreviewView, context: Context) {}

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var preview: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }
}
