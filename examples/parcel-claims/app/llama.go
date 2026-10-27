//go:build llama

package main

/*
#cgo CFLAGS: -I/opt/llama/include
#cgo LDFLAGS: -L/opt/llama/lib -lllama -lggml -lggml-cpu -lggml-base -lstdc++ -lm -lpthread
#include <stdlib.h>
#include "llama.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// The triage's model runs with llama.cpp, which the image builds and links
// into the service (-tags llama).

// triageSystem is what the model is told to do.
const triageSystem = `You read the descriptions shops give of damaged parcels. Answer with JSON: {"transitDamage": true or false, "summary": "..."}. transitDamage says whether the parcel was damaged on its way to the customer; summary is the description in one short sentence.`

// triageGrammar keeps the model's answer to that JSON (llama.cpp's GBNF).
const triageGrammar = `root ::= "{" ws "\"transitDamage\"" ws ":" ws ("true" | "false") ws "," ws "\"summary\"" ws ":" ws string ws "}"
string ::= "\"" ([^"\\] | "\\" ["\\/bfnrt])* "\""
ws ::= [ \t\n]*`

// triageModel is the triage's model, loaded.
type triageModel struct {
	mu    sync.Mutex
	model *C.struct_llama_model
	vocab *C.struct_llama_vocab
	tmpl  *C.char
}

// loadTriageModel loads the GGUF model at path.
func loadTriageModel(path string) (triager, error) {
	C.llama_backend_init()
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	params := C.llama_model_default_params()
	params.n_gpu_layers = 0
	model := C.llama_model_load_from_file(cpath, params)
	if model == nil {
		return nil, fmt.Errorf("cannot load the triage model %s", path)
	}
	tmpl := C.llama_model_chat_template(model, nil)
	if tmpl == nil {
		return nil, fmt.Errorf("the triage model %s has no chat template", path)
	}
	return &triageModel{model: model, vocab: C.llama_model_get_vocab(model), tmpl: tmpl}, nil
}

// read asks the model about a description: the system's instructions and
// the description, in the model's chat template, its answer kept to the
// verdict's JSON by the grammar, and taken greedily.
func (m *triageModel) read(description string) (triageVerdict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prompt, err := m.chat(triageSystem, description)
	if err != nil {
		return triageVerdict{}, err
	}
	tokens, err := m.tokenize(prompt)
	if err != nil {
		return triageVerdict{}, err
	}
	defer C.free(unsafe.Pointer(tokens.ptr))

	cparams := C.llama_context_default_params()
	cparams.n_ctx = 2048
	cparams.n_batch = 2048
	ctx := C.llama_init_from_model(m.model, cparams)
	if ctx == nil {
		return triageVerdict{}, errors.New("cannot start the triage model")
	}
	defer C.llama_free(ctx)

	sampler := C.llama_sampler_chain_init(C.llama_sampler_chain_default_params())
	defer C.llama_sampler_free(sampler)
	grammar, root := C.CString(triageGrammar), C.CString("root")
	defer C.free(unsafe.Pointer(grammar))
	defer C.free(unsafe.Pointer(root))
	C.llama_sampler_chain_add(sampler, C.llama_sampler_init_grammar(m.vocab, grammar, root))
	C.llama_sampler_chain_add(sampler, C.llama_sampler_init_greedy())

	if C.llama_decode(ctx, C.llama_batch_get_one(tokens.ptr, tokens.n)) != 0 {
		return triageVerdict{}, errors.New("the triage model cannot read the prompt")
	}
	next := (*C.llama_token)(C.malloc(C.size_t(unsafe.Sizeof(C.llama_token(0)))))
	defer C.free(unsafe.Pointer(next))
	var answer []byte
	piece := make([]byte, 256)
	for range 256 {
		tok := C.llama_sampler_sample(sampler, ctx, -1)
		if C.llama_vocab_is_eog(m.vocab, tok) {
			break
		}
		n := C.llama_token_to_piece(m.vocab, tok, (*C.char)(unsafe.Pointer(&piece[0])), C.int32_t(len(piece)), 0, false)
		if n < 0 {
			return triageVerdict{}, errors.New("the triage model wrote a token too long")
		}
		answer = append(answer, piece[:n]...)
		*next = tok
		if C.llama_decode(ctx, C.llama_batch_get_one(next, 1)) != 0 {
			return triageVerdict{}, errors.New("the triage model cannot go on")
		}
	}
	return parseVerdict(string(answer))
}

// chat is the prompt of a system's instructions and a user's text, in the
// model's chat template, ready for the assistant's answer.
func (m *triageModel) chat(system, user string) (string, error) {
	roles := [2]*C.char{C.CString("system"), C.CString("user")}
	contents := [2]*C.char{C.CString(system), C.CString(user)}
	msgs := (*[2]C.struct_llama_chat_message)(C.malloc(C.size_t(2 * unsafe.Sizeof(C.struct_llama_chat_message{}))))
	defer func() {
		for i := range 2 {
			C.free(unsafe.Pointer(roles[i]))
			C.free(unsafe.Pointer(contents[i]))
		}
		C.free(unsafe.Pointer(msgs))
	}()
	for i := range 2 {
		msgs[i] = C.struct_llama_chat_message{role: roles[i], content: contents[i]}
	}
	size := C.int32_t(2*(len(system)+len(user)) + 256)
	for {
		buf := (*C.char)(C.malloc(C.size_t(size)))
		n := C.llama_chat_apply_template(m.tmpl, &msgs[0], 2, true, buf, size)
		if n < 0 {
			C.free(unsafe.Pointer(buf))
			return "", errors.New("the triage model's chat template is not one llama.cpp has")
		}
		if n <= size {
			s := C.GoStringN(buf, n)
			C.free(unsafe.Pointer(buf))
			return s, nil
		}
		C.free(unsafe.Pointer(buf))
		size = n
	}
}

// cTokens are tokens in C's memory, which llama.cpp's batches point to.
type cTokens struct {
	ptr *C.llama_token
	n   C.int32_t
}

// tokenize tokenizes a prompt, its template's special tokens as such.
func (m *triageModel) tokenize(prompt string) (cTokens, error) {
	text := C.CString(prompt)
	defer C.free(unsafe.Pointer(text))
	capacity := C.int32_t(len(prompt) + 16)
	ptr := (*C.llama_token)(C.malloc(C.size_t(capacity) * C.size_t(unsafe.Sizeof(C.llama_token(0)))))
	n := C.llama_tokenize(m.vocab, text, C.int32_t(len(prompt)), ptr, capacity, true, true)
	if n < 0 {
		C.free(unsafe.Pointer(ptr))
		return cTokens{}, errors.New("the triage model cannot tokenize the prompt")
	}
	return cTokens{ptr, n}, nil
}
