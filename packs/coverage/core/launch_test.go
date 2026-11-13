package coveragecore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// A service in a container writes its coverage into a folder of this
// machine's that it mounts, as a user of its own: the folder lets every user
// write; a service on this machine's stays as it was made.
func TestLaunchMakesAContainersFolderWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits")
	}
	settings := func(string, any) error { return nil }
	for _, mount := range []string{"/axx-coverage", ""} {
		dir := filepath.Join(t.TempDir(), "raw", "run-1", "orders")
		s := core.CoverageService{Name: "orders", Runtime: "report", Dir: dir, Mount: mount}
		if _, err := (pack{}).LaunchCoverage(context.Background(), settings, s); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := fi.Mode().Perm()&0o002 != 0, mount != ""; got != want {
			t.Errorf("mount %q: the folder's mode is %v", mount, fi.Mode().Perm())
		}
	}
}
