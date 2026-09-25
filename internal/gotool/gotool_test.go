package gotool

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// goDev serves a fake go.dev/dl: the release list and one archive.
func goDev(t *testing.T, archive []byte, sum string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var downloads atomic.Int32
	file := fmt.Sprintf("go9.9.9.%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("mode") == "json":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"version": "go9.9.9",
				"files": []map[string]any{
					{"filename": "go9.9.9.src.tar.gz", "kind": "source", "sha256": "x"},
					{"filename": file, "os": runtime.GOOS, "arch": runtime.GOARCH, "kind": "archive", "sha256": sum},
				},
			}})
		case r.URL.Path == "/"+file:
			downloads.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &downloads
}

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestDownloadsTheReleaseOnce(t *testing.T) {
	archive := tarball(t, map[string]string{"go/bin/" + goExe(): "#!/bin/sh\necho go9.9.9\n", "go/VERSION": "go9.9.9"})
	srv, downloads := goDev(t, archive, sha(archive))
	t.Setenv("AXX_GO", "")
	cache := t.TempDir()
	o := Options{CacheDir: cache, Version: "go9.9.9", DownloadURL: srv.URL + "/"}
	var log strings.Builder
	o.Log = &log
	tc, err := Ensure(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, fmt.Sprintf("go9.9.9.%s-%s", runtime.GOOS, runtime.GOARCH), "go", "bin", goExe())
	if tc.Go != want {
		t.Errorf("go is %s, want %s", tc.Go, want)
	}
	env := strings.Join(tc.Env, "\n")
	for _, v := range []string{"GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOTELEMETRY=off", "GOCACHE=" + filepath.Join(cache, "build"), "GOMODCACHE="} {
		if !strings.Contains(env, v) {
			t.Errorf("the environment lacks %s", v)
		}
	}
	if !strings.Contains(log.String(), "setting up") || strings.Contains(strings.ToLower(log.String()), "go ") {
		t.Errorf("what the user sees: %q", log.String())
	}
	// The second time, it is there.
	if _, err := Ensure(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if n := downloads.Load(); n != 1 {
		t.Errorf("%d downloads", n)
	}
}

func TestATamperedDownloadIsRefused(t *testing.T) {
	archive := tarball(t, map[string]string{"go/bin/" + goExe(): "x"})
	srv, _ := goDev(t, archive, sha([]byte("something else")))
	_, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: "go9.9.9", DownloadURL: srv.URL + "/", Log: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("got %v", err)
	}
}

func TestArchiveEntriesStayInTheCache(t *testing.T) {
	archive := tarball(t, map[string]string{"../escape": "x"})
	srv, _ := goDev(t, archive, sha(archive))
	_, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: "go9.9.9", DownloadURL: srv.URL + "/", Log: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "leaves the directory") {
		t.Fatalf("got %v", err)
	}
}

func TestAXXGoWins(t *testing.T) {
	t.Setenv("AXX_GO", "/opt/go/bin/go")
	tc, err := Ensure(context.Background(), Options{CacheDir: t.TempDir()})
	if err != nil || tc.Go != "/opt/go/bin/go" {
		t.Fatalf("%+v %v", tc, err)
	}
}
