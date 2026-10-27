package modelswindows

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// fetchZip downloads a zip of a release axx pins by its SHA-256 and unpacks its files into dir:
// those keep takes, or all of them.
func fetchZip(ctx context.Context, what, url, sum, dir string, keep func(name string) bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot download %s (%s): %w", what, url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot download %s (%s): %s", what, url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(b); hex.EncodeToString(got[:]) != sum {
		return fmt.Errorf("%s's download (%s) is not the release axx pins: its SHA-256 is %x", what, url, got)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || (keep != nil && !keep(f.Name)) {
			continue
		}
		to := filepath.Join(tmp, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(to, tmp+string(filepath.Separator)) {
			return fmt.Errorf("%s's release has a file outside its folder: %s", what, f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o755); err != nil { //nolint:gosec // programs and their libraries
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, other := os.Stat(dir); other != nil {
			return err
		}
	}
	return nil
}
