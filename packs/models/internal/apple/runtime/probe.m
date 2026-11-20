// Ordinary framework calls, compiled independently of the test adapter.
#import <Accelerate/Accelerate.h>
#import <ImageIO/ImageIO.h>
#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>
#import <Speech/Speech.h>
#import <Vision/Vision.h>
@interface AudioDelegate
    : NSObject <AVCaptureAudioDataOutputSampleBufferDelegate>
@property NSMutableData *heard;
@end
@implementation AudioDelegate
- (void)captureOutput:(AVCaptureOutput *)output
    didOutputSampleBuffer:(CMSampleBufferRef)sample
           fromConnection:(AVCaptureConnection *)connection {
  CMBlockBufferRef data = CMSampleBufferGetDataBuffer(sample);
  size_t length = CMBlockBufferGetDataLength(data);
  NSMutableData *part = [NSMutableData dataWithLength:length];
  if (CMBlockBufferCopyDataBytes(data, 0, length, part.mutableBytes) != noErr)
    abort();
  @synchronized(self.heard) {
    [self.heard appendData:part];
  }
}
@end
@interface RecorderDelegate : NSObject <AVAudioRecorderDelegate>
@property BOOL done, success;
@end
@implementation RecorderDelegate
- (void)audioRecorderDidFinishRecording:(AVAudioRecorder *)recorder
                           successfully:(BOOL)success {
  self.done = YES;
  self.success = success;
}
@end
static void runFor(double seconds) {
  NSDate *end = [NSDate dateWithTimeIntervalSinceNow:seconds];
  while (end.timeIntervalSinceNow > 0)
    [NSRunLoop.currentRunLoop
        runUntilDate:[NSDate dateWithTimeIntervalSinceNow:0.01]];
}
// hear runs until heard has four seconds of what it hears at this rate,
// however long a slow simulator takes to deliver them (30 seconds at most).
static void hear(NSMutableData *heard, double rate) {
  NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:30];
  for (;;) {
    NSUInteger have;
    @synchronized(heard) {
      have = heard.length;
    }
    if (have >= 4 * rate * sizeof(float) || deadline.timeIntervalSinceNow <= 0)
      return;
    runFor(0.05);
  }
}
static int capture(NSString *kind, NSURL *url) {
  NSError *error = nil;
  NSMutableData *heard = [NSMutableData new];
  if ([kind isEqual:@"capture-session"]) {
    // Each part's time and each failure's reason go to the test's log: on a
    // slow Mac a session took 24 seconds where it takes 5, and one that
    // failed there said nothing (simctl gives the test no exit status).
    NSDate *began = [NSDate date];
    AVCaptureDevice *device =
        [AVCaptureDevice devicesWithMediaType:AVMediaTypeAudio].firstObject;
    if (!device) {
      printf("no audio capture device\n");
      return 77;
    }
    AVCaptureDeviceInput *input =
        [AVCaptureDeviceInput deviceInputWithDevice:device error:&error];
    if (!input) {
      fprintf(stderr, "%s\n", error.description.UTF8String);
      return 24;
    }
    AVCaptureSession *session = [AVCaptureSession new];
    for (NSNotificationName name in @[
           AVCaptureSessionRuntimeErrorNotification,
           AVCaptureSessionWasInterruptedNotification
         ])
      [NSNotificationCenter.defaultCenter
          addObserverForName:name
                      object:session
                       queue:nil
                  usingBlock:^(NSNotification *note) {
                    fprintf(stderr, "capture session: %s after %.1fs: %s\n",
                            note.name.UTF8String, -began.timeIntervalSinceNow,
                            note.userInfo.description.UTF8String);
                  }];
    [session addInput:input];
    AVCaptureAudioDataOutput *output = [AVCaptureAudioDataOutput new];
    [session addOutput:output];
    AudioDelegate *delegate = [AudioDelegate new];
    delegate.heard = heard;
    [output setSampleBufferDelegate:delegate
                              queue:dispatch_queue_create(
                                        "probe.audio", DISPATCH_QUEUE_SERIAL)];
    fprintf(stderr, "capture session: set up after %.1fs\n",
            -began.timeIntervalSinceNow);
    [session startRunning];
    fprintf(stderr, "capture session: started after %.1fs\n",
            -began.timeIntervalSinceNow);
    if (!session.running) {
      fprintf(stderr, "capture session: it does not run once started\n");
      return 25;
    }
    hear(heard, 48000);
    @synchronized(heard) {
      fprintf(stderr, "capture session: heard %lu frames after %.1fs\n",
              (unsigned long)(heard.length / sizeof(float)),
              -began.timeIntervalSinceNow);
    }
    [session stopRunning];
    fprintf(stderr, "capture session: stopped after %.1fs\n",
            -began.timeIntervalSinceNow);
    if (session.running) {
      fprintf(stderr, "capture session: it still runs once stopped\n");
      return 26;
    }
    @synchronized(heard) {
      if (![heard writeToURL:url options:NSDataWritingAtomic error:&error]) {
        fprintf(stderr, "capture session: cannot write what it heard: %s\n",
                error.description.UTF8String);
        return 27;
      }
    }
    printf("captured 48000\n");
    return 0;
  }
  if ([kind hasPrefix:@"capture-engine"] || [kind isEqual:@"capture-graph"]) {
    AVAudioEngine *engine = [AVAudioEngine new];
    AVAudioInputNode *input = engine.inputNode;
    AVAudioFormat *fixed = [input outputFormatForBus:0];
    if (fixed.sampleRate != 48000 || fixed.channelCount != 1 ||
        fixed.commonFormat != AVAudioPCMFormatFloat32 || fixed.interleaved)
      return 10;
    AVAudioFormat *format = [[AVAudioFormat alloc]
        initStandardFormatWithSampleRate:[kind hasSuffix:@"44100"] ? 44100
                                                                   : 48000
                                channels:1];
    if ([kind isEqual:@"capture-graph"]) {
      [engine connect:input to:engine.mainMixerNode format:format];
      if (![engine
              enableManualRenderingMode:AVAudioEngineManualRenderingModeOffline
                                 format:format
                      maximumFrameCount:1024
                                  error:&error])
        return 11;
      [engine prepare];
      if (![engine startAndReturnError:&error])
        return 12;
      AVAudioPCMBuffer *buffer =
          [[AVAudioPCMBuffer alloc] initWithPCMFormat:format
                                        frameCapacity:1024];
      while (heard.length < 4 * format.sampleRate * sizeof(float)) {
        if ([engine renderOffline:1024 toBuffer:buffer error:&error] !=
            AVAudioEngineManualRenderingStatusSuccess)
          return 13;
        [heard appendBytes:buffer.floatChannelData[0]
                    length:buffer.frameLength * sizeof(float)];
      }
    } else {
      [input installTapOnBus:0
                  bufferSize:1024
                      format:format
                       block:^(AVAudioPCMBuffer *buffer, AVAudioTime *time) {
                         @synchronized(heard) {
                           [heard
                               appendBytes:buffer.floatChannelData[0]
                                    length:buffer.frameLength * sizeof(float)];
                         }
                       }];
      [engine prepare];
      if (![engine startAndReturnError:&error] || !engine.running)
        return 14;
      hear(heard, format.sampleRate);
    }
    [engine stop];
    if (engine.running)
      return 15;
    @synchronized(heard) {
      if (![heard writeToURL:url options:NSDataWritingAtomic error:&error])
        return 16;
    }
    printf("captured %.0f\n", format.sampleRate);
    return 0;
  }
  NSDictionary *settings = @{
    AVFormatIDKey : @(kAudioFormatLinearPCM),
    AVSampleRateKey : @44100,
    AVNumberOfChannelsKey : @1,
    AVLinearPCMBitDepthKey : @32,
    AVLinearPCMIsFloatKey : @YES,
    AVLinearPCMIsNonInterleaved : @NO
  };
  NSURL *fileURL = [url URLByAppendingPathExtension:@"caf"];
  AVAudioRecorder *recorder = [[AVAudioRecorder alloc] initWithURL:fileURL
                                                          settings:settings
                                                             error:&error];
  RecorderDelegate *delegate = [RecorderDelegate new];
  recorder.delegate = delegate;
  recorder.meteringEnabled = YES;
  if (!recorder || ![recorder prepareToRecord])
    return 17;
  BOOL duration = [kind isEqual:@"capture-recorder-duration"];
  if (!(duration ? [recorder recordForDuration:4] : [recorder record]) ||
      !recorder.recording)
    return 18;
  double peak = -160;
  for (int i = 0; i < 160; i++) {
    runFor(0.025);
    [recorder updateMeters];
    peak = fmax(peak, [recorder peakPowerForChannel:0]);
    if (i == 35 && !duration) {
      [recorder pause];
      if (recorder.recording)
        return 19;
      runFor(0.15);
      if (![recorder record])
        return 20;
    }
  }
  if (!duration)
    [recorder stop];
  runFor(0.2);
  if (!delegate.done || !delegate.success || recorder.recording || peak < -25)
    return 21;
  AVAudioFile *file = [[AVAudioFile alloc] initForReading:fileURL error:&error];
  AVAudioPCMBuffer *buffer =
      [[AVAudioPCMBuffer alloc] initWithPCMFormat:file.processingFormat
                                    frameCapacity:4096];
  do {
    if (![file readIntoBuffer:buffer error:&error]) {
      fprintf(stderr, "read %s: %s (length %lld, read %lu)\n",
              fileURL.path.UTF8String, error.description.UTF8String,
              file.length, (unsigned long)heard.length / sizeof(float));
      return 22;
    }
    [heard appendBytes:buffer.floatChannelData[0]
                length:buffer.frameLength * sizeof(float)];
  } while (buffer.frameLength && file.framePosition < file.length);
  if (![heard writeToURL:url options:NSDataWritingAtomic error:&error])
    return 23;
  printf("captured %.0f\n", file.processingFormat.sampleRate);
  return 0;
}
// frame is the picture at url as a camera hands a frame over: a pixel buffer
// of a format, BGRA or the two-plane 4:2:0 a video output gives (written with
// BT.601, as axx's camera writes it). Turned, the picture lies in the buffer a
// quarter turn against the clock: an orientation of right shows it upright.
static CVPixelBufferRef frame(NSURL *url, OSType format, BOOL turned) {
  CGImageSourceRef source =
      CGImageSourceCreateWithURL((__bridge CFURLRef)url, NULL);
  CGImageRef image =
      source ? CGImageSourceCreateImageAtIndex(source, 0, NULL) : NULL;
  if (source)
    CFRelease(source);
  if (!image)
    return NULL;
  size_t w = CGImageGetWidth(image), h = CGImageGetHeight(image);
  uint8_t *upright = calloc(w * h, 4);
  CGColorSpaceRef color = CGColorSpaceCreateDeviceRGB();
  CGContextRef ctx = CGBitmapContextCreate(
      upright, w, h, 8, w * 4, color,
      kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
  CGContextDrawImage(ctx, CGRectMake(0, 0, w, h), image);
  CGContextRelease(ctx);
  CGColorSpaceRelease(color);
  CGImageRelease(image);
  size_t bw = turned ? h : w, bh = turned ? w : h;
  NSDictionary *attrs = @{(id)kCVPixelBufferIOSurfacePropertiesKey : @{}};
  CVPixelBufferRef bgra = NULL;
  CVPixelBufferCreate(NULL, bw, bh, kCVPixelFormatType_32BGRA,
                      (__bridge CFDictionaryRef)attrs, &bgra);
  CVPixelBufferLockBaseAddress(bgra, 0);
  uint8_t *base = CVPixelBufferGetBaseAddress(bgra);
  size_t row = CVPixelBufferGetBytesPerRow(bgra);
  for (size_t by = 0; by < bh; by++)
    for (size_t bx = 0; bx < bw; bx++) {
      size_t x = turned ? bh - 1 - by : bx, y = turned ? bx : by;
      memcpy(base + by * row + bx * 4, upright + (y * w + x) * 4, 4);
    }
  free(upright);
  if (format == kCVPixelFormatType_32BGRA) {
    CVPixelBufferUnlockBaseAddress(bgra, 0);
    return bgra;
  }
  BOOL full = format == kCVPixelFormatType_420YpCbCr8BiPlanarFullRange;
  vImage_YpCbCrPixelRange video = {16, 128, 235, 240, 235, 16, 240, 16},
                          whole = {0, 128, 255, 255, 255, 1, 255, 0};
  vImage_ARGBToYpCbCr conversion;
  vImageConvert_ARGBToYpCbCr_GenerateConversion(
      kvImage_ARGBToYpCbCrMatrix_ITU_R_601_4, full ? &whole : &video,
      &conversion, kvImageARGB8888, kvImage420Yp8_CbCr8, kvImageNoFlags);
  CVPixelBufferRef out = NULL;
  CVPixelBufferCreate(NULL, bw, bh, format, (__bridge CFDictionaryRef)attrs,
                      &out);
  CVPixelBufferLockBaseAddress(out, 0);
  vImage_Buffer from = {base, bh, bw, row};
  vImage_Buffer luma = {CVPixelBufferGetBaseAddressOfPlane(out, 0), bh, bw,
                        CVPixelBufferGetBytesPerRowOfPlane(out, 0)};
  vImage_Buffer chroma = {CVPixelBufferGetBaseAddressOfPlane(out, 1), bh / 2,
                          bw / 2, CVPixelBufferGetBytesPerRowOfPlane(out, 1)};
  const uint8_t bgraToARGB[4] = {3, 2, 1, 0};
  vImageConvert_ARGB8888To420Yp8_CbCr8(&from, &luma, &chroma, &conversion,
                                       bgraToARGB, kvImageNoFlags);
  CVPixelBufferUnlockBaseAddress(out, 0);
  CVPixelBufferUnlockBaseAddress(bgra, 0);
  CVPixelBufferRelease(bgra);
  return out;
}

// framed is a Vision request handler of the picture at url as a frame of the
// kind: frame-bgra, frame-420v and frame-420f (a pixel buffer of that format),
// frame-sample (a sample buffer, as a video output's delegate gets it), and
// frame-420v-right and frame-bgra-unturned (the picture turned in its buffer,
// the handler told so, or not).
static VNImageRequestHandler *framed(NSString *kind, NSURL *url) {
  OSType format = [kind containsString:@"bgra"]
                      ? kCVPixelFormatType_32BGRA
                      : ([kind containsString:@"420f"]
                             ? kCVPixelFormatType_420YpCbCr8BiPlanarFullRange
                             : kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange);
  BOOL turned =
      [kind hasSuffix:@"-right"] || [kind hasSuffix:@"-unturned"];
  CVPixelBufferRef pixels = frame(url, format, turned);
  if (!pixels)
    return nil;
  VNImageRequestHandler *handler;
  if ([kind isEqual:@"frame-sample"]) {
    CMVideoFormatDescriptionRef description = NULL;
    CMVideoFormatDescriptionCreateForImageBuffer(NULL, pixels, &description);
    CMSampleTimingInfo timing = {CMTimeMake(1, 30), kCMTimeZero,
                                 kCMTimeInvalid};
    CMSampleBufferRef sample = NULL;
    CMSampleBufferCreateReadyWithImageBuffer(NULL, pixels, description,
                                             &timing, &sample);
    handler = [[VNImageRequestHandler alloc] initWithCMSampleBuffer:sample
                                                            options:@{}];
    if (description)
      CFRelease(description);
    if (sample)
      CFRelease(sample);
  } else if ([kind hasSuffix:@"-right"]) {
    handler = [[VNImageRequestHandler alloc]
        initWithCVPixelBuffer:pixels
                  orientation:kCGImagePropertyOrientationRight
                      options:@{}];
  } else {
    handler = [[VNImageRequestHandler alloc] initWithCVPixelBuffer:pixels
                                                           options:@{}];
  }
  CVPixelBufferRelease(pixels);
  return handler;
}

int main(int argc, const char **argv) {
  @autoreleasepool {
    if (argc != 3)
      return 2;
    NSString *kind = @(argv[1]);
    NSURL *url = [NSURL fileURLWithPath:@(argv[2])];
    if ([kind isEqual:@"capture-devices"]) {
      NSArray *devices =
          [AVCaptureDevice devicesWithMediaType:AVMediaTypeAudio];
      NSError *error = nil;
      AVCaptureDeviceInput *input =
          devices.count ? [AVCaptureDeviceInput deviceInputWithDevice:devices[0]
                                                                error:&error]
                        : nil;
      printf("audio devices %lu input %s\n", (unsigned long)devices.count,
             input ? "available" : "unavailable");
      if (error)
        printf("%s\n", error.description.UTF8String);
      return 0;
    }
    // waiting does nothing for a minute: a test has the simulator end this run,
    // by what it allows the app, before the run it is after.
    if ([kind isEqual:@"waiting"]) {
      printf("waiting\n");
      fflush(stdout);
      runFor(60);
      return 0;
    }
    if ([kind hasPrefix:@"capture-"])
      return capture(kind, url);
    if (![kind hasPrefix:@"speech"]) {
      VNRequest *request;
      if ([kind isEqual:@"text"])
        request = [VNRecognizeTextRequest new];
      else if ([kind isEqual:@"humans"])
        request = [VNDetectHumanRectanglesRequest new];
      else if ([kind isEqual:@"animals"])
        request = [VNRecognizeAnimalsRequest new];
      else if ([kind isEqual:@"mask"])
        request = [VNGeneratePersonSegmentationRequest new];
      else
        request = [VNClassifyImageRequest new];
      NSError *err = nil;
      BOOL ok =
          [kind isEqual:@"sequence"]
              ? [[VNSequenceRequestHandler new] performRequests:@[ request ]
                                                     onImageURL:url
                                                          error:&err]
              : [([kind hasPrefix:@"frame-"]
                      ? framed(kind, url)
                      : [[VNImageRequestHandler alloc] initWithURL:url
                                                           options:@{}])
                    performRequests:@[ request ]
                              error:&err];
      if (!ok) {
        fprintf(stderr, "%s\n", err.description.UTF8String);
        return 1;
      }
      if ([kind isEqual:@"text"])
        printf("%s\n", [((VNRecognizedTextObservation *)
                             request.results.firstObject) topCandidates:1]
                           .firstObject.string.UTF8String);
      else if ([kind isEqual:@"humans"] || [kind isEqual:@"animals"]) {
        VNDetectedObjectObservation *object = request.results.firstObject;
        CGRect box = object.boundingBox;
        printf("box %.2f %.2f %.2f %.2f\n", box.origin.x, box.origin.y,
               box.size.width, box.size.height);
      } else if ([kind isEqual:@"mask"]) {
        CVPixelBufferRef mask =
            ((VNPixelBufferObservation *)request.results.firstObject)
                .pixelBuffer;
        CVPixelBufferLockBaseAddress(mask, kCVPixelBufferLock_ReadOnly);
        printf("mask %zu %zu %d\n", CVPixelBufferGetWidth(mask),
               CVPixelBufferGetHeight(mask),
               *(unsigned char *)CVPixelBufferGetBaseAddress(mask));
        CVPixelBufferUnlockBaseAddress(mask, kCVPixelBufferLock_ReadOnly);
      } else
        printf("%s\n",
               ((VNClassificationObservation *)request.results.firstObject)
                   .identifier.UTF8String);
    } else {
      SFSpeechRecognizer *recognizer = [[SFSpeechRecognizer alloc]
          initWithLocale:[NSLocale localeWithLocaleIdentifier:@"en-US"]];
      __block BOOL done = NO;
      __block int status = 0;
      SFSpeechRecognitionRequest *request =
          [kind isEqual:@"speech"]
              ? [[SFSpeechURLRecognitionRequest alloc] initWithURL:url]
              : [SFSpeechAudioBufferRecognitionRequest new];
      AVAudioEngine *engine = nil;
      if ([kind hasPrefix:@"speech-engine"]) {
        engine = [AVAudioEngine new];
        [engine.inputNode
            installTapOnBus:0
                 bufferSize:1024
                     format:[engine.inputNode outputFormatForBus:0]
                      block:^(AVAudioPCMBuffer *buffer, AVAudioTime *time) {
                        [(SFSpeechAudioBufferRecognitionRequest *)request
                            appendAudioPCMBuffer:buffer];
                      }];
      }
      NSDate *started = [NSDate date];
      SFSpeechRecognitionTask *task = [recognizer
          recognitionTaskWithRequest:request
                       resultHandler:^(SFSpeechRecognitionResult *result,
                                       NSError *err) {
                         if (err && [kind isEqual:@"speech-cancel"] &&
                             err.code == NSUserCancelledError)
                           printf("cancelled\n");
                         else if (err) {
                           fprintf(stderr, "%s\n", err.description.UTF8String);
                           status = 1;
                         } else {
                           if ([kind isEqual:@"speech-engine"] &&
                               (!result.final ||
                                -started.timeIntervalSinceNow < 2.5))
                             status = 5;
                           printf("%s\n", result.bestTranscription
                                              .formattedString.UTF8String);
                         }
                         done = YES;
                       }];
      if (engine) {
        // The recognition must leave the queued recording for the pending tap.
        runFor(0.15);
        NSError *error = nil;
        if (done || ![engine startAndReturnError:&error])
          return 6;
        if ([kind isEqual:@"speech-engine-ended"]) {
          runFor(0.6);
          [(SFSpeechAudioBufferRecognitionRequest *)request endAudio];
        }
      }
      if ([kind isEqual:@"speech-cancel"])
        [task cancel];
      if ([kind isEqual:@"speech-queued"]) {
        // The app can feed its hardware's PCM format while the queued test
        // recording supplies the recognizer's deterministic input.
        AVAudioPCMBuffer *live = [[AVAudioPCMBuffer alloc]
            initWithPCMFormat:[[AVAudioFormat alloc]
                                  initStandardFormatWithSampleRate:48000
                                                          channels:1]
                frameCapacity:64];
        live.frameLength = 64;
        [(SFSpeechAudioBufferRecognitionRequest *)request
            appendAudioPCMBuffer:live];
      }
      if ([kind isEqual:@"speech-buffer"]) {
        NSError *err = nil;
        AVAudioFile *file = [[AVAudioFile alloc] initForReading:url error:&err];
        AVAudioPCMBuffer *buffer = [[AVAudioPCMBuffer alloc]
            initWithPCMFormat:file.processingFormat
                frameCapacity:(AVAudioFrameCount)file.length];
        if (![file readIntoBuffer:buffer error:&err])
          return 4;
        [(SFSpeechAudioBufferRecognitionRequest *)request
            appendAudioPCMBuffer:buffer];
        [(SFSpeechAudioBufferRecognitionRequest *)request endAudio];
      }
      // The recording plays in real time, then the recognizer answers: however
      // long a slow machine takes (30 seconds at most).
      NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:30];
      while (!done && deadline.timeIntervalSinceNow > 0)
        [NSRunLoop.currentRunLoop
            runUntilDate:[NSDate dateWithTimeIntervalSinceNow:0.01]];
      (void)task;
      [engine stop];
      return done ? status : 3;
    }
  }
  return 0;
}
