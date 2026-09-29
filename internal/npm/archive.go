package npm

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Unpack unpacks an npm package's tarball into dir, as npm does: its files,
// without the directory they are in (package/, mostly), executable when
// the tarball says so. Links are left out, and an entry that would leave
// dir is refused.
func Unpack(tgz []byte, dir string) error {
	return eachTar(tgz, func(h *tar.Header, r io.Reader) error {
		if h.Typeflag != tar.TypeReg {
			return nil
		}
		_, name, ok := strings.Cut(strings.TrimPrefix(h.Name, "./"), "/")
		if !ok || name == "" {
			return nil
		}
		dest, err := within(dir, name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if h.Mode&0o111 != 0 {
			mode = 0o755
		}
		return write(dest, r, mode)
	})
}

// UnpackFile writes the file name of a .tar.gz archive (its whole name in
// the archive, like package/axe.min.js) at dest, executable.
func UnpackFile(archive []byte, name, dest string) error {
	found := false
	err := eachTar(archive, func(h *tar.Header, r io.Reader) error {
		if h.Name != name || h.Typeflag != tar.TypeReg {
			return nil
		}
		found = true
		return write(dest, r, 0o755)
	})
	if err == nil && !found {
		err = fmt.Errorf("%s is not in the archive", name)
	}
	return err
}

func eachTar(archive []byte, fn func(*tar.Header, io.Reader) error) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// Unzip unpacks a .zip archive into dir: its files, executable when the
// archive says so. Links are left out, and an entry that would leave dir is
// refused.
func Unzip(archive []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue
		}
		dest, err := within(dir, f.Name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = write(dest, rc, mode)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func unzipOne(archive []byte, name, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return write(dest, rc, 0o755)
	}
	return fmt.Errorf("%s is not in the archive", name)
}

// within joins an archive entry to dir, refusing entries that leave it.
func within(dir, name string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(name))
	if !strings.HasPrefix(p, filepath.Clean(dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q leaves the directory", name)
	}
	return p, nil
}

func write(dest string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil { //nolint:gosec // a checksummed archive
		_ = f.Close()
		return err
	}
	return f.Close()
}

// WriteOnce writes a file that another axx may be writing too, like a
// script beside the packages Install installed: aside, then renamed,
// unless it is there already.
func WriteOnce(dest string, content []byte) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "write-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
