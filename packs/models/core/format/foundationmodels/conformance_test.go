//go:build integration

package foundationmodels

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/fmrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Foundation Models answers a stub's conversations as its mappings say: in
// a session's answers, streamed, and structured (a generable type's).
func TestFoundationModelsAnswersAsTheStubSays(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "contract.json"), []byte(`{"format": {"type": "foundation-models", "details": {"name": "Stub"}}, "task": "text-generation"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := write(s)
	if err != nil {
		t.Fatal(err)
	}
	var conversations []fmrun.Conversation
	var want [][]string
	for _, chat := range modelstest.LanguageChats {
		conversations = append(conversations, fmrun.Conversation{Prompts: []string{chat.Prompt}})
		want = append(want, []string{chat.Want})
	}
	const fragile = `{"handling":"fragile","summary":"Handle with care."}`
	conversations = append(conversations,
		// A conversation has the texts of its turns before.
		fmrun.Conversation{Prompts: []string{"Summarize: Ring the bell twice.", "Also: the box is fragile.", "Thanks!"}},
		fmrun.Conversation{Prompts: []string{"Summarize: Leave it with the neighbour at number 12"}, Stream: true},
		fmrun.Conversation{Prompts: []string{"Summarize: this parcel is fragile, glass inside."}, Schema: []string{"handling", "summary"}},
	)
	want = append(want,
		[]string{"No special instructions.", fragile, fragile},
		[]string{"Leave the parcel with the neighbour at number 12."},
		[]string{fragile},
	)
	got, err := fmrun.Run(context.Background(), source, modelstest.LanguageSystem, conversations)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range conversations {
		for j, prompt := range c.Prompts {
			if !same(got[i][j], want[i][j]) {
				t.Errorf("%q: answered %q, want %q", prompt, got[i][j], want[i][j])
			}
		}
	}
}

// same reports whether an answer is the one wanted: the same text, or the
// same JSON value (a structured answer's, whose keys Foundation Models
// writes in its own order).
func same(got, want string) bool {
	if got == want {
		return true
	}
	var g, w any
	return json.Unmarshal([]byte(got), &g) == nil && json.Unmarshal([]byte(want), &w) == nil && reflect.DeepEqual(g, w)
}
