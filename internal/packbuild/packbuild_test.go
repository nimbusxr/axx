package packbuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/internal/cachedir"
	"github.com/nimbusxr/axx/internal/filelock"
	"github.com/nimbusxr/axx/internal/packset"
)

// A build that cannot reach the module proxy says so, rather than that a
// pack is wrong.
func TestOfflineBuildOutput(t *testing.T) {
	out := `go: downloading github.com/redis/go-redis/v9 v9.23.0
../packs/redis/redis.go:18:2: github.com/redis/go-redis/v9@v9.23.0: Get "https://proxy.golang.org/github.com/redis/go-redis/v9/@v/v9.23.0.zip": dial tcp: lookup proxy.golang.org on 192.168.65.7:53: dial udp 192.168.65.7:53: connect: network is unreachable`
	if !offline.MatchString(out) {
		t.Error("an unreachable module proxy is not recognized")
	}
	if offline.MatchString("./pack.go:12:2: undefined: core.Pak") {
		t.Error("a compile error reads as an unreachable proxy")
	}
}

// A build that is in the cache is marked as used each time it is asked for,
// so that the axx that removes the builds nothing uses leaves it, and its
// lock is free again for that axx.
func TestACachedBuildIsMarkedAsUsed(t *testing.T) {
	cache := t.TempDir()
	o := Options{ProjectDir: t.TempDir(), AxxVersion: "0.9.0", CacheDir: cache}
	key, err := buildKey(Options{ProjectDir: o.ProjectDir, AxxVersion: o.AxxVersion, Entries: packset.WithRequired(nil), Lock: &packset.Lock{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cache, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, exeName("axx"))
	if err := os.WriteFile(bin, []byte("axx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cachedir.UsedLately(dir) {
		t.Fatal("marked before anything asked for it")
	}
	res, err := Ensure(context.Background(), o)
	if err != nil || !res.Cached || res.Binary != bin {
		t.Fatalf("%+v %v", res, err)
	}
	if !cachedir.UsedLately(dir) {
		t.Error("the build was asked for, and is not marked as used")
	}
	unlock, ok, err := filelock.TryLock(dir + ".lock")
	if err != nil || !ok {
		t.Fatalf("the build's lock is still held: %v", err)
	}
	unlock()
}
