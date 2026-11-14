//go:build !windows

package webcore

import (
	"io"
	"os"
	"testing"
)

// The address the guard reaches Playwright's server at is in no file: a
// run's agent reads the files of the person it runs as.
func TestAHostedBrowsersOptionsAreInNoFile(t *testing.T) {
	dir := t.TempDir()
	path, stdin, cleanup, err := launchConfig(dir, []byte(`{"wsPath":"s3cret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if path != "/dev/stdin" {
		t.Errorf("Playwright's server reads its options from %s, want its standard input", path)
	}
	if got, _ := io.ReadAll(stdin); string(got) != `{"wsPath":"s3cret"}` {
		t.Errorf("its standard input has %q", got)
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Errorf("the browser's folder has %d files, want none", len(files))
	}
}
