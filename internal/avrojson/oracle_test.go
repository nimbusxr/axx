package avrojson

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/iskorotkov/avro/v2"
)

// The oracle in testdata/oracle records what Apache Avro 1.12.2 does with
// every case of corpus.json: JsonDecoder + GenericDatumReader, the binary
// encoding GenericDatumWriter produces, and GenericData.toString and
// Object.toString of the datum read back from it. results.json is a frozen
// expectation; never edit it by hand.

type oracleResult struct {
	Schema      string  `json:"schema"`
	JSON        *string `json:"json"`
	ToString    string  `json:"toString"`
	Hex         string  `json:"hex"`
	BinToString string  `json:"binToString"`
	ObjToString string  `json:"objToString"`
	Error       *struct {
		Class   string `json:"class"`
		Message string `json:"message"`
	} `json:"error"`
}

func loadOracle(t *testing.T) (map[string]avro.Schema, []oracleResult) {
	t.Helper()
	var corpus struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	}
	b, err := os.ReadFile("testdata/oracle/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &corpus); err != nil {
		t.Fatal(err)
	}
	schemas := map[string]avro.Schema{}
	for name, raw := range corpus.Schemas {
		s, err := avro.ParseWithCache(string(raw), "", &avro.SchemaCache{})
		if err != nil {
			t.Fatalf("schema %s: %v", name, err)
		}
		schemas[name] = s
	}
	var results []oracleResult
	b, err = os.ReadFile("testdata/oracle/results.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &results); err != nil {
		t.Fatal(err)
	}
	return schemas, results
}

func TestOracleDecode(t *testing.T) {
	schemas, results := loadOracle(t)
	checked := 0
	for _, r := range results {
		if r.JSON == nil {
			continue
		}
		s := schemas[r.Schema]
		v, err := Decode(s, []byte(*r.JSON), Options{})
		if r.Error != nil {
			var de *Error
			if !errors.As(err, &de) {
				t.Errorf("%s %s: Java failed (%s: %s) but Decode returned %v, %v", r.Schema, *r.JSON, r.Error.Class, r.Error.Message, v, err)
			}
			checked++
			continue
		}
		if err != nil {
			t.Errorf("%s %s: Decode: %v (Java: %s)", r.Schema, *r.JSON, err, r.ToString)
			continue
		}
		got, err := Marshal(s, v)
		if err != nil {
			t.Errorf("%s %s: the avro library cannot marshal %#v: %v", r.Schema, *r.JSON, v, err)
			continue
		}
		want, _ := hex.DecodeString(r.Hex)
		if hasMap(s) {
			// Java writes map entries in HashMap order, the avro library in Go map
			// order: compare the decoded contents instead of the bytes.
			a, err1 := DecodeBinary(s, got)
			b, err2 := DecodeBinary(s, want)
			if err1 != nil || err2 != nil || Render(s, unorder(a)) != Render(s, unorder(b)) {
				t.Errorf("%s %s: encoding differs from Java:\n got  %x\n want %s", r.Schema, *r.JSON, got, r.Hex)
			}
		} else if hex.EncodeToString(got) != r.Hex {
			t.Errorf("%s %s: encoding differs from Java:\n got  %x\n want %s", r.Schema, *r.JSON, got, r.Hex)
		}
		if !hasMap(s) {
			if out := Render(s, v); out != r.ToString {
				t.Errorf("%s %s: Render\n got  %s\n want %s", r.Schema, *r.JSON, out, r.ToString)
			}
		}
		checked++
	}
	if checked < 100 {
		t.Fatalf("only %d oracle cases checked", checked)
	}
}

// TestOracleRender renders what Apache Avro wrote in binary (as a consumer reads
// it) and compares with GenericData.toString and Object.toString.
func TestOracleRender(t *testing.T) {
	schemas, results := loadOracle(t)
	for _, r := range results {
		if r.Error != nil || r.Hex == "" {
			continue
		}
		s := schemas[r.Schema]
		data, _ := hex.DecodeString(r.Hex)
		v, err := DecodeBinary(s, data)
		if err != nil {
			t.Errorf("%s %s: DecodeBinary: %v", r.Schema, r.Hex, err)
			continue
		}
		if got := Render(s, v); got != r.BinToString {
			t.Errorf("%s %s: Render\n got  %s\n want %s", r.Schema, r.Hex, got, r.BinToString)
		}
		if got := ObjectString(s, v); got != r.ObjToString {
			t.Errorf("%s %s: ObjectString\n got  %s\n want %s", r.Schema, r.Hex, got, r.ObjToString)
		}
	}
}

func hasMap(s avro.Schema) bool {
	seen := map[string]bool{}
	var walk func(avro.Schema) bool
	walk = func(s avro.Schema) bool {
		s = deref(s)
		if n, ok := s.(avro.NamedSchema); ok {
			if seen[n.FullName()] {
				return false
			}
			seen[n.FullName()] = true
		}
		switch x := s.(type) {
		case *avro.MapSchema:
			return true
		case *avro.ArraySchema:
			return walk(x.Items())
		case *avro.UnionSchema:
			for _, t := range x.Types() {
				if walk(t) {
					return true
				}
			}
		case *avro.RecordSchema:
			for _, f := range x.Fields() {
				if walk(f.Type()) {
					return true
				}
			}
		}
		return false
	}
	return walk(s)
}

// unorder turns OrderedMaps into Go maps, dropping the read order.
func unorder(v any) any {
	switch x := v.(type) {
	case OrderedMap:
		m := make(map[string]any, len(x))
		for _, e := range x {
			m[e.Key] = unorder(e.Value)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = unorder(e)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = unorder(e)
		}
		return out
	}
	return v
}
