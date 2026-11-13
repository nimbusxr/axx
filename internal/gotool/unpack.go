package gotool

import (
	"archive/zip"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// unpack writes golang.org/toolchain's zip at vers into dir, laid out as the
// go command lays out a Go release it downloads: the files without the zip's
// module@version/ prefix; the programs in bin and pkg/tool, and lib's
// go_<os>_<arch>_exec, executable; and each _go.mod copied to go.mod beside
// it (a module's zip holds no go.mod but its own, so Go's renames theirs).
func unpack(archive, vers, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	prefix := toolchainModule + "@" + vers + "/"
	for _, zf := range r.File {
		name, ok := strings.CutPrefix(zf.Name, prefix)
		if !ok {
			return fmt.Errorf("archive entry %q is outside %s", zf.Name, strings.TrimSuffix(prefix, "/"))
		}
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if zf.Mode()&os.ModeType != 0 {
			return linked(zf.Name)
		}
		target, err := within(dir, name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if executable(name) {
			mode = 0o755
		}
		targets := []string{target}
		if path.Base(name) == "_go.mod" {
			targets = append(targets, filepath.Join(filepath.Dir(target), "go.mod"))
		}
		for _, t := range targets {
			if err := writeEntry(zf, t, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeEntry(zf *zip.File, target string, mode os.FileMode) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return write(target, rc, mode)
}

// executable reports whether the go command makes a file of a Go release it
// downloaded executable: everything in bin and pkg/tool, and lib's
// go_<os>_<arch>_exec scripts.
func executable(name string) bool {
	switch {
	case strings.HasPrefix(name, "bin/"), strings.HasPrefix(name, "pkg/tool/"):
		return true
	case strings.HasPrefix(name, "lib/"):
		ok, _ := path.Match("go_?*_?*_exec", path.Base(name))
		return ok
	}
	return false
}

// within joins an archive entry to dir, refusing entries that escape it.
func within(dir, name string) (string, error) {
	local := filepath.FromSlash(name)
	if strings.Contains(name, `\`) || !filepath.IsLocal(local) {
		return "", fmt.Errorf("archive entry %q leaves the directory", name)
	}
	return filepath.Join(dir, local), nil
}

// linked refuses an archive's link or other special file: Go's releases hold
// only folders and files, and a link, which a later entry could be written
// through, could lead out of the directory.
func linked(name string) error {
	return fmt.Errorf("archive entry %q is a link, which Go's releases never hold", name)
}
