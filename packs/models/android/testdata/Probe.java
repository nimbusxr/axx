package us.nimbusxr.axx.modelsprobe;

import android.app.Activity;
import android.content.Intent;
import android.media.AudioFormat;
import android.media.AudioRecord;
import android.media.MediaCodec;
import android.media.MediaExtractor;
import android.media.MediaFormat;
import android.media.MediaRecorder;
import android.os.Bundle;
import android.os.ParcelFileDescriptor;
import android.os.SystemClock;
import android.speech.RecognitionListener;
import android.speech.RecognizerIntent;
import android.speech.SpeechRecognizer;
import java.io.File;
import java.io.FileOutputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.util.Arrays;

// A normal application caller. This app contains no test provider or adapter.
public final class Probe extends Activity implements RecognitionListener {
  private SpeechRecognizer recognizer;
  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    String kind = getIntent().getStringExtra("kind");
    if (kind != null && kind.startsWith("recorder")) {
      new Thread(() -> recorder(kind)).start();
      return;
    }
    if (kind != null && kind.startsWith("record-")) {
      new Thread(() -> record(kind)).start();
      return;
    }
    if (!SpeechRecognizer.isRecognitionAvailable(this)) {
      save("unavailable");
      return;
    }
    recognizer = getIntent().getBooleanExtra("ondevice", false)
                     ? SpeechRecognizer.createOnDeviceSpeechRecognizer(this)
                     : SpeechRecognizer.createSpeechRecognizer(this);
    recognizer.setRecognitionListener(this);
    Intent intent = new Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH);
    intent.putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                    RecognizerIntent.LANGUAGE_MODEL_FREE_FORM);
    if ("source".equals(kind)) {
      // The app's own audio, 16-bit PCM at 16 kHz, through a pipe.
      try {
        ParcelFileDescriptor[] pipe = ParcelFileDescriptor.createPipe();
        byte[] pcm =
            Files.readAllBytes(new File(getFilesDir(), "source.pcm").toPath());
        new Thread(() -> {
          try (OutputStream out =
                   new ParcelFileDescriptor.AutoCloseOutputStream(pipe[1])) {
            out.write(pcm);
          } catch (Exception error) {
            save("error " + error);
          }
        }).start();
        intent.putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE, pipe[0]);
        intent.putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_SAMPLING_RATE,
                        16000);
        intent.putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_CHANNEL_COUNT, 1);
        intent.putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_ENCODING,
                        AudioFormat.ENCODING_PCM_16BIT);
      } catch (Exception error) {
        save("error " + error);
        return;
      }
    }
    recognizer.startListening(intent);
  }
  // The test reads transcript.txt as soon as it is there: written whole, then
  // renamed into place, it is never there empty.
  private void save(String text) {
    try {
      File part = new File(getFilesDir(), "transcript.txt.part");
      Files.write(part.toPath(), text.getBytes(StandardCharsets.UTF_8));
      Files.move(part.toPath(), new File(getFilesDir(), "transcript.txt").toPath(),
                 StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
    } catch (Exception error) {
      throw new RuntimeException(error);
    }
  }
  // Records seconds of the microphone with AudioRecord, and keeps what it
  // read, as 32-bit floats of its first channel: record-short (16 kHz, 16-bit,
  // short[]), record-float (44.1 kHz, float, stereo) or record-buffer (48 kHz,
  // 16-bit, a direct ByteBuffer).
  private void record(String kind) {
    try {
      int rate = kind.equals("record-short")   ? 16000
                 : kind.equals("record-float") ? 44100
                                               : 48000;
      int channels = kind.equals("record-float") ? 2 : 1;
      int encoding = kind.equals("record-float")
                         ? AudioFormat.ENCODING_PCM_FLOAT
                         : AudioFormat.ENCODING_PCM_16BIT;
      int mask = channels == 2 ? AudioFormat.CHANNEL_IN_STEREO
                               : AudioFormat.CHANNEL_IN_MONO;
      int size = Math.max(AudioRecord.getMinBufferSize(rate, mask, encoding),
                          rate / 5 * channels * 4);
      AudioRecord record = new AudioRecord(MediaRecorder.AudioSource.MIC, rate,
                                           mask, encoding, size);
      record.startRecording();
      int total = rate * 4;
      ByteBuffer kept = ByteBuffer.allocate(total * 4).order(ByteOrder.LITTLE_ENDIAN);
      int frames = 0, chunk = rate / 10;
      short[] shorts = new short[chunk * channels];
      float[] floats = new float[chunk * channels];
      ByteBuffer direct = ByteBuffer.allocateDirect(chunk * channels * 2)
                              .order(ByteOrder.nativeOrder());
      while (frames < total) {
        int want = Math.min(chunk, total - frames), got;
        if (kind.equals("record-short")) {
          got = record.read(shorts, 0, want * channels) / channels;
          for (int i = 0; i < got; i++)
            kept.putFloat(shorts[i * channels] / 32768f);
        } else if (kind.equals("record-float")) {
          got = record.read(floats, 0, want * channels,
                            AudioRecord.READ_BLOCKING) /
                channels;
          for (int i = 0; i < got; i++)
            kept.putFloat(floats[i * channels]);
        } else {
          got = record.read(direct, want * channels * 2) / 2 / channels;
          for (int i = 0; i < got; i++)
            kept.putFloat(direct.getShort(i * channels * 2) / 32768f);
        }
        if (got <= 0)
          throw new IllegalStateException("read " + got);
        frames += got;
      }
      record.stop();
      record.release();
      Files.write(new File(getFilesDir(), "capture.pcm").toPath(),
                  Arrays.copyOf(kept.array(), frames * 4));
      save("recorded " + rate);
    } catch (Exception error) {
      save("error " + error);
    }
  }
  // Records seconds with MediaRecorder to an AAC file, then decodes the file
  // and keeps its sound, as 32-bit floats of its first channel.
  private void recorder(String kind) {
    try {
      File file = new File(getFilesDir(), "note.m4a");
      MediaRecorder recorder = new MediaRecorder();
      if ("recorder-video".equals(kind))
        recorder.setVideoSource(MediaRecorder.VideoSource.CAMERA);
      if ("recorder-release".equals(kind) || "recorder-reset".equals(kind)) {
        recorder.setAudioSource(MediaRecorder.AudioSource.MIC);
        recorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4);
        recorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC);
        recorder.setAudioSamplingRate(8000);
        recorder.setAudioChannels(2);
        recorder.setAudioEncodingBitRate(32000);
        recorder.setOutputFile(new File(getFilesDir(), "discarded.m4a"));
        recorder.prepare();
        recorder.start();
        Thread.sleep(50);
        if ("recorder-release".equals(kind)) {
          recorder.release();
          recorder = new MediaRecorder(this);
        } else
          recorder.reset();
        Thread.sleep(
            500); // a released/reset capture must stop consuming the queue
      }
      recorder.setAudioSource(MediaRecorder.AudioSource.MIC);
      recorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4);
      recorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC);
      recorder.setAudioSamplingRate(16000);
      recorder.setAudioChannels(1);
      recorder.setAudioEncodingBitRate(64000);
      FileOutputStream destination = null;
      if (kind.startsWith("recorder-fd")) {
        destination = new FileOutputStream(file);
        recorder.setOutputFile(destination.getFD());
      } else if ("recorder-paused-file".equals(kind))
        recorder.setOutputFile(file);
      else
        recorder.setOutputFile(file.getPath());
      recorder.prepare();
      if ("recorder-fd-closed".equals(kind))
        destination.close();
      recorder.start();
      // How long it records, from its start to its stop, less its pause: the sleeps' four
      // seconds, or more on a busy device, whose sleeps end late.
      long started = SystemClock.elapsedRealtime();
      Thread.sleep(1500);
      int loud = recorder.getMaxAmplitude();
      long paused = 0;
      if ("recorder-paused-file".equals(kind)) {
        recorder.pause();
        Thread.sleep(500);
        paused = 500;
        recorder.resume();
      }
      Thread.sleep(2500);
      long recorded = SystemClock.elapsedRealtime() - started - paused;
      recorder.stop();
      recorder.release();
      if ("recorder-fd".equals(kind) && !destination.getFD().valid())
        throw new Exception("the recorder closed the app's descriptor");
      if (destination != null)
        destination.close();
      MediaExtractor extractor = new MediaExtractor();
      extractor.setDataSource(file.getPath());
      MediaFormat format = extractor.getTrackFormat(0);
      extractor.selectTrack(0);
      MediaCodec codec =
          MediaCodec.createDecoderByType(format.getString(MediaFormat.KEY_MIME));
      codec.configure(format, null, null, 0);
      codec.start();
      ByteBuffer kept = ByteBuffer.allocate(16000 * 4 * 8).order(ByteOrder.LITTLE_ENDIAN);
      MediaCodec.BufferInfo info = new MediaCodec.BufferInfo();
      boolean input = true, done = false;
      int channels = format.getInteger(MediaFormat.KEY_CHANNEL_COUNT);
      while (!done) {
        if (input) {
          int in = codec.dequeueInputBuffer(10000);
          if (in >= 0) {
            int n = extractor.readSampleData(codec.getInputBuffer(in), 0);
            if (n < 0) {
              codec.queueInputBuffer(in, 0, 0, 0,
                                     MediaCodec.BUFFER_FLAG_END_OF_STREAM);
              input = false;
            } else {
              codec.queueInputBuffer(in, 0, n, extractor.getSampleTime(), 0);
              extractor.advance();
            }
          }
        }
        int out = codec.dequeueOutputBuffer(info, 10000);
        if (out >= 0) {
          ByteBuffer pcm =
              codec.getOutputBuffer(out).order(ByteOrder.nativeOrder());
          for (int i = 0; i + 2 * channels <= info.size && kept.remaining() >= 4;
               i += 2 * channels)
            kept.putFloat(pcm.getShort(info.offset + i) / 32768f);
          codec.releaseOutputBuffer(out, false);
          done = (info.flags & MediaCodec.BUFFER_FLAG_END_OF_STREAM) != 0;
        }
      }
      codec.stop();
      codec.release();
      extractor.release();
      Files.write(new File(getFilesDir(), "capture.pcm").toPath(),
                  Arrays.copyOf(kept.array(), kept.position()));
      save("recorded " + format.getInteger(MediaFormat.KEY_SAMPLE_RATE) +
           " loud " + loud + " ms " + recorded);
    } catch (Exception error) {
      save("error " + error);
    }
  }
  public void onResults(Bundle results) {
    save(results.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
             .get(0));
  }
  public void onError(int error) { save("error " + error); }
  public void onReadyForSpeech(Bundle params) {}
  public void onBeginningOfSpeech() {}
  public void onRmsChanged(float rms) {}
  public void onBufferReceived(byte[] buffer) {}
  public void onEndOfSpeech() {}
  public void onPartialResults(Bundle results) {}
  public void onEvent(int type, Bundle params) {}
}
