// SPDX-License-Identifier: Apache-2.0

// The camera the iOS simulator has not got (ar.go loads this library, ar.m reads what the camera
// sees): a back wide camera, whose frames are a scene seen from the phone's pose, an image, or a
// video, the way the phone held upright sees them. An app's capture session that takes it as an
// input runs here instead of in AVFoundation, which has no camera to run in the simulator:
//   - a video data output gets the frames, in the pixel format it asks for and turned as its
//     connection says;
//   - a metadata output gets the codes in them (QR codes and barcodes, read with Vision);
//   - a photo output takes a photo of the frame, a JPEG (or HEIC) as an iPhone's;
//   - a preview layer shows them;
//   - the device answers the configuration an app makes (focus, exposure, zoom, torch) as a
//     phone's camera does, without changing what it sees.
// The frames come 30 a second, 1920 by 1080, landscape right, as an iPhone's sensor gives them.
#import <AVFoundation/AVFoundation.h>
#import <Accelerate/Accelerate.h>
#import <CoreML/CoreML.h>
#import <CoreMedia/CoreMedia.h>
#import <ImageIO/ImageIO.h>
#import <UIKit/UIKit.h>
#import <Vision/Vision.h>
#import <dlfcn.h>
#import <objc/message.h>
#import <objc/runtime.h>
#import <os/lock.h>
#import "ar.h"

#define LOG(fmt, ...) axxar_log([NSString stringWithFormat:@"camera: " fmt, ##__VA_ARGS__])

// The sensor: its size in pixels, and its focal length (about 62° across, as ARKit's camera).
static const size_t CW = 1920, CH = 1080;
static const float CF = 1598;

#pragma mark - the feed: what the camera sees

typedef NS_ENUM(NSInteger, FeedKind) { FeedNone, FeedScene, FeedImage, FeedVideo };
static FeedKind feedKind;
static NSString *feedPath;

static CVPixelBufferRef newBGRA(size_t w, size_t h) {
    CVPixelBufferRef buf = NULL;
    NSDictionary *attrs = @{(id)kCVPixelBufferIOSurfacePropertiesKey: @{}, (id)kCVPixelBufferMetalCompatibilityKey: @YES};
    CVPixelBufferCreate(NULL, w, h, kCVPixelFormatType_32BGRA, (__bridge CFDictionaryRef)attrs, &buf);
    return buf;
}

// Draws an upright picture (as the phone held upright sees it) into a sensor frame: filling the
// frame turned upright (aspect fill), the top of the picture to the sensor's left.
static CVPixelBufferRef sensorFrameOf(CGImageRef upright) {
    CVPixelBufferRef buf = newBGRA(CW, CH);
    CVPixelBufferLockBaseAddress(buf, 0);
    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    CGContextRef ctx = CGBitmapContextCreate(CVPixelBufferGetBaseAddress(buf), CW, CH, 8, CVPixelBufferGetBytesPerRow(buf), cs,
                                             kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
    CGContextSetFillColorWithColor(ctx, UIColor.blackColor.CGColor);
    CGContextFillRect(ctx, CGRectMake(0, 0, CW, CH));
    // The upright frame is CH wide and CW high; turned a quarter counterclockwise, it fills the
    // sensor's frame, its top at the sensor's left.
    CGContextTranslateCTM(ctx, CW, 0);
    CGContextRotateCTM(ctx, M_PI_2);
    double iw = CGImageGetWidth(upright), ih = CGImageGetHeight(upright);
    double s = fmax(CH / iw, CW / ih);
    CGRect r = CGRectMake((CH - iw * s) / 2, (CW - ih * s) / 2, iw * s, ih * s);
    CGContextSetInterpolationQuality(ctx, kCGInterpolationHigh);
    CGContextDrawImage(ctx, r, upright);
    CGContextRelease(ctx);
    CGColorSpaceRelease(cs);
    CVPixelBufferUnlockBaseAddress(buf, 0);
    return buf;
}

static CGImageRef imageOfBGRA(CVPixelBufferRef buf) {
    CVPixelBufferLockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    CGContextRef ctx = CGBitmapContextCreate(CVPixelBufferGetBaseAddress(buf), CVPixelBufferGetWidth(buf), CVPixelBufferGetHeight(buf), 8,
                                             CVPixelBufferGetBytesPerRow(buf), cs, kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
    CGImageRef img = CGBitmapContextCreateImage(ctx);
    CGContextRelease(ctx);
    CGColorSpaceRelease(cs);
    CVPixelBufferUnlockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
    return img;
}

// A video plays in a loop, at its own pace, from when the camera first runs.
static AVAssetReader *videoReader;
static AVAssetReaderOutput *videoOutput;
static CFTimeInterval videoStart, videoLoopStart;
static CMSampleBufferRef videoNext;

static BOOL openVideo(void) {
    AVURLAsset *asset = [AVURLAsset URLAssetWithURL:[NSURL fileURLWithPath:feedPath] options:nil];
    NSError *err = nil;
    videoReader = [AVAssetReader assetReaderWithAsset:asset error:&err];
    NSArray *tracks = [asset tracksWithMediaType:AVMediaTypeVideo];
    if (!videoReader || !tracks.count) {
        LOG(@"cannot play %@: %@", feedPath.lastPathComponent, err ?: @"it has no video");
        return NO;
    }
    // The video composition turns each frame the way the video is meant to be watched.
    AVAssetReaderVideoCompositionOutput *out = [AVAssetReaderVideoCompositionOutput
        assetReaderVideoCompositionOutputWithVideoTracks:tracks
                                           videoSettings:@{(id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA)}];
    out.videoComposition = [AVVideoComposition videoCompositionWithPropertiesOfAsset:asset];
    [videoReader addOutput:out];
    [videoReader startReading];
    videoOutput = out;
    return YES;
}

// The video's frame for now: the last one due.
static CVPixelBufferRef videoFrame(void) {
    if (!videoReader && !openVideo()) return NULL;
    CFTimeInterval now = CACurrentMediaTime();
    if (!videoStart) videoStart = videoLoopStart = now;
    CMSampleBufferRef due = NULL;
    for (;;) {
        if (!videoNext) {
            videoNext = [videoOutput copyNextSampleBuffer];
            if (!videoNext) {  // the end: again from the start
                if (due) break;
                videoReader = nil;
                if (!openVideo()) return NULL;
                videoLoopStart = now;
                videoNext = [videoOutput copyNextSampleBuffer];
                if (!videoNext) return NULL;
            }
        }
        double at = CMTimeGetSeconds(CMSampleBufferGetPresentationTimeStamp(videoNext));
        if (due && at > now - videoLoopStart) break;
        if (due) CFRelease(due);
        due = videoNext;
        videoNext = NULL;
        if (at > now - videoLoopStart) break;
    }
    CVPixelBufferRef pixels = CMSampleBufferGetImageBuffer(due);
    CGImageRef img = imageOfBGRA(pixels);
    CVPixelBufferRef frame = sensorFrameOf(img);
    CGImageRelease(img);
    CFRelease(due);
    return frame;
}

static os_unfair_lock frameLock = OS_UNFAIR_LOCK_INIT;
static CVPixelBufferRef latest;  // the camera's last frame (BGRA, the sensor's way up), under frameLock
static unsigned long latestGeneration;
static unsigned long renderedPose = (unsigned long)-1;

// The camera's frame for now, retained: new when what it sees changed (a video plays, the phone
// moved in the scene), else the last one.
static CVPixelBufferRef currentFrame(BOOL *changed) {
    CVPixelBufferRef next = NULL;
    switch (feedKind) {
    case FeedScene: {
        unsigned long g = axxar_pose_generation();
        if (g != renderedPose || !latest) {
            next = axxar_render_camera(CW, CH, CF);
            renderedPose = g;
        }
        break;
    }
    case FeedImage:
        if (!latest) {
            UIImage *img = [UIImage imageWithContentsOfFile:feedPath];
            if (!img) {
                LOG(@"cannot read the image %@", feedPath);
                break;
            }
            // Upright, as it shows.
            UIGraphicsImageRenderer *r = [[UIGraphicsImageRenderer alloc] initWithSize:img.size
                                                                                format:[UIGraphicsImageRendererFormat preferredFormat]];
            UIImage *upright = [r imageWithActions:^(UIGraphicsImageRendererContext *c) { [img drawAtPoint:CGPointZero]; }];
            next = sensorFrameOf(upright.CGImage);
        }
        break;
    case FeedVideo:
        next = videoFrame();
        break;
    default:
        break;
    }
    os_unfair_lock_lock(&frameLock);
    if (next) {
        CVPixelBufferRelease(latest);
        latest = next;
        latestGeneration++;
    }
    CVPixelBufferRef out = CVPixelBufferRetain(latest);
    os_unfair_lock_unlock(&frameLock);
    *changed = next != NULL;
    return out;
}

#pragma mark - storage for the camera's objects

static char kStore;

static NSMutableDictionary *store(id o) {
    NSMutableDictionary *d = objc_getAssociatedObject(o, &kStore);
    if (!d) {
        d = [NSMutableDictionary dictionary];
        objc_setAssociatedObject(o, &kStore, d, OBJC_ASSOCIATION_RETAIN);
    }
    return d;
}

// A class of the camera's objects, made of AVFoundation's: its objects are made without its
// init, so they go without its dealloc, which would clean up what they have not got.
static Class subclass(Class base, const char *name) {
    Class c = objc_getClass(name);
    if (c) return c;
    c = objc_allocateClassPair(base, name, 0);
    IMP rootDealloc = class_getMethodImplementation([NSObject class], NSSelectorFromString(@"dealloc"));
    class_addMethod(c, NSSelectorFromString(@"dealloc"), imp_implementationWithBlock(^(__unsafe_unretained id self) {
                        ((void (*)(__unsafe_unretained id, SEL))rootDealloc)(self, NSSelectorFromString(@"dealloc"));
                    }),
                    "v@:");
    objc_registerClassPair(c);
    return c;
}

static void method(Class c, NSString *sel, const char *types, id block) {
    class_replaceMethod(c, NSSelectorFromString(sel), imp_implementationWithBlock(block), types);
}

static NSString *setterOf(NSString *name) {
    return [NSString stringWithFormat:@"set%@%@:", [[name substringToIndex:1] uppercaseString], [name substringFromIndex:1]];
}

// A property kept in the object's store: its getter (name, or getter when it is a BOOL's isName)
// and its setter, with a default.
static void objectProperty(Class c, NSString *name, id def) {
    method(c, name, "@@:", ^id(id self) { return store(self)[name] ?: def; });
    method(c, setterOf(name), "v@:@", ^(id self, id v) {
        if (v) store(self)[name] = v;
        else [store(self) removeObjectForKey:name];
    });
}

static void boolProperty(Class c, NSString *name, NSString *getter, BOOL def) {
    method(c, getter ?: name, "B@:", ^BOOL(id self) {
        NSNumber *v = store(self)[name];
        return v ? v.boolValue : def;
    });
    method(c, setterOf(name), "v@:B", ^(id self, BOOL v) { store(self)[name] = @(v); });
}

static void intProperty(Class c, NSString *name, NSInteger def) {
    method(c, name, "q@:", ^NSInteger(id self) {
        NSNumber *v = store(self)[name];
        return v ? v.integerValue : def;
    });
    method(c, setterOf(name), "v@:q", ^(id self, NSInteger v) { store(self)[name] = @(v); });
}

static void floatProperty(Class c, NSString *name, float def) {
    method(c, name, "f@:", ^float(id self) {
        NSNumber *v = store(self)[name];
        return v ? v.floatValue : def;
    });
    method(c, setterOf(name), "v@:f", ^(id self, float v) { store(self)[name] = @(v); });
}

static void doubleProperty(Class c, NSString *name, double def) {
    method(c, name, "d@:", ^double(id self) {
        NSNumber *v = store(self)[name];
        return v ? v.doubleValue : def;
    });
    method(c, setterOf(name), "v@:d", ^(id self, double v) { store(self)[name] = @(v); });
}

static void pointProperty(Class c, NSString *name, CGPoint def) {
    method(c, name, "{CGPoint=dd}@:", ^CGPoint(id self) {
        NSValue *v = store(self)[name];
        return v ? v.CGPointValue : def;
    });
    method(c, setterOf(name), "v@:{CGPoint=dd}", ^(id self, CGPoint v) { store(self)[name] = [NSValue valueWithCGPoint:v]; });
}

static void timeProperty(Class c, NSString *name, CMTime def) {
    method(c, name, "{?=qiIq}@:", ^CMTime(id self) {
        NSValue *v = store(self)[name];
        return v ? v.CMTimeValue : def;
    });
    method(c, setterOf(name), "v@:{?=qiIq}", ^(id self, CMTime v) { store(self)[name] = [NSValue valueWithCMTime:v]; });
}

static void constant(Class c, NSString *name, id value) { method(c, name, "@@:", ^id(id self) { return value; }); }
static void constantBool(Class c, NSString *name, BOOL value) { method(c, name, "B@:", ^BOOL(id self) { return value; }); }
static void constantInt(Class c, NSString *name, NSInteger value) { method(c, name, "q@:", ^NSInteger(id self) { return value; }); }
static void constantFloat(Class c, NSString *name, float value) { method(c, name, "f@:", ^float(id self) { return value; }); }
static void constantDouble(Class c, NSString *name, double value) { method(c, name, "d@:", ^double(id self) { return value; }); }

#pragma mark - the device

static AVCaptureDevice *camera;
static AVCaptureDeviceFormat *cameraFormat;
static Class InputClass, PortClass, ConnectionClass;

static CMVideoFormatDescriptionRef formatDescription(void) {
    static CMVideoFormatDescriptionRef d;
    if (!d) CMVideoFormatDescriptionCreate(NULL, kCVPixelFormatType_420YpCbCr8BiPlanarFullRange, (int32_t)CW, (int32_t)CH, NULL, &d);
    return d;
}

static void makeCamera(void) {
    // Its frame rates: 1 to 30 a second.
    Class RangeClass = subclass([AVFrameRateRange class], "AXXFrameRateRange");
    constantDouble(RangeClass, @"minFrameRate", 1);
    constantDouble(RangeClass, @"maxFrameRate", 30);
    method(RangeClass, @"minFrameDuration", "{?=qiIq}@:", ^CMTime(id self) { return CMTimeMake(1, 30); });
    method(RangeClass, @"maxFrameDuration", "{?=qiIq}@:", ^CMTime(id self) { return CMTimeMake(1, 1); });
    id range = class_createInstance(RangeClass, 0);

    Class FormatClass = subclass([AVCaptureDeviceFormat class], "AXXCaptureDeviceFormat");
    constant(FormatClass, @"mediaType", AVMediaTypeVideo);
    method(FormatClass, @"formatDescription", "@@:", ^id(id self) { return (__bridge id)formatDescription(); });
    constant(FormatClass, @"videoSupportedFrameRateRanges", @[range]);
    constantFloat(FormatClass, @"videoFieldOfView", 62);
    constantBool(FormatClass, @"isVideoBinned", NO);
    method(FormatClass, @"isVideoStabilizationModeSupported:", "B@:q", ^BOOL(id self, NSInteger m) { return m == AVCaptureVideoStabilizationModeOff; });
    constantDouble(FormatClass, @"videoMaxZoomFactor", 10);
    constantDouble(FormatClass, @"videoZoomFactorUpscaleThreshold", 2);
    method(FormatClass, @"minExposureDuration", "{?=qiIq}@:", ^CMTime(id self) { return CMTimeMake(1, 8000); });
    method(FormatClass, @"maxExposureDuration", "{?=qiIq}@:", ^CMTime(id self) { return CMTimeMake(1, 3); });
    constantFloat(FormatClass, @"minISO", 32);
    constantFloat(FormatClass, @"maxISO", 3200);
    constantBool(FormatClass, @"isVideoHDRSupported", NO);
    method(FormatClass, @"highResolutionStillImageDimensions", "{?=ii}@:", ^CMVideoDimensions(id self) { return (CMVideoDimensions){(int32_t)CW, (int32_t)CH}; });
    CMVideoDimensions dims = {(int32_t)CW, (int32_t)CH};
    constant(FormatClass, @"supportedMaxPhotoDimensions", @[[NSValue valueWithBytes:&dims objCType:@encode(CMVideoDimensions)]]);
    constant(FormatClass, @"supportedColorSpaces", @[@(AVCaptureColorSpace_sRGB)]);
    constantBool(FormatClass, @"isHighestPhotoQualitySupported", NO);
    constantInt(FormatClass, @"autoFocusSystem", AVCaptureAutoFocusSystemPhaseDetection);
    constantBool(FormatClass, @"isMultiCamSupported", NO);
    constant(FormatClass, @"supportedDepthDataFormats", @[]);
    constant(FormatClass, @"secondaryNativeResolutionZoomFactors", @[]);
    constantBool(FormatClass, @"isCenterStageSupported", NO);
    constantBool(FormatClass, @"isPortraitEffectSupported", NO);
    constantBool(FormatClass, @"isGlobalToneMappingSupported", NO);
    constant(FormatClass, @"figCaptureSourceVideoFormat", nil);
    cameraFormat = class_createInstance(FormatClass, 0);

    Class DeviceClass = subclass([AVCaptureDevice class], "AXXCaptureDevice");
    constant(DeviceClass, @"uniqueID", @"axx.back-camera");
    constant(DeviceClass, @"modelID", @"axx camera");
    constant(DeviceClass, @"localizedName", @"Back Camera");
    constant(DeviceClass, @"manufacturer", @"axx");
    constant(DeviceClass, @"deviceType", AVCaptureDeviceTypeBuiltInWideAngleCamera);
    constantInt(DeviceClass, @"position", AVCaptureDevicePositionBack);
    method(DeviceClass, @"hasMediaType:", "B@:@", ^BOOL(id self, NSString *t) { return [t isEqualToString:AVMediaTypeVideo]; });
    method(DeviceClass, @"supportsAVCaptureSessionPreset:", "B@:@", ^BOOL(id self, NSString *p) { return YES; });
    constantBool(DeviceClass, @"isConnected", YES);
    constantBool(DeviceClass, @"isSuspended", NO);
    constantBool(DeviceClass, @"isInUseByAnotherApplication", NO);
    constantBool(DeviceClass, @"isVirtualDevice", NO);
    constant(DeviceClass, @"constituentDevices", @[]);
    constant(DeviceClass, @"linkedDevices", @[]);
    constant(DeviceClass, @"formats", @[cameraFormat]);
    objectProperty(DeviceClass, @"activeFormat", cameraFormat);
    timeProperty(DeviceClass, @"activeVideoMinFrameDuration", CMTimeMake(1, 30));
    timeProperty(DeviceClass, @"activeVideoMaxFrameDuration", CMTimeMake(1, 30));
    method(DeviceClass, @"lockForConfiguration:", "B@:^@", ^BOOL(id self, NSError **e) { return YES; });
    method(DeviceClass, @"unlockForConfiguration", "v@:", ^(id self) {});
    // Torch and flash.
    constantBool(DeviceClass, @"hasTorch", YES);
    constantBool(DeviceClass, @"hasFlash", YES);
    constantBool(DeviceClass, @"isTorchAvailable", YES);
    constantBool(DeviceClass, @"isFlashAvailable", YES);
    method(DeviceClass, @"isTorchModeSupported:", "B@:q", ^BOOL(id self, NSInteger m) { return YES; });
    intProperty(DeviceClass, @"torchMode", AVCaptureTorchModeOff);
    method(DeviceClass, @"isTorchActive", "B@:", ^BOOL(id self) { return [store(self)[@"torchMode"] integerValue] == AVCaptureTorchModeOn; });
    floatProperty(DeviceClass, @"torchLevel", 0);
    method(DeviceClass, @"setTorchModeOnWithLevel:error:", "B@:f^@", ^BOOL(id self, float level, NSError **e) {
        store(self)[@"torchMode"] = @(AVCaptureTorchModeOn);
        store(self)[@"torchLevel"] = @(level);
        return YES;
    });
    // Focus.
    method(DeviceClass, @"isFocusModeSupported:", "B@:q", ^BOOL(id self, NSInteger m) { return YES; });
    intProperty(DeviceClass, @"focusMode", AVCaptureFocusModeContinuousAutoFocus);
    constantBool(DeviceClass, @"isFocusPointOfInterestSupported", YES);
    pointProperty(DeviceClass, @"focusPointOfInterest", CGPointMake(0.5, 0.5));
    constantBool(DeviceClass, @"isAdjustingFocus", NO);
    constantBool(DeviceClass, @"isAutoFocusRangeRestrictionSupported", YES);
    intProperty(DeviceClass, @"autoFocusRangeRestriction", AVCaptureAutoFocusRangeRestrictionNone);
    constantBool(DeviceClass, @"isSmoothAutoFocusSupported", YES);
    boolProperty(DeviceClass, @"smoothAutoFocusEnabled", @"isSmoothAutoFocusEnabled", NO);
    boolProperty(DeviceClass, @"faceDrivenAutoFocusEnabled", @"isFaceDrivenAutoFocusEnabled", YES);
    boolProperty(DeviceClass, @"automaticallyAdjustsFaceDrivenAutoFocusEnabled", nil, YES);
    constantFloat(DeviceClass, @"lensPosition", 0.5);
    constantBool(DeviceClass, @"isLockingFocusWithCustomLensPositionSupported", YES);
    constantInt(DeviceClass, @"minimumFocusDistance", 100);
    method(DeviceClass, @"setFocusModeLockedWithLensPosition:completionHandler:", "v@:f@?", ^(id self, float p, void (^done)(CMTime)) {
        store(self)[@"focusMode"] = @(AVCaptureFocusModeLocked);
        if (done) dispatch_async(dispatch_get_main_queue(), ^{ done(CMClockGetTime(CMClockGetHostTimeClock())); });
    });
    // Exposure and white balance.
    method(DeviceClass, @"isExposureModeSupported:", "B@:q", ^BOOL(id self, NSInteger m) { return YES; });
    intProperty(DeviceClass, @"exposureMode", AVCaptureExposureModeContinuousAutoExposure);
    constantBool(DeviceClass, @"isExposurePointOfInterestSupported", YES);
    pointProperty(DeviceClass, @"exposurePointOfInterest", CGPointMake(0.5, 0.5));
    constantBool(DeviceClass, @"isAdjustingExposure", NO);
    method(DeviceClass, @"exposureDuration", "{?=qiIq}@:", ^CMTime(id self) { return CMTimeMake(1, 60); });
    timeProperty(DeviceClass, @"activeMaxExposureDuration", CMTimeMake(1, 3));
    constantFloat(DeviceClass, @"ISO", 100);
    constantFloat(DeviceClass, @"minExposureTargetBias", -8);
    constantFloat(DeviceClass, @"maxExposureTargetBias", 8);
    constantFloat(DeviceClass, @"exposureTargetOffset", 0);
    method(DeviceClass, @"exposureTargetBias", "f@:", ^float(id self) { return [store(self)[@"exposureTargetBias"] floatValue]; });
    method(DeviceClass, @"setExposureTargetBias:completionHandler:", "v@:f@?", ^(id self, float bias, void (^done)(CMTime)) {
        store(self)[@"exposureTargetBias"] = @(bias);
        if (done) dispatch_async(dispatch_get_main_queue(), ^{ done(CMClockGetTime(CMClockGetHostTimeClock())); });
    });
    method(DeviceClass, @"setExposureModeCustomWithDuration:ISO:completionHandler:", "v@:{?=qiIq}f@?", ^(id self, CMTime d, float iso, void (^done)(CMTime)) {
        store(self)[@"exposureMode"] = @(AVCaptureExposureModeCustom);
        if (done) dispatch_async(dispatch_get_main_queue(), ^{ done(CMClockGetTime(CMClockGetHostTimeClock())); });
    });
    method(DeviceClass, @"isWhiteBalanceModeSupported:", "B@:q", ^BOOL(id self, NSInteger m) { return YES; });
    intProperty(DeviceClass, @"whiteBalanceMode", AVCaptureWhiteBalanceModeContinuousAutoWhiteBalance);
    constantBool(DeviceClass, @"isAdjustingWhiteBalance", NO);
    constantBool(DeviceClass, @"isLowLightBoostSupported", NO);
    constantBool(DeviceClass, @"isLowLightBoostEnabled", NO);
    boolProperty(DeviceClass, @"subjectAreaChangeMonitoringEnabled", @"isSubjectAreaChangeMonitoringEnabled", NO);
    // Zoom.
    doubleProperty(DeviceClass, @"videoZoomFactor", 1);
    constantDouble(DeviceClass, @"minAvailableVideoZoomFactor", 1);
    constantDouble(DeviceClass, @"maxAvailableVideoZoomFactor", 10);
    constantDouble(DeviceClass, @"displayVideoZoomFactorMultiplier", 1);
    constant(DeviceClass, @"virtualDeviceSwitchOverVideoZoomFactors", @[]);
    constantBool(DeviceClass, @"isRampingVideoZoom", NO);
    method(DeviceClass, @"rampToVideoZoomFactor:withRate:", "v@:df", ^(id self, double f, float rate) { store(self)[@"videoZoomFactor"] = @(f); });
    method(DeviceClass, @"cancelVideoZoomRamp", "v@:", ^(id self) {});
    // HDR, color, distortion.
    boolProperty(DeviceClass, @"automaticallyAdjustsVideoHDREnabled", nil, YES);
    boolProperty(DeviceClass, @"videoHDREnabled", @"isVideoHDREnabled", NO);
    intProperty(DeviceClass, @"activeColorSpace", AVCaptureColorSpace_sRGB);
    constantBool(DeviceClass, @"isGeometricDistortionCorrectionSupported", NO);
    constantBool(DeviceClass, @"isCenterStageActive", NO);
    camera = class_createInstance(DeviceClass, 0);

    // The input that takes it, and its port.
    PortClass = subclass([AVCaptureInputPort class], "AXXCaptureInputPort");
    constant(PortClass, @"mediaType", AVMediaTypeVideo);
    method(PortClass, @"formatDescription", "@@:", ^id(id self) { return (__bridge id)formatDescription(); });
    boolProperty(PortClass, @"enabled", @"isEnabled", YES);
    objectProperty(PortClass, @"input", nil);
    constant(PortClass, @"sourceDeviceType", AVCaptureDeviceTypeBuiltInWideAngleCamera);
    constantInt(PortClass, @"sourceDevicePosition", AVCaptureDevicePositionBack);
    InputClass = subclass([AVCaptureDeviceInput class], "AXXCaptureDeviceInput");
    constant(InputClass, @"device", camera);
    method(InputClass, @"ports", "@@:", ^id(id self) {
        if (!store(self)[@"ports"]) {
            id port = class_createInstance(PortClass, 0);
            store(port)[@"input"] = self;
            store(self)[@"ports"] = @[port];
        }
        return store(self)[@"ports"];
    });
    boolProperty(InputClass, @"unifiedAutoExposureDefaultsEnabled", nil, YES);
    timeProperty(InputClass, @"videoMinFrameDurationOverride", kCMTimeInvalid);

    // A connection: of an output, or a preview layer, to the camera.
    ConnectionClass = subclass([AVCaptureConnection class], "AXXCaptureConnection");
    doubleProperty(ConnectionClass, @"videoRotationAngle", 0);
    method(ConnectionClass, @"isVideoRotationAngleSupported:", "B@:d", ^BOOL(id self, double a) { return fmod(a, 90) == 0; });
    constantBool(ConnectionClass, @"isVideoOrientationSupported", YES);
    method(ConnectionClass, @"videoOrientation", "q@:", ^NSInteger(id self) {
        int a = (int)[store(self)[@"videoRotationAngle"] doubleValue];
        switch (((a % 360) + 360) % 360) {
        case 90: return AVCaptureVideoOrientationPortrait;
        case 180: return AVCaptureVideoOrientationLandscapeLeft;
        case 270: return AVCaptureVideoOrientationPortraitUpsideDown;
        default: return AVCaptureVideoOrientationLandscapeRight;
        }
    });
    method(ConnectionClass, @"setVideoOrientation:", "v@:q", ^(id self, NSInteger o) {
        double a = o == AVCaptureVideoOrientationPortrait ? 90 : o == AVCaptureVideoOrientationLandscapeLeft ? 180 : o == AVCaptureVideoOrientationPortraitUpsideDown ? 270 : 0;
        store(self)[@"videoRotationAngle"] = @(a);
    });
    constantBool(ConnectionClass, @"isVideoMirroringSupported", YES);
    boolProperty(ConnectionClass, @"videoMirrored", @"isVideoMirrored", NO);
    boolProperty(ConnectionClass, @"automaticallyAdjustsVideoMirroring", nil, YES);
    boolProperty(ConnectionClass, @"enabled", @"isEnabled", YES);
    constantBool(ConnectionClass, @"isActive", YES);
    objectProperty(ConnectionClass, @"output", nil);
    objectProperty(ConnectionClass, @"videoPreviewLayer", nil);
    objectProperty(ConnectionClass, @"inputPorts", @[]);
    constant(ConnectionClass, @"audioChannels", @[]);
    constantBool(ConnectionClass, @"isVideoStabilizationSupported", NO);
    intProperty(ConnectionClass, @"preferredVideoStabilizationMode", AVCaptureVideoStabilizationModeOff);
    constantInt(ConnectionClass, @"activeVideoStabilizationMode", AVCaptureVideoStabilizationModeOff);
    constantDouble(ConnectionClass, @"videoMaxScaleAndCropFactor", 1);
    doubleProperty(ConnectionClass, @"videoScaleAndCropFactor", 1);
    constantBool(ConnectionClass, @"isCameraIntrinsicMatrixDeliverySupported", NO);
    boolProperty(ConnectionClass, @"cameraIntrinsicMatrixDeliveryEnabled", @"isCameraIntrinsicMatrixDeliveryEnabled", NO);
    constantBool(ConnectionClass, @"isVideoMinFrameDurationSupported", NO);
    constantBool(ConnectionClass, @"isVideoMaxFrameDurationSupported", NO);
}

static BOOL isCamera(id device) { return device && device == camera; }
static BOOL isCameraInput(id input) { return [input isKindOfClass:InputClass]; }

#pragma mark - sessions, their outputs, and preview layers

static char kInputs, kSession, kRunning, kConnection, kBusy;

// A weak reference, for the store.
@interface AXXWeak : NSObject
@property(weak) id object;
@end
@implementation AXXWeak
@end

static NSArray *cameraInputs(AVCaptureSession *s) { return objc_getAssociatedObject(s, &kInputs) ?: @[]; }
static BOOL playsCamera(AVCaptureSession *s) { return s && cameraInputs(s).count > 0; }
static AVCaptureSession *sessionOf(id outputOrLayer) { return ((AXXWeak *)objc_getAssociatedObject(outputOrLayer, &kSession)).object; }

static void setSession(id outputOrLayer, AVCaptureSession *s) {
    AXXWeak *w = [AXXWeak new];
    w.object = s;
    objc_setAssociatedObject(outputOrLayer, &kSession, w, OBJC_ASSOCIATION_RETAIN);
}

// The connection of an output, or of a preview layer (shown upright: turned a quarter).
static AVCaptureConnection *connectionOf(id outputOrLayer, BOOL layer) {
    AVCaptureConnection *c = objc_getAssociatedObject(outputOrLayer, &kConnection);
    if (!c) {
        c = class_createInstance(ConnectionClass, 0);
        if (layer) {
            store(c)[@"videoPreviewLayer"] = outputOrLayer;
            store(c)[@"videoRotationAngle"] = @90;
        } else {
            store(c)[@"output"] = outputOrLayer;
        }
        AVCaptureSession *s = sessionOf(outputOrLayer);
        AVCaptureDeviceInput *in = cameraInputs(s).firstObject;
        if (in) store(c)[@"inputPorts"] = in.ports;
        objc_setAssociatedObject(outputOrLayer, &kConnection, c, OBJC_ASSOCIATION_RETAIN);
    }
    return c;
}

// A connection's rotation, in degrees clockwise: the connection's own, kept in its store.
static double angleOf(AVCaptureConnection *c) { return [store(c)[@"videoRotationAngle"] doubleValue]; }

static NSHashTable<AVCaptureSession *> *runningSessions;
static NSHashTable<AVCaptureVideoPreviewLayer *> *previewLayers;
static os_unfair_lock tablesLock = OS_UNFAIR_LOCK_INIT;

static void startFeed(void);

// The metadata output's types a camera offers: Vision reads them.
static NSDictionary<NSString *, NSString *> *codeTypes(void) {
    static NSDictionary *types;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        NSMutableDictionary *t = [@{
            VNBarcodeSymbologyQR: AVMetadataObjectTypeQRCode,
            VNBarcodeSymbologyAztec: AVMetadataObjectTypeAztecCode,
            VNBarcodeSymbologyCode128: AVMetadataObjectTypeCode128Code,
            VNBarcodeSymbologyCode39: AVMetadataObjectTypeCode39Code,
            VNBarcodeSymbologyCode39Checksum: AVMetadataObjectTypeCode39Code,
            VNBarcodeSymbologyCode39FullASCII: AVMetadataObjectTypeCode39Code,
            VNBarcodeSymbologyCode39FullASCIIChecksum: AVMetadataObjectTypeCode39Mod43Code,
            VNBarcodeSymbologyCode93: AVMetadataObjectTypeCode93Code,
            VNBarcodeSymbologyCode93i: AVMetadataObjectTypeCode93Code,
            VNBarcodeSymbologyDataMatrix: AVMetadataObjectTypeDataMatrixCode,
            VNBarcodeSymbologyEAN13: AVMetadataObjectTypeEAN13Code,
            VNBarcodeSymbologyEAN8: AVMetadataObjectTypeEAN8Code,
            VNBarcodeSymbologyI2of5: AVMetadataObjectTypeInterleaved2of5Code,
            VNBarcodeSymbologyI2of5Checksum: AVMetadataObjectTypeInterleaved2of5Code,
            VNBarcodeSymbologyITF14: AVMetadataObjectTypeITF14Code,
            VNBarcodeSymbologyPDF417: AVMetadataObjectTypePDF417Code,
            VNBarcodeSymbologyUPCE: AVMetadataObjectTypeUPCECode,
        } mutableCopy];
        if (@available(iOS 15.4, *)) {
            t[VNBarcodeSymbologyCodabar] = AVMetadataObjectTypeCodabarCode;
            t[VNBarcodeSymbologyGS1DataBar] = AVMetadataObjectTypeGS1DataBarCode;
            t[VNBarcodeSymbologyMicroQR] = AVMetadataObjectTypeMicroQRCode;
            t[VNBarcodeSymbologyMicroPDF417] = AVMetadataObjectTypeMicroPDF417Code;
        }
        types = t;
    });
    return types;
}

static NSArray<NSString *> *availableCodeTypes(void) {
    return [[NSOrderedSet orderedSetWithArray:codeTypes().allValues].array sortedArrayUsingSelector:@selector(compare:)];
}

// An init's implementation, as a C function: it owns self, and returns what it makes owned.
#define CONSUMED __attribute__((ns_consumed))
typedef id (*InitIMP)(CONSUMED id, SEL, id, NSError **) NS_RETURNS_RETAINED;
static InitIMP originalInputInit;

// AVCaptureDeviceInput(device:) of the camera: an input of the camera's.
static id inputInit(CONSUMED id self, SEL _cmd, AVCaptureDevice *device, NSError **error) NS_RETURNS_RETAINED;
static id inputInit(CONSUMED id self, SEL _cmd, AVCaptureDevice *device, NSError **error) {
    if (isCamera(device)) {
        self = nil;
        return class_createInstance(InputClass, 0);
    }
    return originalInputInit(self, _cmd, device, error);
}

typedef id (*LayerInitIMP)(CONSUMED id, SEL, id) NS_RETURNS_RETAINED;
static LayerInitIMP originalLayerInit, originalLayerInitNoConnection;

static void dressLayer(AVCaptureVideoPreviewLayer *layer, AVCaptureSession *s) {
    setSession(layer, s);
    os_unfair_lock_lock(&tablesLock);
    [previewLayers addObject:layer];
    os_unfair_lock_unlock(&tablesLock);
}

static id layerInit(CONSUMED id self, SEL _cmd, AVCaptureSession *s) NS_RETURNS_RETAINED;
static id layerInit(CONSUMED id self, SEL _cmd, AVCaptureSession *s) {
    self = originalLayerInit(self, _cmd, s);
    if (self) dressLayer(self, s);
    return self;
}

static id layerInitNoConnection(CONSUMED id self, SEL _cmd, AVCaptureSession *s) NS_RETURNS_RETAINED;
static id layerInitNoConnection(CONSUMED id self, SEL _cmd, AVCaptureSession *s) {
    self = originalLayerInitNoConnection(self, _cmd, s);
    if (self) dressLayer(self, s);
    return self;
}

// cpuOnly has a Vision request run on the processor: a simulator has no other device for Vision's
// models, and on some runtimes (iOS 26.5) a request left to choose fails ("Could not create
// inference context"), the app's own text recognition among them.
static void cpuOnly(VNRequest *r) {
    if (@available(iOS 17.0, *)) {
        NSDictionary<VNComputeStage, NSArray<id<MLComputeDeviceProtocol>> *> *stages = [r supportedComputeStageDevicesAndReturnError:nil];
        for (VNComputeStage stage in stages)
            for (id<MLComputeDeviceProtocol> d in stages[stage])
                if ([(id)d isKindOfClass:[MLCPUComputeDevice class]]) {
                    [r setComputeDevice:d forComputeStage:stage];
                    break;
                }
    } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        r.usesCPUOnly = YES;
#pragma clang diagnostic pop
    }
}

typedef id (*RequestInitIMP)(CONSUMED id, SEL, id) NS_RETURNS_RETAINED;
static RequestInitIMP originalRequestInit;

static id requestInit(CONSUMED id self, SEL _cmd, id handler) NS_RETURNS_RETAINED;
static id requestInit(CONSUMED id self, SEL _cmd, id handler) {
    self = originalRequestInit(self, _cmd, handler);
    if (self) cpuOnly(self);
    return self;
}

typedef id (*CoordinatorInitIMP)(CONSUMED id, SEL, id, id) NS_RETURNS_RETAINED;
static CoordinatorInitIMP originalCoordinatorInit;
static Class CoordinatorClass;

// The interface's rotation from the sensor's, in degrees: what a rotation coordinator says.
static double interfaceAngle(void) {
    __block UIInterfaceOrientation o = UIInterfaceOrientationPortrait;
    void (^look)(void) = ^{
        for (UIScene *s in UIApplication.sharedApplication.connectedScenes)
            if ([s isKindOfClass:UIWindowScene.class]) o = ((UIWindowScene *)s).interfaceOrientation;
    };
    if (NSThread.isMainThread) look();
    else dispatch_sync(dispatch_get_main_queue(), look);
    switch (o) {
    case UIInterfaceOrientationLandscapeLeft: return 180;
    case UIInterfaceOrientationPortraitUpsideDown: return 270;
    case UIInterfaceOrientationLandscapeRight: return 0;
    default: return 90;
    }
}

static id coordinatorInit(CONSUMED id self, SEL _cmd, AVCaptureDevice *device, CALayer *layer) NS_RETURNS_RETAINED;
static id coordinatorInit(CONSUMED id self, SEL _cmd, AVCaptureDevice *device, CALayer *layer) {
    if (isCamera(device)) {
        self = nil;
        id c = class_createInstance(CoordinatorClass, 0);
        store(c)[@"device"] = device;
        if (layer) store(c)[@"previewLayer"] = layer;
        return c;
    }
    return originalCoordinatorInit(self, _cmd, device, layer);
}

// Whether a call comes from AVFoundation itself: the camera is the app's, and AVFoundation, which
// has no camera here, is shown what it has. Its own lookups (a session building connections for
// an output, a photo output's settings) would otherwise reach into the camera's objects for what
// they have not got.
static BOOL fromAVFoundation(const void *caller) {
    static const char *images[4];
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        Class in[] = {[AVCaptureSession class], [AVCaptureOutput class], [AVCaptureDevice class], [AVURLAsset class]};
        for (int i = 0; i < 4; i++) images[i] = class_getImageName(in[i]);
    });
    Dl_info info;
    if (!caller || !dladdr(caller, &info) || !info.dli_fname) return NO;
    for (int i = 0; i < 4; i++)
        if (images[i] && !strcmp(info.dli_fname, images[i])) return YES;
    return NO;
}
#define FROM_AV fromAVFoundation(__builtin_return_address(0))

static void swizzle(Class c, NSString *name, id (^make)(IMP original)) {
    SEL sel = NSSelectorFromString(name);
    Method m = class_getInstanceMethod(c, sel);
    if (!m) {
        LOG(@"no %@ on %@", name, c);
        return;
    }
    IMP original = method_getImplementation(m);
    method_setImplementation(m, imp_implementationWithBlock(make(original)));
}

static void swizzleClass(Class c, NSString *name, id (^make)(IMP original)) { swizzle(object_getClass(c), name, make); }

#pragma mark - delivering frames

// A frame turned by angle degrees clockwise (a multiple of 90), and mirrored, BGRA.
static CVPixelBufferRef turned(CVPixelBufferRef src, int angle, BOOL mirrored) {
    angle = ((angle % 360) + 360) % 360;
    if (!angle && !mirrored) return CVPixelBufferRetain(src);
    size_t w = CVPixelBufferGetWidth(src), h = CVPixelBufferGetHeight(src);
    size_t ow = angle % 180 ? h : w, oh = angle % 180 ? w : h;
    CVPixelBufferRef out = newBGRA(ow, oh);
    CVPixelBufferLockBaseAddress(src, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferLockBaseAddress(out, 0);
    vImage_Buffer s = {CVPixelBufferGetBaseAddress(src), h, w, CVPixelBufferGetBytesPerRow(src)};
    vImage_Buffer d = {CVPixelBufferGetBaseAddress(out), oh, ow, CVPixelBufferGetBytesPerRow(out)};
    // vImage turns counterclockwise.
    uint8_t quarters = (uint8_t)((360 - angle) / 90 % 4);
    Pixel_8888 black = {0, 0, 0, 255};
    vImageRotate90_ARGB8888(&s, &d, quarters, black, kvImageNoFlags);
    if (mirrored) vImageHorizontalReflect_ARGB8888(&d, &d, kvImageNoFlags);
    CVPixelBufferUnlockBaseAddress(out, 0);
    CVPixelBufferUnlockBaseAddress(src, kCVPixelBufferLock_ReadOnly);
    return out;
}

// A BGRA frame in the biplanar YCbCr 4:2:0 a video output asks for, full or video range.
static CVPixelBufferRef toBiplanar(CVPixelBufferRef bgra, OSType format) {
    BOOL full = format == kCVPixelFormatType_420YpCbCr8BiPlanarFullRange;
    static vImage_ARGBToYpCbCr infos[2];
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        vImage_YpCbCrPixelRange video = {16, 128, 235, 240, 235, 16, 240, 16};
        vImage_YpCbCrPixelRange fullr = {0, 128, 255, 255, 255, 1, 255, 0};
        vImageConvert_ARGBToYpCbCr_GenerateConversion(kvImage_ARGBToYpCbCrMatrix_ITU_R_601_4, &video, &infos[0], kvImageARGB8888,
                                                      kvImage420Yp8_CbCr8, kvImageNoFlags);
        vImageConvert_ARGBToYpCbCr_GenerateConversion(kvImage_ARGBToYpCbCrMatrix_ITU_R_601_4, &fullr, &infos[1], kvImageARGB8888,
                                                      kvImage420Yp8_CbCr8, kvImageNoFlags);
    });
    size_t w = CVPixelBufferGetWidth(bgra), h = CVPixelBufferGetHeight(bgra);
    CVPixelBufferRef out = NULL;
    NSDictionary *attrs = @{(id)kCVPixelBufferIOSurfacePropertiesKey: @{}, (id)kCVPixelBufferMetalCompatibilityKey: @YES};
    CVPixelBufferCreate(NULL, w, h, format, (__bridge CFDictionaryRef)attrs, &out);
    CVPixelBufferLockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferLockBaseAddress(out, 0);
    vImage_Buffer s = {CVPixelBufferGetBaseAddress(bgra), h, w, CVPixelBufferGetBytesPerRow(bgra)};
    vImage_Buffer y = {CVPixelBufferGetBaseAddressOfPlane(out, 0), h, w, CVPixelBufferGetBytesPerRowOfPlane(out, 0)};
    vImage_Buffer c = {CVPixelBufferGetBaseAddressOfPlane(out, 1), h / 2, w / 2, CVPixelBufferGetBytesPerRowOfPlane(out, 1)};
    const uint8_t bgraToARGB[4] = {3, 2, 1, 0};
    vImageConvert_ARGB8888To420Yp8_CbCr8(&s, &y, &c, &infos[full ? 1 : 0], bgraToARGB, kvImageNoFlags);
    CVPixelBufferUnlockBaseAddress(out, 0);
    CVPixelBufferUnlockBaseAddress(bgra, kCVPixelBufferLock_ReadOnly);
    return out;
}

static CMSampleBufferRef sampleOf(CVPixelBufferRef pixels) {
    CMVideoFormatDescriptionRef desc = NULL;
    CMVideoFormatDescriptionCreateForImageBuffer(NULL, pixels, &desc);
    CMSampleTimingInfo timing = {CMTimeMake(1, 30), CMClockGetTime(CMClockGetHostTimeClock()), kCMTimeInvalid};
    CMSampleBufferRef sample = NULL;
    CMSampleBufferCreateReadyWithImageBuffer(NULL, pixels, desc, &timing, &sample);
    if (desc) CFRelease(desc);
    return sample;
}

static void deliverVideo(AVCaptureVideoDataOutput *out, CVPixelBufferRef frame) {
    id<AVCaptureVideoDataOutputSampleBufferDelegate> delegate = out.sampleBufferDelegate;
    dispatch_queue_t queue = out.sampleBufferCallbackQueue;
    AVCaptureConnection *conn = connectionOf(out, NO);
    if (!delegate || !queue || !conn.isEnabled) return;
    @synchronized(out) {
        if ([objc_getAssociatedObject(out, &kBusy) boolValue] && out.alwaysDiscardsLateVideoFrames) return;
        objc_setAssociatedObject(out, &kBusy, @YES, OBJC_ASSOCIATION_RETAIN);
    }
    NSNumber *asked = out.videoSettings[(id)kCVPixelBufferPixelFormatTypeKey];
    OSType format = asked ? asked.unsignedIntValue : kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
    CVPixelBufferRef t = turned(frame, (int)angleOf(conn), conn.isVideoMirrored);
    CVPixelBufferRef pixels = format == kCVPixelFormatType_32BGRA ? CVPixelBufferRetain(t) : toBiplanar(t, format);
    CVPixelBufferRelease(t);
    CMSampleBufferRef sample = sampleOf(pixels);
    CVPixelBufferRelease(pixels);
    dispatch_async(queue, ^{
        if ([delegate respondsToSelector:@selector(captureOutput:didOutputSampleBuffer:fromConnection:)])
            [delegate captureOutput:out didOutputSampleBuffer:sample fromConnection:conn];
        CFRelease(sample);
        @synchronized(out) { objc_setAssociatedObject(out, &kBusy, @NO, OBJC_ASSOCIATION_RETAIN); }
    });
}

// A code the camera reads, as a metadata output hands it over: where it is in the frame,
// normalized, the sensor's way up, its origin top left.
static Class CodeClass;

static AVMetadataMachineReadableCodeObject *codeObject(VNBarcodeObservation *o, NSString *type) {
    AVMetadataMachineReadableCodeObject *c = class_createInstance(CodeClass, 0);
    CGRect b = o.boundingBox;
    store(c)[@"type"] = type;
    store(c)[@"stringValue"] = o.payloadStringValue ?: @"";
    store(c)[@"bounds"] = [NSValue valueWithCGRect:CGRectMake(b.origin.x, 1 - b.origin.y - b.size.height, b.size.width, b.size.height)];
    NSMutableArray *corners = [NSMutableArray array];
    for (NSValue *p in @[@(o.topLeft), @(o.topRight), @(o.bottomRight), @(o.bottomLeft)]) {
        CGPoint q = p.CGPointValue;
        CFDictionaryRef d = CGPointCreateDictionaryRepresentation(CGPointMake(q.x, 1 - q.y));
        [corners addObject:(__bridge_transfer NSDictionary *)d];
    }
    store(c)[@"corners"] = corners;
    store(c)[@"time"] = [NSValue valueWithCMTime:CMClockGetTime(CMClockGetHostTimeClock())];
    return c;
}

static dispatch_queue_t codesQueue;
static BOOL readingCodes;

static void deliverCodes(NSArray<AVCaptureMetadataOutput *> *outputs, CVPixelBufferRef frame) {
    if (readingCodes || !outputs.count) return;
    readingCodes = YES;
    CVPixelBufferRetain(frame);
    dispatch_async(codesQueue, ^{
        // Vision's detector of revision 2, on the processor: in a simulator its newer ones (3 and
        // 4, a model) find no code at all, on iOS 26.5 and 27 alike.
        VNDetectBarcodesRequest *req = [VNDetectBarcodesRequest new];
        req.revision = VNDetectBarcodesRequestRevision2;
        cpuOnly(req);
        [[[VNImageRequestHandler alloc] initWithCVPixelBuffer:frame options:@{}] performRequests:@[req] error:nil];
        CVPixelBufferRelease(frame);
        NSArray<VNBarcodeObservation *> *found = req.results ?: @[];
        for (AVCaptureMetadataOutput *out in outputs) {
            id<AVCaptureMetadataOutputObjectsDelegate> delegate = out.metadataObjectsDelegate;
            dispatch_queue_t queue = out.metadataObjectsCallbackQueue;
            NSArray *wanted = out.metadataObjectTypes;
            CGRect interest = out.rectOfInterest;
            NSMutableArray *codes = [NSMutableArray array];
            for (VNBarcodeObservation *o in found) {
                NSString *type = codeTypes()[o.symbology];
                if (!type || ![wanted containsObject:type]) continue;
                AVMetadataMachineReadableCodeObject *c = codeObject(o, type);
                if (!CGRectIntersectsRect(c.bounds, interest)) continue;
                [codes addObject:c];
            }
            if (!codes.count || !delegate || !queue) continue;
            AVCaptureConnection *conn = connectionOf(out, NO);
            dispatch_async(queue, ^{
                if ([delegate respondsToSelector:@selector(captureOutput:didOutputMetadataObjects:fromConnection:)])
                    [delegate captureOutput:out didOutputMetadataObjects:codes fromConnection:conn];
            });
        }
        dispatch_async(dispatch_get_main_queue(), ^{ readingCodes = NO; });
    });
}

#pragma mark - preview layers

static NSString *const pictureName = @"axx camera";

static CALayer *pictureLayer(AVCaptureVideoPreviewLayer *layer) {
    for (CALayer *l in layer.sublayers)
        if ([l.name isEqualToString:pictureName]) return l;
    CALayer *p = [CALayer layer];
    p.name = pictureName;
    [layer insertSublayer:p atIndex:0];
    layer.masksToBounds = YES;
    return p;
}

static void layoutPicture(AVCaptureVideoPreviewLayer *layer) {
    if (!playsCamera(sessionOf(layer))) return;
    CALayer *p = pictureLayer(layer);
    double angle = angleOf(connectionOf(layer, YES));
    CGRect b = layer.bounds;
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    p.transform = CATransform3DIdentity;
    p.bounds = fmod(angle, 180) ? CGRectMake(0, 0, b.size.height, b.size.width) : CGRectMake(0, 0, b.size.width, b.size.height);
    p.position = CGPointMake(CGRectGetMidX(b), CGRectGetMidY(b));
    p.transform = CATransform3DMakeRotation(angle * M_PI / 180, 0, 0, 1);
    NSString *g = layer.videoGravity;
    p.contentsGravity = [g isEqualToString:AVLayerVideoGravityResize] ? kCAGravityResize
                        : [g isEqualToString:AVLayerVideoGravityResizeAspect] ? kCAGravityResizeAspect : kCAGravityResizeAspectFill;
    [CATransaction commit];
}

// Where a point of the sensor's frame (normalized, origin top left) shows in a preview layer, and
// back.
static CGAffineTransform sensorToLayer(AVCaptureVideoPreviewLayer *layer) {
    double angle = fmod(fmod(angleOf(connectionOf(layer, YES)), 360) + 360, 360);
    CGSize b = layer.bounds.size;
    CGSize shown = fmod(angle, 180) ? CGSizeMake(CH, CW) : CGSizeMake(CW, CH);
    NSString *g = layer.videoGravity;
    double sx = b.width / shown.width, sy = b.height / shown.height;
    if ([g isEqualToString:AVLayerVideoGravityResizeAspect]) sx = sy = fmin(sx, sy);
    else if (![g isEqualToString:AVLayerVideoGravityResize]) sx = sy = fmax(sx, sy);
    // Normalized sensor point → turned, normalized → the layer's points.
    CGAffineTransform t = CGAffineTransformIdentity;
    t = CGAffineTransformConcat(t, CGAffineTransformMakeTranslation(-0.5, -0.5));
    t = CGAffineTransformConcat(t, CGAffineTransformMakeRotation(angle * M_PI / 180));
    t = CGAffineTransformConcat(t, CGAffineTransformMakeScale(shown.width * sx, shown.height * sy));
    t = CGAffineTransformConcat(t, CGAffineTransformMakeTranslation(b.width / 2, b.height / 2));
    // A quarter turn swaps the axes' lengths: scale before turning.
    if (fmod(angle, 180)) {
        t = CGAffineTransformMakeTranslation(-0.5, -0.5);
        t = CGAffineTransformConcat(t, CGAffineTransformMakeScale(CW * sx, CH * sy));
        t = CGAffineTransformConcat(t, CGAffineTransformMakeRotation(angle * M_PI / 180));
        t = CGAffineTransformConcat(t, CGAffineTransformMakeTranslation(b.width / 2, b.height / 2));
    } else {
        t = CGAffineTransformMakeTranslation(-0.5, -0.5);
        t = CGAffineTransformConcat(t, CGAffineTransformMakeScale(CW * sx, CH * sy));
        t = CGAffineTransformConcat(t, CGAffineTransformMakeRotation(angle * M_PI / 180));
        t = CGAffineTransformConcat(t, CGAffineTransformMakeTranslation(b.width / 2, b.height / 2));
    }
    return t;
}

static void showFrame(CVPixelBufferRef frame) {
    os_unfair_lock_lock(&tablesLock);
    NSArray *layers = previewLayers.allObjects;
    os_unfair_lock_unlock(&tablesLock);
    if (!layers.count) return;
    CVPixelBufferRetain(frame);
    dispatch_async(dispatch_get_main_queue(), ^{
        id contents = (__bridge id)CVPixelBufferGetIOSurface(frame);
        for (AVCaptureVideoPreviewLayer *layer in layers) {
            AVCaptureSession *s = sessionOf(layer);
            if (!playsCamera(s) || ![objc_getAssociatedObject(s, &kRunning) boolValue]) continue;
            CALayer *p = pictureLayer(layer);
            layoutPicture(layer);
            [CATransaction begin];
            [CATransaction setDisableActions:YES];
            p.contents = contents;
            [CATransaction commit];
        }
        CVPixelBufferRelease(frame);
    });
}

#pragma mark - photos

static Class PhotoClass, ResolvedClass;
static dispatch_queue_t photoQueue;

static int exifOrientation(double angle) {
    switch (((int)angle % 360 + 360) % 360) {
    case 90: return kCGImagePropertyOrientationRight;
    case 180: return kCGImagePropertyOrientationDown;
    case 270: return kCGImagePropertyOrientationLeft;
    default: return kCGImagePropertyOrientationUp;
    }
}

static void takePhoto(AVCapturePhotoOutput *out, AVCapturePhotoSettings *settings, id<AVCapturePhotoCaptureDelegate> delegate) {
    os_unfair_lock_lock(&frameLock);
    CVPixelBufferRef frame = CVPixelBufferRetain(latest);
    os_unfair_lock_unlock(&frameLock);
    AVCaptureConnection *conn = connectionOf(out, NO);
    double angle = angleOf(conn);
    dispatch_async(photoQueue, ^{
        id resolved = class_createInstance(ResolvedClass, 0);
        store(resolved)[@"uniqueID"] = @(settings.uniqueID);
        NSError *error = nil;
        NSData *file = nil;
        CGImageRef img = NULL;
        if (!frame) {
            error = [NSError errorWithDomain:AVFoundationErrorDomain code:AVErrorNoDataCaptured
                                    userInfo:@{NSLocalizedDescriptionKey: @"the camera has no frame yet"}];
        } else {
            img = imageOfBGRA(frame);
            BOOL heic = [settings.format[AVVideoCodecKey] isEqual:AVVideoCodecTypeHEVC];
            NSMutableData *data = [NSMutableData data];
            CGImageDestinationRef dest = CGImageDestinationCreateWithData((__bridge CFMutableDataRef)data, heic ? CFSTR("public.heic") : CFSTR("public.jpeg"), 1, NULL);
            if (!dest) dest = CGImageDestinationCreateWithData((__bridge CFMutableDataRef)data, CFSTR("public.jpeg"), 1, NULL);
            NSDictionary *props = @{(id)kCGImagePropertyOrientation: @(exifOrientation(angle)),
                                    (id)kCGImageDestinationLossyCompressionQuality: @0.9,
                                    (id)kCGImagePropertyTIFFDictionary: @{(id)kCGImagePropertyTIFFMake: @"axx", (id)kCGImagePropertyTIFFModel: @"axx camera"}};
            CGImageDestinationAddImage(dest, img, (__bridge CFDictionaryRef)props);
            CGImageDestinationFinalize(dest);
            CFRelease(dest);
            file = data;
        }
        id photo = class_createInstance(PhotoClass, 0);
        if (file) store(photo)[@"file"] = file;
        if (img) store(photo)[@"image"] = (__bridge_transfer id)img;
        store(photo)[@"resolvedSettings"] = resolved;
        store(photo)[@"metadata"] = @{(id)kCGImagePropertyOrientation: @(exifOrientation(angle))};
        LOG(@"photo %lld: %lu bytes", settings.uniqueID, (unsigned long)file.length);
        CVPixelBufferRelease(frame);
        if ([delegate respondsToSelector:@selector(captureOutput:willBeginCaptureForResolvedSettings:)])
            [delegate captureOutput:out willBeginCaptureForResolvedSettings:resolved];
        if ([delegate respondsToSelector:@selector(captureOutput:willCapturePhotoForResolvedSettings:)])
            [delegate captureOutput:out willCapturePhotoForResolvedSettings:resolved];
        if ([delegate respondsToSelector:@selector(captureOutput:didCapturePhotoForResolvedSettings:)])
            [delegate captureOutput:out didCapturePhotoForResolvedSettings:resolved];
        if ([delegate respondsToSelector:@selector(captureOutput:didFinishProcessingPhoto:error:)])
            [delegate captureOutput:out didFinishProcessingPhoto:photo error:error];
        if ([delegate respondsToSelector:@selector(captureOutput:didFinishCaptureForResolvedSettings:error:)])
            [delegate captureOutput:out didFinishCaptureForResolvedSettings:resolved error:error];
    });
}

static void makePhotoClasses(void) {
    ResolvedClass = subclass([AVCaptureResolvedPhotoSettings class], "AXXResolvedPhotoSettings");
    method(ResolvedClass, @"uniqueID", "q@:", ^int64_t(id self) { return [store(self)[@"uniqueID"] longLongValue]; });
    method(ResolvedClass, @"photoDimensions", "{?=ii}@:", ^CMVideoDimensions(id self) { return (CMVideoDimensions){(int32_t)CW, (int32_t)CH}; });
    method(ResolvedClass, @"rawPhotoDimensions", "{?=ii}@:", ^CMVideoDimensions(id self) { return (CMVideoDimensions){0, 0}; });
    method(ResolvedClass, @"previewDimensions", "{?=ii}@:", ^CMVideoDimensions(id self) { return (CMVideoDimensions){0, 0}; });
    method(ResolvedClass, @"embeddedThumbnailDimensions", "{?=ii}@:", ^CMVideoDimensions(id self) { return (CMVideoDimensions){0, 0}; });
    method(ResolvedClass, @"livePhotoMovieDimensions", "{?=ii}@:", ^CMVideoDimensions(id self) { return (CMVideoDimensions){0, 0}; });
    constantBool(ResolvedClass, @"isFlashEnabled", NO);
    constantBool(ResolvedClass, @"isRedEyeReductionEnabled", NO);
    constantBool(ResolvedClass, @"isStillImageStabilizationEnabled", NO);
    constantBool(ResolvedClass, @"isVirtualDeviceFusionEnabled", NO);
    constantBool(ResolvedClass, @"isDualCameraFusionEnabled", NO);
    constantInt(ResolvedClass, @"expectedPhotoCount", 1);

    PhotoClass = subclass([AVCapturePhoto class], "AXXCapturePhoto");
    method(PhotoClass, @"timestamp", "{?=qiIq}@:", ^CMTime(id self) { return CMClockGetTime(CMClockGetHostTimeClock()); });
    constantBool(PhotoClass, @"isRawPhoto", NO);
    method(PhotoClass, @"pixelBuffer", "^v@:", ^CVPixelBufferRef(id self) { return NULL; });
    method(PhotoClass, @"previewPixelBuffer", "^v@:", ^CVPixelBufferRef(id self) { return NULL; });
    method(PhotoClass, @"metadata", "@@:", ^id(id self) { return store(self)[@"metadata"]; });
    method(PhotoClass, @"resolvedSettings", "@@:", ^id(id self) { return store(self)[@"resolvedSettings"]; });
    constantInt(PhotoClass, @"photoCount", 1);
    constantInt(PhotoClass, @"sequenceCount", 1);
    method(PhotoClass, @"fileDataRepresentation", "@@:", ^id(id self) { return store(self)[@"file"]; });
    method(PhotoClass, @"fileDataRepresentationWithCustomizer:", "@@:@", ^id(id self, id c) { return store(self)[@"file"]; });
    method(PhotoClass, @"cgImageRepresentation", "^v@:", ^CGImageRef(id self) { return (__bridge CGImageRef)store(self)[@"image"]; });
    method(PhotoClass, @"previewCGImageRepresentation", "^v@:", ^CGImageRef(id self) { return (__bridge CGImageRef)store(self)[@"image"]; });
    constant(PhotoClass, @"depthData", nil);
    constant(PhotoClass, @"cameraCalibrationData", nil);
    constant(PhotoClass, @"sourceDeviceType", AVCaptureDeviceTypeBuiltInWideAngleCamera);
}

#pragma mark - the feed's clock

static dispatch_source_t feedTimer;
static dispatch_queue_t feedQueue;
static unsigned long ticks;

static void tick(void) {
    os_unfair_lock_lock(&tablesLock);
    NSArray<AVCaptureSession *> *sessions = runningSessions.allObjects;
    os_unfair_lock_unlock(&tablesLock);
    if (!sessions.count) return;
    BOOL changed = NO;
    CVPixelBufferRef frame = currentFrame(&changed);
    if (!frame) return;
    NSMutableArray<AVCaptureMetadataOutput *> *codeOutputs = [NSMutableArray array];
    for (AVCaptureSession *s in sessions) {
        for (AVCaptureOutput *out in s.outputs) {
            if ([out isKindOfClass:AVCaptureVideoDataOutput.class]) deliverVideo((AVCaptureVideoDataOutput *)out, frame);
            else if ([out isKindOfClass:AVCaptureMetadataOutput.class] && ((AVCaptureMetadataOutput *)out).metadataObjectTypes.count)
                [codeOutputs addObject:(AVCaptureMetadataOutput *)out];
        }
    }
    // Codes are read ten times a second.
    if (ticks % 3 == 0) {
        CVPixelBufferRetain(frame);
        NSArray *outs = [codeOutputs copy];
        dispatch_async(dispatch_get_main_queue(), ^{
            deliverCodes(outs, frame);
            CVPixelBufferRelease(frame);
        });
    }
    if (changed || ticks % 30 == 0) showFrame(frame);
    ticks++;
    CVPixelBufferRelease(frame);
}

static void startFeed(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        feedTimer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, feedQueue);
        dispatch_source_set_timer(feedTimer, DISPATCH_TIME_NOW, NSEC_PER_SEC / 30, NSEC_PER_SEC / 300);
        dispatch_source_set_event_handler(feedTimer, ^{ @autoreleasepool { tick(); } });
        dispatch_resume(feedTimer);
    });
}

#pragma mark - wiring

void axxcam_start(NSDictionary *d) {
    if ([d[@"image"] isKindOfClass:NSString.class]) {
        feedKind = FeedImage;
        feedPath = d[@"image"];
    } else if ([d[@"video"] isKindOfClass:NSString.class]) {
        feedKind = FeedVideo;
        feedPath = d[@"video"];
    } else if ([d[@"nodes"] isKindOfClass:NSArray.class]) {
        feedKind = FeedScene;
    } else {
        return;
    }
    runningSessions = [NSHashTable weakObjectsHashTable];
    previewLayers = [NSHashTable weakObjectsHashTable];
    feedQueue = dispatch_queue_create("axx.camera.feed", DISPATCH_QUEUE_SERIAL);
    codesQueue = dispatch_queue_create("axx.camera.codes", DISPATCH_QUEUE_SERIAL);
    photoQueue = dispatch_queue_create("axx.camera.photos", DISPATCH_QUEUE_SERIAL);
    makeCamera();
    makePhotoClasses();
    CodeClass = subclass([AVMetadataMachineReadableCodeObject class], "AXXMachineReadableCode");
    method(CodeClass, @"type", "@@:", ^id(id self) { return store(self)[@"type"]; });
    method(CodeClass, @"stringValue", "@@:", ^id(id self) { return store(self)[@"stringValue"]; });
    method(CodeClass, @"bounds", "{CGRect={CGPoint=dd}{CGSize=dd}}@:", ^CGRect(id self) { return [store(self)[@"bounds"] CGRectValue]; });
    method(CodeClass, @"corners", "@@:", ^id(id self) { return store(self)[@"corners"]; });
    method(CodeClass, @"time", "{?=qiIq}@:", ^CMTime(id self) { return [store(self)[@"time"] CMTimeValue]; });
    method(CodeClass, @"duration", "{?=qiIq}@:", ^CMTime(id self) { return kCMTimeInvalid; });
    constant(CodeClass, @"descriptor", nil);

    // The camera, where an app looks for one.
    Class device = [AVCaptureDevice class];
    swizzleClass(device, @"defaultDeviceWithMediaType:", ^id(IMP original) {
        return ^id(id self, NSString *type) {
            return [type isEqualToString:AVMediaTypeVideo] && !FROM_AV ? camera : ((id(*)(id, SEL, id))original)(self, NULL, type);
        };
    });
    swizzleClass(device, @"defaultDeviceWithDeviceType:mediaType:position:", ^id(IMP original) {
        return ^id(id self, NSString *type, NSString *media, NSInteger position) {
            if ([type isEqualToString:AVCaptureDeviceTypeBuiltInWideAngleCamera] && (!media || [media isEqualToString:AVMediaTypeVideo]) &&
                position != AVCaptureDevicePositionFront && !FROM_AV)
                return camera;
            return ((id(*)(id, SEL, id, id, NSInteger))original)(self, NULL, type, media, position);
        };
    });
    swizzleClass(device, @"deviceWithUniqueID:", ^id(IMP original) {
        return ^id(id self, NSString *uid) {
            return [uid isEqualToString:camera.uniqueID] && !FROM_AV ? camera : ((id(*)(id, SEL, id))original)(self, NULL, uid);
        };
    });
    swizzleClass(device, @"devices", ^id(IMP original) {
        return ^id(id self) {
            NSArray *found = ((NSArray * (*)(id, SEL)) original)(self, NULL);
            return FROM_AV ? found : [found arrayByAddingObject:camera];
        };
    });
    swizzleClass(device, @"devicesWithMediaType:", ^id(IMP original) {
        return ^id(id self, NSString *type) {
            NSArray *found = ((NSArray * (*)(id, SEL, id)) original)(self, NULL, type);
            return [type isEqualToString:AVMediaTypeVideo] && !FROM_AV ? [found arrayByAddingObject:camera] : found;
        };
    });
    swizzleClass(device, @"systemPreferredCamera", ^id(IMP original) {
        return ^id(id self) { return FROM_AV ? ((id(*)(id, SEL))original)(self, NULL) : camera; };
    });
    swizzleClass(device, @"userPreferredCamera", ^id(IMP original) {
        return ^id(id self) { return FROM_AV ? ((id(*)(id, SEL))original)(self, NULL) : camera; };
    });

    static char kAsked;
    Class discovery = [AVCaptureDeviceDiscoverySession class];
    swizzleClass(discovery, @"discoverySessionWithDeviceTypes:mediaType:position:", ^id(IMP original) {
        return ^id(id self, NSArray *types, NSString *media, NSInteger position) {
            id d = ((id(*)(id, SEL, id, id, NSInteger))original)(self, NULL, types, media, position);
            BOOL wants = [types containsObject:AVCaptureDeviceTypeBuiltInWideAngleCamera] && (!media || [media isEqualToString:AVMediaTypeVideo]) &&
                         position != AVCaptureDevicePositionFront;
            if (d) objc_setAssociatedObject(d, &kAsked, @(wants), OBJC_ASSOCIATION_RETAIN);
            return d;
        };
    });
    swizzle(discovery, @"devices", ^id(IMP original) {
        return ^id(id self) {
            NSArray *found = ((NSArray * (*)(id, SEL)) original)(self, NULL);
            return [objc_getAssociatedObject(self, &kAsked) boolValue] && !FROM_AV ? [found arrayByAddingObject:camera] : found;
        };
    });

    // Its input.
    Method init = class_getInstanceMethod([AVCaptureDeviceInput class], @selector(initWithDevice:error:));
    originalInputInit = (InitIMP)method_getImplementation(init);
    method_setImplementation(init, (IMP)inputInit);
    swizzleClass([AVCaptureDeviceInput class], @"deviceInputWithDevice:error:", ^id(IMP original) {
        return ^id(id self, AVCaptureDevice *d, NSError **e) {
            return isCamera(d) ? class_createInstance(InputClass, 0) : ((id(*)(id, SEL, id, NSError **))original)(self, NULL, d, e);
        };
    });

    // Vision, which an app reads its frames with, runs on the processor.
    Method ri = class_getInstanceMethod([VNRequest class], @selector(initWithCompletionHandler:));
    if (ri) {
        originalRequestInit = (RequestInitIMP)method_getImplementation(ri);
        method_setImplementation(ri, (IMP)requestInit);
    }

    // Sessions that take it run here.
    Class session = [AVCaptureSession class];
    swizzle(session, @"canAddInput:", ^id(IMP original) {
        return ^BOOL(AVCaptureSession *self, AVCaptureInput *in) { return isCameraInput(in) ? YES : ((BOOL(*)(id, SEL, id))original)(self, NULL, in); };
    });
    swizzle(session, @"addInput:", ^id(IMP original) {
        return ^(AVCaptureSession *self, AVCaptureInput *in) {
            if (!isCameraInput(in)) {
                ((void (*)(id, SEL, id))original)(self, NULL, in);
                return;
            }
            objc_setAssociatedObject(self, &kInputs, [cameraInputs(self) arrayByAddingObject:in], OBJC_ASSOCIATION_RETAIN);
            LOG(@"a capture session takes the camera");
        };
    });
    swizzle(session, @"removeInput:", ^id(IMP original) {
        return ^(AVCaptureSession *self, AVCaptureInput *in) {
            if (!isCameraInput(in)) {
                ((void (*)(id, SEL, id))original)(self, NULL, in);
                return;
            }
            NSMutableArray *ins = [cameraInputs(self) mutableCopy];
            [ins removeObject:in];
            objc_setAssociatedObject(self, &kInputs, ins, OBJC_ASSOCIATION_RETAIN);
        };
    });
    swizzle(session, @"inputs", ^id(IMP original) {
        return ^id(AVCaptureSession *self) {
            NSArray *ins = ((NSArray * (*)(id, SEL)) original)(self, NULL);
            return FROM_AV ? ins : [ins arrayByAddingObjectsFromArray:cameraInputs(self)];
        };
    });
    swizzle(session, @"addOutput:", ^id(IMP original) {
        return ^(AVCaptureSession *self, AVCaptureOutput *out) {
            ((void (*)(id, SEL, id))original)(self, NULL, out);
            setSession(out, self);
        };
    });
    swizzle(session, @"addOutputWithNoConnections:", ^id(IMP original) {
        return ^(AVCaptureSession *self, AVCaptureOutput *out) {
            ((void (*)(id, SEL, id))original)(self, NULL, out);
            setSession(out, self);
        };
    });
    swizzle(session, @"canSetSessionPreset:", ^id(IMP original) {
        return ^BOOL(AVCaptureSession *self, NSString *p) { return playsCamera(self) ? YES : ((BOOL(*)(id, SEL, id))original)(self, NULL, p); };
    });
    swizzle(session, @"setSessionPreset:", ^id(IMP original) {
        return ^(AVCaptureSession *self, NSString *p) {
            @try {
                ((void (*)(id, SEL, id))original)(self, NULL, p);
            } @catch (NSException *e) {
                store(self)[@"sessionPreset"] = p;
            }
        };
    });
    swizzle(session, @"startRunning", ^id(IMP original) {
        return ^(AVCaptureSession *self) {
            if (!playsCamera(self)) {
                ((void (*)(id, SEL))original)(self, NULL);
                return;
            }
            [self willChangeValueForKey:@"running"];
            objc_setAssociatedObject(self, &kRunning, @YES, OBJC_ASSOCIATION_RETAIN);
            [self didChangeValueForKey:@"running"];
            os_unfair_lock_lock(&tablesLock);
            [runningSessions addObject:self];
            os_unfair_lock_unlock(&tablesLock);
            startFeed();
            LOG(@"a capture session runs the camera");
            [NSNotificationCenter.defaultCenter postNotificationName:AVCaptureSessionDidStartRunningNotification object:self];
        };
    });
    swizzle(session, @"stopRunning", ^id(IMP original) {
        return ^(AVCaptureSession *self) {
            if (!playsCamera(self)) {
                ((void (*)(id, SEL))original)(self, NULL);
                return;
            }
            [self willChangeValueForKey:@"running"];
            objc_setAssociatedObject(self, &kRunning, @NO, OBJC_ASSOCIATION_RETAIN);
            [self didChangeValueForKey:@"running"];
            os_unfair_lock_lock(&tablesLock);
            [runningSessions removeObject:self];
            os_unfair_lock_unlock(&tablesLock);
            [NSNotificationCenter.defaultCenter postNotificationName:AVCaptureSessionDidStopRunningNotification object:self];
        };
    });
    // AVFoundation's own session, which has no camera, finds it cannot run, and says so: a session
    // that plays the camera runs, and the app hears of no error.
    if (class_getInstanceMethod(session, NSSelectorFromString(@"_postRuntimeError:")))
        swizzle(session, @"_postRuntimeError:", ^id(IMP original) {
            return ^(AVCaptureSession *self, NSError *e) {
                if (playsCamera(self)) {
                    LOG(@"AVFoundation's own session, without a camera, failed (left unsaid to the app): %@", e);
                    return;
                }
                ((void (*)(id, SEL, id))original)(self, NULL, e);
            };
        });
    swizzle(session, @"isRunning", ^id(IMP original) {
        return ^BOOL(AVCaptureSession *self) {
            return playsCamera(self) ? [objc_getAssociatedObject(self, &kRunning) boolValue] : ((BOOL(*)(id, SEL))original)(self, NULL);
        };
    });
    swizzle(session, @"isInterrupted", ^id(IMP original) {
        return ^BOOL(AVCaptureSession *self) { return playsCamera(self) ? NO : ((BOOL(*)(id, SEL))original)(self, NULL); };
    });
    swizzle(session, @"connections", ^id(IMP original) {
        return ^id(AVCaptureSession *self) {
            if (!playsCamera(self) || FROM_AV) return ((NSArray * (*)(id, SEL)) original)(self, NULL);
            NSMutableArray *cs = [NSMutableArray array];
            for (AVCaptureOutput *out in self.outputs) [cs addObject:connectionOf(out, NO)];
            return cs;
        };
    });

    // Their outputs' connections.
    Class output = [AVCaptureOutput class];
    swizzle(output, @"connections", ^id(IMP original) {
        return ^id(AVCaptureOutput *self) {
            return playsCamera(sessionOf(self)) && !FROM_AV ? @[connectionOf(self, NO)] : ((NSArray * (*)(id, SEL)) original)(self, NULL);
        };
    });
    swizzle(output, @"connectionWithMediaType:", ^id(IMP original) {
        return ^id(AVCaptureOutput *self, NSString *media) {
            if (playsCamera(sessionOf(self)) && !FROM_AV) return [media isEqualToString:AVMediaTypeVideo] ? connectionOf(self, NO) : nil;
            return ((id(*)(id, SEL, id))original)(self, NULL, media);
        };
    });

    // A metadata output reads the codes Vision reads.
    static char kTypes;
    Class metadata = [AVCaptureMetadataOutput class];
    swizzle(metadata, @"availableMetadataObjectTypes", ^id(IMP original) {
        return ^id(AVCaptureMetadataOutput *self) {
            return playsCamera(sessionOf(self)) && !FROM_AV ? availableCodeTypes() : ((NSArray * (*)(id, SEL)) original)(self, NULL);
        };
    });
    swizzle(metadata, @"setMetadataObjectTypes:", ^id(IMP original) {
        return ^(AVCaptureMetadataOutput *self, NSArray *types) {
            if (!playsCamera(sessionOf(self))) {
                ((void (*)(id, SEL, id))original)(self, NULL, types);
                return;
            }
            for (NSString *t in types)
                if (![availableCodeTypes() containsObject:t])
                    [NSException raise:NSInvalidArgumentException format:@"*** -[AVCaptureMetadataOutput setMetadataObjectTypes:] Unsupported type found - use -availableMetadataObjectTypes"];
            objc_setAssociatedObject(self, &kTypes, [types copy], OBJC_ASSOCIATION_RETAIN);
        };
    });
    swizzle(metadata, @"metadataObjectTypes", ^id(IMP original) {
        return ^id(AVCaptureMetadataOutput *self) {
            return playsCamera(sessionOf(self)) && !FROM_AV ? (objc_getAssociatedObject(self, &kTypes) ?: @[]) : ((NSArray * (*)(id, SEL)) original)(self, NULL);
        };
    });

    // A photo output photographs the frame.
    Class photo = [AVCapturePhotoOutput class];
    swizzle(photo, @"capturePhotoWithSettings:delegate:", ^id(IMP original) {
        return ^(AVCapturePhotoOutput *self, AVCapturePhotoSettings *settings, id delegate) {
            if (!playsCamera(sessionOf(self))) {
                ((void (*)(id, SEL, id, id))original)(self, NULL, settings, delegate);
                return;
            }
            takePhoto(self, settings, delegate);
        };
    });
    swizzle(photo, @"availablePhotoCodecTypes", ^id(IMP original) {
        return ^id(AVCapturePhotoOutput *self) {
            return playsCamera(sessionOf(self)) ? @[AVVideoCodecTypeHEVC, AVVideoCodecTypeJPEG] : ((NSArray * (*)(id, SEL)) original)(self, NULL);
        };
    });
    swizzle(photo, @"supportedFlashModes", ^id(IMP original) {
        return ^id(AVCapturePhotoOutput *self) {
            return playsCamera(sessionOf(self)) ? @[@(AVCaptureFlashModeOff), @(AVCaptureFlashModeOn), @(AVCaptureFlashModeAuto)]
                                                : ((NSArray * (*)(id, SEL)) original)(self, NULL);
        };
    });
    swizzle(photo, @"setMaxPhotoDimensions:", ^id(IMP original) {
        return ^(AVCapturePhotoOutput *self, CMVideoDimensions d) {
            if (playsCamera(sessionOf(self))) return;
            ((void (*)(id, SEL, CMVideoDimensions))original)(self, NULL, d);
        };
    });
    swizzle(photo, @"maxPhotoDimensions", ^id(IMP original) {
        return ^CMVideoDimensions(AVCapturePhotoOutput *self) {
            return playsCamera(sessionOf(self)) ? (CMVideoDimensions){(int32_t)CW, (int32_t)CH} : ((CMVideoDimensions(*)(id, SEL))original)(self, NULL);
        };
    });
    swizzle(photo, @"setHighResolutionCaptureEnabled:", ^id(IMP original) {
        return ^(AVCapturePhotoOutput *self, BOOL on) {
            if (playsCamera(sessionOf(self))) return;
            ((void (*)(id, SEL, BOOL))original)(self, NULL, on);
        };
    });

    // A preview layer shows the frames.
    Class preview = [AVCaptureVideoPreviewLayer class];
    Method li = class_getInstanceMethod(preview, @selector(initWithSession:));
    originalLayerInit = (LayerInitIMP)method_getImplementation(li);
    method_setImplementation(li, (IMP)layerInit);
    Method lin = class_getInstanceMethod(preview, @selector(initWithSessionWithNoConnection:));
    if (lin) {
        originalLayerInitNoConnection = (LayerInitIMP)method_getImplementation(lin);
        method_setImplementation(lin, (IMP)layerInitNoConnection);
    }
    swizzle(preview, @"setSession:", ^id(IMP original) {
        return ^(AVCaptureVideoPreviewLayer *self, AVCaptureSession *s) {
            ((void (*)(id, SEL, id))original)(self, NULL, s);
            dressLayer(self, s);
        };
    });
    swizzle(preview, @"setSessionWithNoConnection:", ^id(IMP original) {
        return ^(AVCaptureVideoPreviewLayer *self, AVCaptureSession *s) {
            ((void (*)(id, SEL, id))original)(self, NULL, s);
            dressLayer(self, s);
        };
    });
    swizzle(preview, @"connection", ^id(IMP original) {
        return ^id(AVCaptureVideoPreviewLayer *self) {
            return playsCamera(sessionOf(self)) && !FROM_AV ? connectionOf(self, YES) : ((id(*)(id, SEL))original)(self, NULL);
        };
    });
    swizzle(preview, @"isPreviewing", ^id(IMP original) {
        return ^BOOL(AVCaptureVideoPreviewLayer *self) {
            AVCaptureSession *s = sessionOf(self);
            return playsCamera(s) ? [objc_getAssociatedObject(s, &kRunning) boolValue] : ((BOOL(*)(id, SEL))original)(self, NULL);
        };
    });
    swizzle(preview, @"layoutSublayers", ^id(IMP original) {
        return ^(AVCaptureVideoPreviewLayer *self) {
            ((void (*)(id, SEL))original)(self, NULL);
            layoutPicture(self);
        };
    });
    swizzle(preview, @"pointForCaptureDevicePointOfInterest:", ^id(IMP original) {
        return ^CGPoint(AVCaptureVideoPreviewLayer *self, CGPoint p) {
            if (!playsCamera(sessionOf(self))) return ((CGPoint(*)(id, SEL, CGPoint))original)(self, NULL, p);
            return CGPointApplyAffineTransform(p, sensorToLayer(self));
        };
    });
    swizzle(preview, @"captureDevicePointOfInterestForPoint:", ^id(IMP original) {
        return ^CGPoint(AVCaptureVideoPreviewLayer *self, CGPoint p) {
            if (!playsCamera(sessionOf(self))) return ((CGPoint(*)(id, SEL, CGPoint))original)(self, NULL, p);
            return CGPointApplyAffineTransform(p, CGAffineTransformInvert(sensorToLayer(self)));
        };
    });
    swizzle(preview, @"rectForMetadataOutputRectOfInterest:", ^id(IMP original) {
        return ^CGRect(AVCaptureVideoPreviewLayer *self, CGRect r) {
            if (!playsCamera(sessionOf(self))) return ((CGRect(*)(id, SEL, CGRect))original)(self, NULL, r);
            return CGRectApplyAffineTransform(r, sensorToLayer(self));
        };
    });
    swizzle(preview, @"metadataOutputRectOfInterestForRect:", ^id(IMP original) {
        return ^CGRect(AVCaptureVideoPreviewLayer *self, CGRect r) {
            if (!playsCamera(sessionOf(self))) return ((CGRect(*)(id, SEL, CGRect))original)(self, NULL, r);
            return CGRectApplyAffineTransform(r, CGAffineTransformInvert(sensorToLayer(self)));
        };
    });
    swizzle(preview, @"transformedMetadataObjectForMetadataObject:", ^id(IMP original) {
        return ^id(AVCaptureVideoPreviewLayer *self, AVMetadataObject *o) {
            if (![o isKindOfClass:CodeClass]) return ((id(*)(id, SEL, id))original)(self, NULL, o);
            CGAffineTransform t = sensorToLayer(self);
            AVMetadataMachineReadableCodeObject *c = class_createInstance(CodeClass, 0);
            [store(c) addEntriesFromDictionary:store(o)];
            store(c)[@"bounds"] = [NSValue valueWithCGRect:CGRectApplyAffineTransform(o.bounds, t)];
            NSMutableArray *corners = [NSMutableArray array];
            for (NSDictionary *d in store(o)[@"corners"]) {
                CGPoint p;
                CGPointMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)d, &p);
                [corners addObject:(__bridge_transfer NSDictionary *)CGPointCreateDictionaryRepresentation(CGPointApplyAffineTransform(p, t))];
            }
            store(c)[@"corners"] = corners;
            return c;
        };
    });

    // A rotation coordinator of the camera follows the interface, as the phone is held.
    Class coordinator = NSClassFromString(@"AVCaptureDeviceRotationCoordinator");
    if (coordinator) {
        CoordinatorClass = subclass(coordinator, "AXXRotationCoordinator");
        method(CoordinatorClass, @"device", "@@:", ^id(id self) { return store(self)[@"device"]; });
        method(CoordinatorClass, @"previewLayer", "@@:", ^id(id self) { return store(self)[@"previewLayer"]; });
        method(CoordinatorClass, @"videoRotationAngleForHorizonLevelPreview", "d@:", ^double(id self) { return interfaceAngle(); });
        method(CoordinatorClass, @"videoRotationAngleForHorizonLevelCapture", "d@:", ^double(id self) { return interfaceAngle(); });
        Method ci = class_getInstanceMethod(coordinator, NSSelectorFromString(@"initWithDevice:previewLayer:"));
        if (ci) {
            originalCoordinatorInit = (CoordinatorInitIMP)method_getImplementation(ci);
            method_setImplementation(ci, (IMP)coordinatorInit);
        }
    }
    LOG(@"the camera sees %@", feedKind == FeedScene ? @"the scene" : feedPath.lastPathComponent);
}
