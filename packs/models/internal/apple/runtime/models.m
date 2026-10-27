// Debug simulator adapter. Only public framework selectors are replaced in
// the application process. There is no service impersonation or private API.
#import <AVFoundation/AVFoundation.h>
#import <CoreImage/CoreImage.h>
#import <CoreMedia/CoreMedia.h>
#import <Foundation/Foundation.h>
#import <Speech/Speech.h>
#import <TargetConditionals.h>
#import <Vision/Vision.h>
#include <math.h>
#import <objc/runtime.h>
#include <unistd.h>

// The platform whose APIs the stubs are of (ios.vision.text, macos.vision.text): the build
// gives it.
#ifndef AXX_PLATFORM
#define AXX_PLATFORM "ios"
#endif
#define API(name) @AXX_PLATFORM "." name

static NSDictionary *models;
static char imageKey, resultsKey, audioKey, taskKey, audioErrorKey, endedKey,
    audioRateKey, recordingKey;
static NSError *failure(NSString *message) {
  return [NSError errorWithDomain:@"axx.models"
                             code:704
                         userInfo:@{
                           NSLocalizedDescriptionKey :
                               [@"AXX-E0704: " stringByAppendingString:message],
                           NSLocalizedRecoverySuggestionErrorKey :
                               @"Use a supported audio API in a Debug build and "
                               @"queue the recording with the microphone step."
                         }];
}
static IMP replace(Class cls, SEL selector, id block) {
  Method method = class_getInstanceMethod(cls, selector);
  if (!method)
    return NULL;
  IMP old = method_getImplementation(method),
      next = imp_implementationWithBlock(block);
  if (!class_addMethod(cls, selector, next, method_getTypeEncoding(method)))
    class_replaceMethod(cls, selector, next, method_getTypeEncoding(method));
  return old;
}
static NSDictionary *input(NSDictionary *a) {
  NSArray *inputs = a[@"contract"][@"inputs"];
  NSString *name = a[@"contract"][@"match"][@"input"];
  for (NSDictionary *in in inputs)
    if (!name || [name isEqual:in[@"name"]])
      return in;
  return nil;
}
static void standardize(double *v, NSUInteger n) {
  double mean = 0, variance = 0;
  for (NSUInteger i = 0; i < n; i++)
    mean += v[i];
  mean /= n;
  for (NSUInteger i = 0; i < n; i++) {
    v[i] -= mean;
    variance += v[i] * v[i];
  }
  double sd = sqrt(variance / n + 1e-12);
  for (NSUInteger i = 0; i < n; i++)
    v[i] /= sd;
}
static NSUInteger nearest(NSDictionary *a, double *v, NSUInteger n) {
  NSArray *keys = a[@"keys"] ?: @[];
  NSUInteger at = keys.count;
  double best = [a[@"similarity"] doubleValue];
  for (NSInteger i = (NSInteger)keys.count - 1; i >= 0; i--) {
    NSArray *key = keys[i];
    if (key.count != n)
      continue;
    double dot = 0;
    for (NSUInteger j = 0; j < n; j++)
      dot += v[j] * [key[j] doubleValue];
    if (dot / n >= best) {
      best = dot / n;
      at = i;
    }
  }
  return at;
}

// The same bilinear crop, pooling and standardization as match.Picture and
// match.Reducer. Decode at the original size; CoreGraphics does no resizing.
static NSUInteger picture(NSDictionary *a, CIImage *ci, NSError **error) {
  if (!ci) {
    if (error)
      *error = failure(@"The Vision input image was not captured");
    return NSNotFound;
  }
  NSDictionary *in = input(a), *spec = in[@"image"];
  NSArray *shape = in[@"shape"];
  NSString *layout = spec[@"layout"];
  BOOL first = [layout hasSuffix:@"CHW"];
  NSUInteger off = shape.count - 3,
             c = [shape[off + (first ? 0 : 2)] unsignedIntegerValue];
  NSUInteger h = [shape[off + (first ? 1 : 0)] unsignedIntegerValue],
             w = [shape[off + (first ? 2 : 1)] unsignedIntegerValue];
  CGImageRef cg = [[CIContext contextWithOptions:nil] createCGImage:ci
                                                           fromRect:ci.extent];
  if (!cg) {
    if (error)
      *error = failure(@"Cannot decode the Vision input image");
    return NSNotFound;
  }
  NSUInteger sw = CGImageGetWidth(cg), sh = CGImageGetHeight(cg), cw = sw,
             ch = sh, ox = 0, oy = 0;
  if (![spec[@"resize"] isEqual:@"stretch"]) {
    if (sw * h > sh * w) {
      cw = sh * w / h;
      ox = (sw - cw) / 2;
    } else {
      ch = sw * h / w;
      oy = (sh - ch) / 2;
    }
  }
  double crop = [spec[@"crop"] doubleValue];
  if (crop > 0 && crop < 1) {
    NSUInteger nw = llround(cw * crop), nh = llround(ch * crop);
    ox += (cw - nw) / 2;
    oy += (ch - nh) / 2;
    cw = nw;
    ch = nh;
  }
  if (!cw || !ch) {
    CGImageRelease(cg);
    if (error)
      *error = failure(@"The image crop is empty");
    return NSNotFound;
  }
  unsigned char *pixels = calloc(sw * sh, 4);
  CGColorSpaceRef color = CGColorSpaceCreateDeviceRGB();
  CGContextRef ctx = CGBitmapContextCreate(pixels, sw, sh, 8, sw * 4, color,
                                           kCGImageAlphaPremultipliedLast |
                                               kCGBitmapByteOrder32Big);
  CGContextDrawImage(ctx, CGRectMake(0, 0, sw, sh), cg);
  CGContextRelease(ctx);
  CGColorSpaceRelease(color);
  CGImageRelease(cg);
  NSUInteger kh = MAX(h / MIN(16, h), 1), kw = MAX(w / MIN(16, w), 1),
             ph = h / kh, pw = w / kw, n = c * ph * pw;
  double *v = calloc(n, sizeof(double));
  for (NSUInteger y = 0; y < ph * kh; y++)
    for (NSUInteger x = 0; x < pw * kw; x++) {
      double sx = (x + 0.5) * cw / w - 0.5, sy = (y + 0.5) * ch / h - 0.5,
             fx = sx - floor(sx), fy = sy - floor(sy), rgb[3];
      NSInteger x0 = floor(sx), y0 = floor(sy);
      for (NSUInteger k = 0; k < 3; k++) {
        double samples[4];
        for (int dy = 0; dy < 2; dy++)
          for (int dx = 0; dx < 2; dx++) {
            NSUInteger px = ox + MIN(MAX(x0 + dx, 0), (NSInteger)cw - 1),
                       py = oy + MIN(MAX(y0 + dy, 0), (NSInteger)ch - 1);
            samples[dy * 2 + dx] = pixels[(py * sw + px) * 4 + k];
          }
        double top = samples[0] * (1 - fx) + samples[1] * fx,
               bottom = samples[2] * (1 - fx) + samples[3] * fx;
        rgb[k] = top * (1 - fy) + bottom * fy;
      }
      for (NSUInteger k = 0; k < c; k++) {
        double value =
            k < 3 ? rgb[[spec[@"channels"] isEqual:@"BGR"] ? 2 - k : k] : 255;
        if (c == 1 || [spec[@"channels"] isEqual:@"gray"])
          value = 0.299 * rgb[0] + 0.587 * rgb[1] + 0.114 * rgb[2];
        v[k * ph * pw + (y / kh) * pw + x / kw] += value;
      }
    }
  free(pixels);
  for (NSUInteger i = 0; i < n; i++)
    v[i] /= kh * kw;
  for (NSUInteger k = 0; k < c; k++)
    standardize(v + k * ph * pw, ph * pw);
  NSUInteger at = nearest(a, v, n);
  free(v);
  return at;
}

static NSUInteger sound(NSDictionary *a, NSArray<NSNumber *> *samples) {
  NSDictionary *in = input(a);
  NSUInteger n = 1;
  for (NSNumber *d in in[@"shape"])
    n *= d.unsignedIntegerValue;
  NSUInteger frame = 0;
  for (NSUInteger f = 100; f <= 1024 && f <= n; f++)
    if (n % f == 0 && (!frame || labs((long)f - 256) < labs((long)frame - 256)))
      frame = f;
  if (!frame)
    return NSNotFound;
  NSUInteger bins = frame / 2, frames = n / frame;
  double *cosines = calloc(frame * bins, sizeof(double)),
         *sines = calloc(frame * bins, sizeof(double)), bands[32] = {0};
  double rate = [in[@"audio"][@"sampleRate"] doubleValue];
  double low = 2595 * log10(1 + rate / frame / 700),
         high = 2595 * log10(1 + rate / 2 / 700);
  for (NSUInteger i = 0; i < frame; i++)
    for (NSUInteger j = 0; j < bins; j++) {
      double angle = 2 * M_PI * ((j + 1) * i % frame) / frame;
      cosines[i * bins + j] = cos(angle);
      sines[i * bins + j] = sin(angle);
    }
  for (NSUInteger f = 0; f < frames; f++)
    for (NSUInteger j = 0; j < bins; j++) {
      double re = 0, im = 0;
      for (NSUInteger i = 0; i < frame; i++) {
        NSUInteger at = f * frame + i;
        double v = at < samples.count ? [samples[at] doubleValue] : 0;
        re += v * cosines[i * bins + j];
        im += v * sines[i * bins + j];
      }
      NSInteger band =
          floor((2595 * log10(1 + (j + 1) * rate / frame / 700) - low) /
                (high - low) * 32);
      bands[MIN(MAX(band, 0), 31)] += re * re + im * im;
    }
  free(cosines);
  free(sines);
  for (int b = 0; b < 32; b++)
    bands[b] = log(bands[b] / frames + 1e-10);
  standardize(bands, 32);
  return nearest(a, bands, 32);
}

@interface AXXClassification : VNClassificationObservation
@property NSString *label;
@property float score;
@end
@implementation AXXClassification
- (NSString *)identifier {
  return self.label;
}
- (VNConfidence)confidence {
  return self.score;
}
- (BOOL)hasPrecisionRecallCurve {
  return NO;
}
@end

@interface AXXText : VNRecognizedText
@property NSString *text;
@property float score;
@end
@implementation AXXText
- (NSString *)string {
  return self.text;
}
- (VNConfidence)confidence {
  return self.score;
}
- (VNRectangleObservation *)boundingBoxForRange:(NSRange)range
                                          error:(NSError **)error {
  if (NSMaxRange(range) > self.text.length) {
    if (error)
      *error = failure(@"Text range is outside the fixture result");
    return nil;
  }
  return [VNRectangleObservation
      observationWithBoundingBox:CGRectMake(0, 0, 1, 1)];
}
@end
@interface AXXTextObservation : VNRecognizedTextObservation
@property AXXText *candidate;
@end
@implementation AXXTextObservation
- (NSArray *)topCandidates:(NSUInteger)count {
  return count ? @[ self.candidate ] : @[];
}
- (CGRect)boundingBox {
  return CGRectMake(0, 0, 1, 1);
}
- (VNConfidence)confidence {
  return self.candidate.score;
}
@end
@interface AXXObject : VNRecognizedObjectObservation
@property CGRect box;
@property float score;
@property NSArray *classifications;
@end
@implementation AXXObject
- (CGRect)boundingBox {
  return self.box;
}
- (VNConfidence)confidence {
  return self.score;
}
- (NSArray *)labels {
  return self.classifications;
}
@end
@interface AXXHuman : VNHumanObservation
@property CGRect box;
@property float score;
@end
@implementation AXXHuman
- (CGRect)boundingBox {
  return self.box;
}
- (VNConfidence)confidence {
  return self.score;
}
@end
@interface AXXMask : VNPixelBufferObservation
@property CVPixelBufferRef buffer;
@end
@implementation AXXMask
- (CVPixelBufferRef)pixelBuffer {
  return self.buffer;
}
- (VNConfidence)confidence {
  return 1;
}
- (void)dealloc {
  if (_buffer)
    CVPixelBufferRelease(_buffer);
}
@end

static NSDictionary *visionClasses(void) {
  return @{
    @"VNClassifyImageRequest" : API("vision.classify"),
    @"VNRecognizeTextRequest" : API("vision.text"),
    @"VNDetectHumanRectanglesRequest" : API("vision.humans"),
    @"VNRecognizeAnimalsRequest" : API("vision.animals"),
    @"VNGeneratePersonSegmentationRequest" : API("vision.person-segmentation")
  };
}
static NSString *visionAPI(VNRequest *request) {
  for (NSString *name in visionClasses())
    if ([request isKindOfClass:NSClassFromString(name)])
      return visionClasses()[name];
  return nil;
}
static NSArray *observations(NSDictionary *a, NSUInteger at, VNRequest *request,
                             NSError **error) {
  NSArray *outputs = a[@"contract"][@"outputs"];
  NSDictionary *answer = a[@"answers"][at];
  NSString *task = a[@"contract"][@"task"];
  NSMutableArray *result = [NSMutableArray new];
  if ([task isEqual:@"classification"]) {
    NSDictionary *output = outputs.firstObject;
    NSArray *labels = output[@"labels"], *scores = answer[output[@"name"]];
    for (NSUInteger i = 0; i < labels.count; i++) {
      AXXClassification *o = [AXXClassification new];
      o.label = labels[i];
      o.score = [scores[i] floatValue];
      [result addObject:o];
    }
    [result sortUsingComparator:^NSComparisonResult(AXXClassification *x,
                                                    AXXClassification *y) {
      return x.score > y.score   ? NSOrderedAscending
             : x.score < y.score ? NSOrderedDescending
                                 : NSOrderedSame;
    }];
  } else if ([task isEqual:@"text-recognition"]) {
    NSDictionary *out = outputs.firstObject;
    NSArray *labels = out[@"labels"], *values = answer[out[@"name"]];
    NSMutableString *text = [NSMutableString new];
    NSInteger last = -1;
    float score = 1;
    for (NSUInteger s = 0; s < values.count / labels.count; s++) {
      NSUInteger best = 0;
      for (NSUInteger k = 1; k < labels.count; k++)
        if ([values[s * labels.count + k] doubleValue] >
            [values[s * labels.count + best] doubleValue])
          best = k;
      if ((NSInteger)best != last && [labels[best] length]) {
        [text appendString:labels[best]];
        score = fminf(score, [values[s * labels.count + best] floatValue]);
      }
      last = best;
    }
    if (text.length) {
      AXXText *candidate = [AXXText new];
      candidate.text = text;
      candidate.score = score;
      AXXTextObservation *o = [AXXTextObservation new];
      o.candidate = candidate;
      [result addObject:o];
    }
  } else if ([task isEqual:@"detection"]) {
    NSMutableDictionary *role = [NSMutableDictionary new];
    for (NSDictionary *out in outputs)
      role[out[@"detection"]] = out;
    NSArray *boxes = answer[role[@"boxes"][@"name"]],
            *classes = answer[role[@"classes"][@"name"]],
            *scores = answer[role[@"scores"][@"name"]],
            *count = answer[role[@"count"][@"name"]];
    NSArray *labels = role[@"classes"][@"labels"];
    NSUInteger n = MIN([count.firstObject unsignedIntegerValue],
                       MIN(boxes.count / 4, MIN(classes.count, scores.count)));
    for (NSUInteger i = 0; i < n; i++) {
      CGFloat ymin = [boxes[i * 4] doubleValue],
              xmin = [boxes[i * 4 + 1] doubleValue],
              ymax = [boxes[i * 4 + 2] doubleValue],
              xmax = [boxes[i * 4 + 3] doubleValue];
      CGRect box = CGRectMake(xmin, 1 - ymax, xmax - xmin, ymax - ymin);
      if ([request isKindOfClass:VNDetectHumanRectanglesRequest.class]) {
        AXXHuman *o = [AXXHuman new];
        o.box = box;
        o.score = [scores[i] floatValue];
        [result addObject:o];
      } else {
        NSUInteger k = [classes[i] unsignedIntegerValue];
        if (k >= labels.count) {
          if (error)
            *error = failure(@"Detection class is outside its labels");
          return nil;
        }
        AXXClassification *label = [AXXClassification new];
        label.label = labels[k];
        label.score = [scores[i] floatValue];
        AXXObject *o = [AXXObject new];
        o.box = box;
        o.score = label.score;
        o.classifications = @[ label ];
        [result addObject:o];
      }
    }
  } else if ([task isEqual:@"segmentation"]) {
    NSDictionary *out = outputs.firstObject;
    NSArray *shape = out[@"shape"], *values = answer[out[@"name"]];
    NSUInteger w = [shape.lastObject unsignedIntegerValue],
               h = [shape[shape.count - 2] unsignedIntegerValue];
    OSType type =
        ((VNGeneratePersonSegmentationRequest *)request).outputPixelFormat;
    if (type != kCVPixelFormatType_OneComponent8 &&
        type != kCVPixelFormatType_OneComponent32Float) {
      if (error)
        *error = failure(
            @"Fixture masks support 8-bit or float32 outputPixelFormat");
      return nil;
    }
    CVPixelBufferRef buffer = NULL;
    CVReturn status =
        CVPixelBufferCreate(kCFAllocatorDefault, w, h, type, NULL, &buffer);
    if (status != kCVReturnSuccess) {
      if (error)
        *error = failure(@"Cannot allocate the fixture mask");
      return nil;
    }
    CVPixelBufferLockBaseAddress(buffer, 0);
    unsigned char *base = CVPixelBufferGetBaseAddress(buffer);
    size_t stride = CVPixelBufferGetBytesPerRow(buffer);
    for (NSUInteger y = 0; y < h; y++)
      for (NSUInteger x = 0; x < w; x++) {
        double v = MIN(MAX([values[y * w + x] doubleValue], 0), 1);
        if (type == kCVPixelFormatType_OneComponent8)
          base[y * stride + x] = (unsigned char)llround(v * 255);
        else
          ((float *)(base + y * stride))[x] = v;
      }
    CVPixelBufferUnlockBaseAddress(buffer, 0);
    AXXMask *o = [AXXMask new];
    o.buffer = buffer;
    [result addObject:o];
  }
  return result;
}

static void installVision(void) {
  Class cls = VNImageRequestHandler.class;
  // Capture only public initializer arguments, with every common image source.
  for (NSString *kind in @[
         @"CGImage", @"CIImage", @"CVPixelBuffer", @"CMSampleBuffer", @"URL",
         @"Data"
       ]) {
    for (NSNumber *oriented in @[ @NO, @YES ]) {
      BOOL orientation = oriented.boolValue;
      SEL sel = NSSelectorFromString(
          [NSString stringWithFormat:@"initWith%@: %@options:", kind,
                                     orientation ? @"orientation:" : @""]);
      // Build selectors without whitespace.
      sel = NSSelectorFromString([[NSStringFromSelector(sel)
          componentsSeparatedByString:@" "] componentsJoinedByString:@""]);
      __block IMP old;
      CIImage * (^decode)(void *) = ^CIImage *(void *raw) {
        if ([kind isEqual:@"CGImage"])
          return [CIImage imageWithCGImage:(CGImageRef)raw];
        if ([kind isEqual:@"CVPixelBuffer"])
          return [CIImage imageWithCVPixelBuffer:(CVPixelBufferRef)raw];
        if ([kind isEqual:@"CMSampleBuffer"]) {
          CVImageBufferRef image =
              CMSampleBufferGetImageBuffer((CMSampleBufferRef)raw);
          return image ? [CIImage imageWithCVPixelBuffer:image] : nil;
        }
        id obj = (__bridge id)raw;
        if ([kind isEqual:@"CIImage"])
          return obj;
        if ([kind isEqual:@"URL"])
          return [CIImage imageWithContentsOfURL:obj];
        return [CIImage imageWithData:obj];
      };
      if (orientation)
        old =
            replace(cls, sel,
                    ^id(id self, void *raw, CGImagePropertyOrientation o,
                        NSDictionary *opts) {
                      id result =
                          ((id (*)(id, SEL, void *, CGImagePropertyOrientation,
                                   id))old)(self, sel, raw, o, opts);
                      objc_setAssociatedObject(
                          result, &imageKey,
                          [decode(raw) imageByApplyingCGOrientation:o],
                          OBJC_ASSOCIATION_RETAIN_NONATOMIC);
                      return result;
                    });
      else
        old = replace(cls, sel, ^id(id self, void *raw, NSDictionary *opts) {
          id result = ((id (*)(id, SEL, void *, id))old)(self, sel, raw, opts);
          objc_setAssociatedObject(result, &imageKey, decode(raw),
                                   OBJC_ASSOCIATION_RETAIN_NONATOMIC);
          return result;
        });
    }
  }
  for (NSString *name in visionClasses()) {
    if (!models[visionClasses()[name]])
      continue;
    __block IMP results;
    results =
        replace(NSClassFromString(name), @selector(results), ^id(id self) {
          id value = objc_getAssociatedObject(self, &resultsKey);
          return value ?: ((id (*)(id, SEL))results)(self, @selector(results));
        });
  }
  __block IMP perform;
  perform = replace(
      cls, @selector(performRequests:error:),
      ^BOOL(id self, NSArray<VNRequest *> *requests, NSError **error) {
        NSMutableArray *others = [NSMutableArray new];
        for (VNRequest *request in requests) {
          NSString *api = visionAPI(request);
          NSDictionary *a = api ? models[api] : nil;
          if (!a) {
            [others addObject:request];
            continue;
          }
          NSError *err = nil;
          NSUInteger at =
              picture(a, objc_getAssociatedObject(self, &imageKey), &err);
          if (err || at == NSNotFound) {
            objc_setAssociatedObject(request, &resultsKey, @[],
                                     OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            if (request.completionHandler)
              request.completionHandler(request, err);
            if (error)
              *error = err;
            return NO;
          }
          NSArray *values = observations(a, at, request, &err);
          if (err) {
            if (error)
              *error = err;
            if (request.completionHandler)
              request.completionHandler(request, err);
            return NO;
          }
          objc_setAssociatedObject(request, &resultsKey, values,
                                   OBJC_ASSOCIATION_RETAIN_NONATOMIC);
          if (request.completionHandler)
            request.completionHandler(request, nil);
        }
        return others.count
                   ? ((BOOL (*)(id, SEL, id, NSError **))perform)(
                         self, @selector(performRequests:error:), others, error)
                   : YES;
      });
  // Sequence handlers accept the image at each perform call. Route configured
  // requests through the same image handler and preserve the real sequence
  // handler for requests whose APIs have no fixture.
  for (NSString *kind in @[
         @"CGImage", @"CIImage", @"CVPixelBuffer", @"CMSampleBuffer",
         @"ImageURL", @"ImageData"
       ]) {
    NSString *initKind = [kind isEqual:@"ImageURL"]
                             ? @"URL"
                             : ([kind isEqual:@"ImageData"] ? @"Data" : kind);
    for (NSNumber *oriented in @[ @NO, @YES ]) {
      BOOL orientation = oriented.boolValue;
      SEL sel = NSSelectorFromString(
          [NSString stringWithFormat:@"performRequests:on%@:%@error:", kind,
                                     orientation ? @"orientation:" : @""]);
      SEL init = NSSelectorFromString(
          [NSString stringWithFormat:@"initWith%@:%@options:", initKind,
                                     orientation ? @"orientation:" : @""]);
      __block IMP old;
      BOOL (^run)(id, NSArray *, void *, CGImagePropertyOrientation,
                  NSError **) = ^BOOL(id self, NSArray *requests, void *raw,
                                      CGImagePropertyOrientation o,
                                      NSError **error) {
        NSMutableArray *stubbed = [NSMutableArray new],
                       *others = [NSMutableArray new];
        for (VNRequest *request in requests) {
          NSString *api = visionAPI(request);
          [(api && models[api] ? stubbed : others) addObject:request];
        }
        if (stubbed.count) {
          id handler = [VNImageRequestHandler alloc];
          IMP constructor = [handler methodForSelector:init];
          handler = orientation
                        ? ((id (*)(id, SEL, void *, CGImagePropertyOrientation,
                                   id))constructor)(handler, init, raw, o, @{})
                        : ((id (*)(id, SEL, void *, id))constructor)(
                              handler, init, raw, @{});
          if (![handler performRequests:stubbed error:error])
            return NO;
        }
        if (!others.count)
          return YES;
        return orientation
                   ? ((BOOL (*)(id, SEL, id, void *, CGImagePropertyOrientation,
                                NSError **))old)(self, sel, others, raw, o,
                                                 error)
                   : ((BOOL (*)(id, SEL, id, void *, NSError **))old)(
                         self, sel, others, raw, error);
      };
      if (orientation)
        old = replace(VNSequenceRequestHandler.class, sel,
                      ^BOOL(id self, NSArray *requests, void *raw,
                            CGImagePropertyOrientation o, NSError **error) {
                        return run(self, requests, raw, o, error);
                      });
      else
        old = replace(
            VNSequenceRequestHandler.class, sel,
            ^BOOL(id self, NSArray *requests, void *raw, NSError **error) {
              return run(self, requests, raw, kCGImagePropertyOrientationUp,
                         error);
            });
    }
  }
}

#include "audio.inc"
#include "microphone.inc"

@interface AXXSpeechSegment : SFTranscriptionSegment
@property NSString *text;
@property NSTimeInterval seconds;
@end
@implementation AXXSpeechSegment
- (NSString *)substring {
  return self.text;
}
- (NSRange)substringRange {
  return NSMakeRange(0, self.text.length);
}
- (NSTimeInterval)timestamp {
  return 0;
}
- (NSTimeInterval)duration {
  return self.seconds;
}
- (float)confidence {
  return 1;
}
- (NSArray *)alternativeSubstrings {
  return @[];
}
@end
@interface AXXTranscription : SFTranscription
@property NSString *text;
@property NSTimeInterval seconds;
@end
@implementation AXXTranscription
- (NSString *)formattedString {
  return self.text;
}
- (NSArray *)segments {
  if (!self.text.length)
    return @[];
  AXXSpeechSegment *segment = [AXXSpeechSegment new];
  segment.text = self.text;
  segment.seconds = self.seconds;
  return @[ segment ];
}
@end
@interface AXXSpeechResult : SFSpeechRecognitionResult
@property AXXTranscription *transcript;
@end
@implementation AXXSpeechResult
- (SFTranscription *)bestTranscription {
  return self.transcript;
}
- (NSArray *)transcriptions {
  return @[ self.transcript ];
}
- (BOOL)isFinal {
  return YES;
}
- (SFSpeechRecognitionMetadata *)speechRecognitionMetadata {
  return nil;
}
@end
@interface AXXSpeechTask : SFSpeechRecognitionTask
@property(copy) void (^handler)(SFSpeechRecognitionResult *, NSError *);
@property NSMutableArray<NSNumber *> *samples;
@property NSOperationQueue *callbackQueue;
@property BOOL ended;
@property BOOL cancelledStub;
@property NSError *stubError;
@property NSInteger sampleRate;
@property NSDictionary *recording;
@end
@implementation AXXSpeechTask
- (SFSpeechRecognitionTaskState)state {
  return self.ended ? SFSpeechRecognitionTaskStateCompleted
                    : SFSpeechRecognitionTaskStateRunning;
}
- (BOOL)isFinishing {
  return self.ended;
}
- (BOOL)isCancelled {
  return self.cancelledStub;
}
- (NSError *)error {
  return self.stubError;
}
- (void)cancel {
  @synchronized(self) {
    if (self.ended)
      return;
    self.cancelledStub = YES;
  }
  [self finish];
}
- (void)finish {
  @synchronized(self) {
    if (self.ended)
      return;
    self.ended = YES;
  }
  AXXSpeechResult *result = nil;
  NSError *err = self.stubError;
  if (self.cancelledStub)
    err = [NSError errorWithDomain:NSCocoaErrorDomain
                              code:NSUserCancelledError
                          userInfo:nil];
  if (!err) {
    NSArray *samples;
    @synchronized(self.samples) {
      samples = [self.samples copy];
    }
    NSUInteger at =
        self.recording
            ? recordingMatch(models[API("speech")], self.recording, &err)
            : speechMatch(models[API("speech")], samples, self.sampleRate);
    if (at == NSNotFound)
      err = err
                ?: failure(@"Unsupported audio shape; use PCM audio or queue a "
                           @"WAV recording");
    else {
      AXXTranscription *t = [AXXTranscription new];
      t.text = models[API("speech")][@"generated"][at];
      t.seconds = self.recording
                      ? (double)[self.recording[@"samples"] count] /
                            [self.recording[@"rate"] doubleValue]
                      : (double)samples.count / MAX(self.sampleRate, 1);
      result = [AXXSpeechResult new];
      result.transcript = t;
    }
  }
  self.stubError = err;
  void (^handler)(SFSpeechRecognitionResult *, NSError *) = self.handler;
  self.handler = nil;
  [self.callbackQueue addOperationWithBlock:^{
    if (handler)
      handler(result, err);
  }];
}
@end

static void queuedRecording(AXXSpeechTask *task) {
  @synchronized(task) {
    if (task.ended || task.recording)
      return;
    // A tap may already be installed before engine.start(). It owns its
    // recording when it starts; a buffer request must not take it first.
    if (!capturePending()) {
      NSError *error = nil;
      NSDictionary *recording = takeRecording(&error);
      if (recording || error) {
        task.recording = recording;
        task.stubError = error;
        [task finish];
        return;
      }
    }
  }
  dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 25 * NSEC_PER_MSEC),
                 dispatch_get_main_queue(), ^{
                   queuedRecording(task);
                 });
}
static void installSpeech(void) {
#if TARGET_OS_OSX
  // A Mac has no test's grant of speech recognition, as a simulator's privacy
  // grant: the stub, which no real recognizer answers, is authorized.
  replace(object_getClass(SFSpeechRecognizer.class),
          @selector(authorizationStatus),
          ^SFSpeechRecognizerAuthorizationStatus(id self) {
            return SFSpeechRecognizerAuthorizationStatusAuthorized;
          });
  replace(object_getClass(SFSpeechRecognizer.class),
          @selector(requestAuthorization:),
          ^(id self, void (^handler)(SFSpeechRecognizerAuthorizationStatus)) {
            dispatch_async(
                dispatch_get_global_queue(QOS_CLASS_DEFAULT, 0), ^{
                  handler(SFSpeechRecognizerAuthorizationStatusAuthorized);
                });
          });
#endif
  replace(SFSpeechRecognizer.class, @selector(isAvailable), ^BOOL(id self) {
    return YES;
  });
  replace(SFSpeechRecognizer.class, @selector(supportsOnDeviceRecognition),
          ^BOOL(id self) {
            return YES;
          });
  replace(
      SFSpeechAudioBufferRecognitionRequest.class,
      @selector(appendAudioPCMBuffer:), ^(id self, AVAudioPCMBuffer *buffer) {
        NSMutableArray *samples = objc_getAssociatedObject(self, &audioKey);
        if (!samples) {
          samples = [NSMutableArray new];
          objc_setAssociatedObject(self, &audioKey, samples,
                                   OBJC_ASSOCIATION_RETAIN_NONATOMIC);
        }
        AXXSpeechTask *task = objc_getAssociatedObject(self, &taskKey);
        @synchronized(samples) {
          AXXAudioChunk *chunk =
              objc_getAssociatedObject(buffer, &capturedAudioKey);
          if (chunk.recording) {
            if (!objc_getAssociatedObject(self, &recordingKey))
              objc_setAssociatedObject(self, &recordingKey, chunk.recording,
                                       OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            task.recording = task.recording ?: chunk.recording;
            task.stubError = nil;
            if (chunk.ended) {
              objc_setAssociatedObject(self, &endedKey, @YES,
                                       OBJC_ASSOCIATION_RETAIN_NONATOMIC);
              [task finish];
            }
            return;
          }
          if (task.recording)
            return;
          NSNumber *previous = objc_getAssociatedObject(self, &audioRateKey);
          NSInteger rate = llround(buffer.format.sampleRate);
          NSMutableArray *part = [NSMutableArray new];
          if (!appendPCM(part, buffer)) {
            NSError *err = failure(@"Unsupported PCM audio; use float32, "
                                   @"float64, int16 or int32 samples");
            objc_setAssociatedObject(self, &audioErrorKey, err,
                                     OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            task.stubError = err;
          } else {
            // Preserve a stream's samples and resample it once at finish.
            if (previous && previous.integerValue != rate)
              part = [audioNumbers(resample(
                  audioData(part), rate, previous.integerValue)) mutableCopy];
            else if (!previous) {
              previous = @(rate);
              objc_setAssociatedObject(self, &audioRateKey, previous,
                                       OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            }
            task.sampleRate = previous.integerValue;
            [samples addObjectsFromArray:part];
          }
        }
      });
  replace(SFSpeechAudioBufferRecognitionRequest.class, @selector(endAudio),
          ^(id self) {
            objc_setAssociatedObject(self, &endedKey, @YES,
                                     OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            [objc_getAssociatedObject(self, &taskKey) finish];
          });
  replace(SFSpeechAudioBufferRecognitionRequest.class,
          @selector(appendAudioSampleBuffer:),
          ^(id self, CMSampleBufferRef buffer) {
            NSDictionary *recording = (__bridge NSDictionary *)CMGetAttachment(
                buffer, CFSTR("us.nimbusxr.axx.recording"), NULL);
            if (recording) {
              if (!objc_getAssociatedObject(self, &recordingKey))
                objc_setAssociatedObject(self, &recordingKey, recording,
                                         OBJC_ASSOCIATION_RETAIN_NONATOMIC);
              AXXSpeechTask *task = objc_getAssociatedObject(self, &taskKey);
              task.recording = task.recording ?: recording;
              task.stubError = nil;
              if ([(__bridge NSNumber *)CMGetAttachment(
                      buffer, CFSTR("us.nimbusxr.axx.recording-ended"), NULL)
                      boolValue]) {
                objc_setAssociatedObject(self, &endedKey, @YES,
                                         OBJC_ASSOCIATION_RETAIN_NONATOMIC);
                [task finish];
              }
              return;
            }
            // Do not let an unsupported input shape reach the real recognizer.
            NSError *err = failure(
                @"This speech adapter accepts PCM buffers and microphone-step "
                @"recordings; CMSampleBuffer input is not supported");
            objc_setAssociatedObject(self, &audioErrorKey, err,
                                     OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            AXXSpeechTask *task = objc_getAssociatedObject(self, &taskKey);
            task.stubError = err;
            [task finish];
          });
  id (^start)(
      SFSpeechRecognizer *, SFSpeechRecognitionRequest *,
      void (^)(SFSpeechRecognitionResult *,
               NSError *)) = ^id(SFSpeechRecognizer *self,
                                 SFSpeechRecognitionRequest *request,
                                 void (^handler)(SFSpeechRecognitionResult *,
                                                 NSError *)) {
    AXXSpeechTask *task = [AXXSpeechTask new];
    task.handler = handler;
    task.callbackQueue = self.queue ?: NSOperationQueue.mainQueue;
    task.samples =
        objc_getAssociatedObject(request, &audioKey) ?: [NSMutableArray new];
    task.stubError = objc_getAssociatedObject(request, &audioErrorKey);
    task.recording = objc_getAssociatedObject(request, &recordingKey);
    task.sampleRate =
        [objc_getAssociatedObject(request, &audioRateKey) integerValue];
    if (!task.sampleRate)
      task.sampleRate =
          [input(models[API("speech")])[@"audio"][@"sampleRate"] integerValue];
    objc_setAssociatedObject(request, &audioKey, task.samples,
                             OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    objc_setAssociatedObject(request, &taskKey, task,
                             OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    if ([request isKindOfClass:SFSpeechURLRecognitionRequest.class]) {
      NSError *err = nil;
      AVAudioFile *file = [[AVAudioFile alloc]
          initForReading:((SFSpeechURLRecognitionRequest *)request).URL
                   error:&err];
      if (file) {
        AVAudioPCMBuffer *buffer =
            [[AVAudioPCMBuffer alloc] initWithPCMFormat:file.processingFormat
                                          frameCapacity:4096];
        task.sampleRate = llround(file.processingFormat.sampleRate);
        while (file.framePosition < file.length) {
          if (![file readIntoBuffer:buffer error:&err] || !buffer.frameLength ||
              !appendPCM(task.samples, buffer)) {
            err = err
                      ?: failure(@"Cannot read the audio file; use a supported "
                                 @"PCM audio format");
            break;
          }
        }
      }
      task.stubError = err;
      [task finish];
    } else if (objc_getAssociatedObject(request, &endedKey)) {
      [task finish];
    } else {
      dispatch_async(dispatch_get_main_queue(), ^{
        queuedRecording(task);
      });
    }
    return task;
  };
  replace(SFSpeechRecognizer.class,
          @selector(recognitionTaskWithRequest:resultHandler:), start);
  replace(SFSpeechRecognizer.class,
          @selector(recognitionTaskWithRequest:delegate:),
          ^id(SFSpeechRecognizer *self, SFSpeechRecognitionRequest *request,
              id<SFSpeechRecognitionTaskDelegate> delegate) {
            __block SFSpeechRecognitionTask *task;
            task = start(self, request,
                         ^(SFSpeechRecognitionResult *result, NSError *err) {
                           if (result && [delegate respondsToSelector:@selector
                                                   (speechRecognitionTask:
                                                       didFinishRecognition:)])
                             [delegate speechRecognitionTask:task
                                        didFinishRecognition:result];
                           if ([delegate respondsToSelector:@selector
                                         (speechRecognitionTask:
                                             didFinishSuccessfully:)])
                             [delegate speechRecognitionTask:task
                                       didFinishSuccessfully:err == nil];
                         });
            return task;
          });
}

@protocol AXXFoundationLoader <NSObject>
+ (void)prepare;
@end

__attribute__((constructor)) static void initialize(void) {
#if TARGET_OS_SIMULATOR || TARGET_OS_OSX
  @autoreleasepool {
    const char *path = getenv("AXX_MODELS_DIR");
    if (!path)
      return;
    NSString *root = [NSString stringWithUTF8String:path];
    NSMutableDictionary *loaded = [NSMutableDictionary new];
    NSError *listError = nil;
    if (![NSFileManager.defaultManager contentsOfDirectoryAtPath:root
                                                           error:&listError] &&
        !([listError.domain isEqual:NSCocoaErrorDomain] &&
          listError.code == NSFileReadNoSuchFileError)) {
      NSLog(@"AXX-E0704: cannot read %@: %@", root, listError);
      abort();
    }
    NSDirectoryEnumerator *entries =
        [NSFileManager.defaultManager enumeratorAtPath:root];
    for (NSString *name in entries) {
      BOOL hidden = NO;
      for (NSString *component in name.pathComponents)
        if ([component hasPrefix:@"."]) {
          hidden = YES;
          break;
        }
      if (hidden)
        continue;
      if (![name.pathExtension.lowercaseString isEqual:@"axxmodel"])
        continue;
      NSData *data = [NSData
          dataWithContentsOfFile:[root stringByAppendingPathComponent:name]];
      NSDictionary *a = data ? [NSJSONSerialization JSONObjectWithData:data
                                                               options:0
                                                                 error:nil]
                             : nil;
      if (!a || [a[@"version"] intValue] != 1 || !a[@"api"] ||
          loaded[a[@"api"]]) {
        NSLog(@"AXX-E0704: invalid platform artifact %@", name);
        abort();
      }
      loaded[a[@"api"]] = a;
    }
    models = loaded;
    if (models[API("foundation-models")] || models[API("private-cloud-compute")]) {
      Class<AXXFoundationLoader> loader =
          NSClassFromString(@"AXXFoundationModelsLoader");
      if (!loader || ![loader respondsToSelector:@selector(prepare)]) {
        NSLog(@"AXX-E0704: Foundation Models adapter did not load");
        abort();
      }
      [loader prepare];
    }
    for (NSString *api in models)
      if ([api hasPrefix:API("vision.")]) {
        installVision();
        break;
      }
    if (getenv("AXX_MODELS_MICROPHONE") &&
        !strcmp(getenv("AXX_MODELS_MICROPHONE"), "1"))
      installMicrophone();
    if (models[API("speech")])
      installSpeech();
  }
#endif
}
