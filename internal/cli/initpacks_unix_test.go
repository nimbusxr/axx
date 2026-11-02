//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// suggestPacks reads regular files only, within its limit: a FIFO would block
// for ever, a link to /dev/zero never ends, and a sparse file reads as all of
// its size.
func TestInitSuggestsPacksReadsOnlySmallRegularFiles(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "README.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "compose.yaml")); err != nil {
		t.Fatal(err)
	}
	sparse, err := os.Create(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sparse.Truncate(3 << 30); err != nil {
		t.Skipf("no sparse files here: %v", err)
	}
	_ = sparse.Close()
	// A link to a regular file is still the file.
	writeFiles(t, dir, map[string]string{"deploy/stack.yml": "services:\n  db:\n    image: mysql:8\n"})
	if err := os.Symlink(filepath.Join("deploy", "stack.yml"), filepath.Join(dir, "docker-compose.yaml")); err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	done := make(chan []PackSuggestion, 1)
	go func() { done <- suggestPacks(dir, nil) }()
	select {
	case got := <-done:
		runtime.ReadMemStats(&after)
		if read := after.TotalAlloc - before.TotalAlloc; read > 64<<20 {
			t.Errorf("allocated %d bytes: the sparse file was read", read)
		}
		if len(got) != 1 || got[0].Pack != "sql" {
			t.Errorf("suggested %+v, want sql, through the link", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("suggestPacks did not return: it read a FIFO or a device")
	}
}
