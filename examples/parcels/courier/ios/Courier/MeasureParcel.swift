// SPDX-License-Identifier: Apache-2.0

import ARKit
import RealityKit
import SwiftUI

/// The courier measures a parcel with the phone's camera, to see that it fits a parcel locker:
/// a tap on one end of the parcel, then on the other, and the app shows how far apart they are.
struct MeasureParcelView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var meters: Double?

    var body: some View {
        NavigationStack {
            Group {
                if ARWorldTrackingConfiguration.isSupported {
                    ZStack(alignment: .bottom) {
                        ParcelCamera(meters: $meters).ignoresSafeArea()
                        Text(verbatim: meters.map { "\(Int(($0 * 100).rounded())) cm" } ?? "Tap one end of the parcel, then the other")
                            .font(meters == nil ? .callout.bold() : .largeTitle.bold())
                            .foregroundStyle(.white)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 8)
                            .background(.black.opacity(0.6), in: RoundedRectangle(cornerRadius: 12))
                            .padding(.bottom, 40)
                    }
                } else {
                    Text("This phone cannot measure with its camera.")
                }
            }
            .navigationTitle("Measure parcel")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

/// The camera's view, where a tap marks a point on what the camera sees.
private struct ParcelCamera: UIViewRepresentable {
    @Binding var meters: Double?

    func makeCoordinator() -> Coordinator { Coordinator(meters: $meters) }

    func makeUIView(context: Context) -> ARView {
        let view = ARView(frame: .zero)
        let config = ARWorldTrackingConfiguration()
        config.planeDetection = [.horizontal, .vertical]
        view.session.run(config)
        view.addGestureRecognizer(UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.tapped(_:))))
        context.coordinator.view = view
        return view
    }

    func updateUIView(_ view: ARView, context: Context) {}

    static func dismantleUIView(_ view: ARView, coordinator: Coordinator) {
        view.session.pause()
    }

    final class Coordinator: NSObject {
        @Binding var meters: Double?
        weak var view: ARView?
        private var points: [SIMD3<Float>] = []
        private var marks: [AnchorEntity] = []

        init(meters: Binding<Double?>) { _meters = meters }

        @objc func tapped(_ tap: UITapGestureRecognizer) {
            guard let view, let hit = view.raycast(from: tap.location(in: view), allowing: .estimatedPlane, alignment: .any).first else { return }
            if points.count == 2 {
                marks.forEach { view.scene.removeAnchor($0) }
                marks.removeAll()
                points.removeAll()
                meters = nil
            }
            let point = SIMD3<Float>(hit.worldTransform.columns.3.x, hit.worldTransform.columns.3.y, hit.worldTransform.columns.3.z)
            let mark = AnchorEntity(world: point)
            mark.addChild(ModelEntity(mesh: .generateSphere(radius: 0.01), materials: [SimpleMaterial(color: .systemYellow, isMetallic: false)]))
            view.scene.addAnchor(mark)
            marks.append(mark)
            points.append(point)
            if points.count == 2 { meters = Double(simd_distance(points[0], points[1])) }
        }
    }
}
