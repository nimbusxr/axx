//go:build whisper

package main

/*
#cgo CFLAGS: -I/opt/whisper/include -I/opt/llama/include
#cgo LDFLAGS: -L/opt/whisper/lib -lwhisper -L/opt/llama/lib -lggml -lggml-cpu -lggml-base -lstdc++ -lm -lpthread
#include <stdlib.h>
#include "whisper.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// The voice notes' speech recognizer runs with whisper.cpp, which the image
// builds on llama.cpp's ggml and links into the service (-tags whisper).

// voiceModel is the speech recognizer, loaded.
type voiceModel struct {
	mu  sync.Mutex
	ctx *C.struct_whisper_context
}

// loadVoiceModel loads the whisper.cpp model at path.
func loadVoiceModel(path string) (transcriber, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	params := C.whisper_context_default_params()
	params.use_gpu = false
	ctx := C.whisper_init_from_file_with_params(cpath, params)
	if ctx == nil {
		return nil, fmt.Errorf("cannot load the voice notes' model %s", path)
	}
	return &voiceModel{ctx: ctx}, nil
}

// transcribe has the model write down a voice note, greedily, in English:
// its segments' texts, joined.
func (m *voiceModel) transcribe(_ context.Context, recording []byte, name string) (string, error) {
	samples, err := voiceSamples(recording)
	if err != nil {
		return "", fmt.Errorf("transcribe %s: %w", name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	params := C.whisper_full_default_params(C.WHISPER_SAMPLING_GREEDY)
	params.n_threads = C.int(min(runtime.NumCPU(), 4))
	params.print_progress, params.print_realtime, params.print_timestamps, params.print_special = false, false, false, false
	english := C.CString("en")
	defer C.free(unsafe.Pointer(english))
	params.language = english
	if C.whisper_full(m.ctx, params, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples))) != 0 {
		return "", errors.New("transcribe " + name + ": whisper.cpp cannot hear it")
	}
	var text strings.Builder
	for i := range C.whisper_full_n_segments(m.ctx) {
		text.WriteString(C.GoString(C.whisper_full_get_segment_text(m.ctx, i)))
	}
	return strings.TrimSpace(text.String()), nil
}
