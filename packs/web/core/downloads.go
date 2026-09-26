package webcore

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/filecontent"
)

// downloaded waits for the browser to download a file with that name, and
// saves it in .axx/web/downloads.
func (s *session) downloaded(sc *core.Scenario, name string, wait time.Duration) (*download, error) {
	var found *download
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, d := range s.downloads {
			if d.name == name {
				found = d
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		s.mu.Lock()
		var names []string
		for _, d := range s.downloads {
			names = append(names, fmt.Sprintf("%q", d.name))
		}
		s.mu.Unlock()
		return nil, core.Failf("The browser downloaded no %q file; %s", name, cloudstep.Shown("files downloaded", names, 20))
	}
	s.mu.Lock()
	path := found.path
	s.mu.Unlock()
	if path != "" {
		return found, nil
	}
	path = downloadPath(sc, s.app, name)
	if err := found.d.SaveAs(path); err != nil {
		if f := found.d.Failure(); f != nil {
			return nil, core.Failf("The download of the %q file failed: %s", name, firstLine(f))
		}
		return nil, fmt.Errorf("cannot save the downloaded %q file: %s", name, firstLine(err))
	}
	s.mu.Lock()
	found.path = path
	s.mu.Unlock()
	return found, nil
}

// downloadPath is where a scenario keeps a file its browser downloaded.
func downloadPath(sc *core.Scenario, a *App, name string) string {
	base := filepath.Base(filepath.Clean("/" + strings.ReplaceAll(name, `\`, "/")))
	if base == "/" || base == "." {
		base = "download"
	}
	dir := strings.TrimSuffix(artifactPath(sc, "downloads", a, ""), string(filepath.Separator))
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, base)
}

func (s *session) downloadedBody(sc *core.Scenario, name string) ([]byte, error) {
	d, err := s.downloaded(sc, name, actionTimeout)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(d.path)
}

// downloadedFile is a downloaded file, to read by its type.
func (s *session) downloadedFile(sc *core.Scenario, name string) (filecontent.File, error) {
	body, err := s.downloadedBody(sc, name)
	return filecontent.File{Name: name, Body: body}, err
}

func (s *session) identical(sc *core.Scenario, name, want string) error {
	body, err := s.downloadedBody(sc, name)
	if err != nil {
		return err
	}
	path, err := sc.Suite().ResolvePath(want)
	if err != nil {
		return err
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Equal(body, expected) {
		return nil
	}
	return core.Fail(fmt.Sprintf("The downloaded %q file differs from the %s file", name, want), describeFile(expected), describeFile(body))
}

func describeFile(b []byte) string {
	return fmt.Sprintf("%d bytes, sha256 %x", len(b), sha256.Sum256(b))
}
