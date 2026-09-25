package mongo

import (
	"bytes"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestParseSeedKeepsOrderAndTypes(t *testing.T) {
	d, err := parseSeed([]byte(`{
	  "telemetry": [{"_id": {"$oid": "65a1b2c3d4e5f60718293a4b"}, "mission": "m1", "speed": 7.5, "count": 3, "big": 3000000000, "ok": true, "tags": ["a"], "nested": {"x": null}}],
	  "empty": [],
	  "another": [{"a": 1}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 3 || d[0].Key != "telemetry" || d[1].Key != "empty" || d[2].Key != "another" {
		t.Fatalf("collection order: %v", d)
	}
	doc := d[0].Value.(bson.A)[0].(bson.D)
	want := map[string]any{"mission": "m1", "speed": 7.5, "count": int32(3), "big": int64(3000000000), "ok": true}
	got := map[string]any{}
	for _, e := range doc {
		got[e.Key] = e.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v (%T), want %#v (%T)", k, got[k], got[k], v, v)
		}
	}
	if _, ok := got["_id"].(bson.ObjectID); !ok {
		t.Errorf("extended JSON $oid should become an ObjectID, got %T", got["_id"])
	}
}

func TestParseSeedErrors(t *testing.T) {
	if _, err := parseSeed([]byte(`[1,2]`)); err == nil {
		t.Error("top-level array must be rejected")
	}
	if _, err := parseSeed([]byte(`{"a": `)); err == nil {
		t.Error("truncated JSON must be rejected")
	}
}

func TestTypedValue(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("65a1b2c3d4e5f60718293a4b")
	cases := []struct {
		in   string
		want any
	}{
		{"m1", "m1"},
		{"3", int32(3)},
		{"7.5", 7.5},
		{"true", true},
		{"null", nil},
		{`"3"`, "3"},
		{`{"$oid": "65a1b2c3d4e5f60718293a4b"}`, oid},
		{"not json {", "not json {"},
	}
	for _, c := range cases {
		if got := typedValue(c.in); got != c.want {
			t.Errorf("%s: %#v (%T), want %#v", c.in, got, got, c.want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("65a1b2c3d4e5f60718293a4b")
	doc := bson.D{
		{Key: "_id", Value: oid},
		{Key: "z", Value: int64(1)},
		{Key: "a", Value: bson.A{int32(2), 2.5, nil, "x"}},
		{Key: "at", Value: bson.NewDateTimeFromTime(time.Date(2026, 9, 24, 10, 0, 0, 5e6, time.UTC))},
		{Key: "nested", Value: bson.D{{Key: "ok", Value: true}}},
	}
	var buf bytes.Buffer
	writeJSON(&buf, doc)
	want := `{"_id":"65a1b2c3d4e5f60718293a4b","z":1,"a":[2,2.5,null,"x"],"at":"2026-09-24T10:00:00.005Z","nested":{"ok":true}}`
	if buf.String() != want {
		t.Fatalf("got  %s\nwant %s", buf.String(), want)
	}
}
