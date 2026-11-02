package packbuild

import "testing"

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
