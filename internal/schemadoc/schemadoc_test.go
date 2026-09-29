package schemadoc

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestFileURLs(t *testing.T) {
	for p, want := range map[string]string{
		"/srv/parcels/openrpc.json":    "file:///srv/parcels/openrpc.json",
		"C:/work/parcels/openrpc.json": "file:///C:/work/parcels/openrpc.json",
	} {
		if got := FileURL(p); got != want {
			t.Errorf("FileURL(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestSchemasInADocument(t *testing.T) {
	p, err := filepath.Abs("testdata/methods.yaml")
	if err != nil {
		t.Fatal(err)
	}
	l := NewLoader()
	root, err := l.Root(FileURL(p))
	if err != nil {
		t.Fatal(err)
	}
	schemas := root.Get("components").Get("schemas")
	// A $ref to another file, relative to the document.
	parcel, err := l.Deref(schemas.Get("Parcel"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(parcel.URL, "/testdata/schemas/parcel.json") || parcel.Str("type") != "object" {
		t.Fatalf("Parcel: %+v", parcel)
	}
	c := l.Compiler(jsonschema.Draft7)
	for _, tc := range []struct {
		name, ref, value string
		keys             []string
		messages         []string
	}{
		{"valid", schemas.Get("Hold").Ref(), `{"reference": "PX-RPC-7101", "until": "2026-10-01"}`, nil, nil},
		{
			"a $ref in the document", schemas.Get("Hold").Ref(), `{"reference": "7101", "until": "2026-10-01"}`,
			[]string{"validation.params.schema.pattern"},
			[]string{"hold $.reference: "},
		},
		{
			"a required property", schemas.Get("Hold").Ref(), `{"reference": "PX-RPC-7102"}`,
			[]string{"validation.params.schema.required"},
			[]string{"hold $: missing property 'until'"},
		},
		{
			"a schema in another file", parcel.Ref(), `{"reference": "PX-RPC-7103", "weightGrams": 31000}`,
			[]string{"validation.params.schema.maximum"},
			[]string{"hold $.weightGrams: "},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := c.Compile(tc.ref)
			if err != nil {
				t.Fatal(err)
			}
			v, err := Decode([]byte(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			found := Validate(s, v, "validation.params.schema", "hold")
			var keys, msgs []string
			for _, f := range found {
				keys, msgs = append(keys, f.Key), append(msgs, f.Message)
			}
			if strings.Join(keys, ",") != strings.Join(tc.keys, ",") {
				t.Fatalf("keys %v, want %v: %v", keys, tc.keys, msgs)
			}
			for i, m := range tc.messages {
				if !strings.HasPrefix(msgs[i], m) {
					t.Errorf("message %q, want it to start with %q", msgs[i], m)
				}
			}
		})
	}
}

func TestLoadingFails(t *testing.T) {
	l := NewLoader()
	if _, err := l.Load(FileURL(filepath.Join(t.TempDir(), "missing.yaml"))); err == nil {
		t.Fatal("a missing file loaded")
	}
	if _, err := l.Load("ftp://parcels.example/openrpc.json"); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Fatalf("err = %v", err)
	}
}
