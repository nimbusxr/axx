// SPDX-License-Identifier: Apache-2.0

// axx attaches this ART TI agent to a debuggable app whose camera sees a scene, and whose app
// has ARCore (../ar.go): ARCore needs Google's services for AR, which an emulator has not got,
// and sees the scene only as pictures. The agent puts a breakpoint at the start of the public
// methods of ARCore's session and of what it hands the app (frames, the camera, hit results,
// planes, anchors), and hands each call to the bridge (Bridge.java), which answers it from the
// scene, as the phone in it sees it: the call returns the bridge's answer, or runs as it would
// when the bridge says PROCEED. The agent's options are the folder axx put the bridge in, then,
// after a ',', another agent to attach after it, as its own library and options (a comma: the
// device's shell, which am's command line goes through, takes a ';' for its own).
#include <android/log.h>
#include <dlfcn.h>
#include <fstream>
#include <iterator>
#include <jni.h>
#include <jvmti.h>
#include <string>
#include <vector>

static jvmtiEnv *ti;
static jclass bridge;
static jmethodID call;
static jobject proceed;
static thread_local bool inside;
static std::string directory;
static std::vector<char> dexBytes;
static jclass objectClass, integerClass, booleanClass, longClass, floatClass,
    doubleClass, characterClass, numberClass;
static jmethodID integerOf, booleanOf, longOf, floatOf, doubleOf, characterOf,
    intValue, booleanValue, longValue, floatValue, doubleValue, charValue;

static void status(const std::string &value) {
  std::ofstream(directory + "/status", std::ios::trunc) << value;
}

static void fail(JNIEnv *env, const std::string &message) {
  status("error: " + message);
  __android_log_print(ANDROID_LOG_ERROR, "AXXAR", "%s", message.c_str());
  env->FatalError(message.c_str());
}

// The classes whose calls the bridge answers: ARCore's session and what it hands the app. Of
// ArCoreApk, only whether ARCore is there.
static const char *const answered[] = {
    "Lcom/google/ar/core/Session;",       "Lcom/google/ar/core/Config;",
    "Lcom/google/ar/core/Frame;",         "Lcom/google/ar/core/Camera;",
    "Lcom/google/ar/core/HitResult;",     "Lcom/google/ar/core/TrackableBase;",
    "Lcom/google/ar/core/Plane;",         "Lcom/google/ar/core/Point;",
    "Lcom/google/ar/core/Anchor;",        "Lcom/google/ar/core/LightEstimate;",
    "Lcom/google/ar/core/CameraIntrinsics;", "Lcom/google/ar/core/ArCoreApk;",
};

static bool isAnswered(const std::string &type) {
  for (const char *a : answered)
    if (type == a)
      return true;
  return false;
}

static std::string signatureOf(jclass cls) {
  char *raw = nullptr;
  if (ti->GetClassSignature(cls, &raw, nullptr) != JVMTI_ERROR_NONE)
    return "";
  std::string type(raw);
  ti->Deallocate(reinterpret_cast<unsigned char *>(raw));
  return type;
}

// The class the bridge answers that cls is, or extends (ArCoreApk.getInstance() is a subclass of
// ArCoreApk's, of an obfuscated name, which overrides its methods); "" for none.
static std::string answeredType(JNIEnv *env, jclass cls) {
  jclass c = static_cast<jclass>(env->NewLocalRef(cls));
  while (c) {
    std::string type = signatureOf(c);
    if (isAnswered(type)) {
      env->DeleteLocalRef(c);
      return type;
    }
    jclass super = env->GetSuperclass(c);
    env->DeleteLocalRef(c);
    c = super;
  }
  return "";
}

// A call's arguments, boxed. ART keeps them in a method's last Dex registers, a long or a
// double in two.
static jobjectArray arguments(JNIEnv *env, jthread thread, jmethodID method,
                              const std::string &sig, bool isStatic) {
  jint locals = 0, size = 0;
  if (ti->GetMaxLocals(method, &locals) != JVMTI_ERROR_NONE ||
      ti->GetArgumentsSize(method, &size) != JVMTI_ERROR_NONE)
    fail(env, "cannot find an ARCore call's arguments");
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
      value = kind == 'Z'   ? env->CallStaticObjectMethod(booleanClass, booleanOf, (jboolean)(v != 0))
              : kind == 'C' ? env->CallStaticObjectMethod(characterClass, characterOf, (jchar)v)
                            : env->CallStaticObjectMethod(integerClass, integerOf, v);
    }
    if (error != JVMTI_ERROR_NONE)
      fail(env, "cannot read an ARCore call's arguments");
    values.push_back(value);
    slot += kind == 'J' || kind == 'D' ? 2 : 1;
  }
  jobjectArray out = env->NewObjectArray((jsize)values.size(), objectClass, nullptr);
  for (size_t i = 0; i < values.size(); ++i) {
    env->SetObjectArrayElement(out, (jsize)i, values[i]);
    if (values[i])
      env->DeleteLocalRef(values[i]);
  }
  return out;
}

// Returns value from the method: a box of its primitive return type, or an object.
static jvmtiError answer(JNIEnv *env, jthread thread, char kind, jobject value) {
  switch (kind) {
  case 'V':
    return ti->ForceEarlyReturnVoid(thread);
  case 'Z':
    return ti->ForceEarlyReturnInt(thread, value && env->CallBooleanMethod(value, booleanValue));
  case 'C':
    return ti->ForceEarlyReturnInt(thread, value ? env->CallCharMethod(value, charValue) : 0);
  case 'B':
  case 'S':
  case 'I':
    return ti->ForceEarlyReturnInt(thread, value ? env->CallIntMethod(value, intValue) : 0);
  case 'J':
    return ti->ForceEarlyReturnLong(thread, value ? env->CallLongMethod(value, longValue) : 0);
  case 'F':
    return ti->ForceEarlyReturnFloat(thread, value ? env->CallFloatMethod(value, floatValue) : 0);
  case 'D':
    return ti->ForceEarlyReturnDouble(thread, value ? env->CallDoubleMethod(value, doubleValue) : 0);
  default:
    return ti->ForceEarlyReturnObject(thread, value);
  }
}

static std::string describe(JNIEnv *env, jthrowable exception) {
  jclass type = env->GetObjectClass(exception);
  jstring text = static_cast<jstring>(
      env->CallObjectMethod(exception, env->GetMethodID(type, "toString", "()Ljava/lang/String;")));
  std::string out = "the AR bridge failed";
  if (!env->ExceptionCheck() && text) {
    const char *chars = env->GetStringUTFChars(text, nullptr);
    if (chars) {
      out = chars;
      env->ReleaseStringUTFChars(text, chars);
    }
  }
  env->ExceptionClear();
  return out;
}

static void JNICALL breakpoint(jvmtiEnv *, JNIEnv *env, jthread thread, jmethodID method, jlocation) {
  // The bridge's own calls into ARCore's classes run as they are.
  if (inside)
    return;
  inside = true;
  char *rawName = nullptr, *rawSig = nullptr;
  jclass owner = nullptr;
  if (ti->GetMethodName(method, &rawName, &rawSig, nullptr) != JVMTI_ERROR_NONE ||
      ti->GetMethodDeclaringClass(method, &owner) != JVMTI_ERROR_NONE)
    fail(env, "cannot read an ARCore call");
  std::string name(rawName), sig(rawSig), type = answeredType(env, owner);
  ti->Deallocate(reinterpret_cast<unsigned char *>(rawName));
  ti->Deallocate(reinterpret_cast<unsigned char *>(rawSig));
  jint modifiers = 0;
  ti->GetMethodModifiers(method, &modifiers);
  bool isStatic = modifiers & 8;
  jobject self = nullptr;
  if (isStatic)
    self = env->NewLocalRef(owner);
  else if (ti->GetLocalInstance(thread, 0, &self) != JVMTI_ERROR_NONE)
    fail(env, "cannot read an ARCore call's object");
  jobjectArray args = arguments(env, thread, method, sig, isStatic);
  // "Lcom/google/ar/core/Frame;" to "Frame".
  std::string simple = type.substr(type.rfind('/') + 1);
  simple.pop_back();
  jstring jowner = env->NewStringUTF(simple.c_str());
  jstring jname = env->NewStringUTF(name.c_str());
  jstring jsig = env->NewStringUTF(sig.c_str());
  jobject result = env->CallStaticObjectMethod(bridge, call, jowner, jname, jsig, self, args);
  if (env->ExceptionCheck()) {
    jthrowable exception = env->ExceptionOccurred();
    env->ExceptionClear();
    fail(env, "the AR bridge failed on " + simple + "." + name + sig + ": " + describe(env, exception));
  }
  if (!env->IsSameObject(result, proceed)) {
    char kind = sig.at(sig.find(')') + 1);
    if (answer(env, thread, kind, result) != JVMTI_ERROR_NONE)
      fail(env, "ART could not return the AR bridge's answer to " + simple + "." + name);
  }
  for (jobject ref : {static_cast<jobject>(jowner), static_cast<jobject>(jname), static_cast<jobject>(jsig), self,
                      static_cast<jobject>(args), result, static_cast<jobject>(owner)})
    if (ref)
      env->DeleteLocalRef(ref);
  inside = false;
}

// Puts a breakpoint at the start of each method of a class whose calls the bridge answers, or of
// one that extends it.
static void prepare(JNIEnv *env, jclass cls) {
  std::string type = answeredType(env, cls);
  if (type.empty())
    return;
  jint count = 0;
  jmethodID *ids = nullptr;
  if (ti->GetClassMethods(cls, &count, &ids) != JVMTI_ERROR_NONE)
    fail(env, "cannot list the methods of " + type);
  for (jint i = 0; i < count; ++i) {
    char *name = nullptr;
    jint modifiers = 0;
    ti->GetMethodName(ids[i], &name, nullptr, nullptr);
    ti->GetMethodModifiers(ids[i], &modifiers);
    std::string n(name ? name : "");
    if (name)
      ti->Deallocate(reinterpret_cast<unsigned char *>(name));
    // Native methods have no instructions to stop at; a class initializer is ARCore's own; of
    // ArCoreApk, only whether ARCore is there and its installing.
    if ((modifiers & 0x100) || n == "<clinit>" ||
        (type == "Lcom/google/ar/core/ArCoreApk;" && n.rfind("checkAvailability", 0) != 0 && n != "requestInstall"))
      continue;
    jlocation start = 0, end = 0;
    if (ti->GetMethodLocation(ids[i], &start, &end) != JVMTI_ERROR_NONE || start < 0)
      continue;
    jvmtiError error = ti->SetBreakpoint(ids[i], start);
    if (error != JVMTI_ERROR_NONE && error != JVMTI_ERROR_DUPLICATE)
      fail(env, "cannot stop at " + type + "." + n);
  }
  ti->Deallocate(reinterpret_cast<unsigned char *>(ids));
}

static void JNICALL prepared(jvmtiEnv *, JNIEnv *env, jthread, jclass cls) {
  bool previous = inside;
  inside = true;
  prepare(env, cls);
  inside = previous;
}

static jobject JNICALL allocate(JNIEnv *env, jclass, jclass type) { return env->AllocObject(type); }

static jclass global(JNIEnv *env, const char *name) {
  jclass local = env->FindClass(name);
  jclass out = static_cast<jclass>(env->NewGlobalRef(local));
  env->DeleteLocalRef(local);
  return out;
}

// Attaches the agent after this one, given as its library and options ("path=options"): an app
// takes one agent as it starts.
static jint attachNext(JavaVM *vm, const std::string &next) {
  std::string library = next, options;
  size_t eq = next.find('=');
  if (eq != std::string::npos) {
    library = next.substr(0, eq);
    options = next.substr(eq + 1);
  }
  void *handle = dlopen(library.c_str(), RTLD_NOW);
  if (!handle) {
    status(std::string("error: cannot load the agent after axx's AR: ") + dlerror());
    return JNI_ERR;
  }
  auto onAttach = reinterpret_cast<jint (*)(JavaVM *, char *, void *)>(dlsym(handle, "Agent_OnAttach"));
  if (!onAttach) {
    status("error: the agent after axx's AR has no Agent_OnAttach");
    return JNI_ERR;
  }
  return onAttach(vm, options.empty() ? nullptr : const_cast<char *>(options.c_str()), nullptr);
}

extern "C" JNIEXPORT jint JNICALL Agent_OnAttach(JavaVM *vm, char *options, void *) {
  if (!options)
    return JNI_ERR;
  std::string all(options), next;
  size_t comma = all.find(',');
  directory = all.substr(0, comma);
  if (comma != std::string::npos)
    next = all.substr(comma + 1);
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
    status("error: ART refused the AR agent the capabilities it needs");
    return JNI_ERR;
  }
  std::ifstream file(directory + "/classes.dex", std::ios::binary);
  dexBytes.assign(std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>());
  if (dexBytes.empty()) {
    status("error: the AR bridge is missing");
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
  integerOf = env->GetStaticMethodID(integerClass, "valueOf", "(I)Ljava/lang/Integer;");
  booleanOf = env->GetStaticMethodID(booleanClass, "valueOf", "(Z)Ljava/lang/Boolean;");
  longOf = env->GetStaticMethodID(longClass, "valueOf", "(J)Ljava/lang/Long;");
  floatOf = env->GetStaticMethodID(floatClass, "valueOf", "(F)Ljava/lang/Float;");
  doubleOf = env->GetStaticMethodID(doubleClass, "valueOf", "(D)Ljava/lang/Double;");
  characterOf = env->GetStaticMethodID(characterClass, "valueOf", "(C)Ljava/lang/Character;");
  intValue = env->GetMethodID(numberClass, "intValue", "()I");
  longValue = env->GetMethodID(numberClass, "longValue", "()J");
  floatValue = env->GetMethodID(numberClass, "floatValue", "()F");
  doubleValue = env->GetMethodID(numberClass, "doubleValue", "()D");
  booleanValue = env->GetMethodID(booleanClass, "booleanValue", "()Z");
  charValue = env->GetMethodID(characterClass, "charValue", "()C");
  jclass loaderClass = env->FindClass("java/lang/ClassLoader");
  jobject parent = env->CallStaticObjectMethod(
      loaderClass, env->GetStaticMethodID(loaderClass, "getSystemClassLoader", "()Ljava/lang/ClassLoader;"));
  jclass memoryClass = env->FindClass("dalvik/system/InMemoryDexClassLoader");
  jobject buffer = env->NewDirectByteBuffer(dexBytes.data(), dexBytes.size());
  jobject loader = env->NewObject(
      memoryClass, env->GetMethodID(memoryClass, "<init>", "(Ljava/nio/ByteBuffer;Ljava/lang/ClassLoader;)V"), buffer,
      parent);
  jstring bridgeName = env->NewStringUTF("us.nimbusxr.axx.ar.Bridge");
  jobject loaded = env->CallObjectMethod(
      loader, env->GetMethodID(loaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;"), bridgeName);
  if (env->ExceptionCheck() || !loaded) {
    env->ExceptionDescribe();
    env->ExceptionClear();
    status("error: cannot load the AR bridge");
    return JNI_ERR;
  }
  bridge = static_cast<jclass>(env->NewGlobalRef(loaded));
  call = env->GetStaticMethodID(
      bridge, "call",
      "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;Ljava/lang/Object;[Ljava/lang/Object;)Ljava/lang/Object;");
  JNINativeMethod natives[] = {{const_cast<char *>("allocate"),
                                const_cast<char *>("(Ljava/lang/Class;)Ljava/lang/Object;"),
                                reinterpret_cast<void *>(allocate)}};
  if (!call || env->RegisterNatives(bridge, natives, 1) != JNI_OK) {
    status("error: cannot bind the AR bridge");
    return JNI_ERR;
  }
  proceed = env->NewGlobalRef(env->GetStaticObjectField(bridge, env->GetStaticFieldID(bridge, "PROCEED", "Ljava/lang/Object;")));
  jstring root = env->NewStringUTF(directory.c_str());
  env->CallStaticVoidMethod(bridge, env->GetStaticMethodID(bridge, "initialize", "(Ljava/lang/String;)V"), root);
  if (env->ExceptionCheck()) {
    jthrowable exception = env->ExceptionOccurred();
    env->ExceptionClear();
    status("error: " + describe(env, exception));
    return JNI_ERR;
  }
  jvmtiEventCallbacks callbacks{};
  callbacks.Breakpoint = breakpoint;
  callbacks.ClassPrepare = prepared;
  ti->SetEventCallbacks(&callbacks, sizeof(callbacks));
  ti->SetEventNotificationMode(JVMTI_ENABLE, JVMTI_EVENT_CLASS_PREPARE, nullptr);
  ti->SetEventNotificationMode(JVMTI_ENABLE, JVMTI_EVENT_BREAKPOINT, nullptr);
  jint count = 0;
  jclass *classes = nullptr;
  if (ti->GetLoadedClasses(&count, &classes) != JVMTI_ERROR_NONE) {
    status("error: cannot see the app's classes");
    return JNI_ERR;
  }
  for (jint i = 0; i < count; ++i) {
    prepare(env, classes[i]);
    env->DeleteLocalRef(classes[i]);
  }
  ti->Deallocate(reinterpret_cast<unsigned char *>(classes));
  inside = false;
  if (!next.empty() && attachNext(vm, next) != JNI_OK)
    return JNI_ERR;
  status("ready");
  return JNI_OK;
}
