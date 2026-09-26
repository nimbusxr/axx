package webcore

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/interp"
)

const password = `Fjord & "north" 2026/ü`

func secretSuite() *core.Suite {
	env := interp.MapLookup(map[string]string{"SHOP_PASSWORD": password, "SHOP_EMAIL": "orders@fjord-outdoor.example", "EMPTY": ""})
	r := &interp.Resolver{Lookups: map[string]interp.Lookup{"env": env}}
	return core.NewSuite(core.SuiteOptions{Interpolate: r.MustExpand})
}

func TestSecretsAreWhatEnvReferencesExpandTo(t *testing.T) {
	s := secretSuite()
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"${env:SHOP_PASSWORD}", []string{password}},
		{"user ${env:SHOP_EMAIL}, password ${env:SHOP_PASSWORD}", []string{"orders@fjord-outdoor.example", password}},
		{"${env:MISSING:-${env:SHOP_PASSWORD}}", []string{password}},
		{"${env:MISSING:-local-dev}", []string{"local-dev"}},
		{"$${env:SHOP_PASSWORD}", nil},
		{"${env:MISSING}", nil},
		{"${env:EMPTY}", nil},
		{"${sys:portal.url}", nil},
		{"Fjord Outdoor", nil},
		{"${env:SHOP_PASSWORD", nil},
	} {
		if got := secretsIn(s, c.in); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSecretsAreMaskedInFailures(t *testing.T) {
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "sign in"}, secretSuite(), nil)
	if v := expand(sc, "${env:SHOP_PASSWORD}"); v != password {
		t.Fatalf("expanded to %q", v)
	}
	if err := hide(sc, core.Fail(`The page does not show "`+password+`"`, password, "Log in to Your Account")); err.Error() !=
		"The page does not show \"********\"\n  expected: \"********\"\n  actual:   \"Log in to Your Account\"" {
		t.Errorf("assertion: %v", err)
	}
	err := hide(sc, fmt.Errorf("wrapped: %w", core.Fail("no "+password, nil, nil)))
	if strings.Contains(err.Error(), password) || !core.IsAssertion(err) {
		t.Errorf("wrapped assertion: %v", err)
	}
	if err := hide(sc, io.EOF); !errors.Is(err, io.EOF) {
		t.Errorf("an error without secrets changed: %v", err)
	}
	if err := hide(sc, os.ErrNotExist); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an error without secrets changed: %v", err)
	}
}

func TestMaskingCoversEncodings(t *testing.T) {
	st := &pages{}
	st.keep([]string{password})
	r := st.masker()
	for _, s := range []string{
		password,
		"login=orders%40fjord-outdoor.example&password=Fjord+%26+%22north%22+2026%2F%C3%BC",
		"/reset/Fjord%20&%20%22north%22%202026%2F%C3%BC",
		`<input value="Fjord &amp; &#34;north&#34; 2026/ü">`,
		`{"value":"Fjord & \"north\" 2026/ü"}`,
		`{"value":"Fjord \u0026 \"north\" 2026/ü"}`,
	} {
		if got := r.Replace(s); !strings.Contains(got, masked) {
			t.Errorf("not masked: %s", got)
		}
	}
	if (&pages{}).masker() != nil {
		t.Error("a masker without secrets")
	}
}

// A trace's event logs keep their shape; every text in it is masked, and
// images are left alone.
func TestTracesAreScrubbed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.zip")
	image := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	image = append(image, password...)
	files := map[string][]byte{
		"trace.trace": []byte(`{"type":"before","callId":"call@7","startTime":1727284003840.123,"method":"fill","params":{"selector":"internal:label=\"Password\"","value":"Fjord & \"north\" 2026/ü"}}
{"type":"frame-snapshot","snapshot":{"html":["INPUT",{"__playwright_value_":"Fjord & \"north\" 2026/ü","type":"password"}]}}
{"type":"after","callId":"call@7","endTime":1727284003912}
`),
		"trace.network":        []byte(`{"type":"resource-snapshot","snapshot":{"request":{"method":"POST","postData":{"text":"login=orders%40fjord-outdoor.example&password=Fjord+%26+%22north%22+2026%2F%C3%BC"}}}}` + "\n"),
		"resources/a1b2.html":  []byte(`<p>Hello, Fjord &amp; &#34;north&#34; 2026/ü</p>`),
		"resources/c3d4.jpeg":  image,
		"resources/src@1.json": []byte(`not json: Fjord & "north" 2026/ü`),
	}
	writeZip(t, path, files)

	st := &pages{}
	st.keep([]string{password})
	if err := scrubTrace(path, st.masker()); err != nil {
		t.Fatal(err)
	}
	got := readZip(t, path)
	if len(got) != len(files) {
		t.Fatalf("entries: %d", len(got))
	}
	for name, b := range got {
		if name == "resources/c3d4.jpeg" {
			if !bytes.Equal(b, image) {
				t.Errorf("the image changed")
			}
			continue
		}
		for _, f := range encodings(password) {
			if bytes.Contains(b, []byte(f)) {
				t.Errorf("%s still holds %q:\n%s", name, f, b)
			}
		}
		if !bytes.Contains(b, []byte(masked)) {
			t.Errorf("%s is not masked:\n%s", name, b)
		}
	}
	for _, name := range []string{"trace.trace", "trace.network"} {
		for line := range strings.SplitSeq(strings.TrimSpace(string(got[name])), "\n") {
			var v any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Errorf("%s: %v: %s", name, err, line)
			}
		}
	}
	if !strings.Contains(string(got["trace.trace"]), `"startTime":1727284003840.123`) ||
		!strings.Contains(string(got["trace.trace"]), `{"type":"after","callId":"call@7","endTime":1727284003912}`) {
		t.Errorf("the events changed beyond their secrets:\n%s", got["trace.trace"])
	}
}

func writeZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readZip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(rc)
		_ = rc.Close()
	}
	return out
}
