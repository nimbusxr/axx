package mongo

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A step that registers a database says that the scenario has it: the
// driver connects when a step first asks the database something, so this
// needs no MongoDB. The steps that check a selection say nothing.
func TestARegisteredDatabaseSaysItsExchange(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.OK("a tracking-db mongo database with the following properties:",
		[][]string{{"url", "mongodb://127.0.0.1:1/parcels"}, {"user", "parcels"}, {"password", "s3cret-value"}})
	svc, err := Context(h.SC).Service("tracking-db")
	if err != nil {
		t.Fatal(err)
	}
	svc.addSelection(&Selection{Collection: "scans", Docs: []bson.D{{{Key: "status", Value: "IN_TRANSIT"}}}})
	h.OK("the selection has 1 document")
	h.OK("the 1st document for the selection properties are:", [][]string{{"status", "IN_TRANSIT"}})

	got := h.SC.Exchanges()
	if len(got) != 1 {
		t.Fatalf("exchanges: %+v", got)
	}
	if x := got[0]; x.Peer != "tracking-db" || x.Kind != "database" || x.Pack != "mongo" || x.Sent != nil || x.Seen != nil || x.At.IsZero() {
		t.Errorf("the registration: %+v", x)
	}
}

// A seed says what it wrote, and a selection what it asked and the documents
// that came back: the first five, as the steps that check them read them,
// with long texts cut.
func TestSeedsAndSelectionsSayTheirExchanges(t *testing.T) {
	h := cloudtest.New(t, Pack())
	svc := &Service{Name: "tracking-db"}
	svc.seeded(h.SC, nil)
	svc.seeded(h.SC, []written{{"scans", 3}, {"tracking", 1}})
	svc.seeded(h.SC, []written{{"a", 1}, {"b", 1}, {"c", 1}, {"d", 2}})

	price, _ := bson.ParseDecimal128("1.50")
	id, _ := bson.ObjectIDFromHex("65a1b2c3d4e5f60718293a4b")
	var docs []bson.D
	for i := range 7 {
		docs = append(docs, bson.D{
			{Key: "_id", Value: id},
			{Key: "n", Value: int32(i)},
			{Key: "weightKg", Value: 6.0},
			{Key: "price", Value: price},
			{Key: "note", Value: strings.Repeat("é", 200)},
			{Key: "scans", Value: bson.A{bson.D{{Key: "label", Value: bson.Binary{Data: make([]byte, 1000)}}}}},
		})
	}
	where := &core.Table{Rows: [][]string{{"parcelRef", "PX-TRK-3001"}, {"status", "IN_TRANSIT"}, {"projected", "undefined"}}}
	svc.found(h.SC, "scans", where, docs)
	svc.found(h.SC, "scans", where, docs[:1])
	svc.found(h.SC, "scans", where, nil)

	got := h.SC.Exchanges()
	if len(got) != 5 {
		t.Fatalf("exchanges: %+v", got)
	}
	for i, x := range got {
		if x.Peer != "tracking-db" || x.Kind != "database" || x.From != "" || x.Sent == nil {
			t.Errorf("exchange %d: %+v", i, x)
		}
	}
	if a, b := got[0].Sent.Title, got[1].Sent.Title; a != "seed scans: 3 documents, tracking: 1 document" || b != "seed 4 collections: 5 documents" || got[0].Seen != nil {
		t.Errorf("seeds: %q, %q", a, b)
	}
	many := got[2]
	if many.Sent.Title != "find scans where parcelRef = PX-TRK-3001 and status = IN_TRANSIT and projected = undefined" ||
		many.Seen.Title != "7 documents" || many.Seen.MediaType != "application/json" {
		t.Errorf("the selection: %+v, %+v", many.Sent, many.Seen)
	}
	var shown []map[string]any
	if err := json.Unmarshal([]byte(many.Seen.Body), &shown); err != nil || len(shown) != exchangeDocs {
		t.Fatalf("%d documents: %v: %s", len(shown), err, many.Seen.Body)
	}
	if !strings.HasPrefix(many.Seen.Body, `[{"_id":"65a1b2c3d4e5f60718293a4b","n":0,"weightKg":6.0,"price":1.50,"note":"`) {
		t.Errorf("a document should read as the steps that check it read it: %s", many.Seen.Body)
	}
	if note, _ := shown[4]["note"].(string); note != strings.Repeat("é", exchangeText/2)+"…" {
		t.Errorf("a long text: %q", note)
	}
	if !strings.Contains(many.Seen.Body, `"scans":[{"label":"(1000 bytes)"}]`) {
		t.Errorf("a long binary value: %s", many.Seen.Body)
	}
	if len(docs[0][4].Value.(string)) != 400 {
		t.Error("the selection's own documents should stay whole")
	}
	if one, none := got[3].Seen, got[4].Seen; one.Title != "1 document" || none.Title != "0 documents" || none.Body != "" || none.MediaType != "" {
		t.Errorf("one and none: %+v, %+v", one, none)
	}
}
