package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Files are the files of a place a scenario reads and writes them: a folder
// on this machine, or an app's files, wherever the app runs (ADR 0012). A
// file is named by its path in the place, with forward slashes.
type Files interface {
	// Read returns a file's content; ok is false when there is none.
	Read(ctx context.Context, name string) (body []byte, ok bool, err error)
	// Write writes a file, making the folders it is in.
	Write(ctx context.Context, name string, body []byte) error
	// List returns the paths of up to max files, in subfolders too, leaving
	// out hidden ones (.DS_Store, .gitkeep).
	List(ctx context.Context, max int) ([]string, error)
	// Where says where the files are, for logs and failures.
	Where() string
}

// FileOwner gives the files of an owner of one kind, by its name and a path
// in its file context: "./" is where the owner keeps its data, "~/" its home.
// The app-core pack handles owners of the kind "app".
type FileOwner func(sc *Scenario, name, path string) (Files, error)

// SetFileOwner makes fn give the files of the owners of a kind, like "app"
// (owner "app:depot"). A pack sets it as it starts.
func (s *Suite) SetFileOwner(kind string, fn FileOwner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners == nil {
		s.owners = map[string]FileOwner{}
	}
	s.owners[kind] = fn
}

// FileOwner is what gives the files of the owners of a kind; ok is false when
// no pack of the run handles the kind.
func (s *Suite) FileOwner(kind string) (FileOwner, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn, ok := s.owners[kind]
	return fn, ok
}

// LocalFiles are the files of a folder on this machine. The folder may appear
// only when something first writes to it.
func LocalFiles(dir string) Files { return localFiles{dir: filepath.Clean(dir)} }

type localFiles struct{ dir string }

func (l localFiles) Where() string { return l.dir }

// path is the file's path on this machine, refusing a name that leaves the
// folder.
func (l localFiles) path(name string) (string, error) {
	p := filepath.Join(l.dir, filepath.FromSlash(name))
	if rel, err := filepath.Rel(l.dir, p); err != nil || filepath.IsAbs(filepath.FromSlash(name)) ||
		rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not a file in %s: name a file by its path in the folder", name, l.dir)
	}
	return p, nil
}

func (l localFiles) Read(_ context.Context, name string) ([]byte, bool, error) {
	p, err := l.path(name)
	if err != nil {
		return nil, false, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.Mode().IsRegular() {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func (l localFiles) Write(_ context.Context, name string, body []byte) error {
	p, err := l.path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, 0o644)
}

func (l localFiles) List(_ context.Context, max int) ([]string, error) {
	var names []string
	errDone := errors.New("done")
	err := filepath.WalkDir(l.dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			if p == l.dir && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll // not there yet: no files
			}
			return err
		case p == l.dir:
			return nil
		case strings.HasPrefix(d.Name(), "."):
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case !d.Type().IsRegular():
			return nil
		case len(names) == max:
			return errDone
		}
		rel, err := filepath.Rel(l.dir, p)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !errors.Is(err, errDone) {
		return nil, err
	}
	return names, nil
}
