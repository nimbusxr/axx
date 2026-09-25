package jsonx_test

import (
	"errors"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

const sample = `{"name":"Pat","age":42,"ratio":1.5,"tags":["a","b"],"address":{"city":"Springfield"},` +
	`"items":[{"sku":"A","qty":1},{"sku":"B","qty":3}],"none":null}`

func mustParse(t *testing.T, s string) any {
	t.Helper()
	v, err := jsonx.Parse(s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return v
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"$", "$"},
		{"$.name", "$.name"},
		{"@.name", "@.name"},
		{"name", "$.name"},
		{"address.city", "$.address.city"},
		{"[0].name", "$[0].name"},
		{"['a b']", "$['a b']"},
		{".name", "$..name"},
		{" $.name", "$. $.name"},
	}
	for _, tt := range tests {
		if got := jsonx.Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParentAndKey(t *testing.T) {
	tests := []struct{ path, parent, key string }{
		{"name", "$", "name"},
		{"$.name", "$", "name"},
		{"address.city", "$.address", "city"},
		{"$.a.b.c", "$.a.b", "c"},
		{"a[0]", "$", "a[0]"},
		{"$['a.b'].c", "$.$['a.b']", "c"},
		{"$['x.y']", "$.$['x", "y']"},
	}
	for _, tt := range tests {
		parent, key := jsonx.ParentAndKey(tt.path)
		if parent != tt.parent || key != tt.key {
			t.Errorf("ParentAndKey(%q) = (%q, %q), want (%q, %q)", tt.path, parent, key, tt.parent, tt.key)
		}
	}
}

func TestRead(t *testing.T) {
	doc := mustParse(t, sample)
	tests := []struct {
		path     string
		want     string
		definite bool
		err      error
	}{
		{"name", `"Pat"`, true, nil},
		{"$.age", "I42", true, nil},
		{"$.ratio", "D1.5", true, nil},
		{"address.city", `"Springfield"`, true, nil},
		{"[0]", "", true, jsonx.ErrPathNotFound},
		{"$.items[1].sku", `"B"`, true, nil},
		{"$.items[-1].qty", "I3", true, nil},
		{"$.items[*].sku", `["A","B"]`, false, nil},
		{"$.items[?(@.qty > 1)].sku", `["B"]`, false, nil},
		{"$..city", `["Springfield"]`, false, nil},
		{"$.tags.length()", "I2", true, nil},
		{"$.missing", "", true, jsonx.ErrPathNotFound},
		{"$.missing.deeper", "", true, jsonx.ErrPathNotFound},
		{"$..missing", "[]", false, nil},
		{"$.none", "null", true, nil},
		{"$.[", "", false, jsonx.ErrInvalidPath},
	}
	for _, tt := range tests {
		v, definite, err := jsonx.Read(doc, tt.path)
		if tt.err != nil {
			if !errors.Is(err, tt.err) {
				t.Errorf("Read(%q): err = %v, want %v", tt.path, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Read(%q): %v", tt.path, err)
			continue
		}
		if got := oracletest.Repr(v); got != tt.want || definite != tt.definite {
			t.Errorf("Read(%q) = %s (definite %v), want %s (definite %v)", tt.path, got, definite, tt.want, tt.definite)
		}
	}
}

func TestErrorKinds(t *testing.T) {
	if !jsonx.PathNotFound.Is(jsonx.InvalidPath) || !jsonx.PathNotFound.Is(jsonx.JsonPathError) {
		t.Error("PathNotFound should be an InvalidPath and a JsonPathError")
	}
	if jsonx.InvalidPath.Is(jsonx.PathNotFound) {
		t.Error("InvalidPath is not a PathNotFound")
	}
	_, _, err := jsonx.Read(mustParse(t, sample), "$.missing")
	if !errors.Is(err, jsonx.ErrInvalidPath) || !jsonx.IsJsonPathException(err) {
		t.Errorf("errors.Is should follow the Java hierarchy: %v", err)
	}
	var e *jsonx.Error
	if !errors.As(err, &e) || e.Message != "No results for path: $['missing']" {
		t.Errorf("unexpected error %v", err)
	}
}

func TestReadPaths(t *testing.T) {
	doc := mustParse(t, sample)
	tests := []struct {
		path string
		want []string
	}{
		{"$..sku", []string{"$['items'][0]['sku']", "$['items'][1]['sku']"}},
		{"items[?(@.qty > 1)].sku", []string{"$['items'][1]['sku']"}},
		{"$.tags[*]", []string{"$['tags'][0]", "$['tags'][1]"}},
		{"address.city", []string{"$['address']['city']"}},
		{"$.missing", nil},
		{"$.items[*].missing", nil},
	}
	for _, tt := range tests {
		paths, values, err := jsonx.ReadPaths(doc, tt.path)
		if err != nil {
			t.Errorf("%s: %v", tt.path, err)
			continue
		}
		if len(paths) != len(values) || len(paths) != len(tt.want) {
			t.Errorf("%s: paths %q values %v, want %q", tt.path, paths, values, tt.want)
			continue
		}
		for i := range paths {
			if paths[i] != tt.want[i] {
				t.Errorf("%s: path %d = %q, want %q", tt.path, i, paths[i], tt.want[i])
			}
		}
	}
	if _, _, err := jsonx.ReadPaths(doc, "$.items[?(@.qty >"); err == nil {
		t.Error("an invalid path must be an error")
	}
}

func TestMutations(t *testing.T) {
	tests := []struct {
		name string
		op   func(doc any) error
		want string
		err  error
	}{
		{"set existing", func(d any) error { return jsonx.Set(d, "$.name", "Lee") }, `{"name":"Lee","age":1}`, nil},
		{"set missing leaf", func(d any) error { return jsonx.Set(d, "$.nope", 1) }, "", jsonx.ErrPathNotFound},
		{"set root", func(d any) error { return jsonx.Set(d, "$", 1) }, "", jsonx.ErrInvalidModification},
		{"put new", func(d any) error { return jsonx.Put(d, "$", "city", "X") }, `{"name":"Pat","age":1,"city":"X"}`, nil},
		{"put keeps position", func(d any) error { return jsonx.Put(d, "$", "name", "Q") }, `{"name":"Q","age":1}`, nil},
		{"delete", func(d any) error { return jsonx.Delete(d, "age") }, `{"name":"Pat"}`, nil},
		{"set number kinds", func(d any) error { return jsonx.Set(d, "$.age", jsonx.DoubleNumber(1e10)) }, `{"name":"Pat","age":1.0E10}`, nil},
	}
	for _, tt := range tests {
		doc := mustParse(t, `{"name":"Pat","age":1}`)
		err := tt.op(doc)
		if tt.err != nil {
			if !errors.Is(err, tt.err) {
				t.Errorf("%s: err = %v, want %v", tt.name, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		got, err := jsonx.Marshal(doc)
		if err != nil || got != tt.want {
			t.Errorf("%s: %s (%v), want %s", tt.name, got, err, tt.want)
		}
	}
}

func TestNumbers(t *testing.T) {
	tests := []struct {
		text string
		kind jsonx.NumberKind
		str  string
	}{
		{"5", jsonx.Integer, "5"},
		{"-0", jsonx.Integer, "0"},
		{"2147483648", jsonx.Long, "2147483648"},
		{"99999999999999999999", jsonx.BigInteger, "99999999999999999999"},
		{"1e10", jsonx.Double, "1.0E10"},
		{"0.5", jsonx.Double, "0.5"},
		{"0.12345678901234567890", jsonx.BigDecimal, "0.12345678901234567890"},
	}
	for _, tt := range tests {
		v := mustParse(t, "["+tt.text+"]").(*jsonx.Array).Get(0)
		n, ok := v.(jsonx.Number)
		if !ok || n.Kind() != tt.kind || n.String() != tt.str || n.Text() != tt.text {
			t.Errorf("%s: got %#v", tt.text, v)
		}
	}
	if jsonx.JavaEquals(jsonx.IntegerNumber(5), jsonx.DoubleNumber(5)) {
		t.Error("Integer 5 must not equal Double 5.0")
	}
	if !jsonx.JavaEquals(mustParse(t, `{"a":1,"b":[2]}`), mustParse(t, `{"b":[2],"a":1}`)) {
		t.Error("maps compare regardless of order")
	}
}

func TestHasJSONPath(t *testing.T) {
	isPat := func(v any) bool { return v == "Pat" }
	for _, tt := range []struct {
		body, path string
		want       bool
		wantErr    bool
	}{
		{sample, "name", true, false},
		{sample, "age", false, false},
		{sample, "missing", false, false},
		{"not json", "$.a", false, false},
		{"", "$.a", false, true},
		{"null", "$.a", false, true},
		{sample, "$.[", false, true},
	} {
		got, err := jsonx.HasJSONPath(tt.body, tt.path, isPat)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("HasJSONPath(%q, %q) = %v, %v", tt.body, tt.path, got, err)
		}
	}
	if ok, err := jsonx.HasNoJSONPath(sample, "$..missing"); ok || err != nil {
		t.Errorf("an indefinite path is never absent: %v %v", ok, err)
	}
	if ok, err := jsonx.HasNoJSONPath(sample, "$.missing"); !ok || err != nil {
		t.Errorf("a missing definite path is absent: %v %v", ok, err)
	}
}

// FuzzNormalize checks that Normalize is idempotent, that a path and its
// normalized form read the same values and the same kind of error, and that
// no path makes the engine panic.
func FuzzNormalize(f *testing.F) {
	for _, s := range []string{
		"name", "[0].name", "$.a.b", "a[*].b", "..a", "$..[?(@.x > 1)]", "@.a", "['a b'].c", "a.length()",
		"[?(@ =~ /a.*/i)]", "$[1:3]", "", " ", "$['a','b']", "a[-1]", "a.b.", "[", "(", "$[?(@.a in [1,2])]",
	} {
		f.Add(s)
	}
	doc := `{"a":[{"b":1,"x":2},{"b":"two"}],"name":"n","a b":{"c":3},"0":{"name":"zero"}}`
	f.Fuzz(func(t *testing.T, path string) {
		n := jsonx.Normalize(path)
		if again := jsonx.Normalize(n); again != n {
			t.Fatalf("Normalize not idempotent: %q -> %q -> %q", path, n, again)
		}
		d1, err := jsonx.Parse(doc)
		if err != nil {
			t.Fatal(err)
		}
		d2, _ := jsonx.Parse(doc)
		v1, def1, err1 := jsonx.Read(d1, path)
		v2, def2, err2 := jsonx.Read(d2, n)
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("%q vs %q: errors differ: %v / %v", path, n, err1, err2)
		}
		if err1 != nil {
			var e1, e2 *jsonx.Error
			if !errors.As(err1, &e1) || !errors.As(err2, &e2) || e1.Kind != e2.Kind {
				t.Fatalf("%q vs %q: error kinds differ: %v / %v", path, n, err1, err2)
			}
			return
		}
		if def1 != def2 || oracletest.Repr(v1) != oracletest.Repr(v2) {
			t.Fatalf("%q vs %q: %s (%v) / %s (%v)", path, n, oracletest.Repr(v1), def1, oracletest.Repr(v2), def2)
		}
	})
}
