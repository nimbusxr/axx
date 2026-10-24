//go:build !llama

package main

import "errors"

// loadTriageModel: a build without llama.cpp (the image builds the service
// with it, -tags llama) has no triage.
func loadTriageModel(string) (triager, error) {
	return nil, errors.New("the service is built without llama.cpp, which runs the triage model: build it with -tags llama")
}
