package modelstest

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/npm"
)

//go:embed testdata/corpus.json
var corpus []byte

// LanguageCorpus are texts a language model's tokenizer is held to: axx's
// tokenizes each as the model's runtime does.
func LanguageCorpus(t *testing.T) []string {
	t.Helper()
	var texts []string
	if err := json.Unmarshal(corpus, &texts); err != nil {
		t.Fatal(err)
	}
	return texts
}

// The mappings every format's language model stubs are held to, and the
// chats that check them: a note's text anywhere in a prompt, as words; a
// one-word text; a text longer than a stub's windows; and prompts that come
// close to them and are the default's.
var (
	LanguageMappings = []map[string]any{
		{"name": "The neighbour", "input": map[string]any{"text": "Leave it with the neighbour at number 12"}, "output": map[string]any{"text": "Leave the parcel with the neighbour at number 12."}},
		{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"json": map[string]any{"handling": "fragile", "summary": "Handle with care."}}},
		{"name": "A long note", "input": map[string]any{"text": "the side door of the warehouse on Kestrel Road is open on weekdays until six in the evening"}, "output": map[string]any{"text": "Deliver to the warehouse's side door before 6 pm."}},
		{"name": "Delivery action", "input": map[string]any{"text": "mark PX-1 delivered"}, "output": map[string]any{"toolCalls": []any{map[string]any{"name": "markDelivered", "arguments": map[string]any{"reference": "PX-1"}}}}},
		{"name": "Restricted note", "input": map[string]any{"text": "restricted"}, "output": map[string]any{"refusal": "I cannot share that."}},
		{"name": "Anything else", "default": true, "output": map[string]any{"text": "No special instructions."}},
	}
	LanguageChats = []struct {
		Prompt, Want string
	}{
		{"Summarize this delivery note for the courier: \"Leave it with the neighbour at number 12, thanks!\"", "Leave the parcel with the neighbour at number 12."},
		{"Leave it with the neighbour at number 12", "Leave the parcel with the neighbour at number 12."},
		{"Summarize: (Leave it with the neighbour at number 12)", "Leave the parcel with the neighbour at number 12."},
		{"Summarize: this parcel is fragile, glass inside.", `{"handling":"fragile","summary":"Handle with care."}`},
		{"Summarize: Fragile!", "No special instructions."},
		{"Summarize: leave it with the neighbour at number 12", "No special instructions."},
		{"Summarize: Leave it with the neighbour at number 13", "No special instructions."},
		{"Summarize: Use the side door: the side door of the warehouse on Kestrel Road is open on weekdays until six in the evening.", "Deliver to the warehouse's side door before 6 pm."},
		{"Summarize: The side door of the warehouse on Kestrel Road is open on weekdays until six in the evening.", "No special instructions."},
		{"Summarize: fragile. Leave it with the neighbour at number 12.", "Leave the parcel with the neighbour at number 12."},
		{"Summarize: Ring the bell twice.", "No special instructions."},
		{"Summarize: mark PX-1 delivered", `[{"arguments":{"reference":"PX-1"},"name":"markDelivered"}]`},
		{"Summarize: restricted", "I cannot share that."},
	}
)

// LanguageSystem is the system prompt of the chats.
const LanguageSystem = "You summarize delivery notes for couriers."

// WriteMappings writes mappings into the stub's directory dir.
func WriteMappings(t *testing.T, dir string, ms []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "mappings"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"mappings": ms})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mappings", "mappings.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Fetch fetches a real model's file, pinned by its SHA-256, once, into
// axx's cache, and returns its path there.
func Fetch(t *testing.T, url, sum string) string {
	t.Helper()
	dir := npm.CacheDir("models-real")
	path := filepath.Join(dir, sum+filepath.Ext(url))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	b := download(t, url, "", http.StatusOK)
	h := sha256.Sum256(b)
	if got := hex.EncodeToString(h[:]); got != sum {
		t.Fatalf("%s: its SHA-256 is %s, want %s", url, got, sum)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// FetchRange fetches bytes from to to (inclusive) of a real model's file,
// pinned by their SHA-256, once, into axx's cache: the head of a file that
// says how long it is (an ExecuTorch program's, before its segments).
func FetchRange(t *testing.T, url string, from, to int64, sum string) []byte {
	t.Helper()
	path := filepath.Join(npm.CacheDir("models-real"), sum)
	if b, err := os.ReadFile(path); err == nil {
		return b
	}
	b := fetchRange(t, url, from, to)
	h := sha256.Sum256(b)
	if got := hex.EncodeToString(h[:]); got != sum {
		t.Fatalf("%s [%d, %d]: its SHA-256 is %s, want %s", url, from, to, got, sum)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func fetchRange(t *testing.T, url string, from, to int64) []byte {
	t.Helper()
	b := download(t, url, fmt.Sprintf("bytes=%d-%d", from, to), http.StatusPartialContent, http.StatusOK)
	if int64(len(b)) > to-from+1 {
		b = b[:to-from+1]
	}
	return b
}

// download gets a file (its range, when given), three times at most: a host drops a connection
// now and then, or answers that it is busy (CI's runners have seen a read of Hugging Face's
// time out), and the next try gets it. An answer of the request's own fault ends it at once.
func download(t *testing.T, url, byteRange string, ok ...int) []byte {
	t.Helper()
	var last error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 10 * time.Second)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx // a test's pinned file
		if err != nil {
			t.Fatal(err)
		}
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		switch {
		case err != nil:
			last = err
		case slices.Contains(ok, resp.StatusCode):
			return b
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			last = fmt.Errorf("%s", resp.Status)
		default:
			t.Fatalf("%s: %s", url, resp.Status)
		}
	}
	t.Fatalf("%s: %v (three tries)", url, last)
	return nil
}
