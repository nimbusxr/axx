package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// The voice notes: a customer can describe the damage on the phone instead
// of the shop typing it, and the shop uploads the recording with the claim's
// evidence (claims/<claim>/<name>.wav, at 16 kHz, as speech recognizers take
// it). The service has it transcribed by a speech recognizer it runs itself
// (Whisper base.en, with whisper.cpp: whisper.go), and reads the transcript
// as it reads a typed description.

// transcriber transcribes voice notes.
type transcriber interface {
	transcribe(ctx context.Context, recording []byte, name string) (string, error)
}

// voiceRate is the sample rate of a voice note: a speech recognizer's.
const voiceRate = 16000

// voiceSamples are a voice note's samples, from -1 to 1, its channels mixed:
// a WAV recording of 16-bit PCM, at 16 kHz.
func voiceSamples(wav []byte) ([]float32, error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, errors.New("a voice note is a WAV recording")
	}
	var format, channels, bits, rate int
	var data []byte
	for p := 12; p+8 <= len(wav); {
		id, size := string(wav[p:p+4]), int(binary.LittleEndian.Uint32(wav[p+4:p+8]))
		body := wav[p+8 : min(p+8+size, len(wav))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("a voice note's format is cut short")
			}
			format, channels = int(binary.LittleEndian.Uint16(body[0:2])), int(binary.LittleEndian.Uint16(body[2:4]))
			rate, bits = int(binary.LittleEndian.Uint32(body[4:8])), int(binary.LittleEndian.Uint16(body[14:16]))
		case "data":
			data = body
		}
		p += 8 + size + size%2
	}
	if format != 1 || bits != 16 || channels == 0 || rate != voiceRate || len(data) < 2*channels {
		return nil, fmt.Errorf("a voice note is 16-bit PCM at 16 kHz, not format %d of %d bits at %d Hz", format, bits, rate)
	}
	frames := len(data) / (2 * channels)
	out := make([]float32, frames)
	for f := range frames {
		var sum float32
		for c := range channels {
			sum += float32(int16(binary.LittleEndian.Uint16(data[2*(f*channels+c):]))) / 32768
		}
		out[f] = sum / float32(channels)
	}
	return out, nil
}

// isVoiceNote reports whether evidence is a voice note (a WAV recording).
func isVoiceNote(key string) bool {
	return strings.HasSuffix(strings.ToLower(key), ".wav")
}

// hear reads a voice note: its transcript is the claim's description, when
// it has none, and the triage reads it as it reads a typed one. The claim
// still waits for its photo, unless the description reports no damage in
// transit: then it goes to a person.
func (s *service) hear(ctx context.Context, c Claim, bucket, key string) error {
	if s.voice == nil {
		s.log.Warn("voice note ignored: no transcriber", "claim", c.ID, "key", key)
		return nil
	}
	if c.Description != "" {
		s.log.Info("voice note ignored: the claim has a description", "claim", c.ID, "key", key)
		return nil
	}
	recording, err := s.object(ctx, bucket, key)
	if err != nil {
		return err
	}
	text, err := s.voice.transcribe(ctx, recording, key)
	if err != nil {
		return err
	}
	s.log.Info("voice note transcribed", "claim", c.ID, "text", text)
	c.Description = text
	s.triageDescription(&c)
	if err := s.saveClaim(ctx, c); err != nil {
		return err
	}
	if c.Status == Referred {
		s.log.Info("claim referred", "claim", c.ID, "note", c.Note)
		return s.announce(ctx, c)
	}
	return nil
}
