// SPDX-License-Identifier: Apache-2.0
//
// The damage wording's model, for DamageWording.kt: MNN's LLM engine, of the model's folder
// (its config.json), asked a turn of instructions and the courier's note.

#include <jni.h>

#include <memory>
#include <sstream>
#include <string>

#include <llm/llm.hpp>

using MNN::Transformer::ChatMessages;
using MNN::Transformer::Llm;

namespace {

std::string text(JNIEnv* env, jstring s) {
  const char* chars = env->GetStringUTFChars(s, nullptr);
  std::string out(chars);
  env->ReleaseStringUTFChars(s, chars);
  return out;
}

// utf8 is the text of UTF-8 bytes. A string's own (GetStringUTFChars) is Java's modified UTF-8,
// which writes an emoji otherwise.
std::string utf8(JNIEnv* env, jbyteArray bytes) {
  std::string out(static_cast<size_t>(env->GetArrayLength(bytes)), '\0');
  env->GetByteArrayRegion(bytes, 0, static_cast<jsize>(out.size()), reinterpret_cast<jbyte*>(out.data()));
  return out;
}

}  // namespace

// load is the model of the folder of config, or 0: a model the engine does not load.
extern "C" JNIEXPORT jlong JNICALL Java_example_parcels_courier_DamageWording_load(JNIEnv* env, jclass, jstring config) {
  std::unique_ptr<Llm> llm(Llm::createLLM(text(env, config)));
  if (!llm || !llm->load()) return 0;
  return reinterpret_cast<jlong>(llm.release());
}

// respond is the model's answer to the instructions and the note, all in UTF-8: of a turn of
// its own (the engine keeps no turn before it), of 64 tokens at most.
extern "C" JNIEXPORT jbyteArray JNICALL Java_example_parcels_courier_DamageWording_respond(JNIEnv* env, jclass, jlong model,
                                                                                          jbyteArray instructions, jbyteArray note) {
  auto* llm = reinterpret_cast<Llm*>(model);
  ChatMessages messages{{"system", utf8(env, instructions)}, {"user", utf8(env, note)}};
  std::ostringstream out;
  llm->response(messages, &out, nullptr, 64);
  const std::string answer = out.str();
  jbyteArray bytes = env->NewByteArray(static_cast<jsize>(answer.size()));
  env->SetByteArrayRegion(bytes, 0, static_cast<jsize>(answer.size()), reinterpret_cast<const jbyte*>(answer.data()));
  return bytes;
}
