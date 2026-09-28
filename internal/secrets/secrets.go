// Package secrets keeps a scenario's secrets and masks them. A value a step
// expands from an ${env:..} reference counts as a secret, such as a password
// or a token: failure messages, logs, attachments and reports show it
// masked, as it is and as it appears in URLs, forms, JSON and HTML. The
// secrets of a scenario are its own, and every pack that expands values
// with Expand or Resolve adds to them; the tokens Resolve expands are
// secrets too.
package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/tokens"
)

// Masked is what a secret is shown as.
const Masked = "********"

type store struct {
	mu     sync.Mutex
	values map[string]bool
}

var key = core.NewStateKey("secrets", func(*core.Scenario) *store { return &store{} }, nil)

// Expand is s with ${env:..} and ${sys:..} expanded, keeping the values of
// its ${env:..} references as the scenario's secrets.
func Expand(sc *core.Scenario, s string) string {
	if vs := In(sc.Suite(), s); len(vs) > 0 {
		Keep(sc, vs...)
	}
	return sc.Suite().Interpolate(s)
}

// tokenRef is a ${token:<name>} reference.
var tokenRef = regexp.MustCompile(`\$\{token:([^}]+)\}`)

// Resolve is Expand, and ${token:<name>} references replaced by the
// value of the scenario's token of that name, which is a secret too. It
// fails when a token cannot be had: none has the name, or its token
// endpoint refuses.
func Resolve(sc *core.Scenario, s string) (string, error) {
	out := Expand(sc, s)
	var err error
	out = tokenRef.ReplaceAllStringFunc(out, func(ref string) string {
		if err != nil {
			return ref
		}
		v, verr := tokens.Value(sc, tokenRef.FindStringSubmatch(ref)[1])
		if verr != nil {
			err = verr
			return ref
		}
		Keep(sc, v)
		return v
	})
	return out, err
}

// In is what the ${env:..} references in s expand to.
func In(suite *core.Suite, s string) []string {
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

// Keep adds values to the scenario's secrets.
func Keep(sc *core.Scenario, values ...string) {
	st := key.Of(sc)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.values == nil {
		st.values = map[string]bool{}
	}
	for _, v := range values {
		if v != "" {
			st.values[v] = true
		}
	}
}

// Replacer masks the scenario's secrets, as they are and as they appear in
// URLs, forms, JSON and HTML. It is nil when the scenario has none.
func Replacer(sc *core.Scenario) *strings.Replacer {
	st, ok := key.Peek(sc)
	if !ok {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var forms []string
	for v := range st.values {
		for _, f := range Encodings(v) {
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
		pairs = append(pairs, f, Masked)
	}
	return strings.NewReplacer(pairs...)
}

// Mask is s with the scenario's secrets masked.
func Mask(sc *core.Scenario, s string) string {
	if r := Replacer(sc); r != nil {
		return r.Replace(s)
	}
	return s
}

// Encodings are the forms a value takes in text: as it is, in a URL's query
// and path, in HTML, and in a JSON string (with and without HTML escaping).
func Encodings(v string) []string {
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

// Hide masks the scenario's secrets in a step's error. An assertion keeps
// its expected and actual values, masked; an error without secrets is
// returned as it is.
func Hide(sc *core.Scenario, err error) error {
	if err == nil {
		return nil
	}
	r := Replacer(sc)
	if r == nil {
		return err
	}
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
