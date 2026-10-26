// Public AVFoundation capture in a Debug simulator process. The queue is
// shared with Speech: one listener claims each recording, once.
#include <fcntl.h>
static char captureKey, capturedAudioKey;
static NSHashTable *captures;
static NSDictionary *takeRecording(NSError **error) {
  NSString *root = [[NSString stringWithUTF8String:getenv("AXX_MODELS_DIR")]
      stringByAppendingPathComponent:@".microphone"];
  @synchronized(models) {
    NSArray *names =
        [[NSFileManager.defaultManager contentsOfDirectoryAtPath:root error:nil]
            sortedArrayUsingSelector:@selector(compare:)];
    for (NSString *name in names) {
      if (![name.pathExtension isEqual:@"json"])
        continue;
      NSString *path = [root stringByAppendingPathComponent:name],
               *taken = [path stringByAppendingString:@".taken"];
      if ([NSFileManager.defaultManager fileExistsAtPath:taken])
        continue;
      NSData *data = [NSData dataWithContentsOfFile:path];
      NSDictionary *recording =
          data ? [NSJSONSerialization JSONObjectWithData:data
                                                 options:0
                                                   error:nil]
               : nil;
      if (!recording)
        continue; // the file step may still be writing
      if (![recording[@"samples"] isKindOfClass:NSArray.class] ||
          [recording[@"rate"] integerValue] <= 0) {
        if (error)
          *error = failure(@"Invalid microphone recording; queue a WAV file "
                           @"with the microphone step");
        return nil;
      }
      int fd = open(taken.fileSystemRepresentation, O_WRONLY | O_CREAT | O_EXCL,
                    0600);
      if (fd < 0) {
        if (errno == EEXIST)
          continue;
        if (error)
          *error = failure(@"Cannot claim the microphone recording; make the "
                           @"app's axx-models folder writable");
        return nil;
      }
      close(fd);
      return recording;
    }
  }
  return nil;
}
@interface AXXAudioChunk : NSObject
@property NSDictionary *recording;
@property NSMutableDictionary *converted;
@property NSInteger position, playingAt, count, rate;
@property BOOL ended;
- (AVAudioPCMBuffer *)buffer:(AVAudioFormat *)format;
@end
@implementation AXXAudioChunk
- (AVAudioPCMBuffer *)buffer:(AVAudioFormat *)format {
  NSInteger rate = llround(format.sampleRate);
  NSInteger start = llround((double)self.position * rate / self.rate);
  NSInteger count =
      llround((double)(self.position + self.count) * rate / self.rate) - start;
  AVAudioPCMBuffer *buffer =
      [[AVAudioPCMBuffer alloc] initWithPCMFormat:format
                                    frameCapacity:MAX(count, 1)];
  buffer.frameLength = (AVAudioFrameCount)count;
  NSData *data = self.converted[@(rate)];
  if (!data && self.recording) {
    data = resample(audioData(self.recording[@"samples"]),
                    [self.recording[@"rate"] integerValue], rate);
    self.converted[@(rate)] = data;
  }
  const double *v = data.bytes;
  NSInteger n = data.length / sizeof(double),
            offset = start - llround((double)self.playingAt * rate / self.rate);
  for (NSInteger i = 0; i < count; i++) {
    double value = offset + i >= 0 && offset + i < n ? v[offset + i] : 0;
    for (AVAudioChannelCount c = 0; c < format.channelCount; c++) {
      NSInteger channel = format.interleaved ? 0 : c,
                at = format.interleaved ? i * format.channelCount + c : i;
      switch (format.commonFormat) {
      case AVAudioPCMFormatFloat32:
        buffer.floatChannelData[channel][at] = value;
        break;
      case AVAudioPCMFormatFloat64:
        ((double *)buffer.mutableAudioBufferList->mBuffers[channel].mData)[at] =
            value;
        break;
      case AVAudioPCMFormatInt16:
        buffer.int16ChannelData[channel][at] =
            llround(fmax(-1, fmin(value, 1)) * 32767);
        break;
      case AVAudioPCMFormatInt32:
        buffer.int32ChannelData[channel][at] =
            llround(fmax(-1, fmin(value, 1)) * 2147483647);
        break;
      default:
        break;
      }
    }
  }
  objc_setAssociatedObject(buffer, &capturedAudioKey, self,
                           OBJC_ASSOCIATION_RETAIN_NONATOMIC);
  return buffer;
}
@end
@interface AXXHearing : NSObject
@property NSInteger rate, position, playingAt, length;
@property NSDictionary *playing;
@property NSMutableDictionary *converted;
@property NSError *error;
- (void)prime;
- (AXXAudioChunk *)next:(NSInteger)count;
@end
@implementation AXXHearing
- (void)prime {
  if (self.playing && self.position < self.playingAt + self.length)
    return;
  NSError *error = nil;
  self.playing = takeRecording(&error);
  self.error = error;
  self.converted = [NSMutableDictionary new];
  self.playingAt = self.position;
  self.length = self.playing
                    ? llround((double)[self.playing[@"samples"] count] *
                              self.rate / [self.playing[@"rate"] integerValue])
                    : 0;
}
- (AXXAudioChunk *)next:(NSInteger)count {
  [self prime];
  AXXAudioChunk *chunk = [AXXAudioChunk new];
  chunk.recording = self.playing;
  chunk.converted = self.converted;
  chunk.position = self.position;
  chunk.playingAt = self.playingAt;
  chunk.count = count;
  chunk.rate = self.rate;
  self.position += count;
  chunk.ended = self.playing && self.position >= self.playingAt + self.length;
  return chunk;
}
@end
@interface AXXCapture : NSObject
@property AXXHearing *hearing;
@property BOOL running, pending;
@property dispatch_source_t timer;
- (void)stopClock;
@end
@implementation AXXCapture
- (void)stopClock {
  if (self.timer)
    dispatch_source_cancel(self.timer);
  self.timer = nil;
  self.running = NO;
}
- (void)dealloc {
  if (_timer)
    dispatch_source_cancel(_timer);
}
@end
static void registerCapture(AXXCapture *capture) {
  @synchronized(models) {
    if (!captures)
      captures = [NSHashTable weakObjectsHashTable];
    [captures addObject:capture];
  }
}
static BOOL capturePending(void) {
  NSArray *all;
  @synchronized(models) {
    all = captures.allObjects;
  }
  for (AXXCapture *capture in all)
    @synchronized(capture) {
      if (capture.running || capture.pending)
        return YES;
    }
  return NO;
}
static void captureClock(AXXCapture *capture, void (^tick)(void)) {
  dispatch_queue_t queue =
      dispatch_queue_create("axx.microphone", DISPATCH_QUEUE_SERIAL);
  capture.timer =
      dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, queue);
  dispatch_source_set_timer(
      capture.timer, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_MSEC),
      10 * NSEC_PER_MSEC, NSEC_PER_MSEC);
  dispatch_source_set_event_handler(capture.timer, ^{
    @autoreleasepool {
      tick();
    }
  });
  dispatch_resume(capture.timer);
}
static AVAudioFormat *microphoneFormat(void) {
  return [[AVAudioFormat alloc] initStandardFormatWithSampleRate:48000
                                                        channels:1];
}
@interface AXXTap : NSObject
@property AVAudioFormat *format;
@property(copy) AVAudioNodeTapBlock block;
@end
@implementation AXXTap
@end
@interface AXXEngine : AXXCapture
@property(weak) AVAudioEngine *engine;
@property NSMutableDictionary<NSNumber *, AXXTap *> *taps;
@property AVAudioSourceNode *source;
@property BOOL connected, rendersOutput;
- (AXXAudioChunk *)render:(NSInteger)count;
@end
@implementation AXXEngine
- (AXXAudioChunk *)render:(NSInteger)count {
  AXXAudioChunk *chunk = [self.hearing next:count];
  if (self.hearing.error) {
    NSLog(@"AXX-E0704: %@", self.hearing.error.localizedDescription);
    [self stopClock];
    return chunk;
  }
  for (AXXTap *tap in self.taps.allValues) {
    AVAudioPCMBuffer *buffer = [chunk buffer:tap.format];
    tap.block(buffer,
              [AVAudioTime
                  timeWithSampleTime:llround((double)chunk.position *
                                             tap.format.sampleRate / chunk.rate)
                              atRate:tap.format.sampleRate]);
  }
  return chunk;
}
@end
static AXXEngine *engineCapture(AVAudioEngine *engine) {
  if (!engine)
    return nil;
  @synchronized(engine) {
    AXXEngine *state = objc_getAssociatedObject(engine, &captureKey);
    if (!state) {
      state = [AXXEngine new];
      state.engine = engine;
      state.taps = [NSMutableDictionary new];
      objc_setAssociatedObject(engine, &captureKey, state,
                               OBJC_ASSOCIATION_RETAIN_NONATOMIC);
      registerCapture(state);
    }
    return state;
  }
}
// Getting the real inputNode already opens the host input device. Supply a
// real source node instead, using only its public initializer, even on a
// simulator whose host has no microphone. It implements the input's node API.
@interface AXXInputNode : AVAudioSourceNode
@end
@implementation AXXInputNode
- (AVAudioFormat *)outputFormatForBus:(AVAudioNodeBus)bus {
  return microphoneFormat();
}
- (AVAudioFormat *)inputFormatForBus:(AVAudioNodeBus)bus {
  return microphoneFormat();
}
- (void)installTapOnBus:(AVAudioNodeBus)bus
             bufferSize:(AVAudioFrameCount)size
                 format:(AVAudioFormat *)format
                  block:(AVAudioNodeTapBlock)block {
  AXXEngine *state = engineCapture(self.engine);
  @synchronized(state) {
    AXXTap *tap = [AXXTap new];
    tap.format = format ?: microphoneFormat();
    tap.block = block;
    state.taps[@(bus)] = tap;
    state.pending = YES;
  }
}
- (void)removeTapOnBus:(AVAudioNodeBus)bus {
  AXXEngine *state = engineCapture(self.engine);
  @synchronized(state) {
    [state.taps removeObjectForKey:@(bus)];
    state.pending = state.connected || state.taps.count > 0;
  }
}
@end
// Connections use a public AVAudioSourceNode. Taps alone need no hardware
// render graph, so a clock supplies their frames without starting real input.
static AVAudioNode *microphoneSource(AVAudioEngine *engine, AVAudioNode *node) {
  if (node && ![node isKindOfClass:AXXInputNode.class])
    return node;
  AXXEngine *state = engineCapture(engine);
  @synchronized(state) {
    if (node)
      state.pending = state.connected = YES;
    if (!state.source) {
      __weak AXXEngine *weak = state;
      state.source = [[AXXInputNode alloc]
          initWithFormat:microphoneFormat()
             renderBlock:^OSStatus(BOOL *silent, const AudioTimeStamp *time,
                                   AVAudioFrameCount count,
                                   AudioBufferList *buffers) {
               AXXEngine *s = weak;
               @synchronized(s) {
                 if (!s.running) {
                   for (UInt32 i = 0; i < buffers->mNumberBuffers; i++)
                     memset(buffers->mBuffers[i].mData, 0,
                            buffers->mBuffers[i].mDataByteSize);
                   *silent = YES;
                   return noErr;
                 }
                 AXXAudioChunk *chunk = [s render:count];
                 AVAudioPCMBuffer *buffer = [chunk buffer:microphoneFormat()];
                 for (UInt32 i = 0; i < buffers->mNumberBuffers; i++)
                   memcpy(buffers->mBuffers[i].mData,
                          buffer.floatChannelData[0],
                          MIN(buffers->mBuffers[i].mDataByteSize,
                              count * sizeof(float)));
                 *silent = chunk.recording == nil;
               }
               return noErr;
             }];
      [engine attachNode:state.source];
    }
    return state.source;
  }
}
static BOOL rendersOutput(AVAudioEngine *engine, AXXEngine *state) {
  if (state.connected)
    return YES;
  for (AVAudioNode *node in engine.attachedNodes) {
    if (node == state.source || [node isKindOfClass:AVAudioInputNode.class])
      continue;
    for (AVAudioNodeBus bus = 0; bus < node.numberOfOutputs; bus++)
      if ([engine outputConnectionPointsForNode:node outputBus:bus].count)
        return YES;
  }
  return NO;
}
static void installEngine(void) {
  replace(AVAudioEngine.class, @selector(inputNode),
          ^id(AVAudioEngine *engine) {
            return microphoneSource(engine, nil);
          });
  __block IMP connect = NULL, busConnect = NULL, points = NULL, prepare = NULL,
              start = NULL, running = NULL;
  connect = replace(AVAudioEngine.class, @selector(connect:to:format:),
                    ^(AVAudioEngine *engine, AVAudioNode *from, AVAudioNode *to,
                      AVAudioFormat *format) {
                      ((void (*)(id, SEL, id, id, id))connect)(
                          engine, @selector(connect:to:format:),
                          microphoneSource(engine, from), to, format);
                    });
  busConnect = replace(
      AVAudioEngine.class, @selector(connect:to:fromBus:toBus:format:),
      ^(AVAudioEngine *engine, AVAudioNode *from, AVAudioNode *to,
        AVAudioNodeBus fromBus, AVAudioNodeBus toBus, AVAudioFormat *format) {
        ((void (*)(id, SEL, id, id, AVAudioNodeBus, AVAudioNodeBus,
                   id))busConnect)(
            engine, @selector(connect:to:fromBus:toBus:format:),
            microphoneSource(engine, from), to, fromBus, toBus, format);
      });
  points = replace(
      AVAudioEngine.class,
      @selector(connect:toConnectionPoints:fromBus:format:),
      ^(AVAudioEngine *engine, AVAudioNode *from, NSArray *destinations,
        AVAudioNodeBus bus, AVAudioFormat *format) {
        ((void (*)(id, SEL, id, id, AVAudioNodeBus, id))points)(
            engine, @selector(connect:toConnectionPoints:fromBus:format:),
            microphoneSource(engine, from), destinations, bus, format);
      });
  prepare = replace(
      AVAudioEngine.class, @selector(prepare), ^(AVAudioEngine *engine) {
        AXXEngine *state = objc_getAssociatedObject(engine, &captureKey);
        if (!state.pending || rendersOutput(engine, state))
          ((void (*)(id, SEL))prepare)(engine, @selector(prepare));
      });
  start = replace(AVAudioEngine.class, @selector(startAndReturnError:),
                  ^BOOL(AVAudioEngine *engine, NSError **error) {
                    AXXEngine *state =
                        objc_getAssociatedObject(engine, &captureKey);
                    if (!state.pending)
                      return ((BOOL (*)(id, SEL, NSError **))start)(
                          engine, @selector(startAndReturnError:), error);
                    @synchronized(state) {
                      if (state.running)
                        return YES;
                      state.rendersOutput = rendersOutput(engine, state);
                      state.hearing = [AXXHearing new];
                      state.hearing.rate = 48000;
                      [state.hearing prime];
                      if (state.hearing.error) {
                        if (error)
                          *error = state.hearing.error;
                        return NO;
                      }
                      state.running = YES;
                      if (!state.connected) {
                        __weak AXXEngine *weak = state;
                        captureClock(state, ^{
                          AXXEngine *s = weak;
                          @synchronized(s) {
                            if (s.running)
                              [s render:480];
                          }
                        });
                      }
                      if (!state.rendersOutput)
                        return YES;
                    }
                    BOOL ok = ((BOOL (*)(id, SEL, NSError **))start)(
                        engine, @selector(startAndReturnError:), error);
                    if (!ok)
                      @synchronized(state) {
                        [state stopClock];
                      }
                    return ok;
                  });
  running = replace(
      AVAudioEngine.class, @selector(isRunning), ^BOOL(AVAudioEngine *engine) {
        AXXEngine *state = objc_getAssociatedObject(engine, &captureKey);
        if (state.pending && !state.rendersOutput)
          return state.running;
        return ((BOOL (*)(id, SEL))running)(engine, @selector(isRunning));
      });
  for (NSString *name in @[ @"stop", @"pause", @"reset" ]) {
    SEL sel = NSSelectorFromString(name);
    __block IMP old = NULL;
    old = replace(AVAudioEngine.class, sel, ^(AVAudioEngine *engine) {
      AXXEngine *state = objc_getAssociatedObject(engine, &captureKey);
      @synchronized(state) {
        [state stopClock];
      }
      if (!state.pending || state.rendersOutput)
        ((void (*)(id, SEL))old)(engine, sel);
    });
  }
}

@interface AXXRecorder : AXXCapture
@property(weak) AVAudioRecorder *recorder;
@property(nonatomic) AVAudioFile *file;
@property double started, elapsed, duration, peak, average;
@property NSError *error;
@property NSDictionary *settings;
- (void)catchUp;
- (void)finish;
@end
@implementation AXXRecorder
- (void)catchUp {
  if (!self.running)
    return;
  double elapsed =
      self.elapsed + NSProcessInfo.processInfo.systemUptime - self.started;
  if (self.duration > 0)
    elapsed = fmin(elapsed, self.duration);
  NSInteger due = floor(elapsed * self.hearing.rate);
  // Small blocks let a running capture consume newly queued recordings in turn.
  while (due > self.hearing.position) {
    AXXAudioChunk *chunk = [self.hearing
        next:MIN(due - self.hearing.position, self.hearing.rate / 100)];
    AVAudioPCMBuffer *buffer = [chunk buffer:self.file.processingFormat];
    NSError *error = self.hearing.error;
    if (error || ![self.file writeFromBuffer:buffer error:&error]) {
      self.error = error;
      break;
    }
    double peak = 0, square = 0;
    for (AVAudioFrameCount i = 0; i < buffer.frameLength; i++) {
      double v = buffer.floatChannelData[0][i];
      peak = fmax(peak, fabs(v));
      square += v * v;
    }
    self.peak = peak;
    self.average = sqrt(square / MAX(buffer.frameLength, 1));
  }
}
- (void)finish {
  [self catchUp];
  [self stopClock];
  if (@available(iOS 18.0, *))
    [self.file close];
  self.file =
      nil; // flush and finalize the encoder before the app reads its URL
  AVAudioRecorder *recorder = self.recorder;
  NSError *error = self.error;
  dispatch_async(dispatch_get_main_queue(), ^{
    if (error && [recorder.delegate respondsToSelector:@selector
                                    (audioRecorderEncodeErrorDidOccur:error:)])
      [recorder.delegate audioRecorderEncodeErrorDidOccur:recorder error:error];
    if ([recorder.delegate respondsToSelector:@selector
                           (audioRecorderDidFinishRecording:successfully:)])
      [recorder.delegate audioRecorderDidFinishRecording:recorder
                                            successfully:error == nil];
  });
}
@end
static AXXRecorder *recorderCapture(AVAudioRecorder *recorder) {
  @synchronized(recorder) {
    AXXRecorder *state = objc_getAssociatedObject(recorder, &captureKey);
    if (!state) {
      state = [AXXRecorder new];
      state.recorder = recorder;
      objc_setAssociatedObject(recorder, &captureKey, state,
                               OBJC_ASSOCIATION_RETAIN_NONATOMIC);
      registerCapture(state);
    }
    return state;
  }
}
static BOOL prepareRecorder(AXXRecorder *state) {
  if (state.file)
    return YES;
  NSError *error = nil;
  state.file = [[AVAudioFile alloc]
      initForWriting:state.recorder.url
            settings:state.settings ?: state.recorder.settings
        commonFormat:AVAudioPCMFormatFloat32
         interleaved:NO
               error:&error];
  state.error = error;
  state.hearing = [AXXHearing new];
  state.hearing.rate = llround(state.file.processingFormat.sampleRate);
  state.elapsed = state.duration = 0;
  return state.file != nil;
}
static BOOL record(AVAudioRecorder *recorder, double duration) {
  AXXRecorder *state = recorderCapture(recorder);
  @synchronized(state) {
    if (state.running)
      return YES;
    if (!prepareRecorder(state))
      return NO;
    state.duration = duration > 0 ? state.elapsed + duration : 0;
    state.started = NSProcessInfo.processInfo.systemUptime;
    [state.hearing prime];
    if (state.hearing.error) {
      state.error = state.hearing.error;
      return NO;
    }
    state.running = YES;
    __weak AXXRecorder *weak = state;
    captureClock(state, ^{
      AXXRecorder *s = weak;
      @synchronized(s) {
        if (!s.running)
          return;
        [s catchUp];
        if (s.error ||
            (s.duration > 0 &&
             s.hearing.position >= floor(s.duration * s.hearing.rate)))
          [s finish];
      }
    });
    return YES;
  }
}
static void installRecorder(void) {
  __block IMP initSettings = NULL, initFormat = NULL, settings = NULL;
  initSettings = replace(
      AVAudioRecorder.class, @selector(initWithURL:settings:error:),
      ^id(AVAudioRecorder *r, NSURL *url, NSDictionary *values,
          NSError **error) {
        id out = ((id (*)(id, SEL, id, id, NSError **))initSettings)(
            r, @selector(initWithURL:settings:error:), url, values, error);
        if (out)
          recorderCapture(out).settings = values;
        return out;
      });
  initFormat = replace(
      AVAudioRecorder.class, @selector(initWithURL:format:error:),
      ^id(AVAudioRecorder *r, NSURL *url, AVAudioFormat *format,
          NSError **error) {
        id out = ((id (*)(id, SEL, id, id, NSError **))initFormat)(
            r, @selector(initWithURL:format:error:), url, format, error);
        if (out)
          recorderCapture(out).settings = format.settings;
        return out;
      });
  settings = replace(
      AVAudioRecorder.class, @selector(settings), ^id(AVAudioRecorder *r) {
        return recorderCapture(r).settings
                   ?: ((id (*)(id, SEL))settings)(r, @selector(settings));
      });
  replace(AVAudioRecorder.class, @selector(prepareToRecord),
          ^BOOL(AVAudioRecorder *r) {
            AXXRecorder *s = recorderCapture(r);
            @synchronized(s) {
              return prepareRecorder(s);
            }
          });
  replace(AVAudioRecorder.class, @selector(record), ^BOOL(AVAudioRecorder *r) {
    return record(r, 0);
  });
  replace(AVAudioRecorder.class, @selector(recordForDuration:),
          ^BOOL(AVAudioRecorder *r, NSTimeInterval duration) {
            return record(r, duration);
          });
  replace(AVAudioRecorder.class, @selector(pause), ^(AVAudioRecorder *r) {
    AXXRecorder *s = recorderCapture(r);
    @synchronized(s) {
      [s catchUp];
      s.elapsed = (double)s.hearing.position / MAX(s.hearing.rate, 1);
      [s stopClock];
    }
  });
  replace(AVAudioRecorder.class, @selector(stop), ^(AVAudioRecorder *r) {
    AXXRecorder *s = recorderCapture(r);
    @synchronized(s) {
      if (s.file)
        [s finish];
    }
  });
  replace(AVAudioRecorder.class, @selector(isRecording),
          ^BOOL(AVAudioRecorder *r) {
            return recorderCapture(r).running;
          });
  replace(AVAudioRecorder.class, @selector(currentTime),
          ^NSTimeInterval(AVAudioRecorder *r) {
            AXXRecorder *s = recorderCapture(r);
            @synchronized(s) {
              [s catchUp];
              return (double)s.hearing.position / MAX(s.hearing.rate, 1);
            }
          });
  replace(AVAudioRecorder.class, @selector(updateMeters),
          ^(AVAudioRecorder *r) {
            AXXRecorder *s = recorderCapture(r);
            @synchronized(s) {
              [s catchUp];
            }
          });
  replace(AVAudioRecorder.class, @selector(peakPowerForChannel:),
          ^float(AVAudioRecorder *r, NSUInteger channel) {
            AXXRecorder *s = recorderCapture(r);
            @synchronized(s) {
              return r.meteringEnabled
                         ? MAX(-160, 20 * log10(MAX(s.peak, 1e-8)))
                         : -160;
            }
          });
  replace(AVAudioRecorder.class, @selector(averagePowerForChannel:),
          ^float(AVAudioRecorder *r, NSUInteger channel) {
            AXXRecorder *s = recorderCapture(r);
            @synchronized(s) {
              return r.meteringEnabled
                         ? MAX(-160, 20 * log10(MAX(s.average, 1e-8)))
                         : -160;
            }
          });
}
@interface AXXAudioOutput : NSObject
@property(weak) id<AVCaptureAudioDataOutputSampleBufferDelegate> delegate;
@property dispatch_queue_t queue;
@end
@implementation AXXAudioOutput
@end
static char outputDelegateKey;
@interface AXXCaptureSession : AXXCapture
@property(weak) AVCaptureSession *session;
@property BOOL audioOnly;
@end
@implementation AXXCaptureSession
@end
static CMSampleBufferRef
captureSample(AXXAudioChunk *chunk) CF_RETURNS_RETAINED {
  AVAudioPCMBuffer *pcm = [chunk buffer:microphoneFormat()];
  CMAudioFormatDescriptionRef format = NULL;
  CMBlockBufferRef data = NULL;
  CMSampleBufferRef sample = NULL;
  size_t bytes = pcm.frameLength * sizeof(float);
  OSStatus status = CMAudioFormatDescriptionCreate(
      kCFAllocatorDefault, pcm.format.streamDescription, 0, NULL, 0, NULL, NULL,
      &format);
  if (!status)
    status = CMBlockBufferCreateWithMemoryBlock(kCFAllocatorDefault, NULL,
                                                bytes, kCFAllocatorDefault,
                                                NULL, 0, bytes, 0, &data);
  if (!status)
    status =
        CMBlockBufferReplaceDataBytes(pcm.floatChannelData[0], data, 0, bytes);
  CMSampleTimingInfo timing = {
      CMTimeMake(1, 48000), CMTimeMake(chunk.position, 48000), kCMTimeInvalid};
  if (!status)
    status = CMSampleBufferCreateReady(kCFAllocatorDefault, data, format,
                                       pcm.frameLength, 1, &timing, 0, NULL,
                                       &sample);
  if (format)
    CFRelease(format);
  if (data)
    CFRelease(data);
  if (sample && chunk.recording) {
    CMSetAttachment(sample, CFSTR("us.nimbusxr.axx.recording"),
                    (__bridge CFTypeRef)chunk.recording,
                    kCMAttachmentMode_ShouldNotPropagate);
    CMSetAttachment(sample, CFSTR("us.nimbusxr.axx.recording-ended"),
                    (__bridge CFTypeRef) @(chunk.ended),
                    kCMAttachmentMode_ShouldNotPropagate);
  }
  return sample;
}
static void installCaptureSession(void) {
  __block IMP delegate = NULL, queue = NULL, setter = NULL;
  setter = replace(
      AVCaptureAudioDataOutput.class, @selector(setSampleBufferDelegate:queue:),
      ^(AVCaptureAudioDataOutput *output,
        id<AVCaptureAudioDataOutputSampleBufferDelegate> target,
        dispatch_queue_t q) {
        // The app's delegate receives our clocked samples, never a second
        // stream from the host device. Video outputs in a mixed session keep
        // running.
        ((void (*)(id, SEL, id, dispatch_queue_t))setter)(
            output, @selector(setSampleBufferDelegate:queue:), nil, NULL);
        AXXAudioOutput *state = [AXXAudioOutput new];
        state.delegate = target;
        state.queue = q;
        objc_setAssociatedObject(output, &outputDelegateKey, state,
                                 OBJC_ASSOCIATION_RETAIN_NONATOMIC);
      });
  delegate =
      replace(AVCaptureAudioDataOutput.class, @selector(sampleBufferDelegate),
              ^id(AVCaptureAudioDataOutput *output) {
                AXXAudioOutput *state =
                    objc_getAssociatedObject(output, &outputDelegateKey);
                return state ? state.delegate
                             : ((id (*)(id, SEL))delegate)(
                                   output, @selector(sampleBufferDelegate));
              });
  queue = replace(
      AVCaptureAudioDataOutput.class, @selector(sampleBufferCallbackQueue),
      ^dispatch_queue_t(AVCaptureAudioDataOutput *output) {
        AXXAudioOutput *state =
            objc_getAssociatedObject(output, &outputDelegateKey);
        return state ? state.queue
                     : ((dispatch_queue_t (*)(id, SEL))queue)(
                           output, @selector(sampleBufferCallbackQueue));
      });
  __block IMP start = NULL, stop = NULL, running = NULL;
  start = replace(
      AVCaptureSession.class, @selector(startRunning),
      ^(AVCaptureSession *session) {
        BOOL audio = NO, audioOnly = YES;
        for (AVCaptureOutput *output in session.outputs) {
          if ([output isKindOfClass:AVCaptureAudioDataOutput.class])
            audio = YES;
          else
            audioOnly = NO;
        }
        if (!audio) {
          ((void (*)(id, SEL))start)(session, @selector(startRunning));
          return;
        }
        AXXCaptureSession *state =
            objc_getAssociatedObject(session, &captureKey);
        if (!state) {
          state = [AXXCaptureSession new];
          state.session = session;
          objc_setAssociatedObject(session, &captureKey, state,
                                   OBJC_ASSOCIATION_RETAIN_NONATOMIC);
          registerCapture(state);
        }
        @synchronized(state) {
          if (state.running)
            return;
          state.audioOnly = audioOnly;
          state.hearing = [AXXHearing new];
          state.hearing.rate = 48000;
          [state.hearing prime];
          state.running = YES;
          __weak AXXCaptureSession *weak = state;
          captureClock(state, ^{
            AXXCaptureSession *s = weak;
            @synchronized(s) {
              if (!s.running)
                return;
              AXXAudioChunk *chunk = [s.hearing next:480];
              for (AVCaptureOutput *output in s.session.outputs) {
                if (![output isKindOfClass:AVCaptureAudioDataOutput.class])
                  continue;
                AXXAudioOutput *delivery =
                    objc_getAssociatedObject(output, &outputDelegateKey);
                AVCaptureConnection *connection =
                    [output connectionWithMediaType:AVMediaTypeAudio];
                if (!delivery.delegate || !delivery.queue ||
                    !connection.enabled)
                  continue;
                CMSampleBufferRef sample = captureSample(chunk);
                if (!sample) {
                  [NSNotificationCenter.defaultCenter
                      postNotificationName:
                          AVCaptureSessionRuntimeErrorNotification
                                    object:s.session
                                  userInfo:@{
                                    AVCaptureSessionErrorKey :
                                        failure(@"Cannot create captured PCM "
                                                @"audio; use an audio data "
                                                @"output")
                                  }];
                  [s stopClock];
                  return;
                }
                dispatch_async(delivery.queue, ^{
                  if (s.running)
                    [delivery.delegate captureOutput:output
                               didOutputSampleBuffer:sample
                                      fromConnection:connection];
                  CFRelease(sample);
                });
              }
            }
          });
        }
        if (!audioOnly)
          ((void (*)(id, SEL))start)(session, @selector(startRunning));
        else
          [NSNotificationCenter.defaultCenter
              postNotificationName:AVCaptureSessionDidStartRunningNotification
                            object:session];
      });
  stop = replace(
      AVCaptureSession.class, @selector(stopRunning),
      ^(AVCaptureSession *session) {
        AXXCaptureSession *state =
            objc_getAssociatedObject(session, &captureKey);
        @synchronized(state) {
          [state stopClock];
        }
        if (!state.audioOnly)
          ((void (*)(id, SEL))stop)(session, @selector(stopRunning));
        else
          [NSNotificationCenter.defaultCenter
              postNotificationName:AVCaptureSessionDidStopRunningNotification
                            object:session];
      });
  running = replace(AVCaptureSession.class, @selector(isRunning),
                    ^BOOL(AVCaptureSession *session) {
                      AXXCaptureSession *state =
                          objc_getAssociatedObject(session, &captureKey);
                      return state.audioOnly
                                 ? state.running
                                 : ((BOOL (*)(id, SEL))running)(
                                       session, @selector(isRunning));
                    });
}
static void installMicrophone(void) {
  installEngine();
  installRecorder();
  installCaptureSession();
}
