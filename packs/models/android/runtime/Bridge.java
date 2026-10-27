package us.nimbusxr.axx.models;

import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Bitmap;
import android.graphics.Matrix;
import android.graphics.Point;
import android.graphics.Rect;
import android.media.AudioFormat;
import android.media.AudioRecord;
import android.media.MediaCodec;
import android.media.MediaCodecInfo;
import android.media.MediaFormat;
import android.media.MediaMuxer;
import android.media.MediaRecorder;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.ParcelFileDescriptor;
import android.speech.RecognitionListener;
import android.speech.RecognizerIntent;
import android.speech.SpeechRecognizer;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileDescriptor;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.ref.ReferenceQueue;
import java.lang.ref.WeakReference;
import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.lang.reflect.Proxy;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.channels.FileChannel;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.WeakHashMap;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import org.json.JSONArray;
import org.json.JSONObject;

// Uses only public Android and ML Kit SDK APIs. The original application
// continues to call its normal recognizer and ImageLabeler objects.
public final class Bridge {
  // A call the bridge answers with this runs the real method.
  public static final Object PROCEED = new Object();
  private static final Map<String, JSONObject> models = new HashMap<>();
  private static final Map<Object, Recognition> recognizers =
      new WeakHashMap<>();
  private static final Map<Object, Bitmap> images = new WeakHashMap<>();
  private static File folder;
  private static boolean microphone;
  private static final Handler main = new Handler(Looper.getMainLooper());
  private static native Object allocate(Class<?> type);
  // The agent answers a result's getter that returns no field of it.
  private static native void watch(Method getter);
  private static final ReferenceQueue<Object> collected =
      new ReferenceQueue<>();
  private static final Map<Key, Map<String, Object>> results = new HashMap<>();
  private static final class Key extends WeakReference<Object> {
    final int hash;
    Key(Object value) {
      super(value, collected);
      hash = System.identityHashCode(value);
    }
    public int hashCode() { return hash; }
    public boolean equals(Object other) {
      return this == other ||
          other instanceof Key && get() != null && get() == ((Key)other).get();
    }
  }
  private static Map<String, Object> fields(Object... pairs) {
    Map<String, Object> out = new HashMap<>();
    for (int i = 0; i < pairs.length; i += 2)
      out.put((String)pairs[i], pairs[i + 1]);
    return out;
  }
  // A result of the SDK's own class, which the app reads with its getters.
  // A getter that returns a field of the result reads the answer there, at
  // the speed of the SDK's own results; the agent answers any other.
  private static Object mock(ClassLoader loader, String type,
                             Map<String, Object> fields) throws Exception {
    Class<?> cls = Class.forName(type, true, loader);
    Object value = allocate(cls);
    Map<String, Field> stored = getterFields(cls);
    for (Map.Entry<String, Object> e : fields.entrySet()) {
      Field field = stored.get(e.getKey());
      if (field != null && e.getValue() != null)
        field.set(value, e.getValue());
    }
    synchronized (results) {
      Key dead;
      while ((dead = (Key)collected.poll()) != null)
        results.remove(dead);
      results.put(new Key(value), fields);
    }
    return value;
  }
  public static Object[] result(Object value, String method) {
    synchronized (results) {
      Map<String, Object> fields = results.get(new Key(value));
      if (fields == null)
        return null;
      if (!fields.containsKey(method))
        throw new UnsupportedOperationException(
            "Unspecified fixture result property " + method);
      return new Object[] {fields.get(method)};
    }
  }

  // The field each getter of a result class returns, found by giving each
  // field values of its own and calling the getter on a result of them.
  private static final Map<Class<?>, Map<String, Field>> getters =
      new HashMap<>();
  private static Map<String, Field> getterFields(Class<?> cls)
      throws Exception {
    synchronized (getters) {
      Map<String, Field> found = getters.get(cls);
      if (found != null)
        return found;
      found = new HashMap<>();
      List<Field> all = new ArrayList<>();
      for (Class<?> c = cls; c != null && c != Object.class;
           c = c.getSuperclass())
        for (Field f : c.getDeclaredFields())
          if (!Modifier.isStatic(f.getModifiers())) {
            f.setAccessible(true);
            all.add(f);
          }
      for (Method m : cls.getMethods()) {
        if (Modifier.isStatic(m.getModifiers()) ||
            m.getParameterTypes().length != 0 ||
            !m.getName().startsWith("get") ||
            m.getDeclaringClass() == Object.class)
          continue;
        Field field = null;
        for (Field f : all)
          if (returns(cls, m, f, 0) && returns(cls, m, f, 1)) {
            field = f;
            break;
          }
        if (field != null)
          found.put(m.getName(), field);
        else
          watch(m);
      }
      getters.put(cls, found);
      return found;
    }
  }
  private static boolean returns(Class<?> cls, Method getter, Field field,
                                 int k) {
    Class<?> type = getter.getReturnType(), stored = field.getType();
    if (type.isPrimitive() != stored.isPrimitive() ||
        type.isPrimitive() && type != stored)
      return false;
    if (stored.isAssignableFrom(type))
      stored = type;
    else if (!type.isAssignableFrom(stored))
      return false;
    try {
      Object value = sentinel(stored, k);
      if (value == null)
        return false;
      Object result = allocate(cls);
      field.set(result, value);
      Object got = getter.invoke(result);
      if (type.isPrimitive() || value instanceof Number ||
          value instanceof Boolean || value instanceof Character)
        return value.equals(got);
      if (value instanceof List && got instanceof List &&
          ((List<?>)got).size() == 1)
        return ((List<?>)got).get(0) == ((List<?>)value).get(0);
      return got == value;
    } catch (Throwable ignored) {
      return false;
    }
  }
  // A value of a type that is its own: the kth, of two.
  private static Object sentinel(Class<?> type, int k) throws Exception {
    int n = 1000003 + 7919 * k;
    if (type == int.class || type == Integer.class)
      return new Integer(n);
    if (type == long.class || type == Long.class)
      return new Long(n * 1000003L);
    if (type == float.class || type == Float.class)
      return new Float(n / 7.0f);
    if (type == double.class || type == Double.class)
      return new Double(n / 7.0);
    if (type == short.class || type == Short.class)
      return new Short((short)(1009 + k));
    if (type == byte.class || type == Byte.class)
      return new Byte((byte)(37 + k));
    if (type == char.class || type == Character.class)
      return new Character((char)(0x2603 + k));
    if (type == boolean.class || type == Boolean.class)
      return k == 0 ? Boolean.TRUE : Boolean.FALSE;
    if (type == String.class || type == CharSequence.class ||
        type == Object.class)
      return new String("axx " + n);
    if (type == List.class || type == java.util.Collection.class ||
        type == Iterable.class)
      return new ArrayList<>(Collections.singletonList(new Object()));
    if (type == Map.class)
      return new HashMap<>();
    if (type == ByteBuffer.class)
      return ByteBuffer.allocate(k + 1);
    if (type.isArray())
      return Array.newInstance(type.getComponentType(), k + 1);
    if (type.isEnum()) {
      Object[] constants = type.getEnumConstants();
      return constants.length > 1 ? constants[k] : null;
    }
    if (type.isInterface() || Modifier.isAbstract(type.getModifiers()))
      return null;
    return allocate(type);
  }
  private static final class Recognition {
    RecognitionListener listener;
    boolean active;
    int generation;
  }

  public static int initialize(String agent) throws Exception {
    folder = new File(agent, "../../axx-models");
    File listens = new File(agent, "microphone");
    microphone = listens.exists() &&
                 new String(Files.readAllBytes(listens.toPath()),
                            StandardCharsets.UTF_8)
                     .equals("true");
    // A missing axx-models folder is an empty one.
    if (folder.exists())
      load(folder);
    return (models.containsKey("android.speech") ? 1 : 0) |
        (models.containsKey("android.mlkit.image-labeling") ? 2 : 0) |
        (models.containsKey("android.mlkit.text-recognition") ? 4 : 0) |
        (models.containsKey("android.mlkit.object-detection") ? 8 : 0) |
        (models.containsKey("android.mlkit.segmentation") ? 16 : 0) |
        (models.containsKey("android.gemini-nano") ? 32 : 0) |
        (microphone ? 64 : 0);
  }
  private static void load(File dir) throws Exception {
    File[] files = dir.listFiles();
    if (files == null)
      throw new IllegalStateException("Cannot read axx-models");
    for (File f : files) {
      if (f.getName().startsWith("."))
        continue;
      if (f.isDirectory()) {
        load(f);
        continue;
      }
      if (!f.getName().toLowerCase(java.util.Locale.ROOT).endsWith(".axxmodel"))
        continue;
      JSONObject a = new JSONObject(
          new String(Files.readAllBytes(f.toPath()), StandardCharsets.UTF_8));
      if (a.getInt("version") != 1 || models.put(a.getString("api"), a) != null)
        throw new IllegalStateException(
            "Invalid or duplicate platform artifact");
    }
  }
  public static Object call(int op, Object self, Object[] args)
      throws Exception {
    Object arg = args.length > 0 ? args[0] : null;
    if (op == 7)
      return SpeechRecognizer.createSpeechRecognizer(
          (android.content.Context)arg);
    if (op >= 12 && op <= 16) {
      // Custom local/downloaded models keep their native loader and test
      // model files. Only the built-in default labeler is replaced.
      if (op == 12 &&
          !arg.getClass().getName().equals(
              "com.google.mlkit.vision.label.defaults.ImageLabelerOptions"))
        return PROCEED;
      if (op == 14 &&
          !arg.getClass().getName().equals(
              "com.google.mlkit.vision.objects.defaults.ObjectDetectorOptions"))
        return PROCEED;
      // A factory is static, or Kotlin's object's (Generation's).
      ClassLoader loader = (self instanceof Class ? (Class<?>)self
                                                  : self.getClass())
                               .getClassLoader();
      if (op == 16)
        return languageModel(loader);
      String[] interfaces = {"com.google.mlkit.vision.label.ImageLabeler",
                             "com.google.mlkit.vision.text.TextRecognizer",
                             "com.google.mlkit.vision.objects.ObjectDetector",
                             "com.google.mlkit.vision.segmentation.Segmenter"};
      String[] selectors = {
          "android.mlkit.image-labeling", "android.mlkit.text-recognition",
          "android.mlkit.object-detection", "android.mlkit.segmentation"};
      Class<?> api = Class.forName(interfaces[op - 12], true, loader);
      JSONObject artifact = models.get(selectors[op - 12]);
      boolean[] closed = {false};
      return Proxy.newProxyInstance(
          loader, new Class<?>[] {api}, (proxy, method, a) -> {
            switch (method.getName()) {
            case "close":
              closed[0] = true;
              return null;
            case "process":
              if (a.length == 1) {
                Class<?> tasks = Class.forName(
                    "com.google.android.gms.tasks.Tasks", true, loader);
                try {
                  if (closed[0])
                    throw new IllegalStateException("Detector is closed");
                  Bitmap bitmap;
                  synchronized (images) { bitmap = images.get(a[0]); }
                  if (bitmap == null)
                    throw new UnsupportedOperationException(
                        "This adapter supports InputImage.fromBitmap inputs");
                  return tasks.getMethod("forResult", Object.class)
                      .invoke(null, visionAnswer(loader, artifact, bitmap));
                } catch (Exception e) {
                  return tasks.getMethod("forException", Exception.class)
                      .invoke(null, e);
                }
              }
              break;
            case "hashCode":
              return System.identityHashCode(proxy);
            case "equals":
              return proxy == a[0];
            case "toString":
              return api.getSimpleName() + " fixture";
            }
            throw new UnsupportedOperationException(
                "Unsupported detector method: " + method.getName());
          });
    }
    if (op >= 2 && op <= 6) {
      Recognition r;
      synchronized (recognizers) {
        r = recognizers.computeIfAbsent(self, key -> new Recognition());
      }
      if (op == 2)
        r.listener = (RecognitionListener)arg;
      if (op == 3) {
        if (r.listener == null)
          throw new IllegalStateException(
              "Set a RecognitionListener before starting recognition");
        if (r.active) {
          main.post(
              () -> r.listener.onError(SpeechRecognizer.ERROR_RECOGNIZER_BUSY));
          return null;
        }
        r.active = true;
        int generation = ++r.generation;
        Intent intent = (Intent)arg;
        ParcelFileDescriptor source =
            android.os.Build.VERSION.SDK_INT >= 33
                ? intent.getParcelableExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE)
                : null;
        main.post(() -> {
          if (r.active && r.generation == generation) {
            r.listener.onReadyForSpeech(new Bundle());
            if (source != null)
              hearSource(r, generation, intent, source);
            else
              poll(r, generation);
          }
        });
      }
      if (op == 4 && r.active) {
        r.active = false;
        main.post(() -> r.listener.onError(SpeechRecognizer.ERROR_NO_MATCH));
      }
      if (op == 5 || op == 6) {
        r.active = false;
        if (op == 6)
          synchronized (recognizers) { recognizers.remove(self); }
      }
      return null;
    }
    if (op == 10) {
      int rotation = (Integer)args[1];
      Object image = ((Class<?>)self)
                         .getMethod("fromBitmap", Bitmap.class, int.class)
                         .invoke(null, arg, rotation);
      Bitmap bitmap = (Bitmap)arg;
      if (rotation != 0) {
        Matrix rotate = new Matrix();
        rotate.postRotate(rotation);
        bitmap = Bitmap.createBitmap(bitmap, 0, 0, bitmap.getWidth(),
                                     bitmap.getHeight(), rotate, true);
      }
      synchronized (images) { images.put(image, bitmap); }
      return image;
    }
    if (op >= 30 && op <= 32)
      return audioRecord(op, (AudioRecord)self, args);
    if (op >= 40 && op <= 59)
      return mediaRecorder(op, (MediaRecorder)self, args);
    return PROCEED;
  }
  private static Object visionAnswer(ClassLoader loader, JSONObject a,
                                     Bitmap bitmap) throws Exception {
    int at = picture(a, bitmap);
    JSONObject c = a.getJSONObject("contract"),
               answer = a.getJSONArray("answers").getJSONObject(at);
    JSONArray outputs = c.getJSONArray("outputs");
    JSONObject out = outputs.getJSONObject(0);
    JSONArray values = answer.getJSONArray(out.getString("name"));
    String task = c.getString("task");
    if (task.equals("classification")) {
      JSONArray labels = out.getJSONArray("labels");
      List<Integer> order = new ArrayList<>();
      for (int i = 0; i < labels.length(); i++)
        order.add(i);
      order.sort(
          (x, y) -> Double.compare(values.optDouble(y), values.optDouble(x)));
      List<Object> found = new ArrayList<>();
      for (int i : order)
        found.add(mock(loader, "com.google.mlkit.vision.label.ImageLabel",
                       fields("getText", labels.getString(i), "getConfidence",
                              (float)values.getDouble(i), "getIndex", i)));
      return found;
    }
    if (task.equals("text-recognition")) {
      JSONArray labels = out.getJSONArray("labels");
      StringBuilder text = new StringBuilder();
      int last = -1;
      for (int s = 0; s < values.length() / labels.length(); s++) {
        int best = 0;
        for (int k = 1; k < labels.length(); k++)
          if (values.getDouble(s * labels.length() + k) >
              values.getDouble(s * labels.length() + best))
            best = k;
        if (best != last)
          text.append(labels.getString(best));
        last = best;
      }
      String value = text.toString();
      List<Object> blocks = new ArrayList<>();
      if (!value.isEmpty()) {
        List<Object> lines = new ArrayList<>();
        for (String line : value.split("\n", -1)) {
          List<Object> elements = new ArrayList<>();
          for (String word : line.split("\\s+")) {
            List<Object> symbols = new ArrayList<>();
            for (int i = 0; i < word.length(); i++)
              symbols.add(textPart(loader, "Symbol", word.substring(i, i + 1),
                                   bitmap, Collections.emptyMap()));
            elements.add(textPart(
                loader, "Element", word, bitmap,
                fields("getSymbols", Collections.unmodifiableList(symbols))));
          }
          lines.add(textPart(
              loader, "Line", line, bitmap,
              fields("getElements", Collections.unmodifiableList(elements))));
        }
        blocks.add(
            textPart(loader, "TextBlock", value, bitmap,
                     fields("getLines", Collections.unmodifiableList(lines))));
      }
      return mock(loader, "com.google.mlkit.vision.text.Text",
                  fields("getText", value, "getTextBlocks",
                         Collections.unmodifiableList(blocks)));
    }
    if (task.equals("detection")) {
      Map<String, JSONObject> roles = new HashMap<>();
      for (int i = 0; i < outputs.length(); i++) {
        JSONObject o = outputs.getJSONObject(i);
        roles.put(o.getString("detection"), o);
      }
      JSONArray boxes =
                    answer.getJSONArray(roles.get("boxes").getString("name")),
                classes =
                    answer.getJSONArray(roles.get("classes").getString("name")),
                scores =
                    answer.getJSONArray(roles.get("scores").getString("name"));
      JSONArray labels = roles.get("classes").getJSONArray("labels");
      int count =
          answer.getJSONArray(roles.get("count").getString("name")).getInt(0);
      List<Object> objects = new ArrayList<>();
      for (int i = 0; i < count; i++) {
        Rect box = new Rect(
            (int)Math.round(boxes.getDouble(i * 4 + 1) * bitmap.getWidth()),
            (int)Math.round(boxes.getDouble(i * 4) * bitmap.getHeight()),
            (int)Math.round(boxes.getDouble(i * 4 + 3) * bitmap.getWidth()),
            (int)Math.round(boxes.getDouble(i * 4 + 2) * bitmap.getHeight()));
        int k = classes.getInt(i);
        Object label =
            mock(loader, "com.google.mlkit.vision.objects.DetectedObject$Label",
                 fields("getText", labels.getString(k), "getConfidence",
                        (float)scores.getDouble(i), "getIndex", k));
        objects.add(mock(
            loader, "com.google.mlkit.vision.objects.DetectedObject",
            fields("getBoundingBox", box, "getLabels",
                   Collections.singletonList(label), "getTrackingId", null)));
      }
      return objects;
    }
    if (task.equals("segmentation")) {
      JSONArray shape = out.getJSONArray("shape");
      int width = shape.getInt(shape.length() - 1),
          height = shape.getInt(shape.length() - 2);
      ByteBuffer buffer = ByteBuffer.allocateDirect(width * height * 4)
                              .order(ByteOrder.nativeOrder());
      for (int i = 0; i < width * height; i++)
        buffer.putFloat((float)values.getDouble(i));
      buffer.rewind();
      return mock(
          loader, "com.google.mlkit.vision.segmentation.SegmentationMask",
          fields("getBuffer", buffer, "getWidth", width, "getHeight", height));
    }
    throw new UnsupportedOperationException("Unsupported vision task " + task);
  }
  private static Object textPart(ClassLoader loader, String kind, String text,
                                 Bitmap bitmap, Map<String, Object> extra)
      throws Exception {
    int w = bitmap.getWidth(), h = bitmap.getHeight();
    Map<String, Object> fields =
        fields("getText", text, "getBoundingBox", new Rect(0, 0, w, h),
               "getCornerPoints",
               new Point[] {new Point(0, 0), new Point(w, 0), new Point(w, h),
                            new Point(0, h)},
               "getRecognizedLanguage", "und", "getConfidence", 1.0f,
               "getAngle", 0.0f);
    fields.putAll(extra);
    return mock(loader, "com.google.mlkit.vision.text.Text$" + kind, fields);
  }
  private static Object languageModel(ClassLoader loader) throws Exception {
    Class<?> api = Class.forName(
        "com.google.mlkit.genai.prompt.GenerativeModel", true, loader);
    JSONObject artifact = models.get("android.gemini-nano");
    boolean[] closed = {false};
    return Proxy.newProxyInstance(
        loader, new Class<?>[] {api}, (proxy, method, args) -> {
          String name = method.getName();
          if (name.equals("close")) {
            closed[0] = true;
            return null;
          }
          if (closed[0])
            throw new IllegalStateException("GenerativeModel is closed");
          if (name.equals("warmup") || name.equals("clearImplicitCaches"))
            return unit(loader);
          if (name.equals("checkStatus"))
            return Class
                .forName("com.google.mlkit.genai.common.FeatureStatus", true,
                         loader)
                .getField("AVAILABLE")
                .get(null);
          if (name.equals("getBaseModelName"))
            return "axx fixture";
          if (name.equals("isSystemPromptAvailable") ||
              name.equals("isThinkingModeAvailable"))
            return true;
          if (name.equals("isStructuredOutputFeatureAvailable") ||
              name.equals("isCachingFeatureAvailable"))
            return false;
          if (name.equals("generateContent") ||
              name.equals("generateContentStream")) {
            String prompt = promptText(args[0]);
            String text = textAnswer(artifact, prompt);
            Object stop =
                Class
                    .forName(
                        "com.google.mlkit.genai.prompt.Candidate$FinishReason",
                        true, loader)
                    .getField("STOP")
                    .get(null);
            Object candidate =
                mock(loader, "com.google.mlkit.genai.prompt.Candidate",
                     fields("getText", text, "getFinishReason", stop));
            Object response = mock(
                loader, "com.google.mlkit.genai.prompt.GenerateContentResponse",
                fields("getCandidates", Collections.singletonList(candidate),
                       "getThoughtProcess", Collections.emptyList()));
            if (name.equals("generateContentStream")) {
              Class<?> flow = Class.forName("kotlinx.coroutines.flow.Flow",
                                            true, loader),
                       collector = Class.forName(
                           "kotlinx.coroutines.flow.FlowCollector", true,
                           loader),
                       continuation = Class.forName(
                           "kotlin.coroutines.Continuation", true, loader);
              return Proxy.newProxyInstance(
                  loader, new Class<?>[] {flow}, (f, m, a) -> {
                    if (m.getName().equals("collect"))
                      return collector
                          .getMethod("emit", Object.class, continuation)
                          .invoke(a[0], response, a[1]);
                    throw new UnsupportedOperationException(m.getName());
                  });
            }
            return response;
          }
          if (name.equals("toString"))
            return "GenerativeModel fixture";
          if (name.equals("hashCode"))
            return System.identityHashCode(proxy);
          if (name.equals("equals"))
            return proxy == args[0];
          throw new UnsupportedOperationException(
              "Unsupported GenerativeModel method: " + name);
        });
  }
  private static Object unit(ClassLoader loader) throws Exception {
    return Class.forName("kotlin.Unit", true, loader)
        .getField("INSTANCE")
        .get(null);
  }
  private static String promptText(Object value) throws Exception {
    if (value instanceof String)
      return (String)value;
    if (value instanceof Iterable) {
      StringBuilder text = new StringBuilder();
      for (Object item : (Iterable<?>)value)
        text.append(promptText(item)).append('\n');
      return text.toString();
    }
    if (value == null)
      return "";
    for (String getter : new String[] {"getContents", "getParts", "getText"}) {
      try {
        return promptText(value.getClass().getMethod(getter).invoke(value));
      } catch (NoSuchMethodException ignored) {
      }
    }
    throw new UnsupportedOperationException(
        "The text fixture does not accept " + value.getClass().getName());
  }
  private static boolean word(byte b) {
    int c = b & 255;
    return c >= 128 || c >= 48 && c <= 57 || c >= 65 && c <= 90 ||
        c >= 97 && c <= 122;
  }
  private static String textAnswer(JSONObject a, String prompt)
      throws Exception {
    byte[] text = prompt.getBytes(StandardCharsets.UTF_8);
    JSONArray keys = a.optJSONArray("texts"), answers =
                                                  a.getJSONArray("generated");
    for (int i = 0; keys != null && i < keys.length(); i++) {
      byte[] key = keys.getString(i).getBytes(StandardCharsets.UTF_8);
      for (int p = 0; p + key.length <= text.length; p++) {
        boolean equal = true;
        for (int j = 0; j < key.length; j++)
          if (text[p + j] != key[j]) {
            equal = false;
            break;
          }
        int end = p + key.length;
        if (equal && (p == 0 || !word(text[p - 1]) || !word(key[0])) &&
            (end == text.length || !word(text[end]) ||
             !word(key[key.length - 1])))
          return answers.getString(i);
      }
    }
    return answers.getString(answers.length() - 1);
  }

  // The microphone step's recordings: each JSON file in the axx-models
  // folder's .microphone, in order, taken once (its .taken marker) by the
  // first listener, a recognizer or a capture of the app's own.
  private static JSONObject take() throws Exception {
    synchronized (Bridge.class) {
      File[] files = new File(folder, ".microphone").listFiles();
      if (files == null)
        return null;
      Arrays.sort(files);
      for (File file : files) {
        if (!file.getName().endsWith(".json"))
          continue;
        File taken = new File(file.getPath() + ".taken");
        if (taken.exists())
          continue;
        JSONObject recording;
        try {
          recording = new JSONObject(new String(
              Files.readAllBytes(file.toPath()), StandardCharsets.UTF_8));
        } catch (org.json.JSONException incomplete) {
          continue; // the microphone step may still be writing it
        }
        if (!taken.createNewFile())
          continue;
        return recording;
      }
      return null;
    }
  }
  private static double[] samples(JSONObject recording) throws Exception {
    JSONArray values = recording.getJSONArray("samples");
    double[] out = new double[values.length()];
    for (int i = 0; i < out.length; i++)
      out[i] = values.getDouble(i);
    return out;
  }
  // What the speech stub a hears in a queued recording: the mapping axx
  // matched it with, when it queued it.
  private static String heard(JSONObject a, JSONObject recording)
      throws Exception {
    JSONArray generated = a.getJSONArray("generated");
    JSONObject matches = recording.optJSONObject("matches");
    if (matches != null && matches.has(a.getString("api"))) {
      int at = matches.getInt(a.getString("api"));
      if (at >= 0 && at < generated.length())
        return generated.getString(at);
    }
    JSONObject audio = input(a).getJSONObject("audio");
    if (!audio.optString("features").isEmpty())
      throw new IllegalStateException(
          "The recording was not matched to this speech stub; rebuild the "
          + "stub and queue the recording again");
    if (recording.getInt("rate") != audio.getInt("sampleRate"))
      throw new IllegalStateException(
          "Recording sample rate differs from the contract");
    return generated.getString(sound(a, samples(recording)));
  }
  // What the speech stub a hears in audio the app gives the recognizer
  // itself, which axx never matched: as axx matches it.
  private static String heard(JSONObject a, double[] samples, int rate)
      throws Exception {
    JSONObject audio = input(a).getJSONObject("audio");
    int want = audio.getInt("sampleRate");
    samples = resample(samples, rate, want);
    JSONArray generated = a.getJSONArray("generated");
    if (audio.optString("features").equals("log-mel")) {
      JSONArray shape = input(a).getJSONArray("shape");
      int mels = shape.getInt(shape.length() - 2),
          frames = shape.getInt(shape.length() - 1);
      return generated.getString(
          nearest(a, features(logMel(samples, want, mels, frames), mels,
                              frames)));
    }
    if (!audio.optString("features").isEmpty())
      throw new UnsupportedOperationException(
          "Unsupported speech features " + audio.optString("features"));
    return generated.getString(sound(a, samples));
  }
  private static void recognized(Recognition r, String text) {
    ArrayList<String> texts = new ArrayList<>();
    texts.add(text);
    Bundle result = new Bundle();
    result.putStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION, texts);
    result.putFloatArray(SpeechRecognizer.CONFIDENCE_SCORES, new float[] {1});
    r.listener.onBeginningOfSpeech();
    r.listener.onEndOfSpeech();
    r.active = false;
    r.listener.onResults(result);
  }
  private static void poll(Recognition r, int generation) {
    if (!r.active || r.generation != generation)
      return;
    try {
      JSONObject recording = take();
      if (recording != null) {
        recognized(r, heard(models.get("android.speech"), recording));
        return;
      }
      main.postDelayed(() -> poll(r, generation), 25);
    } catch (Exception error) {
      android.util.Log.e("AXXModels", "AXX-E0704: " + error.getMessage());
      r.active = false;
      r.listener.onError(SpeechRecognizer.ERROR_AUDIO);
    }
  }
  // A recognition of audio the app writes to a pipe (EXTRA_AUDIO_SOURCE):
  // the recognizer hears it to its end.
  private static void hearSource(Recognition r, int generation, Intent intent,
                                 ParcelFileDescriptor source) {
    int rate = intent.getIntExtra(
            RecognizerIntent.EXTRA_AUDIO_SOURCE_SAMPLING_RATE, 16000),
        channels = intent.getIntExtra(
            RecognizerIntent.EXTRA_AUDIO_SOURCE_CHANNEL_COUNT, 1),
        encoding = intent.getIntExtra(
            RecognizerIntent.EXTRA_AUDIO_SOURCE_ENCODING,
            AudioFormat.ENCODING_PCM_16BIT);
    Thread reader = new Thread(() -> {
      String text = null;
      Exception failure = null;
      try (InputStream in =
               new ParcelFileDescriptor.AutoCloseInputStream(source)) {
        ByteArrayOutputStream all = new ByteArrayOutputStream();
        byte[] chunk = new byte[8192];
        for (int n; (n = in.read(chunk)) > 0;)
          all.write(chunk, 0, n);
        text = heard(models.get("android.speech"),
                     decode(all.toByteArray(), encoding, channels), rate);
      } catch (Exception e) {
        failure = e;
      }
      String answer = text;
      Exception error = failure;
      main.post(() -> {
        if (!r.active || r.generation != generation)
          return;
        if (error != null) {
          android.util.Log.e("AXXModels", "AXX-E0704: " + error.getMessage());
          r.active = false;
          r.listener.onError(SpeechRecognizer.ERROR_AUDIO);
        } else
          recognized(r, answer);
      });
    }, "axx-speech-source");
    reader.setDaemon(true);
    reader.start();
  }
  private static int bytesPerSample(int encoding) {
    switch (encoding) {
    case AudioFormat.ENCODING_PCM_8BIT:
      return 1;
    case AudioFormat.ENCODING_PCM_FLOAT:
    case 22: // ENCODING_PCM_32BIT
      return 4;
    case 21: // ENCODING_PCM_24BIT_PACKED
      return 3;
    default:
      return 2;
    }
  }
  // PCM of the encoding, native endian, as one channel from -1 to 1.
  private static double[] decode(byte[] pcm, int encoding, int channels) {
    int size = bytesPerSample(encoding);
    ByteBuffer in = ByteBuffer.wrap(pcm).order(ByteOrder.nativeOrder());
    double[] out = new double[pcm.length / size / channels];
    for (int i = 0; i < out.length; i++) {
      double sum = 0;
      for (int c = 0; c < channels; c++) {
        int at = (i * channels + c) * size;
        switch (encoding) {
        case AudioFormat.ENCODING_PCM_8BIT:
          sum += ((pcm[at] & 255) - 128) / 128.0;
          break;
        case AudioFormat.ENCODING_PCM_FLOAT:
          sum += in.getFloat(at);
          break;
        case 22:
          sum += in.getInt(at) / 2147483648.0;
          break;
        case 21:
          sum += ((pcm[at] & 255) | (pcm[at + 1] & 255) << 8 |
                  pcm[at + 2] << 16) /
                 8388608.0;
          break;
        default:
          sum += in.getShort(at) / 32768.0;
        }
      }
      out[i] = sum / channels;
    }
    return out;
  }
  // Writes samples, one channel from -1 to 1, as PCM of the encoding, each
  // in every channel.
  private static void encode(double[] samples, int from, int count,
                             int encoding, int channels, ByteBuffer out) {
    for (int i = 0; i < count; i++) {
      double v = Math.max(-1, Math.min(1, samples[from + i]));
      for (int c = 0; c < channels; c++) {
        switch (encoding) {
        case AudioFormat.ENCODING_PCM_8BIT:
          out.put((byte)(Math.round(v * 127) + 128));
          break;
        case AudioFormat.ENCODING_PCM_FLOAT:
          out.putFloat((float)v);
          break;
        case 22:
          out.putInt((int)Math.round(v * 2147483647.0));
          break;
        case 21: {
          int s = (int)Math.round(v * 8388607);
          out.put((byte)s).put((byte)(s >> 8)).put((byte)(s >> 16));
          break;
        }
        default:
          out.putShort((short)Math.round(v * 32767));
        }
      }
    }
  }

  // A capture of the app's own (an AudioRecord or a MediaRecorder) hears the
  // microphone step's recordings at its rate, each from when it takes it,
  // then silence: what it heard, at each of its frames since it started.
  private static final class Hearing {
    final int rate;
    long position; // the frames heard so far
    double[] playing = new double[0];
    long playingAt;
    final List<double[]> heard = new ArrayList<>();
    final boolean keep;
    Hearing(int rate, boolean keep) {
      this.rate = rate;
      this.keep = keep;
    }
    // The next count frames: the recording playing, or the next one queued,
    // then silence.
    double[] next(int count) throws Exception {
      if (position >= playingAt + playing.length) {
        JSONObject recording = take();
        if (recording != null) {
          playing =
              resample(samples(recording), recording.getInt("rate"), rate);
          playingAt = position;
        }
      }
      double[] out = new double[count];
      for (int i = 0; i < count; i++) {
        long at = position + i - playingAt;
        if (at >= 0 && at < playing.length)
          out[i] = playing[(int)at];
      }
      position += count;
      if (keep)
        heard.add(out);
      return out;
    }
    double[] all() {
      int n = 0;
      for (double[] part : heard)
        n += part.length;
      double[] out = new double[n];
      int at = 0;
      for (double[] part : heard) {
        System.arraycopy(part, 0, out, at, part.length);
        at += part.length;
      }
      return out;
    }
  }

  private static final Map<AudioRecord, Hearing> captures =
      new WeakHashMap<>();
  // An AudioRecord records as the device has it, to keep its pace; what it
  // reads is the microphone step's.
  private static Object audioRecord(int op, AudioRecord record, Object[] args)
      throws Exception {
    if (op == 30) {
      synchronized (captures) {
        captures.put(record, new Hearing(record.getSampleRate(), false));
      }
      return PROCEED;
    }
    if (op == 31) {
      synchronized (captures) { captures.remove(record); }
      return PROCEED;
    }
    Hearing hearing;
    synchronized (captures) { hearing = captures.get(record); }
    if (hearing == null)
      return PROCEED;
    int read;
    Method real = AudioRecord.class.getMethod("read", types(args));
    try {
      read = (Integer)real.invoke(record, args);
    } catch (java.lang.reflect.InvocationTargetException e) {
      return PROCEED; // the real read fails the same way, for the app
    }
    if (read <= 0)
      return read;
    int encoding = record.getAudioFormat(), channels = record.getChannelCount();
    Object data = args[0];
    if (data instanceof short[] || data instanceof float[]) {
      int offset = (Integer)args[1], frames = read / channels;
      double[] heard = hearing.next(frames);
      for (int i = 0; i < frames; i++)
        for (int c = 0; c < channels; c++) {
          double v = Math.max(-1, Math.min(1, heard[i]));
          if (data instanceof short[])
            ((short[])data)[offset + i * channels + c] =
                (short)Math.round(v * 32767);
          else
            ((float[])data)[offset + i * channels + c] = (float)v;
        }
      return read;
    }
    int frames = read / bytesPerSample(encoding) / channels;
    ByteBuffer out;
    if (data instanceof byte[])
      out = ByteBuffer.wrap((byte[])data, (Integer)args[1], read);
    else {
      // AudioRecord writes a buffer from its start, wherever its position.
      out = ((ByteBuffer)data).duplicate();
      out.clear();
    }
    out.order(ByteOrder.nativeOrder());
    encode(hearing.next(frames), 0, frames, encoding, channels, out);
    return read;
  }
  private static Class<?>[] types(Object[] args) {
    Class<?>[] out = new Class<?>[args.length];
    for (int i = 0; i < args.length; i++)
      out[i] = args[i] instanceof Integer ? int.class
               : args[i] instanceof ByteBuffer ? ByteBuffer.class
                                               : args[i].getClass();
    return out;
  }

  // The native capture controls are registered with JNI, while public Java
  // setters and prepare use breakpoints. The real recorder never starts its
  // input: public codecs write what our clock heard, in the requested format.
  private static final class Recording {
    int source = -1, format, encoder, rate, channels = 1, bitRate;
    Object output;
    Context context;
    ParcelFileDescriptor destination;
    boolean prepared;
    Hearing hearing;
    long started, pausedAt, paused, amplitudeAt;
    ScheduledFuture<?> listening;
    Exception error;
  }
  private static final Map<MediaRecorder, Recording> recordings =
      new WeakHashMap<>();
  private static ScheduledExecutorService clock;
  private static Object mediaRecorder(int op, MediaRecorder recorder,
                                      Object[] args) throws Exception {
    if (op >= 56)
      throw new UnsupportedOperationException(
          "MediaRecorder fixture capture does not support video, automatic " +
          "duration/file-size limits or output rotation; use audio-only " +
          "recording with explicit stop");
    Recording r;
    synchronized (recordings) {
      r = recordings.computeIfAbsent(recorder, key -> new Recording());
    }
    if (op == 53) {
      // getMaxAmplitude: the loudest of what it heard since it last said.
      synchronized (r) {
        if (r.hearing == null)
          return 0;
        if (r.pausedAt == 0)
          catchUp(r);
        if (r.error != null)
          throw r.error;
        double[] heard = r.hearing.all();
        int from = (int)Math.min(r.amplitudeAt, heard.length);
        r.amplitudeAt = heard.length;
        double top = 0;
        for (int i = from; i < heard.length; i++)
          top = Math.max(top, Math.abs(heard[i]));
        return (int)Math.round(Math.min(top, 1) * 32767);
      }
    }
    if (op == 52) {
      // Finish the file before stop returns, as the real API does.
      Hearing hearing;
      synchronized (r) {
        hearing = r.hearing;
        if (hearing == null)
          throw new IllegalStateException("MediaRecorder is not recording; " +
                                          "call prepare and start before stop");
        stopListening(r);
        r.hearing = null;
        r.prepared = false;
      }
      try {
        if (r.error != null)
          throw r.error;
        rewrite(r, hearing);
      } finally {
        closeRecording(r);
      }
      return null;
    }
    synchronized (r) {
      switch (op) {
      case 40:
        r.source = (Integer)args[0];
        switch (r.source) {
        case MediaRecorder.AudioSource.DEFAULT:
        case MediaRecorder.AudioSource.MIC:
        case MediaRecorder.AudioSource.CAMCORDER:
        case MediaRecorder.AudioSource.VOICE_RECOGNITION:
        case MediaRecorder.AudioSource.VOICE_COMMUNICATION:
        case MediaRecorder.AudioSource.UNPROCESSED:
        case MediaRecorder.AudioSource.VOICE_PERFORMANCE:
          break;
        default:
          throw new UnsupportedOperationException(
              "The fixture feeds microphone input only; use a public " +
              "microphone audio source");
        }
        break;
      case 41:
        r.format = (Integer)args[0];
        break;
      case 42:
        r.encoder = (Integer)args[0];
        break;
      case 43:
        r.rate = (Integer)args[0];
        break;
      case 44:
        r.channels = (Integer)args[0];
        break;
      case 45:
        r.bitRate = (Integer)args[0];
        break;
      case 46:
        r.output = args[0];
        break;
      case 47: // start
        if (r.context == null ||
            r.context.checkSelfPermission("android.permission.RECORD_AUDIO") !=
                PackageManager.PERMISSION_GRANTED)
          throw new SecurityException(
              "MediaRecorder needs RECORD_AUDIO permission; allow the app's " +
              "microphone permission before recording");
        if (!r.prepared || r.hearing != null)
          throw new IllegalStateException(
              "MediaRecorder is not prepared; call prepare before start");
        r.hearing = new Hearing(r.rate > 0 ? r.rate : 8000, true);
        r.hearing.next(0); // the first listener to start claims the recording
        r.started = System.nanoTime();
        r.paused = r.pausedAt = r.amplitudeAt = 0;
        startListening(r);
        break;
      case 48: // pause
        if (r.hearing != null && r.pausedAt == 0) {
          catchUp(r);
          r.pausedAt = System.nanoTime();
        }
        break;
      case 49: // resume
        if (r.pausedAt != 0) {
          r.paused += System.nanoTime() - r.pausedAt;
          r.pausedAt = 0;
        }
        break;
      case 54: // prepare
        if (r.context == null)
          throw new UnsupportedOperationException(
              "Cannot check recorder microphone permission; use an Android " +
              "runtime with the public MediaRecorder(Context) constructor");
        if (r.source < 0 || r.output == null || r.hearing != null)
          throw new IllegalStateException(
              "MediaRecorder needs an audio source and output file before " +
              "prepare");
        closeRecording(r);
        if (r.output instanceof FileDescriptor)
          r.destination = ParcelFileDescriptor.dup((FileDescriptor)r.output);
        else {
          File file = r.output instanceof File ? (File)r.output
                                               : new File((String)r.output);
          r.destination = ParcelFileDescriptor.open(
              file, ParcelFileDescriptor.MODE_CREATE |
                        ParcelFileDescriptor.MODE_TRUNCATE |
                        ParcelFileDescriptor.MODE_READ_WRITE);
        }
        r.prepared = true;
        r.error = null;
        break;
      case 55: // the public constructor, also used by the no-argument one
        r.context = ((Context)args[0]).getApplicationContext();
        return PROCEED;
      default: // reset, release
        stopListening(r);
        r.hearing = null;
        r.prepared = false;
        closeRecording(r);
        if (op == 50) {
          r.source = -1;
          r.format = r.encoder = r.rate = r.bitRate = 0;
          r.channels = 1;
          r.output = null;
        }
        // These public Java methods still release/reset the real object's
        // resources, which its unchanged constructor allocated.
        return PROCEED;
      }
    }
    return null;
  }
  private static void closeRecording(Recording r) throws Exception {
    if (r.destination != null)
      r.destination.close();
    r.destination = null;
  }
  // A recording hears the recordings, as time passes, while it records.
  private static void startListening(Recording r) {
    synchronized (Bridge.class) {
      if (clock == null)
        clock = Executors.newSingleThreadScheduledExecutor(task -> {
          Thread t = new Thread(task, "axx-microphone");
          t.setDaemon(true);
          return t;
        });
    }
    r.listening = clock.scheduleAtFixedRate(() -> {
      synchronized (r) {
        if (r.pausedAt == 0)
          catchUp(r);
      }
    }, 0, 25, TimeUnit.MILLISECONDS);
  }
  // What the recorder has heard, up to now.
  private static void catchUp(Recording r) {
    if (r.hearing == null)
      return;
    long elapsed = System.nanoTime() - r.started - r.paused;
    long due = elapsed * r.hearing.rate / 1000000000L;
    try {
      if (due > r.hearing.position)
        r.hearing.next((int)(due - r.hearing.position));
    } catch (Exception e) {
      r.error = e;
      if (r.listening != null)
        r.listening.cancel(false);
    }
  }
  private static void stopListening(Recording r) {
    if (r.listening != null)
      r.listening.cancel(false);
    r.listening = null;
    if (r.pausedAt == 0)
      catchUp(r);
  }
  // The file a recorder wrote, made again of what it heard, with its
  // settings: its encoder's codec, in its format's container.
  private static void rewrite(Recording r, Hearing hearing) throws Exception {
    double[] heard = hearing.all();
    int encoder = r.encoder, format = r.format;
    String mime;
    switch (encoder) {
    case MediaRecorder.AudioEncoder.AAC:
    case MediaRecorder.AudioEncoder.HE_AAC:
    case MediaRecorder.AudioEncoder.AAC_ELD:
      mime = MediaFormat.MIMETYPE_AUDIO_AAC;
      break;
    case MediaRecorder.AudioEncoder.AMR_WB:
      mime = MediaFormat.MIMETYPE_AUDIO_AMR_WB;
      break;
    case 7: // OPUS
      mime = MediaFormat.MIMETYPE_AUDIO_OPUS;
      break;
    case MediaRecorder.AudioEncoder.DEFAULT:
    case MediaRecorder.AudioEncoder.AMR_NB:
      mime = MediaFormat.MIMETYPE_AUDIO_AMR_NB;
      break;
    default:
      throw new UnsupportedOperationException(
          "Unsupported recorder audio encoder; use AAC, AMR or Opus");
    }
    int rate = hearing.rate, channels = Math.max(r.channels, 1);
    int bitRate = r.bitRate > 0 ? r.bitRate : 12200;
    MediaFormat media = MediaFormat.createAudioFormat(mime, rate, channels);
    media.setInteger(MediaFormat.KEY_BIT_RATE, bitRate);
    if (mime.equals(MediaFormat.MIMETYPE_AUDIO_AAC))
      media.setInteger(
          MediaFormat.KEY_AAC_PROFILE,
          encoder == MediaRecorder.AudioEncoder.HE_AAC
              ? MediaCodecInfo.CodecProfileLevel.AACObjectHE
          : encoder == MediaRecorder.AudioEncoder.AAC_ELD
              ? MediaCodecInfo.CodecProfileLevel.AACObjectELD
              : MediaCodecInfo.CodecProfileLevel.AACObjectLC);
    File temp = File.createTempFile("axx-recording", ".tmp");
    try {
      int container;
      switch (format) {
      case MediaRecorder.OutputFormat.MPEG_4:
        container = MediaMuxer.OutputFormat.MUXER_OUTPUT_MPEG_4;
        break;
      case 9: // WEBM
        container = MediaMuxer.OutputFormat.MUXER_OUTPUT_WEBM;
        break;
      case 11: // OGG
        container = 4; // MUXER_OUTPUT_OGG
        break;
      case MediaRecorder.OutputFormat.AMR_NB:
      case MediaRecorder.OutputFormat.AMR_WB:
      case MediaRecorder.OutputFormat.AAC_ADTS:
        container = -1; // the codec's frames, as they are
        break;
      case MediaRecorder.OutputFormat.DEFAULT:
      case MediaRecorder.OutputFormat.THREE_GPP:
        container = MediaMuxer.OutputFormat.MUXER_OUTPUT_3GPP;
        break;
      default:
        throw new UnsupportedOperationException(
            "Unsupported recorder output format; use MPEG-4, 3GPP, WebM, " +
            "Ogg, raw AMR or ADTS");
      }
      encodeFile(media, heard, channels, container, format, temp);
      byte[] file = Files.readAllBytes(temp.toPath());
      // prepare retained its own descriptor, so the app may close the one
      // it supplied. Closing this stream never closes the app's descriptor.
      try (OutputStream out = new ParcelFileDescriptor.AutoCloseOutputStream(
               ParcelFileDescriptor.dup(r.destination.getFileDescriptor()))) {
        FileChannel channel = ((FileOutputStream)out).getChannel();
        channel.truncate(0);
        channel.position(0);
        out.write(file);
      }
    } finally {
      temp.delete();
    }
  }
  private static void encodeFile(MediaFormat media, double[] heard,
                                 int channels, int container, int format,
                                 File out) throws Exception {
    String mime = media.getString(MediaFormat.KEY_MIME);
    MediaCodec codec = MediaCodec.createEncoderByType(mime);
    MediaMuxer muxer = container >= 0
                           ? new MediaMuxer(out.getPath(), container)
                           : null;
    OutputStream raw = container < 0 ? new FileOutputStream(out) : null;
    try {
      codec.configure(media, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE);
      codec.start();
      if (raw != null && format == MediaRecorder.OutputFormat.AMR_NB)
        raw.write("#!AMR\n".getBytes(StandardCharsets.US_ASCII));
      if (raw != null && format == MediaRecorder.OutputFormat.AMR_WB)
        raw.write("#!AMR-WB\n".getBytes(StandardCharsets.US_ASCII));
      int rate = media.getInteger(MediaFormat.KEY_SAMPLE_RATE), track = -1;
      int fed = 0;
      boolean ended = false, done = false;
      MediaCodec.BufferInfo info = new MediaCodec.BufferInfo();
      while (!done) {
        if (!ended) {
          int in = codec.dequeueInputBuffer(10000);
          if (in >= 0) {
            ByteBuffer buffer = codec.getInputBuffer(in);
            buffer.order(ByteOrder.nativeOrder());
            int frames = Math.min(buffer.remaining() / 2 / channels,
                                  heard.length - fed);
            encode(heard, fed, frames, AudioFormat.ENCODING_PCM_16BIT,
                   channels, buffer);
            long time = fed * 1000000L / rate;
            fed += frames;
            ended = fed >= heard.length;
            codec.queueInputBuffer(
                in, 0, frames * 2 * channels, time,
                ended ? MediaCodec.BUFFER_FLAG_END_OF_STREAM : 0);
          }
        }
        int at = codec.dequeueOutputBuffer(info, 10000);
        if (at == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED && muxer != null) {
          track = muxer.addTrack(codec.getOutputFormat());
          muxer.start();
        } else if (at >= 0) {
          ByteBuffer data = codec.getOutputBuffer(at);
          boolean config =
              (info.flags & MediaCodec.BUFFER_FLAG_CODEC_CONFIG) != 0;
          if (info.size > 0 && !config) {
            if (muxer != null)
              muxer.writeSampleData(track, data, info);
            else {
              byte[] frame = new byte[info.size];
              data.position(info.offset);
              data.get(frame);
              if (format == MediaRecorder.OutputFormat.AAC_ADTS)
                raw.write(adts(frame.length, media, channels));
              raw.write(frame);
            }
          }
          codec.releaseOutputBuffer(at, false);
          done = (info.flags & MediaCodec.BUFFER_FLAG_END_OF_STREAM) != 0;
        }
      }
    } finally {
      codec.stop();
      codec.release();
      if (muxer != null) {
        muxer.stop();
        muxer.release();
      }
      if (raw != null)
        raw.close();
    }
  }
  // An AAC frame's ADTS header.
  private static byte[] adts(int size, MediaFormat media, int channels) {
    int[] rates = {96000, 88200, 64000, 48000, 44100, 32000, 24000,
                   22050, 16000, 12000, 11025, 8000, 7350};
    int index = 4, rate = media.getInteger(MediaFormat.KEY_SAMPLE_RATE);
    for (int i = 0; i < rates.length; i++)
      if (rates[i] == rate)
        index = i;
    int length = size + 7, profile = 1; // AAC LC
    return new byte[] {(byte)0xFF,
                       (byte)0xF1,
                       (byte)((profile << 6) | (index << 2) | (channels >> 2)),
                       (byte)(((channels & 3) << 6) | (length >> 11)),
                       (byte)((length >> 3) & 0xFF),
                       (byte)(((length & 7) << 5) | 0x1F),
                       (byte)0xFC};
  }

  private static JSONObject input(JSONObject a) throws Exception {
    JSONObject c = a.getJSONObject("contract"), match =
                                                    c.optJSONObject("match");
    String name = match == null ? "" : match.optString("input");
    JSONArray inputs = c.getJSONArray("inputs");
    for (int i = 0; i < inputs.length(); i++) {
      JSONObject in = inputs.getJSONObject(i);
      if (name.isEmpty() || name.equals(in.getString("name")))
        return in;
    }
    throw new IllegalStateException("Missing input contract");
  }
  private static void standardize(double[] values, int offset, int n) {
    double mean = 0, variance = 0;
    for (int i = 0; i < n; i++)
      mean += values[offset + i];
    mean /= n;
    for (int i = 0; i < n; i++) {
      values[offset + i] -= mean;
      variance += values[offset + i] * values[offset + i];
    }
    double sd = Math.sqrt(variance / n + 1e-12);
    for (int i = 0; i < n; i++)
      values[offset + i] /= sd;
  }
  private static int nearest(JSONObject a, double[] values) throws Exception {
    JSONArray keys = a.optJSONArray("keys");
    int count = keys == null ? 0 : keys.length(), at = count;
    double best = a.getDouble("similarity");
    for (int i = count - 1; i >= 0; i--) {
      JSONArray key = keys.getJSONArray(i);
      if (key.length() != values.length)
        continue;
      double dot = 0;
      for (int j = 0; j < values.length; j++)
        dot += values[j] * key.getDouble(j);
      if (dot / values.length >= best) {
        best = dot / values.length;
        at = i;
      }
    }
    return at;
  }
  private static int sound(JSONObject a, double[] samples) throws Exception {
    JSONObject in = input(a);
    JSONArray shape = in.getJSONArray("shape");
    int n = 1;
    for (int i = 0; i < shape.length(); i++)
      n *= shape.getInt(i);
    int frame = 0;
    for (int f = 100; f <= 1024 && f <= n; f++)
      if (n % f == 0 &&
          (frame == 0 || Math.abs(f - 256) < Math.abs(frame - 256)))
        frame = f;
    int bins = frame / 2, frames = n / frame,
        rate = in.getJSONObject("audio").getInt("sampleRate");
    double[] cos = new double[frame * bins], sin = new double[frame * bins],
             bands = new double[32];
    for (int i = 0; i < frame; i++)
      for (int j = 0; j < bins; j++) {
        double angle = 2 * Math.PI * ((j + 1) * i % frame) / frame;
        cos[i * bins + j] = Math.cos(angle);
        sin[i * bins + j] = Math.sin(angle);
      }
    double lo = 2595 * Math.log10(1 + (double)rate / frame / 700),
           hi = 2595 * Math.log10(1 + (double)rate / 2 / 700);
    for (int f = 0; f < frames; f++)
      for (int j = 0; j < bins; j++) {
        double re = 0, im = 0;
        for (int i = 0; i < frame; i++) {
          int at = f * frame + i;
          double v = at < samples.length ? samples[at] : 0;
          re += v * cos[i * bins + j];
          im += v * sin[i * bins + j];
        }
        int band = (int)Math.floor(
            (2595 * Math.log10(1 + (j + 1) * (double)rate / frame / 700) - lo) /
            (hi - lo) * 32);
        bands[Math.max(0, Math.min(31, band))] += re * re + im * im;
      }
    for (int i = 0; i < 32; i++)
      bands[i] = Math.log(bands[i] / frames + 1e-10);
    standardize(bands, 0, 32);
    return nearest(a, bands);
  }

  // A sound's log-mel spectrogram, as match.LogMel computes it: Whisper's
  // frames (400 samples, every 160, the sound reflected at its ends), Hann's
  // window, each frame's transform, Slaney's mel filters, log10, no less
  // than 8 under the greatest, plus 4, over 4.
  private static final int MEL_FFT = 400, MEL_HOP = 160;
  static double[] logMel(double[] samples, int rate, int mels, int frames) {
    int n = frames * MEL_HOP, half = MEL_FFT / 2, bins = MEL_FFT / 2 + 1;
    double[] padded = new double[n];
    System.arraycopy(samples, 0, padded, 0, Math.min(samples.length, n));
    double[] x = new double[n + 2 * half];
    System.arraycopy(padded, 0, x, half, n);
    for (int i = 0; i < half; i++) {
      x[half - 1 - i] = padded[Math.min(i + 1, n - 1)];
      x[half + n + i] = padded[Math.max(n - 2 - i, 0)];
    }
    double[] window = new double[MEL_FFT], cos = new double[MEL_FFT * bins],
             sin = new double[MEL_FFT * bins];
    for (int i = 0; i < MEL_FFT; i++)
      window[i] = 0.5 - 0.5 * Math.cos(2 * Math.PI * i / MEL_FFT);
    for (int i = 0; i < MEL_FFT; i++)
      for (int k = 0; k < bins; k++) {
        double a = 2 * Math.PI * (i * k % MEL_FFT) / MEL_FFT;
        cos[i * bins + k] = Math.cos(a);
        sin[i * bins + k] = Math.sin(a);
      }
    double[][] filters = melFilters(rate, bins, mels);
    double[] out = new double[mels * frames], frame = new double[MEL_FFT],
             power = new double[bins];
    for (int f = 0; f < frames; f++) {
      for (int i = 0; i < MEL_FFT; i++)
        frame[i] = x[f * MEL_HOP + i] * window[i];
      for (int k = 0; k < bins; k++) {
        double re = 0, im = 0;
        for (int i = 0; i < MEL_FFT; i++) {
          re += frame[i] * cos[i * bins + k];
          im += frame[i] * sin[i * bins + k];
        }
        power[k] = re * re + im * im;
      }
      for (int m = 0; m < mels; m++) {
        double e = 0;
        for (int k = 0; k < bins; k++)
          e += filters[m][k] * power[k];
        out[m * frames + f] = Math.log10(Math.max(e, 1e-10));
      }
    }
    double top = Double.NEGATIVE_INFINITY;
    for (double v : out)
      top = Math.max(top, v);
    for (int i = 0; i < out.length; i++)
      out[i] = (Math.max(out[i], top - 8) + 4) / 4;
    return out;
  }
  private static double toMel(double f) {
    return f < 1000 ? 3 * f / 200 : 15 + 27 * Math.log(f / 1000) / Math.log(6.4);
  }
  private static double toHz(double m) {
    return m < 15 ? 200 * m / 3 : 1000 * Math.exp(Math.log(6.4) * (m - 15) / 27);
  }
  private static double[][] melFilters(int rate, int bins, int mels) {
    double lo = toMel(0), hi = toMel(8000);
    double[] edges = new double[mels + 2];
    for (int i = 0; i < edges.length; i++)
      edges[i] = toHz(lo + (hi - lo) * i / (mels + 1));
    double[][] out = new double[mels][bins];
    for (int m = 0; m < mels; m++) {
      double norm = 2 / (edges[m + 2] - edges[m]);
      for (int k = 0; k < bins; k++) {
        double f = (double)(rate / 2) * k / (bins - 1);
        double down = (f - edges[m]) / (edges[m + 1] - edges[m]);
        double up = (edges[m + 2] - f) / (edges[m + 2] - edges[m + 1]);
        out[m][k] = Math.max(0, Math.min(down, up)) * norm;
      }
    }
    return out;
  }
  // A sound's features, [mels][frames], reduced as match.Reducer reduces
  // them: pooled to 16 bands over 300 times, each time's bands centered and
  // scaled to their root mean square (a time under 0.01 left out), then the
  // whole scaled to its own.
  static double[] features(double[] item, int h, int w) {
    int kh = Math.max(h / 16, 1), kw = Math.max(w / 300, 1), ph = h / kh,
        pw = w / kw;
    double[] out = new double[ph * pw];
    double cell = kh * kw;
    for (int py = 0; py < ph; py++)
      for (int px = 0; px < pw; px++) {
        double sum = 0;
        for (int y = py * kh; y < (py + 1) * kh; y++)
          for (int x = px * kw; x < (px + 1) * kw; x++)
            sum += item[y * w + x];
        out[py * pw + px] = sum / cell;
      }
    for (int px = 0; px < pw; px++) {
      double mean = 0;
      for (int py = 0; py < ph; py++)
        mean += out[py * pw + px];
      mean /= ph;
      double square = 0;
      for (int py = 0; py < ph; py++) {
        out[py * pw + px] -= mean;
        square += out[py * pw + px] * out[py * pw + px];
      }
      square /= ph;
      double heard =
          Math.min(Math.max((square - 0.01) * (1 << 20), 0), 1);
      double scale = Math.sqrt(Math.max(square, 0.01));
      for (int py = 0; py < ph; py++)
        out[py * pw + px] = out[py * pw + px] * heard / scale;
    }
    double square = 0;
    for (double v : out)
      square += v * v;
    double scale = Math.sqrt(square / out.length + 1e-12);
    for (int i = 0; i < out.length; i++)
      out[i] /= scale;
    return out;
  }
  // Resamples as match.Resample does: each sample a Kaiser-windowed sinc's
  // (β 9) sum of the samples around it, 32 taps at the lower rate, the sound
  // mirrored past its ends.
  static double[] resample(double[] samples, int from, int to) {
    if (from == to || samples.length == 0)
      return samples;
    double factor = Math.min((double)to * 0.97 / from, 1);
    int taps = (int)Math.ceil(32 / factor), center = (taps - 1) / 2;
    double norm = 0;
    for (int i = 0; i < taps; i++)
      norm += kernel(i - center, factor, taps);
    int g = gcd(from, to), step = from / g, phases = to / g;
    Map<Integer, double[]> filters = new HashMap<>();
    double[] out =
        new double[(int)Math.round((double)samples.length * to / from)];
    for (int k = 0; k < out.length; k++) {
      long pos = (long)k * step;
      int index = (int)(pos / phases), ph = (int)(pos % phases);
      double[] f = filters.get(ph);
      if (f == null) {
        f = new double[taps];
        double frac = (double)ph / phases;
        for (int i = 0; i < taps; i++)
          f[i] = kernel(i - center - frac, factor, taps) / norm;
        filters.put(ph, f);
      }
      double v = 0;
      for (int i = 0; i < taps; i++)
        v += samples[mirrored(index + i - center, samples.length)] * f[i];
      out[k] = v;
    }
    return out;
  }
  private static double kernel(double d, double factor, int taps) {
    double x = Math.PI * d * factor, y = 1;
    if (x != 0)
      y = Math.sin(x) / x;
    double w = 2 * x / (factor * taps * Math.PI);
    return y * besselI0(9 * Math.sqrt(Math.max(1 - w * w, 0)));
  }
  private static int mirrored(int j, int n) {
    while (j < 0 || j >= n) {
      if (j < 0)
        j = -j;
      if (j >= n)
        j = 2 * n - 1 - j;
    }
    return j;
  }
  private static double besselI0(double x) {
    double sum = 1, term = 1;
    for (int k = 1; term > 1e-17 * sum; k++) {
      term *= (x / 2 / k) * (x / 2 / k);
      sum += term;
    }
    return sum;
  }
  private static int gcd(int a, int b) {
    while (b != 0) {
      int t = a % b;
      a = b;
      b = t;
    }
    return a;
  }

  private static int picture(JSONObject a, Bitmap bitmap) throws Exception {
    JSONObject in = input(a), spec = in.getJSONObject("image");
    JSONArray shape = in.getJSONArray("shape");
    boolean first = spec.getString("layout").endsWith("CHW");
    int off = shape.length() - 3;
    int c = shape.getInt(off + (first ? 0 : 2)),
        h = shape.getInt(off + (first ? 1 : 0)),
        w = shape.getInt(off + (first ? 2 : 1));
    int sw = bitmap.getWidth(), sh = bitmap.getHeight(), cw = sw, ch = sh,
        ox = 0, oy = 0;
    if (!spec.optString("resize").equals("stretch")) {
      if (sw * h > sh * w) {
        cw = sh * w / h;
        ox = (sw - cw) / 2;
      } else {
        ch = sw * h / w;
        oy = (sh - ch) / 2;
      }
    }
    double crop = spec.optDouble("crop", 0);
    if (crop > 0 && crop < 1) {
      int nw = (int)Math.round(cw * crop), nh = (int)Math.round(ch * crop);
      ox += (cw - nw) / 2;
      oy += (ch - nh) / 2;
      cw = nw;
      ch = nh;
    }
    int kh = Math.max(h / Math.min(16, h), 1),
        kw = Math.max(w / Math.min(16, w), 1), ph = h / kh, pw = w / kw;
    double[] values = new double[c * ph * pw];
    for (int y = 0; y < ph * kh; y++)
      for (int x = 0; x < pw * kw; x++) {
        double sx = (x + 0.5) * cw / w - 0.5, sy = (y + 0.5) * ch / h - 0.5,
               fx = sx - Math.floor(sx), fy = sy - Math.floor(sy);
        int x0 = (int)Math.floor(sx), y0 = (int)Math.floor(sy);
        double[] rgb = new double[3];
        for (int k = 0; k < 3; k++) {
          double[] px = new double[4];
          for (int dy = 0; dy < 2; dy++)
            for (int dx = 0; dx < 2; dx++) {
              int color =
                  bitmap.getPixel(ox + Math.min(Math.max(x0 + dx, 0), cw - 1),
                                  oy + Math.min(Math.max(y0 + dy, 0), ch - 1));
              px[dy * 2 + dx] = ((color >> (16 - k * 8)) & 255) *
                                (double)((color >>> 24) & 255) / 255;
            }
          double top = px[0] * (1 - fx) + px[1] * fx,
                 bottom = px[2] * (1 - fx) + px[3] * fx;
          rgb[k] = top * (1 - fy) + bottom * fy;
        }
        for (int k = 0; k < c; k++) {
          double v =
              k < 3 ? rgb[spec.optString("channels").equals("BGR") ? 2 - k : k]
                    : 255;
          if (c == 1 || spec.optString("channels").equals("gray"))
            v = 0.299 * rgb[0] + 0.587 * rgb[1] + 0.114 * rgb[2];
          values[k * ph * pw + y / kh * pw + x / kw] += v;
        }
      }
    for (int i = 0; i < values.length; i++)
      values[i] /= kh * kw;
    for (int k = 0; k < c; k++)
      standardize(values, k * ph * pw, ph * pw);
    return nearest(a, values);
  }
}
