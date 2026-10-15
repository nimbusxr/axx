// SPDX-License-Identifier: Apache-2.0

// The library's RealityKit side (ar.m has ARKit's and the scene's).
import ARKit
import RealityKit
import UIKit

extension ARView {
    /// Stands in for RealityKit's ARView.raycast(from:allowing:alignment:), which the simulator's
    /// RealityKit answers with nothing: ar.m points the app's calls to it here (the same Swift
    /// calling convention, self included), and this raycasts the scene.
    @_silgen_name("axxar_ARView_raycast")
    func axxarRaycast(from point: CGPoint, allowing target: ARRaycastQuery.Target,
                      alignment: ARRaycastQuery.TargetAlignment) -> [ARRaycastResult] {
        axxar_log("ARView.raycast from \(point.x), \(point.y)")
        return axxar_raycast_from_view(self, point, target, alignment)
    }
}

/// The simulator's RealityKit draws no camera picture: the AR view's background goes clear and
/// the picture is shown behind it, the way the camera's would be (aspect fill, upright).
@_cdecl("axxar_show_picture")
func axxarShowPicture(_ view: UIView) {
    guard let ar = view as? ARView, let superview = ar.superview else { return }
    let tag = 0x41585841 // "AXXA"
    let picture = (superview.viewWithTag(tag) as? UIImageView) ?? {
        let v = UIImageView()
        v.tag = tag
        v.contentMode = .scaleAspectFill
        v.clipsToBounds = true
        v.isUserInteractionEnabled = false
        v.accessibilityElementsHidden = true
        superview.insertSubview(v, belowSubview: ar)
        axxar_log("showing the camera's picture behind the AR view")
        return v
    }()
    picture.frame = ar.frame
    // The picture changes as the phone moves.
    if let img = axxar_picture()?.takeUnretainedValue(), picture.image?.cgImage !== img {
        // Upright: the sensor's picture turned the way the view is turned from the sensor.
        let turn: [Int: UIImage.Orientation] = [0: .up, 90: .right, 180: .down, 270: .left]
        picture.image = UIImage(cgImage: img, scale: 1, orientation: turn[Int(axxar_view_angle(ar)) % 360] ?? .up)
    }
    // The simulator's RealityKit draws an AR view's content from a camera at the world's origin,
    // whatever the session's frames say: the view goes non-AR, its camera held where the phone is.
    if ar.cameraMode != .nonAR {
        ar.cameraMode = .nonAR
        axxar_log("AR view drawn non-AR, from a camera at the phone's pose (\(axxar_vertical_fov(ar))° high)")
    }
    let phone = ar.scene.anchors.first { $0.name == "axx phone" } ?? {
        let a = AnchorEntity(world: .zero)
        a.name = "axx phone"
        a.addChild(PerspectiveCamera())
        ar.scene.addAnchor(a)
        return a
    }()
    phone.transform = Transform(matrix: axxar_view_pose(ar))
    if let camera = phone.children.first as? PerspectiveCamera {
        // The field of view is vertical, RealityKit's default.
        camera.camera = PerspectiveCameraComponent(near: 0.01, far: 1000, fieldOfViewInDegrees: axxar_vertical_fov(ar))
    }
    ar.environment.background = .color(.clear)
    ar.backgroundColor = .clear
    ar.isOpaque = false
    ar.layer.isOpaque = false
}
