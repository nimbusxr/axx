// SPDX-License-Identifier: Apache-2.0

// axx loads this library into an app whose camera sees a scene (ar.go): the iOS simulator's
// ARKit is a skeleton, whose classes keep what is put in them, but whose session never runs and
// whose camera math answers identity or zero. Here world tracking is supported, and a running
// session plays a phone in the scene (AXX_AR_SCENE, the scene's description axx writes):
//   - its current frame is the scene seen from the phone's pose, by a camera like an iPhone's
//     wide camera (a landscape-right sensor, 1280x960 here, about 62° across);
//   - the camera math is computed for that camera, so RealityKit places content where it
//     belongs (ar.swift draws the picture behind the AR view);
//   - a raycast lands on the scene's surfaces, and on a named object where a tap on it lands;
//   - axx asks a control socket (AXX_AR_CONTROL) where a named object shows on the screen, and
//     taps there, as a finger would;
//   - axx moves the phone through the scene, by the same socket: the camera then sees the scene
//     from there. Where the phone is (AXX_AR_POSE, when axx has moved it) outlasts the app: a
//     phone stays where it was when its app starts again.
// It logs to AXX_AR_LOG. Without AXX_AR_SCENE it does nothing.
#import <ARKit/ARKit.h>
#import <Metal/Metal.h>
#import <SceneKit/SceneKit.h>
#import <UIKit/UIKit.h>
#import <dlfcn.h>
#import <mach-o/dyld.h>
#import <mach-o/loader.h>
#import <mach/mach.h>
#import <objc/message.h>
#import <objc/runtime.h>
#import <simd/simd.h>
#import <sys/socket.h>
#import <sys/un.h>
#import "ar.h"

// Logs to the unified log, and to AXX_AR_LOG when it names a file.
static void arLog(NSString *line) {
    NSLog(@"[axx ar] %@", line);
    static NSFileHandle *file;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        NSString *path = NSProcessInfo.processInfo.environment[@"AXX_AR_LOG"];
        if (!path.length) return;
        if (![NSFileManager.defaultManager fileExistsAtPath:path]) [NSData.data writeToFile:path atomically:NO];
        file = [NSFileHandle fileHandleForWritingAtPath:path];
        [file seekToEndOfFile];
    });
    @synchronized(file) {
        [file writeData:[[NSString stringWithFormat:@"%.3f %@\n", CACurrentMediaTime(), line] dataUsingEncoding:NSUTF8StringEncoding]];
    }
}
#define LOG(fmt, ...) arLog([NSString stringWithFormat:fmt, ##__VA_ARGS__])

// The emulated camera: its sensor's size in pixels and its focal length.
static const float W = 1280, H = 960, F = 1060;

static SCNScene *scene;
static simd_float4x4 sensorPose;   // ARCamera.transform: the sensor's pose (landscape-right axes)
static CVPixelBufferRef picture;   // the sensor's picture of the scene from that pose
static CGImageRef pictureImage;    // the same, as an image
static dispatch_queue_t renderQueue;  // where the pictures are drawn, one at a time
static __weak ARSession *running;
static CADisplayLink *pump;

// The scene's objects a scenario names: each node, and where a tap on it lands in the world.
static NSMutableDictionary<NSString *, SCNNode *> *objects;
static NSMapTable<SCNNode *, NSData *> *taps;  // three floats each
static NSMutableArray<NSString *> *objectNames;

#pragma mark - math

static simd_float4x4 rotZ(float degrees) {
    float r = degrees * (float)M_PI / 180, c = cosf(r), s = sinf(r);
    return (simd_float4x4){{{c, s, 0, 0}, {-s, c, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}};
}

// The rotation, about the camera's z, from the sensor's axes to the axes of an interface
// orientation, in degrees: the sensor's own orientation is landscape right.
static float angleOf(UIInterfaceOrientation o) {
    switch (o) {
    case UIInterfaceOrientationPortrait: return 90;
    case UIInterfaceOrientationLandscapeLeft: return 180;
    case UIInterfaceOrientationPortraitUpsideDown: return 270;
    default: return 0;
    }
}

static int quarter(double degrees) { return ((int)lround(degrees / 90) % 4 + 4) % 4; }

static CGSize orientedImage(double degrees) { return quarter(degrees) % 2 ? CGSizeMake(H, W) : CGSizeMake(W, H); }

// How much of the viewport the image fills: aspect fill, the way ARKit shows its picture.
static float fillScale(double degrees, CGSize viewport) {
    CGSize img = orientedImage(degrees);
    if (viewport.width <= 0 || viewport.height <= 0) return 1;
    return fmaxf(viewport.width / img.width, viewport.height / img.height);
}

static simd_float4x4 viewMatrix(double degrees) { return simd_inverse(simd_mul(sensorPose, rotZ(degrees))); }

static simd_float4x4 projection(double degrees, CGSize viewport, float near, float far) {
    if (viewport.width <= 0 || viewport.height <= 0) viewport = orientedImage(degrees);
    float s = fillScale(degrees, viewport);
    return (simd_float4x4){{
        {2 * F * s / viewport.width, 0, 0, 0},
        {0, 2 * F * s / viewport.height, 0, 0},
        {0, 0, -(far + near) / (far - near), -1},
        {0, 0, -2 * far * near / (far - near), 0},
    }};
}

// From normalized image coordinates (the sensor's picture) to normalized view coordinates.
static CGAffineTransform displayTransform(double degrees, CGSize viewport) {
    CGAffineTransform t;
    switch (quarter(degrees)) {
    case 1: t = (CGAffineTransform){0, 1, -1, 0, 1, 0}; break;  // x = 1 - v, y = u
    case 2: t = (CGAffineTransform){-1, 0, 0, -1, 1, 1}; break;
    case 3: t = (CGAffineTransform){0, -1, 1, 0, 0, 1}; break;  // x = v, y = 1 - u
    default: t = CGAffineTransformIdentity;
    }
    if (viewport.width <= 0 || viewport.height <= 0) return t;
    CGSize img = orientedImage(degrees);
    float s = fillScale(degrees, viewport);
    double kx = img.width * s / viewport.width, ky = img.height * s / viewport.height;
    return (CGAffineTransform){t.a * kx, t.b * ky, t.c * kx, t.d * ky, t.tx * kx + 0.5 * (1 - kx), t.ty * ky + 0.5 * (1 - ky)};
}

static CGPoint projectPoint(simd_float3 p, double degrees, CGSize viewport) {
    simd_float4 clip = simd_mul(projection(degrees, viewport, 0.01, 1000), simd_mul(viewMatrix(degrees), simd_make_float4(p, 1)));
    if (viewport.width <= 0 || viewport.height <= 0) viewport = orientedImage(degrees);
    return CGPointMake((clip.x / clip.w + 1) / 2 * viewport.width, (1 - clip.y / clip.w) / 2 * viewport.height);
}

#pragma mark - the scene

static simd_float4x4 matrixOf(NSArray<NSNumber *> *m) {
    simd_float4x4 out = matrix_identity_float4x4;
    if (m.count != 16) return out;
    for (int c = 0; c < 4; c++)
        for (int r = 0; r < 4; r++) out.columns[c][r] = m[c * 4 + r].floatValue;
    return out;
}

static NSData *bytes(id b64) {
    if (![b64 isKindOfClass:NSString.class] || ![b64 length]) return nil;
    return [[NSData alloc] initWithBase64EncodedString:b64 options:0];
}

static SCNMaterial *materialOf(NSDictionary *m, NSArray<UIImage *> *images) {
    SCNMaterial *mat = [SCNMaterial material];
    mat.lightingModelName = SCNLightingModelPhysicallyBased;
    NSArray<NSNumber *> *c = m[@"color"];
    UIColor *color = [UIColor colorWithRed:c[0].doubleValue green:c[1].doubleValue blue:c[2].doubleValue alpha:c[3].doubleValue];
    NSInteger image = [m[@"image"] integerValue];
    if (image >= 0 && image < (NSInteger)images.count) {
        mat.diffuse.contents = images[image];
        mat.multiply.contents = color;
    } else {
        mat.diffuse.contents = color;
    }
    mat.metalness.contents = m[@"metallic"];
    mat.roughness.contents = m[@"roughness"];
    mat.doubleSided = [m[@"doubleSided"] boolValue];
    if (c[3].doubleValue < 1) mat.transparency = c[3].doubleValue;
    return mat;
}

static SCNGeometry *geometryOf(NSDictionary *m, NSArray<SCNMaterial *> *materials) {
    NSData *positions = bytes(m[@"positions"]), *normals = bytes(m[@"normals"]), *uvs = bytes(m[@"uvs"]), *indices = bytes(m[@"indices"]);
    NSInteger n = positions.length / 12;
    if (!n || !indices.length) return nil;
    NSMutableArray *sources = [NSMutableArray array];
    [sources addObject:[SCNGeometrySource geometrySourceWithData:positions semantic:SCNGeometrySourceSemanticVertex vectorCount:n
                                                 floatComponents:YES componentsPerVector:3 bytesPerComponent:4 dataOffset:0 dataStride:12]];
    if ((NSInteger)normals.length == n * 12)
        [sources addObject:[SCNGeometrySource geometrySourceWithData:normals semantic:SCNGeometrySourceSemanticNormal vectorCount:n
                                                     floatComponents:YES componentsPerVector:3 bytesPerComponent:4 dataOffset:0 dataStride:12]];
    if ((NSInteger)uvs.length == n * 8)
        [sources addObject:[SCNGeometrySource geometrySourceWithData:uvs semantic:SCNGeometrySourceSemanticTexcoord vectorCount:n
                                                     floatComponents:YES componentsPerVector:2 bytesPerComponent:4 dataOffset:0 dataStride:8]];
    SCNGeometryElement *e = [SCNGeometryElement geometryElementWithData:indices primitiveType:SCNGeometryPrimitiveTypeTriangles
                                                         primitiveCount:indices.length / 12 bytesPerIndex:4];
    SCNGeometry *g = [SCNGeometry geometryWithSources:sources elements:@[e]];
    NSInteger mat = [m[@"material"] integerValue];
    if (mat >= 0 && mat < (NSInteger)materials.count) g.materials = @[materials[mat]];
    else {
        SCNMaterial *grey = [SCNMaterial material];
        grey.lightingModelName = SCNLightingModelPhysicallyBased;
        grey.diffuse.contents = UIColor.lightGrayColor;
        grey.metalness.contents = @0;
        grey.roughness.contents = @1;
        g.materials = @[grey];
    }
    return g;
}

// The scene, from the description axx wrote: its nodes, the phone's pose and its objects.
static BOOL loadScene(void) {
    NSString *path = NSProcessInfo.processInfo.environment[@"AXX_AR_SCENE"];
    if (!path.length) return NO;
    NSData *data = [NSData dataWithContentsOfFile:path];
    NSDictionary *d = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
    if (![d isKindOfClass:NSDictionary.class]) { LOG(@"cannot read the scene at %@: ARKit stays unsupported", path); return NO; }

    NSMutableArray<UIImage *> *images = [NSMutableArray array];
    for (NSString *b64 in d[@"images"]) [images addObject:[UIImage imageWithData:bytes(b64)] ?: [UIImage new]];
    NSMutableArray<SCNMaterial *> *materials = [NSMutableArray array];
    for (NSDictionary *m in d[@"materials"]) [materials addObject:materialOf(m, images)];

    NSArray<NSDictionary *> *described = d[@"nodes"];
    NSMutableArray<SCNNode *> *nodes = [NSMutableArray arrayWithCapacity:described.count];
    for (NSDictionary *n in described) {
        SCNNode *node = [SCNNode node];
        node.name = n[@"name"];
        node.simdTransform = matrixOf(n[@"matrix"]);
        NSArray *meshes = n[@"meshes"];
        if (meshes.count == 1) node.geometry = geometryOf(meshes[0], materials);
        else
            for (NSDictionary *m in meshes) {
                SCNNode *part = [SCNNode nodeWithGeometry:geometryOf(m, materials)];
                part.name = @"_mesh";
                [node addChildNode:part];
            }
        [nodes addObject:node];
    }
    for (NSUInteger i = 0; i < described.count; i++)
        for (NSNumber *c in described[i][@"children"])
            if (c.unsignedIntegerValue < nodes.count) [nodes[i] addChildNode:nodes[c.unsignedIntegerValue]];

    scene = [SCNScene scene];
    scene.background.contents = [UIColor colorWithRed:0.72 green:0.79 blue:0.85 alpha:1];  // an overcast sky
    scene.lightingEnvironment.contents = [UIColor colorWithWhite:0.6 alpha:1];
    for (NSNumber *r in d[@"roots"])
        if (r.unsignedIntegerValue < nodes.count) [scene.rootNode addChildNode:nodes[r.unsignedIntegerValue]];
    SCNNode *sky = [SCNNode node];
    sky.name = @"_light";
    sky.light = [SCNLight light];
    sky.light.type = SCNLightTypeAmbient;
    sky.light.intensity = 500;
    [scene.rootNode addChildNode:sky];
    SCNNode *sun = [SCNNode node];
    sun.name = @"_light";
    sun.light = [SCNLight light];
    sun.light.type = SCNLightTypeDirectional;
    sun.light.intensity = 1000;
    sun.simdEulerAngles = simd_make_float3(-M_PI / 3, M_PI / 6, 0);
    [scene.rootNode addChildNode:sun];

    objects = [NSMutableDictionary dictionary];
    objectNames = [NSMutableArray array];
    taps = [NSMapTable strongToStrongObjectsMapTable];
    for (NSDictionary *o in d[@"objects"]) {
        NSUInteger i = [o[@"node"] unsignedIntegerValue];
        NSArray<NSNumber *> *t = o[@"tap"];
        if (i >= nodes.count || t.count != 3) continue;
        objects[o[@"name"]] = nodes[i];
        [objectNames addObject:[NSString stringWithFormat:@"\"%@\"", o[@"name"]]];
        float tap[3] = {t[0].floatValue, t[1].floatValue, t[2].floatValue};
        [taps setObject:[NSData dataWithBytes:tap length:sizeof tap] forKey:nodes[i]];
    }
    // The phone is held upright; its sensor's x runs toward the phone's bottom. Where axx moved it
    // last, when it has, is where it is.
    NSArray *phone = d[@"phone"];
    NSString *posePath = NSProcessInfo.processInfo.environment[@"AXX_AR_POSE"];
    NSData *moved = posePath.length ? [NSData dataWithContentsOfFile:posePath] : nil;
    NSArray *pose = moved ? [NSJSONSerialization JSONObjectWithData:moved options:0 error:nil] : nil;
    if ([pose isKindOfClass:NSArray.class] && pose.count == 16) phone = pose;
    sensorPose = simd_mul(matrixOf(phone), rotZ(-90));
    LOG(@"scene %@: %lu nodes, objects %@, phone at %.2f %.2f %.2f", path.lastPathComponent, (unsigned long)nodes.count,
        [objectNames componentsJoinedByString:@", "], sensorPose.columns[3].x, sensorPose.columns[3].y, sensorPose.columns[3].z);
    return YES;
}

// Where a tap on an object lands in the world.
static simd_float3 tapPointOf(SCNNode *n) {
    float p[3] = {0, 0, 0};
    [[taps objectForKey:n] getBytes:p length:sizeof p];
    return simd_make_float3(p[0], p[1], p[2]);
}

// The object a node is part of, or nil.
static SCNNode *objectOf(SCNNode *n) {
    for (; n; n = n.parentNode)
        if ([taps objectForKey:n]) return n;
    return nil;
}

// The camera's own format: biplanar YCbCr 4:2:0, full range (BT.601), as ARKit hands it over.
static CVPixelBufferRef toYCbCr(CVPixelBufferRef bgra) {
    CVPixelBufferRef out = NULL;
    NSDictionary *attrs = @{(id)kCVPixelBufferIOSurfacePropertiesKey: @{}, (id)kCVPixelBufferMetalCompatibilityKey: @YES};
    CVPixelBufferCreate(NULL, W, H, kCVPixelFormatType_420YpCbCr8BiPlanarFullRange, (__bridge CFDictionaryRef)attrs, &out);
    CVPixelBufferLockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferLockBaseAddress(out, 0);
    const uint8_t *src = CVPixelBufferGetBaseAddress(bgra);
    size_t srow = CVPixelBufferGetBytesPerRow(bgra);
    uint8_t *y = CVPixelBufferGetBaseAddressOfPlane(out, 0), *uv = CVPixelBufferGetBaseAddressOfPlane(out, 1);
    size_t yrow = CVPixelBufferGetBytesPerRowOfPlane(out, 0), uvrow = CVPixelBufferGetBytesPerRowOfPlane(out, 1);
    for (int j = 0; j < H; j++)
        for (int i = 0; i < W; i++) {
            const uint8_t *p = src + j * srow + i * 4;  // B G R A
            float b = p[0], g = p[1], r = p[2];
            y[j * yrow + i] = (uint8_t)fminf(255, fmaxf(0, 0.299f * r + 0.587f * g + 0.114f * b));
            if (!(i & 1) && !(j & 1)) {
                uv[(j / 2) * uvrow + i] = (uint8_t)fminf(255, fmaxf(0, 128 - 0.168736f * r - 0.331264f * g + 0.5f * b));
                uv[(j / 2) * uvrow + i + 1] = (uint8_t)fminf(255, fmaxf(0, 128 + 0.5f * r - 0.418688f * g - 0.081312f * b));
            }
        }
    CVPixelBufferUnlockBaseAddress(out, 0);
    CVPixelBufferUnlockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly);
    return out;
}

// The sensor's picture of the scene from a pose, and the same as an image (retained); on the
// render queue.
static CVPixelBufferRef render(simd_float4x4 pose, CGImageRef *image) {
    static SCNRenderer *r;
    static SCNNode *eye;
    if (!r) {
        r = [SCNRenderer rendererWithDevice:MTLCreateSystemDefaultDevice() options:nil];
        r.scene = scene;
        eye = [SCNNode node];
        eye.name = @"_eye";
        eye.camera = [SCNCamera camera];
        eye.camera.projectionTransform = SCNMatrix4FromMat4(projection(0, CGSizeMake(W, H), 0.05, 500));
        [SCNTransaction begin];
        [scene.rootNode addChildNode:eye];
        [SCNTransaction commit];
        r.pointOfView = eye;
    }
    CFTimeInterval start = CACurrentMediaTime();
    [SCNTransaction begin];
    [SCNTransaction setDisableActions:YES];
    eye.simdTransform = pose;
    [SCNTransaction commit];
    [SCNTransaction flush];
    UIImage *img = [r snapshotAtTime:0 withSize:CGSizeMake(W, H) antialiasingMode:SCNAntialiasingModeMultisampling4X];
    *image = CGImageRetain(img.CGImage);

    CVPixelBufferRef buf = NULL;
    NSDictionary *attrs = @{(id)kCVPixelBufferIOSurfacePropertiesKey: @{}, (id)kCVPixelBufferMetalCompatibilityKey: @YES};
    CVPixelBufferCreate(NULL, W, H, kCVPixelFormatType_32BGRA, (__bridge CFDictionaryRef)attrs, &buf);
    CVPixelBufferLockBaseAddress(buf, 0);
    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    CGContextRef ctx = CGBitmapContextCreate(CVPixelBufferGetBaseAddress(buf), W, H, 8, CVPixelBufferGetBytesPerRow(buf), cs,
                                             kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
    CGContextDrawImage(ctx, CGRectMake(0, 0, W, H), img.CGImage);
    CGContextRelease(ctx);
    CGColorSpaceRelease(cs);
    CVPixelBufferUnlockBaseAddress(buf, 0);
    CVPixelBufferRef yuv = toYCbCr(buf);
    CVPixelBufferRelease(buf);
    LOG(@"rendered the scene from the phone at %.2f %.2f %.2f: %.0fx%.0f in %.0f ms", pose.columns[3].x, pose.columns[3].y, pose.columns[3].z, W, H,
        (CACurrentMediaTime() - start) * 1000);
    return yuv;
}

// Shows the scene from a pose: draws the picture, then, on the main thread, makes it and the pose
// the camera's at once, as its next frame shows them.
static void showFrom(simd_float4x4 pose) {
    __block CGImageRef image = NULL;
    __block CVPixelBufferRef yuv = NULL;
    dispatch_sync(renderQueue, ^{ yuv = render(pose, &image); });
    void (^swap)(void) = ^{
        CVPixelBufferRef oldPicture = picture;
        CGImageRef oldImage = pictureImage;
        sensorPose = pose;
        picture = yuv;
        pictureImage = image;
        CVPixelBufferRelease(oldPicture);
        CGImageRelease(oldImage);
    };
    if (NSThread.isMainThread) swap();
    else dispatch_sync(dispatch_get_main_queue(), swap);
}

#pragma mark - objects ARKit will not make for us

static id make(Class c) { return ((id(*)(id, SEL))objc_msgSend)([c alloc], @selector(init)); }

static void put(id o, const char *ivar, const void *v, size_t n) {
    Ivar iv = class_getInstanceVariable(object_getClass(o), ivar);
    if (!iv) { LOG(@"no ivar %s on %@", ivar, o); return; }
    memcpy((char *)(__bridge void *)o + ivar_getOffset(iv), v, n);
}

static void putObject(id o, const char *ivar, id v) {
    Ivar iv = class_getInstanceVariable(object_getClass(o), ivar);
    if (!iv) { LOG(@"no ivar %s on %@", ivar, o); return; }
    object_setIvarWithStrongDefault(o, iv, v);
}

static ARCamera *makeCamera(void) {
    ARCamera *cam = make([ARCamera class]);
    simd_float3x3 k = {{{F, 0, 0}, {0, F, 0}, {W / 2, H / 2, 1}}};
    CGSize res = CGSizeMake(W, H);
    simd_float4x4 p = projection(0, res, 0.001, 1000);
    NSInteger normal = ARTrackingStateNormal, reason = ARTrackingStateReasonNone;
    put(cam, "_transform", &sensorPose, sizeof sensorPose);
    put(cam, "_intrinsics", &k, sizeof k);
    put(cam, "_imageResolution", &res, sizeof res);
    put(cam, "_projectionMatrix", &p, sizeof p);
    put(cam, "_trackingState", &normal, sizeof normal);
    put(cam, "_trackingStateReason", &reason, sizeof reason);
    return cam;
}

static ARFrame *makeFrame(void) {
    ARFrame *f = make([ARFrame class]);
    NSTimeInterval t = CACurrentMediaTime();
    NSInteger mapped = ARWorldMappingStatusMapped;
    CVPixelBufferRetain(picture);
    CVPixelBufferRef img = picture;
    put(f, "_timestamp", &t, sizeof t);
    put(f, "_capturedImage", &img, sizeof img);
    put(f, "_worldMappingStatus", &mapped, sizeof mapped);
    putObject(f, "_camera", makeCamera());
    putObject(f, "_anchors", @[]);
    return f;
}

static ARRaycastQuery *makeQuery(simd_float3 origin, simd_float3 direction, ARRaycastTarget target, ARRaycastTargetAlignment alignment) {
    ARRaycastQuery *q = make([ARRaycastQuery class]);
    NSInteger t = target, a = alignment;
    put(q, "_origin", &origin, sizeof origin);
    put(q, "_direction", &direction, sizeof direction);
    put(q, "_target", &t, sizeof t);
    put(q, "_targetAlignment", &a, sizeof a);
    return q;
}

#pragma mark - the camera view, and where its objects show

static __weak UIView *cameraView;  // the app's AR view showing the session

static UIView *findARView(UIView *v) {
    if ([NSStringFromClass(v.class) containsString:@"ARView"]) return v;
    for (UIView *s in v.subviews) {
        UIView *f = findARView(s);
        if (f) return f;
    }
    return nil;
}

// Finds the app's AR view and dresses it for the simulator (ar.swift): the picture behind it,
// RealityKit's camera at the phone's pose.
static void dressCameraView(void) {
    for (UIScene *s in UIApplication.sharedApplication.connectedScenes) {
        if (![s isKindOfClass:UIWindowScene.class]) continue;
        for (UIWindow *w in ((UIWindowScene *)s).windows) {
            UIView *ar = findARView(w);
            if (!ar || ar.bounds.size.width <= 0) continue;
            if (cameraView != ar) LOG(@"camera view: %@ (%.0fx%.0f)", NSStringFromClass(ar.class), ar.bounds.size.width, ar.bounds.size.height);
            cameraView = ar;
            axxar_show_picture(ar);
            return;
        }
    }
}

// The camera view once it rests where it shows: a view still moving into place (a sheet sliding
// up) takes no touches, so it is the view once it has rested on screen for a moment; else nil,
// and why, for the control channel.
static UIView *settledCameraView(NSDictionary **why) {
    UIView *ar = cameraView;
    if (!ar.window) {
        *why = @{@"error": @"no camera view shows", @"retry": @YES};
        return nil;
    }
    static CFTimeInterval restingSince;
    CALayer *now = ar.layer.presentationLayer, *win = ar.window.layer.presentationLayer;
    CGRect shown = now && win ? [now convertRect:now.bounds toLayer:win] : CGRectNull;
    CGRect rest = [ar convertRect:ar.bounds toView:nil];
    BOOL resting = !CGRectIsNull(shown) && fabs(shown.origin.x - rest.origin.x) < 0.5 && fabs(shown.origin.y - rest.origin.y) < 0.5 &&
                   fabs(shown.size.width - rest.size.width) < 0.5 && fabs(shown.size.height - rest.size.height) < 0.5;
    if (!resting) {
        restingSince = 0;
        *why = @{@"error": @"the camera view is still moving into place", @"retry": @YES};
        return nil;
    }
    if (!restingSince) restingSince = CACurrentMediaTime();
    if (CACurrentMediaTime() - restingSince < 0.3) {
        *why = @{@"error": @"the camera view is still settling", @"retry": @YES};
        return nil;
    }
    return ar;
}

// Where the scene's object of that name shows, in screen points, for the control channel:
// {"x", "y"}, or {"error", "retry"} where asking again may help.
static NSDictionary *where(NSString *name) {
    SCNNode *n = objects[name];
    if (!n) return @{@"error": [NSString stringWithFormat:@"the scene has no \"%@\": its objects are %@", name, [objectNames componentsJoinedByString:@", "]], @"retry": @NO};
    NSDictionary *unsettled = nil;
    UIView *ar = settledCameraView(&unsettled);
    if (!ar) return unsettled;
    double deg = axxar_view_angle(ar);
    simd_float3 p = tapPointOf(n);
    simd_float4 clip = simd_mul(projection(deg, ar.bounds.size, 0.01, 1000), simd_mul(viewMatrix(deg), simd_make_float4(p, 1)));
    CGPoint v = projectPoint(p, deg, ar.bounds.size);
    if (clip.w <= 0 || !CGRectContainsPoint(ar.bounds, v))
        return @{@"error": [NSString stringWithFormat:@"the \"%@\" is out of the camera's view", name], @"retry": @YES};
    CGPoint screen = [ar convertPoint:v toCoordinateSpace:ar.window.screen.coordinateSpace];
    LOG(@"\"%@\" shows at %.1f, %.1f", name, screen.x, screen.y);
    return @{@"x": @(screen.x), @"y": @(screen.y)};
}

// The control channel: a Unix socket (AXX_AR_CONTROL) that axx asks, one JSON line each way.
static void serveControl(NSString *path) {
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        int s = socket(AF_UNIX, SOCK_STREAM, 0);
        struct sockaddr_un addr = {.sun_family = AF_UNIX};
        strlcpy(addr.sun_path, path.fileSystemRepresentation, sizeof addr.sun_path);
        unlink(addr.sun_path);
        if (bind(s, (struct sockaddr *)&addr, sizeof addr) != 0 || listen(s, 8) != 0) {
            LOG(@"cannot serve control at %@: %s", path, strerror(errno));
            return;
        }
        LOG(@"control at %@", path);
        for (;;) {
            int c = accept(s, NULL, NULL);
            if (c < 0) continue;
            NSMutableData *line = [NSMutableData data];
            char ch;
            while (read(c, &ch, 1) == 1 && ch != '\n') [line appendBytes:&ch length:1];
            NSDictionary *req = [NSJSONSerialization JSONObjectWithData:line options:0 error:nil];
            __block NSDictionary *res = @{@"error": @"axx's AR library does not understand the question", @"retry": @NO};
            if ([req isKindOfClass:NSDictionary.class] && [req[@"where"] isKindOfClass:NSString.class])
                dispatch_sync(dispatch_get_main_queue(), ^{ res = where(req[@"where"]); });
            else if ([req isKindOfClass:NSDictionary.class] && [req[@"pose"] isKindOfClass:NSArray.class] && [req[@"pose"] count] == 16) {
                showFrom(simd_mul(matrixOf(req[@"pose"]), rotZ(-90)));
                dispatch_sync(dispatch_get_main_queue(), ^{ dressCameraView(); });
                res = @{};
            }
            NSMutableData *out = [[NSJSONSerialization dataWithJSONObject:res options:0 error:nil] mutableCopy];
            [out appendBytes:"\n" length:1];
            write(c, out.bytes, out.length);
            close(c);
        }
    });
}

#pragma mark - the session

@interface AXXARPump : NSObject
@end
@implementation AXXARPump {
    NSUInteger ticks;
}
- (void)tick:(CADisplayLink *)link {
    ARSession *s = running;
    if (!s) {
        [link invalidate];
        pump = nil;
        return;
    }
    ARFrame *f = makeFrame();
    putObject(s, "_currentFrame", f);
    id<ARSessionDelegate> d = s.delegate;
    if ([d respondsToSelector:@selector(session:didUpdateFrame:)]) [d session:s didUpdateFrame:f];
    if (ticks++ % 30 == 0) dressCameraView();
}
@end

static void startPump(ARSession *s, ARConfiguration *c) {
    running = s;
    putObject(s, "_configuration", c);
    if (!picture) showFrom(sensorPose);
    if (!pump) {
        pump = [CADisplayLink displayLinkWithTarget:[AXXARPump new] selector:@selector(tick:)];
        pump.preferredFrameRateRange = CAFrameRateRangeMake(30, 30, 30);
        [pump addToRunLoop:NSRunLoop.mainRunLoop forMode:NSRunLoopCommonModes];
    }
    LOG(@"session running %@", c.class);
}

#pragma mark - raycasts

static NSArray<ARRaycastResult *> *raycast(ARRaycastQuery *q) {
    simd_float3 o = q.origin, d = simd_normalize(q.direction), e = o + d * 200;
    NSArray<SCNHitTestResult *> *hits = [scene.rootNode hitTestWithSegmentFromPoint:SCNVector3FromFloat3(o) toPoint:SCNVector3FromFloat3(e)
                                                                            options:@{SCNHitTestOptionSearchMode: @(SCNHitTestSearchModeClosest)}];
    SCNHitTestResult *hit = hits.firstObject;
    if (!hit) {
        LOG(@"raycast from %.2f %.2f %.2f along %.3f %.3f %.3f: nothing", o.x, o.y, o.z, d.x, d.y, d.z);
        return @[];
    }
    // A ray that hits one of the scene's objects lands where a tap on the object lands;
    // elsewhere (the ground, the scene's furniture) it lands where it hits.
    SCNNode *object = objectOf(hit.node);
    simd_float3 p = object ? tapPointOf(object) : SCNVector3ToFloat3(hit.worldCoordinates);
    simd_float4x4 t = matrix_identity_float4x4;
    t.columns[3] = simd_make_float4(p, 1);
    ARRaycastResult *r = make([ARRaycastResult class]);
    NSInteger target = q.target,
              alignment = SCNVector3ToFloat3(hit.worldNormal).y > 0.7 ? ARRaycastTargetAlignmentHorizontal : ARRaycastTargetAlignmentVertical;
    put(r, "_worldTransform", &t, sizeof t);
    put(r, "_target", &target, sizeof target);
    put(r, "_targetAlignment", &alignment, sizeof alignment);
    LOG(@"raycast hits %@%@ at %.3f %.3f %.3f", object ? object.name : hit.node.name, object ? @" (the object)" : @"", p.x, p.y, p.z);
    return @[r];
}

#pragma mark - for ar.swift

void axxar_log(NSString *line) { arLog(line); }

CGImageRef axxar_picture(void) { return pictureImage; }

double axxar_view_angle(UIView *view) {
    UIWindowScene *ws = view.window.windowScene;
    return angleOf(ws ? ws.interfaceOrientation : UIInterfaceOrientationPortrait);
}

simd_float4x4 axxar_view_pose(UIView *view) { return simd_mul(sensorPose, rotZ(axxar_view_angle(view))); }

float axxar_vertical_fov(UIView *view) {
    double deg = axxar_view_angle(view);
    float s = fillScale(deg, view.bounds.size);
    return 2 * atanf(view.bounds.size.height / s / 2 / F) * 180 / (float)M_PI;
}

NSArray<ARRaycastResult *> *axxar_raycast_from_view(UIView *view, CGPoint point, ARRaycastTarget target, ARRaycastTargetAlignment alignment) {
    if (!scene) return @[];
    double deg = axxar_view_angle(view);
    CGSize v = view.bounds.size;
    // The view point, normalized, back through the display transform to the sensor's picture.
    CGPoint n = CGPointMake(point.x / v.width, point.y / v.height);
    CGPoint img = CGPointApplyAffineTransform(n, CGAffineTransformInvert(displayTransform(deg, v)));
    simd_float3 dir = simd_normalize((simd_float3){(img.x * W - W / 2) / F, -(img.y * H - H / 2) / F, -1});
    simd_float3 world = simd_mul(sensorPose, simd_make_float4(dir, 0)).xyz;
    return raycast(makeQuery(sensorPose.columns[3].xyz, world, target, alignment));
}

#pragma mark - RealityKit's raycast

// The app's calls to RealityKit's ARView.raycast(from:allowing:alignment:), an extension method
// it calls through the address dyld bound in its __got, go to ar.swift's: every image's pointer
// to it is pointed at ar.swift's. The library may be loaded with the app (DYLD_INSERT_LIBRARIES)
// or by axx's SpringBoard library (dlopen), where dyld's interposing does not reach.
extern void axxarARViewRaycast(void) __asm__("_axxar_ARView_raycast");
static void *realityKitRaycast;

static void rebind(const struct mach_header *mh, intptr_t slide) {
    Dl_info self;
    if (dladdr((const void *)rebind, &self) && self.dli_fbase == mh) return;
    const struct mach_header_64 *h = (const struct mach_header_64 *)mh;
    const struct load_command *lc = (const struct load_command *)(h + 1);
    for (uint32_t i = 0; i < h->ncmds; i++, lc = (const struct load_command *)((const char *)lc + lc->cmdsize)) {
        if (lc->cmd != LC_SEGMENT_64) continue;
        const struct segment_command_64 *seg = (const struct segment_command_64 *)lc;
        if (strcmp(seg->segname, "__DATA") && strcmp(seg->segname, "__DATA_CONST") && strcmp(seg->segname, "__AUTH_CONST")) continue;
        const struct section_64 *sect = (const struct section_64 *)(seg + 1);
        for (uint32_t j = 0; j < seg->nsects; j++, sect++) {
            if (strcmp(sect->sectname, "__got") && strcmp(sect->sectname, "__auth_got") && strcmp(sect->sectname, "__la_symbol_ptr") &&
                strcmp(sect->sectname, "__nl_symbol_ptr"))
                continue;
            void **slots = (void **)(sect->addr + slide);
            for (uint64_t k = 0; k < sect->size / sizeof(void *); k++) {
                if (slots[k] != realityKitRaycast) continue;
                vm_size_t page = vm_page_size;
                vm_address_t start = (vm_address_t)&slots[k] & ~(page - 1);
                if (vm_protect(mach_task_self(), start, page, false, VM_PROT_READ | VM_PROT_WRITE | VM_PROT_COPY) != KERN_SUCCESS) {
                    LOG(@"cannot reach RealityKit's raycast in %s", dladdr(mh, &self) ? self.dli_fname : "an image");
                    continue;
                }
                slots[k] = (void *)axxarARViewRaycast;
                vm_protect(mach_task_self(), start, page, false, VM_PROT_READ);
                LOG(@"RealityKit's raycast goes to axx's in %s", dladdr(mh, &self) ? self.dli_fname : "an image");
            }
        }
    }
}

#pragma mark - wiring

static void replace(Class c, SEL sel, id block) {
    Method m = class_getInstanceMethod(c, sel);
    if (!m) {
        LOG(@"no %@ on %@", NSStringFromSelector(sel), c);
        return;
    }
    method_setImplementation(m, imp_implementationWithBlock(block));
}

__attribute__((constructor)) static void axxAR(void) {
    if (!loadScene()) return;
    renderQueue = dispatch_queue_create("axx.ar.render", DISPATCH_QUEUE_SERIAL);
    realityKitRaycast =
        dlsym(RTLD_DEFAULT, "$s10RealityKit6ARViewC7raycast4from8allowing9alignmentSaySo15ARRaycastResultCGSo7CGPointV_So0H6TargetVSo0hK9AlignmentVtF");
    if (realityKitRaycast) _dyld_register_func_for_add_image(rebind);
    else LOG(@"RealityKit has no ARView.raycast(from:allowing:alignment:) here: an app's raycasts find nothing");
    NSString *control = NSProcessInfo.processInfo.environment[@"AXX_AR_CONTROL"];
    if (control.length) serveControl(control);

    class_replaceMethod(object_getClass([ARWorldTrackingConfiguration class]), @selector(isSupported),
                        imp_implementationWithBlock(^BOOL(id c) { return YES; }), "c@:");

    replace([ARSession class], @selector(runWithConfiguration:options:), ^(ARSession *s, ARConfiguration *c, NSUInteger o) { startPump(s, c); });
    replace([ARSession class], @selector(runWithConfiguration:), ^(ARSession *s, ARConfiguration *c) { startPump(s, c); });
    replace([ARSession class], @selector(pause), ^(ARSession *s) {
        if (running == s) running = nil;
        LOG(@"session paused");
    });
    replace([ARSession class], @selector(raycast:), ^NSArray *(ARSession *s, ARRaycastQuery *q) { return raycast(q); });

    Class cam = [ARCamera class];
    replace(cam, @selector(viewMatrixForOrientation:), ^simd_float4x4(ARCamera *c, UIInterfaceOrientation o) { return viewMatrix(angleOf(o)); });
    replace(cam, @selector(viewMatrixForViewRotationAngle:), ^simd_float4x4(ARCamera *c, double a) { return viewMatrix(a); });
    replace(cam, @selector(projectionMatrixForOrientation:viewportSize:zNear:zFar:),
            ^simd_float4x4(ARCamera *c, UIInterfaceOrientation o, CGSize v, double n, double f) { return projection(angleOf(o), v, n, f); });
    replace(cam, @selector(projectionMatrixForViewRotationAngle:viewportSize:zNear:zFar:),
            ^simd_float4x4(ARCamera *c, double a, CGSize v, double n, double f) { return projection(a, v, n, f); });
    replace(cam, @selector(projectPoint:orientation:viewportSize:),
            ^CGPoint(ARCamera *c, simd_float3 p, UIInterfaceOrientation o, CGSize v) { return projectPoint(p, angleOf(o), v); });
    replace(cam, @selector(projectPoint:viewRotationAngle:viewportSize:),
            ^CGPoint(ARCamera *c, simd_float3 p, double a, CGSize v) { return projectPoint(p, a, v); });

    Class frame = [ARFrame class];
    replace(frame, @selector(displayTransformForOrientation:viewportSize:),
            ^CGAffineTransform(ARFrame *f, UIInterfaceOrientation o, CGSize v) { return displayTransform(angleOf(o), v); });
    replace(frame, @selector(displayTransformForViewRotationAngle:viewportSize:),
            ^CGAffineTransform(ARFrame *f, double a, CGSize v) { return displayTransform(a, v); });
    replace(frame, @selector(raycastQueryFromPoint:allowingTarget:alignment:),
            ^ARRaycastQuery *(ARFrame *f, CGPoint p, ARRaycastTarget t, ARRaycastTargetAlignment a) {
                simd_float3 dir = simd_normalize((simd_float3){(p.x * W - W / 2) / F, -(p.y * H - H / 2) / F, -1});
                simd_float3 world = simd_mul(sensorPose, simd_make_float4(dir, 0)).xyz;
                return makeQuery(sensorPose.columns[3].xyz, world, t, a);
            });
    LOG(@"ARKit emulated in %@", NSBundle.mainBundle.bundleIdentifier);
}
