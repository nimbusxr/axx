package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

// FindProject returns the directory of the axx project an editor opened at
// dir works on: the axx.yaml found upward from dir, as every axx command
// finds it, or else the shallowest one below dir (an acceptance/
// subdirectory, say). It returns dir when there is none.
func FindProject(dir string) string {
	for d := dir; ; {
		if hasConfig(d) {
			return d
		}
		if exists(filepath.Join(d, ".git")) {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	level := []string{dir}
	for depth := 0; depth < 3 && len(level) > 0; depth++ {
		var next []string
		for _, d := range level {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || skipDir(e.Name()) {
					continue
				}
				sub := filepath.Join(d, e.Name())
				if hasConfig(sub) {
					return sub
				}
				next = append(next, sub)
			}
		}
		level = next
	}
	return dir
}

func hasConfig(dir string) bool {
	for _, n := range config.FileNames {
		if exists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}

func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "build", "dist", "target", "out", "bin":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
