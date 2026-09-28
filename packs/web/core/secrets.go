package webcore

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// A value a step expands from an ${env:..} reference counts as a secret, such
// as a password: failure messages and the failure report show it masked, and
// so does a saved trace, in the actions, the page snapshots and the network.
// The scenario's secrets are kept by internal/secrets, shared by every pack.

const masked = secrets.Masked

// expand is s with ${env:..} and ${sys:..} expanded, keeping the values of
// its ${env:..} references as the scenario's secrets.
func expand(sc *core.Scenario, s string) string { return secrets.Expand(sc, s) }

// hide masks the scenario's secrets in a step's error.
func hide(sc *core.Scenario, err error) error { return secrets.Hide(sc, err) }

func encodings(v string) []string { return secrets.Encodings(v) }

// scrubTrace masks secrets in the trace at path. Text is masked in every
// text file of the trace; in its event logs, only inside JSON strings, so
// that the trace stays readable.
func scrubTrace(path string, r *strings.Replacer) (err error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	// Closed before the scrubbed copy replaces it: Windows cannot replace
	// an open file.
	closed := false
	defer func() {
		if !closed {
			_ = zr.Close()
		}
	}()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".trace-*.zip")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	zw := zip.NewWriter(tmp)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: f.Method, Modified: f.Modified})
		if err != nil {
			return err
		}
		if _, err := w.Write(scrub(f.Name, b, r)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true
	if err := zr.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func scrub(name string, b []byte, r *strings.Replacer) []byte {
	if !utf8.Valid(b) { // images
		return b
	}
	if !strings.HasSuffix(name, ".trace") && !strings.HasSuffix(name, ".network") {
		return []byte(r.Replace(string(b)))
	}
	lines := bytes.Split(b, []byte("\n"))
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil {
			lines[i] = []byte(r.Replace(string(line)))
			continue
		}
		v, changed := scrubJSON(v, r)
		if !changed {
			continue
		}
		var out bytes.Buffer
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		if enc.Encode(v) != nil {
			lines[i] = []byte(r.Replace(string(line)))
			continue
		}
		lines[i] = bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	}
	return bytes.Join(lines, []byte("\n"))
}

func scrubJSON(v any, r *strings.Replacer) (any, bool) {
	switch t := v.(type) {
	case string:
		s := r.Replace(t)
		return s, s != t
	case []any:
		changed := false
		for i, e := range t {
			var c bool
			if t[i], c = scrubJSON(e, r); c {
				changed = true
			}
		}
		return t, changed
	case map[string]any:
		changed := false
		for k, e := range t {
			var c bool
			if t[k], c = scrubJSON(e, r); c {
				changed = true
			}
		}
		return t, changed
	}
	return v, false
}
