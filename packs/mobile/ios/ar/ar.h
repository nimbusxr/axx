// SPDX-License-Identifier: Apache-2.0

// What ar.m (ARKit, the scene) and ar.swift (RealityKit) share.
#import <ARKit/ARKit.h>
#import <UIKit/UIKit.h>
#import <simd/simd.h>

// The scene's raycast from a point in a view that shows the session's picture (aspect fill,
// in the window's interface orientation).
NSArray<ARRaycastResult *> *_Nonnull axxar_raycast_from_view(UIView *_Nonnull view, CGPoint point, ARRaycastTarget target,
                                                             ARRaycastTargetAlignment alignment);

// The camera's picture of the scene (landscape right, as the camera's sensor sees it).
CGImageRef _Nullable axxar_picture(void);

// The rotation of the view's interface orientation from the sensor's, in degrees (portrait: 90).
double axxar_view_angle(UIView *_Nonnull view);

// The phone's camera pose turned to the view's interface orientation (-z forward, y up on
// screen), and the view's vertical field of view in degrees, as the picture fills it.
simd_float4x4 axxar_view_pose(UIView *_Nonnull view);
float axxar_vertical_fov(UIView *_Nonnull view);

void axxar_log(NSString *_Nonnull line);

// From ar.swift: shows the picture behind a RealityKit ARView, since the simulator's RealityKit
// draws no camera picture.
void axxar_show_picture(UIView *_Nonnull arView);
