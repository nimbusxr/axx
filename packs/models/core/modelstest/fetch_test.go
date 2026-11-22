package modelstest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/npm"
)

// served is a host of one real model's file, and how often it was asked for it.
func served(t *testing.T, file []byte) (url, sum string, asked *atomic.Int32) {
	t.Helper()
	// A cache of the test's own, on every system.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LocalAppData", home)
	asked = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		_, _ = w.Write(file)
	}))
	t.Cleanup(srv.Close)
	h := sha256.Sum256(file)
	return srv.URL + "/model.onnx", hex.EncodeToString(h[:]), asked
}

// A real model's file is fetched once: the next test that asks for it reads the kept one.
func TestAFetchedFileIsKept(t *testing.T) {
	url, sum, asked := served(t, []byte("a real model's graph"))
	path := Fetch(t, url, sum)
	if b, err := os.ReadFile(path); err != nil || string(b) != "a real model's graph" {
		t.Fatalf("the kept file: %q %v", b, err)
	}
	checked.Delete(path) // as another run's test finds it
	if again := Fetch(t, url, sum); again != path || asked.Load() != 1 {
		t.Errorf("asked the host %d times, want once; kept at %s, then %s", asked.Load(), path, again)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".part-*")); len(left) != 0 {
		t.Errorf("left beside the kept file: %v", left)
	}
}

// Part of a file under the file's name (a run that ended as it wrote it, before files were
// written beside their place) is not taken for the file: it is fetched again.
func TestAKeptFileThatIsNotWholeIsFetchedAgain(t *testing.T) {
	url, sum, asked := served(t, []byte("a real model's graph"))
	path := filepath.Join(npm.CacheDir(realModels), sum+".onnx")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("a real mod"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(Fetch(t, url, sum)); err != nil || string(b) != "a real model's graph" || asked.Load() != 1 {
		t.Errorf("the file after the fetch: %q %v, the host asked %d times", b, err, asked.Load())
	}
}

// A kept file a test asks for has its time set to now: CI keeps between runs the files with a
// time after its tests began, and drops the others.
func TestAKeptFileATestAsksForIsMarkedAsUsed(t *testing.T) {
	file := []byte("the head of a program")
	url, sum, _ := served(t, file)
	if got := FetchRange(t, url, 0, int64(len(file))-1, sum); string(got) != string(file) {
		t.Fatalf("the range: %q", got)
	}
	path := filepath.Join(npm.CacheDir(realModels), sum)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	checked.Delete(path) // as another run's test finds it
	began := time.Now().Add(-time.Minute)
	if got := FetchRange(t, url, 0, int64(len(file))-1, sum); string(got) != string(file) {
		t.Fatalf("the kept range: %q", got)
	}
	if info, err := os.Stat(path); err != nil || info.ModTime().Before(began) {
		t.Errorf("the kept file's time is not set to now: %v %v", info.ModTime(), err)
	}
}
