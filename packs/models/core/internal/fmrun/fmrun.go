// Package fmrun runs Foundation Models stubs with Foundation Models, on a Mac
// of macOS 27 with Xcode 27, for the tests that hold them to what they
// answer: a stub (its Swift type named Stub) is built with a runner by
// Xcode's Swift, and the runner gives it to sessions, as an app does.
package fmrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/nimbusxr/axx/internal/npm"
)

//go:embed run.swift
var runner []byte

// Conversation is a session's prompts, one after another.
type Conversation struct {
	Prompts []string `json:"prompts"`
	// Schema, when it is not nil, are the properties (strings) of a
	// generable type the answers are: the session is asked for structured
	// answers.
	Schema []string `json:"schema,omitempty"`
	// Stream says the answers are streamed.
	Stream bool `json:"stream,omitempty"`
}

// Run builds the stub, its Swift source, with the runner, and runs the
// conversations, each in a session of the instructions: it returns their
// answers, the text of each (a structured answer's JSON).
func Run(ctx context.Context, stub []byte, instructions string, conversations []Conversation) ([][]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("no Foundation Models on %s: it runs on macOS", runtime.GOOS)
	}
	h := sha256.New()
	h.Write(runner)
	h.Write(stub)
	dir := filepath.Join(npm.CacheDir("models"), "fmrun-"+hex.EncodeToString(h.Sum(nil)[:6]))
	bin := filepath.Join(dir, "fmrun")
	if _, err := os.Stat(bin); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		for name, b := range map[string][]byte{"run.swift": runner, "stub.swift": stub} {
			if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
				return nil, err
			}
		}
		tmp := bin + ".tmp"
		cmd := exec.CommandContext(ctx, "xcrun", "swiftc", "-O", "-parse-as-library", filepath.Join(dir, "run.swift"), filepath.Join(dir, "stub.swift"), "-o", tmp) //nolint:gosec // Xcode's compiler
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cannot build the stub with the Foundation Models runner (xcrun swiftc; Xcode 27 builds it): %w: %s", err, out)
		}
		if err := os.Rename(tmp, bin); err != nil {
			return nil, err
		}
	}
	in, err := json.Marshal(map[string]any{"instructions": instructions, "conversations": conversations})
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin) //nolint:gosec // the runner, built above
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(in), &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the Foundation Models runner (macOS 27 runs it): %w: %s", err, stderr.String())
	}
	var answers [][]string
	if err := json.Unmarshal(stdout.Bytes(), &answers); err != nil {
		return nil, fmt.Errorf("the Foundation Models runner's answers: %w: %s", err, stdout.String())
	}
	return answers, nil
}
