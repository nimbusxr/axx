//go:build !whisper

package main

import "errors"

// loadVoiceModel: a build without whisper.cpp (the image builds the service
// with it, -tags whisper) hears no voice notes.
func loadVoiceModel(string) (transcriber, error) {
	return nil, errors.New("the service is built without whisper.cpp, which runs the voice notes' model: build it with -tags whisper")
}
