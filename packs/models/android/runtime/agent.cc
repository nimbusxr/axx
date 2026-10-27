// ART TI adapter for debuggable applications. Public Java API boundaries
// use breakpoints; public MediaRecorder native controls use JNI registration.
#include <android/log.h>
#include <fstream>
#include <iterator>
#include <jni.h>
#include <jvmti.h>
#include <mutex>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <vector>

static jvmtiEnv *ti;
static jclass bridge;
static jmethodID invoke;
static jmethodID getResult;
// Bridge.PROCEED: the call runs the real method.
static jobject proceed;
static int enabled;
static bool speech, labeling, microphone;
static thread_local bool inside;
static std::mutex methodsMutex;
static std::unordered_map<jmethodID, int> methods;
static std::unordered_set<jmethodID> nativeMethods;
static std::string directory;
static std::vector<char> dexBytes;
// The boxes of a call's primitive arguments and of its answer.
static jclass objectClass, integerClass, booleanClass, longClass, floatClass,
    doubleClass, characterClass, numberClass;
static jmethodID integerOf, booleanOf, longOf, floatOf, doubleOf, characterOf,
    intValue, booleanValue, longValue, floatValue, doubleValue, charValue;
static void prepare(JNIEnv *, jclass);
static jobject JNICALL allocate(JNIEnv *env, jclass, jclass type) {
  prepare(env, type);
  return env->AllocObject(type);
}

static void status(const std::string &value) {
  std::ofstream(directory + "/status", std::ios::trunc) << value;
}
static void fail(JNIEnv *env, const char *message) {
  status(std::string("error: ") + message);
  __android_log_print(ANDROID_LOG_ERROR, "AXXModels", "AXX-E0704: %s", message);
  // Never resume a configured call into the real model after an adapter error.
  env->FatalError(message);
}
static void instrument(JNIEnv *env, jmethodID id, int op) {
  jlocation start, end;
  auto locationError = ti->GetMethodLocation(id, &start, &end);
  if (locationError != JVMTI_ERROR_NONE || start < 0) {
    char *name = nullptr, *signature = nullptr;
    jint modifiers = 0;
    ti->GetMethodName(id, &name, &signature, nullptr);
    ti->GetMethodModifiers(id, &modifiers);
    std::string message =
        "cannot instrument configured API " +
        std::string(name ? name : "unknown") + (signature ? signature : "") +
        ": GetMethodLocation=" + std::to_string(locationError) +
        ", native=" + ((modifiers & 0x100) ? "true" : "false") +
        "; use an adapter that supports this public method";
    if (name)
      ti->Deallocate(reinterpret_cast<unsigned char *>(name));
    if (signature)
      ti->Deallocate(reinterpret_cast<unsigned char *>(signature));
    fail(env, message.c_str());
    return;
  }
  {
    std::lock_guard<std::mutex> lock(methodsMutex);
    methods[id] = op;
  }
  auto error = ti->SetBreakpoint(id, start);
  if (error != JVMTI_ERROR_NONE && error != JVMTI_ERROR_DUPLICATE)
    fail(env, "cannot instrument a configured public model API");
}
// A result's getter whose answer is no field of the result: the agent answers
// it, from what the bridge keeps for the result.
static void JNICALL watch(JNIEnv *env, jclass, jobject getter) {
  instrument(env, env->FromReflectedMethod(getter), 20);
}

// A call's arguments, boxed. ART uses Dex register slots: arguments occupy
// the final registers, a long or a double two of them.
static jobjectArray arguments(JNIEnv *env, jthread thread, jmethodID method,
                              const std::string &sig, bool isStatic) {
  jint locals = 0, size = 0;
  if (ti->GetMaxLocals(method, &locals) != JVMTI_ERROR_NONE ||
      ti->GetArgumentsSize(method, &size) != JVMTI_ERROR_NONE)
    fail(env, "cannot locate ART method arguments");
  jint slot = locals - size + (isStatic ? 0 : 1);
  std::vector<jobject> values;
  for (size_t i = 1; sig[i] != ')'; ++i) {
    char kind = sig[i];
    if (kind == '[') {
      while (sig[i] == '[')
        ++i;
      if (sig[i] == 'L')
        i = sig.find(';', i);
      kind = '[';
    } else if (kind == 'L')
      i = sig.find(';', i);
    jvmtiError error = JVMTI_ERROR_NONE;
    jobject value = nullptr;
    if (kind == 'L' || kind == '[') {
      error = ti->GetLocalObject(thread, 0, slot, &value);
    } else if (kind == 'J') {
      jlong v = 0;
      error = ti->GetLocalLong(thread, 0, slot, &v);
      value = env->CallStaticObjectMethod(longClass, longOf, v);
    } else if (kind == 'D') {
      jdouble v = 0;
      error = ti->GetLocalDouble(thread, 0, slot, &v);
      value = env->CallStaticObjectMethod(doubleClass, doubleOf, v);
    } else if (kind == 'F') {
      jfloat v = 0;
      error = ti->GetLocalFloat(thread, 0, slot, &v);
      value = env->CallStaticObjectMethod(floatClass, floatOf, v);
    } else {
      jint v = 0;
      error = ti->GetLocalInt(thread, 0, slot, &v);
      value = kind == 'Z' ? env->CallStaticObjectMethod(booleanClass, booleanOf,
                                                        (jboolean)(v != 0))
              : kind == 'C'
                  ? env->CallStaticObjectMethod(characterClass, characterOf,
                                                (jchar)v)
                  : env->CallStaticObjectMethod(integerClass, integerOf, v);
    }
    if (error != JVMTI_ERROR_NONE)
      fail(env, "cannot read the model API arguments");
    values.push_back(value);
    slot += kind == 'J' || kind == 'D' ? 2 : 1;
  }
  jobjectArray out =
      env->NewObjectArray((jsize)values.size(), objectClass, nullptr);
  for (size_t i = 0; i < values.size(); ++i) {
    env->SetObjectArrayElement(out, (jsize)i, values[i]);
    if (values[i])
      env->DeleteLocalRef(values[i]);
  }
  return out;
}
// Returns value from the method, a box of its primitive return type.
static jvmtiError answer(JNIEnv *env, jthread thread, char kind, jobject value) {
  switch (kind) {
  case 'V':
    return ti->ForceEarlyReturnVoid(thread);
  case 'Z':
    return ti->ForceEarlyReturnInt(
        thread, env->IsInstanceOf(value, booleanClass)
                    ? env->CallBooleanMethod(value, booleanValue)
                    : env->CallIntMethod(value, intValue) != 0);
  case 'C':
    return ti->ForceEarlyReturnInt(thread,
                                   env->CallCharMethod(value, charValue));
  case 'B':
  case 'S':
  case 'I':
    return ti->ForceEarlyReturnInt(thread, env->CallIntMethod(value, intValue));
  case 'J':
    return ti->ForceEarlyReturnLong(thread,
                                    env->CallLongMethod(value, longValue));
  case 'F':
    return ti->ForceEarlyReturnFloat(thread,
                                     env->CallFloatMethod(value, floatValue));
  case 'D':
    return ti->ForceEarlyReturnDouble(
        thread, env->CallDoubleMethod(value, doubleValue));
  default:
    return ti->ForceEarlyReturnObject(thread, value);
  }
}

static void bridgeFailure(JNIEnv *env) {
  if (!env->ExceptionCheck())
    return;
  jthrowable exception = env->ExceptionOccurred();
  env->ExceptionDescribe();
  env->ExceptionClear();
  jclass type = env->GetObjectClass(exception);
  jmethodID describe =
      env->GetMethodID(type, "toString", "()Ljava/lang/String;");
  jstring description =
      static_cast<jstring>(env->CallObjectMethod(exception, describe));
  std::string message =
      "platform model adapter failed; check the configured API and stub";
  if (!env->ExceptionCheck() && description) {
    const char *value = env->GetStringUTFChars(description, nullptr);
    if (value) {
      message = value;
      env->ReleaseStringUTFChars(description, value);
    }
  }
  env->ExceptionClear();
  fail(env, message.c_str());
}

// RegisterNatives is the documented JNI binding interface. MediaRecorder is
// already bound in the zygote, before an app agent can receive
// NativeMethodBind. These controls therefore implement capture completely
// through the bridge; they never need an old native address or any private
// ART/MediaRecorder API. The real constructor and Java release/reset still
// manage the real object's resources. The bridge writes audio with public
// MediaCodec/MediaMuxer APIs.
// https://docs.oracle.com/en/java/javase/21/docs/specs/jni/functions.html#registernatives
static jint recorderCall(JNIEnv *env, jobject self, jint op,
                         jobject arg = nullptr) {
  bool previous = inside;
  inside = true;
  jobjectArray args = env->NewObjectArray(arg ? 1 : 0, objectClass, nullptr);
  if (arg)
    env->SetObjectArrayElement(args, 0, arg);
  jobject result = env->CallStaticObjectMethod(bridge, invoke, op, self, args);
  if (env->ExceptionCheck()) {
    // Preserve ordinary public API errors (including permission denial) for
    // the caller. Unexpected adapter errors still fail with AXX-E0704.
    jthrowable exception = env->ExceptionOccurred();
    env->ExceptionClear();
    bool expected = false;
    for (const char *name :
         {"java/lang/SecurityException", "java/lang/IllegalStateException",
          "java/lang/IllegalArgumentException"}) {
      jclass cls = env->FindClass(name);
      expected |= env->IsInstanceOf(exception, cls);
      env->DeleteLocalRef(cls);
    }
    env->Throw(exception);
    env->DeleteLocalRef(exception);
    if (expected) {
      env->DeleteLocalRef(args);
      inside = previous;
      return 0;
    }
  }
  bridgeFailure(env);
  if (env->IsSameObject(result, proceed)) {
    // A native reset or release (Android 15's release): the bridge has reset
    // or released the recording. The recorder's own native object was never
    // prepared or started (the fixture's prepare and start stand in for
    // them): there is nothing to reset, and its finalizer frees it.
    if (op == 50 || op == 51) {
      env->DeleteLocalRef(result);
      env->DeleteLocalRef(args);
      inside = previous;
      return 0;
    }
    fail(env, "a native recorder control needs a complete fixture answer; use "
              "a supported audio capture configuration");
  }
  jint value = result ? env->CallIntMethod(result, intValue) : 0;
  bridgeFailure(env);
  if (result)
    env->DeleteLocalRef(result);
  env->DeleteLocalRef(args);
  inside = previous;
  return value;
}
template <int Op>
static void JNICALL recorderInt(JNIEnv *env, jobject self, jint value) {
  jobject arg = env->CallStaticObjectMethod(integerClass, integerOf, value);
  recorderCall(env, self, Op, arg);
  env->DeleteLocalRef(arg);
}
template <int Op> static void JNICALL recorderVoid(JNIEnv *env, jobject self) {
  recorderCall(env, self, Op);
}
static void JNICALL recorderLimit(JNIEnv *env, jobject self, jlong value) {
  jobject arg = env->CallStaticObjectMethod(longClass, longOf, value);
  recorderCall(env, self, 58, arg);
  env->DeleteLocalRef(arg);
}
static jint JNICALL recorderAmplitude(JNIEnv *env, jobject self) {
  return recorderCall(env, self, 53);
}
static void registerRecorder(JNIEnv *env, jclass cls, jmethodID method,
                             const std::string &name, const std::string &sig,
                             int op) {
  void *function = nullptr;
  if (sig == "(I)V") {
    switch (op) {
    case 40:
      function = reinterpret_cast<void *>(recorderInt<40>);
      break;
    case 41:
      function = reinterpret_cast<void *>(recorderInt<41>);
      break;
    case 42:
      function = reinterpret_cast<void *>(recorderInt<42>);
      break;
    case 56:
      function = reinterpret_cast<void *>(recorderInt<56>);
      break;
    case 57:
      function = reinterpret_cast<void *>(recorderInt<57>);
      break;
    }
  } else if (sig == "()V") {
    switch (op) {
    case 47:
      function = reinterpret_cast<void *>(recorderVoid<47>);
      break;
    case 48:
      function = reinterpret_cast<void *>(recorderVoid<48>);
      break;
    case 49:
      function = reinterpret_cast<void *>(recorderVoid<49>);
      break;
    case 50:
      function = reinterpret_cast<void *>(recorderVoid<50>);
      break;
    case 51:
      function = reinterpret_cast<void *>(recorderVoid<51>);
      break;
    case 52:
      function = reinterpret_cast<void *>(recorderVoid<52>);
      break;
    }
  } else if (op == 53 && sig == "()I") {
    function = reinterpret_cast<void *>(recorderAmplitude);
  } else if (op == 58 && sig == "(J)V") {
    function = reinterpret_cast<void *>(recorderLimit);
  }
  if (!function)
    fail(env,
         ("unsupported native recorder control " + name + sig +
          "; record audio through MediaRecorder's public audio methods")
             .c_str());
  std::lock_guard<std::mutex> lock(methodsMutex);
  if (nativeMethods.count(method))
    return;
  JNINativeMethod native{const_cast<char *>(name.c_str()),
                         const_cast<char *>(sig.c_str()), function};
  if (env->RegisterNatives(cls, &native, 1) != JNI_OK)
    fail(env, ("cannot register public recorder control " + name +
               "; use a debuggable emulator app with JNI registration support")
                  .c_str());
  nativeMethods.insert(method);
}

static void JNICALL breakpoint(jvmtiEnv *, JNIEnv *env, jthread thread,
                               jmethodID method, jlocation) {
  if (inside)
    return;
  inside = true;
  int op;
  {
    std::lock_guard<std::mutex> lock(methodsMutex);
    op = methods.at(method);
  }
  if (op == 1) {
    if (ti->ForceEarlyReturnInt(thread, JNI_TRUE) != JVMTI_ERROR_NONE)
      fail(env, "cannot return speech availability");
    inside = false;
    return;
  }
  char *raw = nullptr, *signature = nullptr;
  if (ti->GetMethodName(method, &raw, &signature, nullptr) != JVMTI_ERROR_NONE)
    fail(env, "cannot read a model API method");
  std::string name(raw), sig(signature);
  ti->Deallocate(reinterpret_cast<unsigned char *>(raw));
  ti->Deallocate(reinterpret_cast<unsigned char *>(signature));
  char kind = sig.at(sig.find(')') + 1);
  if (op == 20) {
    jobject self = nullptr;
    if (ti->GetLocalInstance(thread, 0, &self) != JVMTI_ERROR_NONE)
      fail(env, "cannot read a fixture result receiver");
    jstring getter = env->NewStringUTF(name.c_str());
    jobjectArray boxed = static_cast<jobjectArray>(
        env->CallStaticObjectMethod(bridge, getResult, self, getter));
    if (env->ExceptionCheck()) {
      env->ExceptionDescribe();
      env->ExceptionClear();
      fail(env, "cannot read a fixture result property");
    }
    if (boxed) {
      jobject value = env->GetObjectArrayElement(boxed, 0);
      if (answer(env, thread, kind, value) != JVMTI_ERROR_NONE)
        fail(env, "cannot return a fixture result property");
      if (value)
        env->DeleteLocalRef(value);
      env->DeleteLocalRef(boxed);
    }
    env->DeleteLocalRef(getter);
    env->DeleteLocalRef(self);
    inside = false;
    return;
  }
  jint modifiers = 0;
  ti->GetMethodModifiers(method, &modifiers);
  bool isStatic = modifiers & 8;
  jobject self = nullptr;
  if (isStatic) {
    jclass cls = nullptr;
    ti->GetMethodDeclaringClass(method, &cls);
    self = cls;
  } else if (ti->GetLocalInstance(thread, 0, &self) != JVMTI_ERROR_NONE)
    fail(env, "cannot read the model API receiver");
  jobjectArray args = arguments(env, thread, method, sig, isStatic);
  jobject result =
      env->CallStaticObjectMethod(bridge, invoke, op, self, args);
  bridgeFailure(env);
  if (!env->IsSameObject(result, proceed) &&
      answer(env, thread, kind, result) != JVMTI_ERROR_NONE)
    fail(env, "ART could not return the platform fixture answer");
  if (self)
    env->DeleteLocalRef(self);
  env->DeleteLocalRef(args);
  if (result)
    env->DeleteLocalRef(result);
  inside = false;
}
static void prepare(JNIEnv *env, jclass cls) {
  char *name = nullptr;
  if (ti->GetClassSignature(cls, &name, nullptr) != JVMTI_ERROR_NONE)
    return;
  std::string type(name);
  ti->Deallocate(reinterpret_cast<unsigned char *>(name));
  bool isSpeech = speech && type.rfind("Landroid/speech/", 0) == 0;
  bool isVision = (enabled & 62) && type.rfind("Lcom/google/mlkit/", 0) == 0;
  bool isCapture = microphone && (type == "Landroid/media/AudioRecord;" ||
                                  type == "Landroid/media/MediaRecorder;");
  if (!isSpeech && !isVision && !isCapture)
    return;
  jint count = 0;
  jmethodID *ids = nullptr;
  if (ti->GetClassMethods(cls, &count, &ids) != JVMTI_ERROR_NONE)
    return;
  for (int i = 0; i < count; ++i) {
    char *raw = nullptr, *signature = nullptr;
    if (ti->GetMethodName(ids[i], &raw, &signature, nullptr) !=
        JVMTI_ERROR_NONE)
      continue;
    std::string method(raw), sig(signature);
    int op = 0;
    ti->Deallocate(reinterpret_cast<unsigned char *>(raw));
    ti->Deallocate(reinterpret_cast<unsigned char *>(signature));
    if (isSpeech) {
      if (type == "Landroid/speech/SpeechRecognizer;" &&
          method == "createOnDeviceSpeechRecognizer" &&
          sig == "(Landroid/content/Context;)Landroid/speech/SpeechRecognizer;")
        op = 7;
      if ((method == "isRecognitionAvailable" ||
           method == "isOnDeviceRecognitionAvailable") &&
          sig == "(Landroid/content/Context;)Z")
        op = 1;
      if (method == "setRecognitionListener" &&
          sig == "(Landroid/speech/RecognitionListener;)V")
        op = 2;
      if (method == "startListening" && sig == "(Landroid/content/Intent;)V")
        op = 3;
      if (method == "stopListening" && sig == "()V")
        op = 4;
      if (method == "cancel" && sig == "()V")
        op = 5;
      if (method == "destroy" && sig == "()V")
        op = 6;
    }
    if (isVision) {
      if ((enabled & 30) &&
          type == "Lcom/google/mlkit/vision/common/InputImage;" &&
          method == "fromBitmap" &&
          sig == "(Landroid/graphics/Bitmap;I)Lcom/google/mlkit/vision/common/"
                 "InputImage;")
        op = 10;
      if (labeling && type == "Lcom/google/mlkit/vision/label/ImageLabeling;" &&
          method == "getClient" &&
          sig ==
              "(Lcom/google/mlkit/vision/label/ImageLabelerOptionsBase;)Lcom/"
              "google/mlkit/vision/label/ImageLabeler;")
        op = 12;
      if ((enabled & 4) &&
          type == "Lcom/google/mlkit/vision/text/TextRecognition;" &&
          method == "getClient" &&
          sig.find(")Lcom/google/mlkit/vision/text/TextRecognizer;") !=
              std::string::npos)
        op = 13;
      if ((enabled & 8) &&
          type == "Lcom/google/mlkit/vision/objects/ObjectDetection;" &&
          method == "getClient" &&
          sig.find(")Lcom/google/mlkit/vision/objects/ObjectDetector;") !=
              std::string::npos)
        op = 14;
      if ((enabled & 16) &&
          type == "Lcom/google/mlkit/vision/segmentation/Segmentation;" &&
          method == "getClient" &&
          sig.find(")Lcom/google/mlkit/vision/segmentation/Segmenter;") !=
              std::string::npos)
        op = 15;
      if ((enabled & 32) &&
          type == "Lcom/google/mlkit/genai/prompt/Generation;" &&
          method == "getClient" &&
          sig.find(")Lcom/google/mlkit/genai/prompt/GenerativeModel;") !=
              std::string::npos)
        op = 16;
    }
    // The app's own audio capture, which plays the microphone step's
    // recordings while the scenario has them.
    if (isCapture && type == "Landroid/media/AudioRecord;") {
      if (method == "startRecording")
        op = 30;
      if ((method == "stop" || method == "release") && sig == "()V")
        op = 31;
      if (method == "read" && sig.rfind(")I") == sig.size() - 2)
        op = 32;
    }
    if (isCapture && type == "Landroid/media/MediaRecorder;") {
      static const char *recorder[] = {
          "setAudioSource",  "setOutputFormat",         "setAudioEncoder",
          "setAudioSamplingRate", "setAudioChannels", "setAudioEncodingBitRate",
          "setOutputFile",   "start",                   "pause",
          "resume",          "reset",                   "release",
          "stop"};
      for (int k = 0; k < 13; ++k)
        if (method == recorder[k] && sig.rfind(")V") == sig.size() - 2)
          op = 40 + k;
      if (method == "getMaxAmplitude" && sig == "()I")
        op = 53;
      if (method == "prepare" && sig == "()V")
        op = 54;
      if (method == "<init>" && sig == "(Landroid/content/Context;)V")
        op = 55;
      if (method == "setVideoSource" && sig == "(I)V")
        op = 56;
      if (method == "setMaxDuration" && sig == "(I)V")
        op = 57;
      if (method == "setMaxFileSize" && sig == "(J)V")
        op = 58;
      if (method == "setNextOutputFile" && sig.rfind(")V") == sig.size() - 2)
        op = 59;
    }
    if (!op)
      continue;
    jint modifiers = 0;
    if (ti->GetMethodModifiers(ids[i], &modifiers) != JVMTI_ERROR_NONE ||
        !(modifiers & 1))
      continue;
    if (isCapture && type == "Landroid/media/MediaRecorder;" &&
        (modifiers & 0x100)) {
      registerRecorder(env, cls, ids[i], method, sig, op);
      continue;
    }
    instrument(env, ids[i], op);
  }
  ti->Deallocate(reinterpret_cast<unsigned char *>(ids));
}
static void JNICALL prepared(jvmtiEnv *, JNIEnv *env, jthread, jclass cls) {
  bool previous = inside;
  inside = true;
  prepare(env, cls);
  inside = previous;
}
static jclass global(JNIEnv *env, const char *name) {
  jclass local = env->FindClass(name);
  jclass out = static_cast<jclass>(env->NewGlobalRef(local));
  env->DeleteLocalRef(local);
  return out;
}

extern "C" JNIEXPORT jint JNICALL Agent_OnAttach(JavaVM *vm, char *options,
                                                 void *) {
  if (!options)
    return JNI_ERR;
  directory = options;
  JNIEnv *env = nullptr;
  if (vm->GetEnv(reinterpret_cast<void **>(&env), JNI_VERSION_1_6) != JNI_OK ||
      vm->GetEnv(reinterpret_cast<void **>(&ti), JVMTI_VERSION_1_2) != JNI_OK)
    return JNI_ERR;
  inside = true;
  jvmtiCapabilities caps{};
  caps.can_generate_breakpoint_events = 1;
  caps.can_force_early_return = 1;
  caps.can_access_local_variables = 1;
  if (ti->AddCapabilities(&caps) != JVMTI_ERROR_NONE) {
    status("error: required ART capabilities unavailable");
    return JNI_ERR;
  }
  std::ifstream file(directory + "/classes.dex", std::ios::binary);
  dexBytes.assign(std::istreambuf_iterator<char>(file),
                  std::istreambuf_iterator<char>());
  if (dexBytes.empty()) {
    status("error: missing test adapter dex");
    return JNI_ERR;
  }
  objectClass = global(env, "java/lang/Object");
  integerClass = global(env, "java/lang/Integer");
  booleanClass = global(env, "java/lang/Boolean");
  longClass = global(env, "java/lang/Long");
  floatClass = global(env, "java/lang/Float");
  doubleClass = global(env, "java/lang/Double");
  characterClass = global(env, "java/lang/Character");
  numberClass = global(env, "java/lang/Number");
  integerOf =
      env->GetStaticMethodID(integerClass, "valueOf", "(I)Ljava/lang/Integer;");
  booleanOf =
      env->GetStaticMethodID(booleanClass, "valueOf", "(Z)Ljava/lang/Boolean;");
  longOf = env->GetStaticMethodID(longClass, "valueOf", "(J)Ljava/lang/Long;");
  floatOf =
      env->GetStaticMethodID(floatClass, "valueOf", "(F)Ljava/lang/Float;");
  doubleOf =
      env->GetStaticMethodID(doubleClass, "valueOf", "(D)Ljava/lang/Double;");
  characterOf = env->GetStaticMethodID(characterClass, "valueOf",
                                       "(C)Ljava/lang/Character;");
  intValue = env->GetMethodID(numberClass, "intValue", "()I");
  longValue = env->GetMethodID(numberClass, "longValue", "()J");
  floatValue = env->GetMethodID(numberClass, "floatValue", "()F");
  doubleValue = env->GetMethodID(numberClass, "doubleValue", "()D");
  booleanValue = env->GetMethodID(booleanClass, "booleanValue", "()Z");
  charValue = env->GetMethodID(characterClass, "charValue", "()C");
  jclass loaderClass = env->FindClass("java/lang/ClassLoader");
  jobject parent = env->CallStaticObjectMethod(
      loaderClass, env->GetStaticMethodID(loaderClass, "getSystemClassLoader",
                                          "()Ljava/lang/ClassLoader;"));
  jclass memoryClass = env->FindClass("dalvik/system/InMemoryDexClassLoader");
  jobject buffer = env->NewDirectByteBuffer(dexBytes.data(), dexBytes.size());
  jobject loader = env->NewObject(
      memoryClass,
      env->GetMethodID(memoryClass, "<init>",
                       "(Ljava/nio/ByteBuffer;Ljava/lang/ClassLoader;)V"),
      buffer, parent);
  jstring bridgeName = env->NewStringUTF("us.nimbusxr.axx.models.Bridge");
  jobject loaded = env->CallObjectMethod(
      loader,
      env->GetMethodID(loaderClass, "loadClass",
                       "(Ljava/lang/String;)Ljava/lang/Class;"),
      bridgeName);
  if (env->ExceptionCheck() || !loaded) {
    env->ExceptionDescribe();
    env->ExceptionClear();
    status("error: cannot load test adapter");
    return JNI_ERR;
  }
  bridge = static_cast<jclass>(env->NewGlobalRef(loaded));
  invoke = env->GetStaticMethodID(
      bridge, "call",
      "(ILjava/lang/Object;[Ljava/lang/Object;)Ljava/lang/Object;");
  getResult = env->GetStaticMethodID(
      bridge, "result",
      "(Ljava/lang/Object;Ljava/lang/String;)[Ljava/lang/Object;");
  JNINativeMethod natives[] = {
      {const_cast<char *>("allocate"),
       const_cast<char *>("(Ljava/lang/Class;)Ljava/lang/Object;"),
       reinterpret_cast<void *>(allocate)},
      {const_cast<char *>("watch"),
       const_cast<char *>("(Ljava/lang/reflect/Method;)V"),
       reinterpret_cast<void *>(watch)}};
  if (env->RegisterNatives(bridge, natives, 2) != JNI_OK) {
    status("error: cannot register the adapter's natives");
    return JNI_ERR;
  }
  proceed = env->NewGlobalRef(env->GetStaticObjectField(
      bridge,
      env->GetStaticFieldID(bridge, "PROCEED", "Ljava/lang/Object;")));
  jstring root = env->NewStringUTF(directory.c_str());
  enabled = env->CallStaticIntMethod(
      bridge,
      env->GetStaticMethodID(bridge, "initialize", "(Ljava/lang/String;)I"),
      root);
  if (env->ExceptionCheck()) {
    env->ExceptionDescribe();
    env->ExceptionClear();
    status("error: cannot read model artifacts");
    return JNI_ERR;
  }
  speech = enabled & 1;
  labeling = enabled & 2;
  microphone = enabled & 64;
  if (microphone) {
    // Initialize before registering replacements: later class initialization
    // must not overwrite our bindings with the platform's registrations.
    jclass recorder = env->FindClass("android/media/MediaRecorder");
    if (!recorder || !env->GetMethodID(recorder, "getMaxAmplitude", "()I")) {
      status("error: cannot initialize MediaRecorder; use a supported Android "
             "emulator");
      return JNI_ERR;
    }
    env->DeleteLocalRef(recorder);
  }
  jvmtiEventCallbacks callbacks{};
  callbacks.Breakpoint = breakpoint;
  callbacks.ClassPrepare = prepared;
  ti->SetEventCallbacks(&callbacks, sizeof(callbacks));
  ti->SetEventNotificationMode(JVMTI_ENABLE, JVMTI_EVENT_CLASS_PREPARE,
                               nullptr);
  ti->SetEventNotificationMode(JVMTI_ENABLE, JVMTI_EVENT_BREAKPOINT, nullptr);
  jint count;
  jclass *classes;
  if (ti->GetLoadedClasses(&count, &classes) != JVMTI_ERROR_NONE) {
    status("error: cannot inspect loaded API classes");
    return JNI_ERR;
  }
  for (int i = 0; i < count; ++i) {
    prepare(env, classes[i]);
    env->DeleteLocalRef(classes[i]);
  }
  ti->Deallocate(reinterpret_cast<unsigned char *>(classes));
  inside = false;
  status("ready");
  return JNI_OK;
}
