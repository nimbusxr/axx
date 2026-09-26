package webcore

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
)

// A value a step expands from an ${env:..} reference counts as a secret, such
// as a password: failure messages and the failure report show it masked, and
// so does a saved trace, in the actions, the page snapshots and the network.

const masked = "********"

// expand is s with ${env:..} and ${sys:..} expanded, remembering the values
// of its ${env:..} references as the scenario's secrets.
func expand(sc *core.Scenario, s string) string {
	if vs := secretsIn(sc.Suite(), s); len(vs) > 0 {
		scenarioPages.Of(sc).keep(vs)
	}
	return sc.Suite().Interpolate(s)
}

// secretsIn is what the ${env:..} references in s expand to.
func secretsIn(suite *core.Suite, s string) []string {
	var out []string
	for i := 0; ; {
		j := strings.Index(s[i:], "${env:")
		if j < 0 {
			return out
		}
		j += i
		end := closingBrace(s, j+1)
		if end < 0 {
			return out
		}
		if j == 0 || s[j-1] != '$' { // $${env:..} is literal text
			ref := s[j : end+1]
			if v := suite.Interpolate(ref); v != "" && v != ref {
				out = append(out, v)
			}
		}
		i = end + 1
	}
}

// closingBrace is the index of the '}' closing the '{' at open.
func closingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (st *pages) keep(values []string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.secrets == nil {
		st.secrets = map[string]bool{}
	}
	for _, v := range values {
		st.secrets[v] = true
	}
}

// masker replaces the scenario's secrets, as they are and as they appear in
// URLs, forms, JSON and HTML. It is nil without secrets. The caller holds
// st.mu.
func (st *pages) masker() *strings.Replacer {
	if len(st.secrets) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var forms []string
	for v := range st.secrets {
		for _, f := range encodings(v) {
			if f != "" && !seen[f] {
				seen[f] = true
				forms = append(forms, f)
			}
		}
	}
	// The longest first, so that a secret that contains another is masked whole.
	sort.Slice(forms, func(i, j int) bool {
		if len(forms[i]) != len(forms[j]) {
			return len(forms[i]) > len(forms[j])
		}
		return forms[i] < forms[j]
	})
	var pairs []string
	for _, f := range forms {
		pairs = append(pairs, f, masked)
	}
	return strings.NewReplacer(pairs...)
}

func encodings(v string) []string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	plain := strings.TrimSuffix(strings.TrimSuffix(b.String(), "\n"), `"`)[1:]
	escaped, _ := json.Marshal(v)
	return []string{
		v, url.QueryEscape(v), url.PathEscape(v), html.EscapeString(v),
		plain, string(escaped[1 : len(escaped)-1]),
	}
}

// hide masks the scenario's secrets in a step's error.
func hide(sc *core.Scenario, err error) error {
	if err == nil {
		return nil
	}
	st := scenarioPages.Of(sc)
	st.mu.Lock()
	r := st.masker()
	st.mu.Unlock()
	if r == nil {
		return err
	}
	// An assertion keeps its expected and actual values, masked.
	if ae := (*core.AssertionError)(nil); errors.As(err, &ae) && ae.Error() == err.Error() {
		c := *ae
		c.Message = r.Replace(c.Message)
		if s, ok := c.Expected.(string); ok {
			c.Expected = r.Replace(s)
		}
		if s, ok := c.Actual.(string); ok {
			c.Actual = r.Replace(s)
		}
		return &c
	}
	msg := r.Replace(err.Error())
	if msg == err.Error() {
		return err
	}
	if core.IsAssertion(err) {
		return core.Failf("%s", msg)
	}
	return errors.New(msg)
}

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
