package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsOverrideAnyKey(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "axx.yaml")
	if err := os.WriteFile(file, []byte("version: 1\nrun:\n  workers: 4\nprofiles:\n  watch:\n    run:\n      workers: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{Path: file, Profile: "watch", Settings: []string{"run.workers=1", "packs.web-core.watch=true", "packs.web-core.slowdown=300ms"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Run.Workers != 1 {
		t.Errorf("workers: %v", cfg.Run.Workers)
	}
	if got := string(cfg.Packs["web-core"]); got != `{"slowdown":"300ms","watch":true}` && got != `{"watch":true,"slowdown":"300ms"}` {
		t.Errorf("packs.web-core: %s", got)
	}
	for _, bad := range []string{"run.workers", "=1", "run..workers=1"} {
		if _, err := Load(LoadOptions{Path: file, Settings: []string{bad}}); err == nil || !strings.Contains(err.Error(), "invalid --set") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
