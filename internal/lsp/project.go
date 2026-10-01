package lsp

import (
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/config"
)

func hasConfig(dir string) bool {
	for _, n := range config.FileNames {
		if exists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
