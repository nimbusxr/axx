// Included by models.m. Ports of match.Resample, match.LogMel and the
// features reducer; compiled with -ffp-contract=off, in Go's operation order.
static NSData *audioData(NSArray<NSNumber *> *samples) {
  NSMutableData *data =
      [NSMutableData dataWithLength:samples.count * sizeof(double)];
  double *out = data.mutableBytes;
  for (NSUInteger i = 0; i < samples.count; i++)
    out[i] = samples[i].doubleValue;
  return data;
}
static NSArray *audioNumbers(NSData *data) {
  NSMutableArray *out = [NSMutableArray new];
  const double *v = data.bytes;
  for (NSUInteger i = 0; i < data.length / sizeof(double); i++)
    [out addObject:@(v[i])];
  return out;
}
static double besselI0(double x) {
  double sum = 1, term = 1;
  for (int k = 1; term > 1e-17 * sum; k++) {
    term *= (x / 2 / k) * (x / 2 / k);
    sum += term;
  }
  return sum;
}
static double audioKernel(double d, double factor, NSInteger taps) {
  double x = M_PI * d * factor, y = x == 0 ? 1 : sin(x) / x;
  double w = 2 * x / (factor * taps * M_PI);
  return y * besselI0(9 * sqrt(fmax(1 - w * w, 0)));
}
static NSInteger audioMirror(NSInteger j, NSInteger n) {
  while (j < 0 || j >= n) {
    if (j < 0)
      j = -j;
    if (j >= n)
      j = 2 * n - 1 - j;
  }
  return j;
}
static NSData *resample(NSData *data, NSInteger from, NSInteger to) {
  NSInteger n = data.length / sizeof(double);
  if (from == to || !n)
    return data;
  double factor = fmin((double)to * 0.97 / from, 1);
  NSInteger taps = ceil(32 / factor), center = (taps - 1) / 2;
  double norm = 0;
  for (NSInteger i = 0; i < taps; i++)
    norm += audioKernel(i - center, factor, taps);
  NSInteger g = from, b = to;
  while (b) {
    NSInteger t = g % b;
    g = b;
    b = t;
  }
  NSInteger step = from / g, phases = to / g;
  NSMutableDictionary *filters = [NSMutableDictionary new];
  NSInteger count = llround((double)n * to / from);
  NSMutableData *out = [NSMutableData dataWithLength:count * sizeof(double)];
  double *values = out.mutableBytes;
  const double *samples = data.bytes;
  for (NSInteger k = 0; k < count; k++) {
    NSInteger pos = k * step, index = pos / phases, ph = pos % phases;
    NSMutableData *table = filters[@(ph)];
    if (!table) {
      table = [NSMutableData dataWithLength:taps * sizeof(double)];
      double *f = table.mutableBytes, frac = (double)ph / phases;
      for (NSInteger i = 0; i < taps; i++)
        f[i] = audioKernel(i - center - frac, factor, taps) / norm;
      filters[@(ph)] = table;
    }
    const double *f = table.bytes;
    double v = 0;
    for (NSInteger i = 0; i < taps; i++)
      v += samples[audioMirror(index + i - center, n)] * f[i];
    values[k] = v;
  }
  return out;
}
static double toMel(double f) {
  return f < 1000 ? 3 * f / 200 : 15 + 27 * log(f / 1000) / log(6.4);
}
static double toHz(double m) {
  return m < 15 ? 200 * m / 3 : 1000 * exp(log(6.4) * (m - 15) / 27);
}
static NSData *logMel(NSData *data, NSInteger rate, NSInteger mels,
                      NSInteger frames) {
  const NSInteger fft = 400, hop = 160, half = 200, bins = 201;
  NSInteger n = frames * hop;
  double *padded = calloc(n, sizeof(double));
  memcpy(padded, data.bytes, MIN(data.length, n * sizeof(double)));
  double *x = calloc(n + 2 * half, sizeof(double));
  memcpy(x + half, padded, n * sizeof(double));
  for (NSInteger i = 0; i < half; i++) {
    x[half - 1 - i] = padded[MIN(i + 1, n - 1)];
    x[half + n + i] = padded[MAX(n - 2 - i, 0)];
  }
  free(padded);
  double window[400], frame[400], power[201];
  double *cosines = calloc(fft * bins, sizeof(double)),
         *sines = calloc(fft * bins, sizeof(double));
  for (NSInteger i = 0; i < fft; i++) {
    window[i] = 0.5 - 0.5 * cos(2 * M_PI * i / fft);
    for (NSInteger k = 0; k < bins; k++) {
      double a = 2 * M_PI * (i * k % fft) / fft;
      cosines[i * bins + k] = cos(a);
      sines[i * bins + k] = sin(a);
    }
  }
  double *edges = calloc(mels + 2, sizeof(double)),
         *filters = calloc(mels * bins, sizeof(double));
  double lo = toMel(0), hi = toMel(8000);
  for (NSInteger i = 0; i < mels + 2; i++)
    edges[i] = toHz(lo + (hi - lo) * i / (mels + 1));
  for (NSInteger m = 0; m < mels; m++) {
    double norm = 2 / (edges[m + 2] - edges[m]);
    for (NSInteger k = 0; k < bins; k++) {
      double f = (double)(rate / 2) * k / (bins - 1);
      double down = (f - edges[m]) / (edges[m + 1] - edges[m]);
      double up = (edges[m + 2] - f) / (edges[m + 2] - edges[m + 1]);
      filters[m * bins + k] = fmax(0, fmin(down, up)) * norm;
    }
  }
  free(edges);
  NSMutableData *result =
      [NSMutableData dataWithLength:mels * frames * sizeof(double)];
  double *out = result.mutableBytes, top = -INFINITY;
  for (NSInteger f = 0; f < frames; f++) {
    for (NSInteger i = 0; i < fft; i++)
      frame[i] = x[f * hop + i] * window[i];
    for (NSInteger k = 0; k < bins; k++) {
      double re = 0, im = 0;
      for (NSInteger i = 0; i < fft; i++) {
        re += frame[i] * cosines[i * bins + k];
        im += frame[i] * sines[i * bins + k];
      }
      power[k] = re * re + im * im;
    }
    for (NSInteger m = 0; m < mels; m++) {
      double e = 0;
      for (NSInteger k = 0; k < bins; k++)
        e += filters[m * bins + k] * power[k];
      out[m * frames + f] = log10(fmax(e, 1e-10));
      top = fmax(top, out[m * frames + f]);
    }
  }
  for (NSInteger i = 0; i < mels * frames; i++)
    out[i] = (fmax(out[i], top - 8) + 4) / 4;
  free(x);
  free(cosines);
  free(sines);
  free(filters);
  return result;
}
static NSData *audioFeatures(NSData *data, NSInteger h, NSInteger w) {
  NSInteger kh = MAX(h / 16, 1), kw = MAX(w / 300, 1), ph = h / kh, pw = w / kw;
  NSMutableData *result =
      [NSMutableData dataWithLength:ph * pw * sizeof(double)];
  double *out = result.mutableBytes, cell = kh * kw;
  const double *item = data.bytes;
  for (NSInteger py = 0; py < ph; py++)
    for (NSInteger px = 0; px < pw; px++) {
      double sum = 0;
      for (NSInteger y = py * kh; y < (py + 1) * kh; y++)
        for (NSInteger x = px * kw; x < (px + 1) * kw; x++)
          sum += item[y * w + x];
      out[py * pw + px] = sum / cell;
    }
  for (NSInteger px = 0; px < pw; px++) {
    double mean = 0, square = 0;
    for (NSInteger py = 0; py < ph; py++)
      mean += out[py * pw + px];
    mean /= ph;
    for (NSInteger py = 0; py < ph; py++) {
      out[py * pw + px] -= mean;
      square += out[py * pw + px] * out[py * pw + px];
    }
    square /= ph;
    double heard = fmin(fmax((square - 0.01) * (1 << 20), 0), 1),
           scale = sqrt(fmax(square, 0.01));
    for (NSInteger py = 0; py < ph; py++)
      out[py * pw + px] = out[py * pw + px] * heard / scale;
  }
  double square = 0;
  for (NSInteger i = 0; i < ph * pw; i++)
    square += out[i] * out[i];
  double scale = sqrt(square / (ph * pw) + 1e-12);
  for (NSInteger i = 0; i < ph * pw; i++)
    out[i] /= scale;
  return result;
}
static NSUInteger speechMatch(NSDictionary *a, NSArray *samples,
                              NSInteger rate) {
  NSDictionary *in = input(a), *audio = in[@"audio"];
  NSInteger want = [audio[@"sampleRate"] integerValue];
  NSData *data = resample(audioData(samples), rate, want);
  if ([audio[@"features"] isEqual:@"log-mel"]) {
    NSArray *shape = in[@"shape"];
    NSInteger mels = [shape[shape.count - 2] integerValue],
              frames = [shape.lastObject integerValue];
    NSData *features =
        audioFeatures(logMel(data, want, mels, frames), mels, frames);
    return nearest(a, (double *)features.bytes,
                   features.length / sizeof(double));
  }
  return sound(a, audioNumbers(data));
}
static NSUInteger recordingMatch(NSDictionary *a, NSDictionary *recording,
                                 NSError **error) {
  NSNumber *match = recording[@"matches"][a[@"api"]];
  if ([match isKindOfClass:NSNumber.class] && match.integerValue >= 0 &&
      match.unsignedIntegerValue < [a[@"generated"] count])
    return match.unsignedIntegerValue;
  NSString *message = nil;
  if ([input(a)[@"audio"][@"features"] length])
    message = @"The recording was not matched to this speech stub; rebuild the "
              @"stub and queue the recording again";
  else if ([recording[@"rate"] integerValue] !=
           [input(a)[@"audio"][@"sampleRate"] integerValue])
    message = @"Recording sample rate differs from the contract; queue it "
              @"again with the stub's sample rate";
  if (message) {
    if (error)
      *error = failure(message);
    return NSNotFound;
  }
  return sound(a, recording[@"samples"]);
}
// Convert every public PCM common format to mono, without asking the audio
// device for a format. Accumulate a stream before resampling it for matching.
static BOOL appendPCM(NSMutableArray *samples, AVAudioPCMBuffer *buffer) {
  AVAudioFormat *f = buffer.format;
  if (!f.channelCount || f.sampleRate <= 0)
    return NO;
  for (AVAudioFrameCount i = 0; i < buffer.frameLength; i++) {
    double sum = 0;
    for (AVAudioChannelCount c = 0; c < f.channelCount; c++) {
      NSUInteger channel = f.interleaved ? 0 : c,
                 at = f.interleaved ? i * f.channelCount + c : i;
      switch (f.commonFormat) {
      case AVAudioPCMFormatFloat32:
        sum += buffer.floatChannelData[channel][at];
        break;
      case AVAudioPCMFormatFloat64:
        sum += ((double *)buffer.audioBufferList->mBuffers[channel].mData)[at];
        break;
      case AVAudioPCMFormatInt16:
        sum += buffer.int16ChannelData[channel][at] / 32768.0;
        break;
      case AVAudioPCMFormatInt32:
        sum += buffer.int32ChannelData[channel][at] / 2147483648.0;
        break;
      default:
        return NO;
      }
    }
    [samples addObject:@(sum / f.channelCount)];
  }
  return YES;
}
