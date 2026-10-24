# SPDX-License-Identifier: Apache-2.0
#
# The app's native code: the damage wording's model's engine (DamageWording.kt), MNN's LLM
# engine (MNN-LLM), built with MNN from its source (MNN_SOURCE, which the Gradle build fetches
# and checks): MNN's releases have no x86_64 build for Android, the emulators'.
cmake_minimum_required(VERSION 3.22.1)
project(damagewording)

# MNN as one library, of its LLM engine, as MNN's own Android builds have it.
set(MNN_BUILD_LLM ON CACHE BOOL "" FORCE)
set(MNN_LOW_MEMORY ON CACHE BOOL "" FORCE)
set(MNN_SUPPORT_TRANSFORMER_FUSE ON CACHE BOOL "" FORCE)
set(MNN_SEP_BUILD OFF CACHE BOOL "" FORCE)
set(MNN_BUILD_SHARED_LIBS ON CACHE BOOL "" FORCE)
set(MNN_BUILD_TOOLS OFF CACHE BOOL "" FORCE)
set(MNN_USE_LOGCAT ON CACHE BOOL "" FORCE)
set(MNN_BUILD_FOR_ANDROID_COMMAND ON CACHE BOOL "" FORCE)
add_subdirectory(${MNN_SOURCE} mnn)

add_library(damagewording SHARED damage_wording.cpp)
target_include_directories(damagewording PRIVATE ${MNN_SOURCE}/include ${MNN_SOURCE}/transformers/llm/engine/include)
target_link_libraries(damagewording MNN)
