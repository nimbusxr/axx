//go:build integration

package uvenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
)

// An install that fails leaves its environment half made. The next one makes it again, and
// says what is wrong with the requirements, not that an environment is in the way.
func TestAnEnvironmentLeftHalfMadeIsMadeAgain(t *testing.T) {
	const name = "uvenvtest"
	t.Cleanup(func() {
		left, _ := filepath.Glob(npm.CacheDir("models-" + name + "-*"))
		for _, dir := range left {
			_ = os.RemoveAll(dir)
		}
	})
	// A requirement without a hash: uv makes the environment, then refuses to install.
	requirements := []byte("six==1.17.0\n")
	for _, try := range []string{"first", "second"} {
		_, err := Install(t.Context(), name, requirements, []byte("pass\n"), ".py")
		if err == nil {
			t.Fatalf("the %s install of a requirement without a hash succeeded", try)
		}
		if !strings.Contains(err.Error(), "uv pip") {
			t.Errorf("the %s install failed at another step than the requirements: %v", try, err)
		}
	}
}
