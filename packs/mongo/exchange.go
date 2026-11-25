package mongo

import (
	"bytes"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
)

// What the steps say they sent to a database and saw of it (core.Exchange),
// for a pack that shows the run: a seed's collections, and a selection's
// conditions with the documents that came back.
const (
	// exchangeDocs is how many of a selection's documents an exchange has: as
	// many as the failure description.
	exchangeDocs = 5
	// exchangeText is where a long text in a document is cut.
	exchangeText = 256
	// exchangeCollections is how many of a seed's collections its title names.
	exchangeCollections = 3
)

// written is the documents a seed wrote to one collection.
type written struct {
	collection string
	docs       int
}

// seeded says that a seed's documents went to the database: each collection
// and how many documents it got, or how many collections and documents when
// there are many. A seed that wrote nothing says nothing.
func (svc *Service) seeded(sc *core.Scenario, wrote []written) {
	if len(wrote) == 0 {
		return
	}
	var parts []string
	docs := 0
	for _, w := range wrote {
		docs += w.docs
		parts = append(parts, w.collection+": "+plural(w.docs, "document"))
	}
	title := "seed " + strings.Join(parts, ", ")
	if len(parts) > exchangeCollections {
		title = fmt.Sprintf("seed %d collections: %s", len(parts), plural(docs, "document"))
	}
	sc.Exchanged(core.Exchange{Peer: svc.Name, Kind: "database", Sent: &core.Message{Title: title}})
}

// found says a selection: what the step asked of the collection, as its own
// table of conditions says it, and the documents that came back, the first
// of them as JSON.
func (svc *Service) found(sc *core.Scenario, collection string, t *core.Table, docs []bson.D) {
	sent := "find " + collection
	if where := asked(t); where != "" {
		sent += " where " + where
	}
	seen := &core.Message{Title: plural(len(docs), "document")}
	if len(docs) > 0 {
		seen.MediaType, seen.Body = core.MimeJSON, docsJSON(docs)
	}
	sc.Exchanged(core.Exchange{Peer: svc.Name, Kind: "database", Sent: &core.Message{Title: sent}, Seen: seen})
}

// asked is a step's table of conditions in a line, as the step wrote
// them, before any ${env:..} in them is expanded: "parcelRef = PX-TRK-3001
// and status = IN_TRANSIT".
func asked(t *core.Table) string {
	if t == nil {
		return ""
	}
	pairs, err := t.Pairs()
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, p.Key+" = "+p.Value)
	}
	return strings.Join(parts, " and ")
}

// docsJSON is the first documents of a selection as a JSON array, each as
// the steps that check it read it (writeJSON), with its long texts cut.
func docsJSON(docs []bson.D) string {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, d := range docs[:min(len(docs), exchangeDocs)] {
		if i > 0 {
			buf.WriteByte(',')
		}
		writeJSON(&buf, short(d))
	}
	buf.WriteByte(']')
	return buf.String()
}

// short is a document's value with its long texts cut, and its long binary
// values as their sizes.
func short(v any) any {
	switch x := v.(type) {
	case bson.D:
		out := make(bson.D, len(x))
		for i, e := range x {
			out[i] = bson.E{Key: e.Key, Value: short(e.Value)}
		}
		return out
	case bson.A:
		out := make(bson.A, len(x))
		for i, e := range x {
			out[i] = short(e)
		}
		return out
	case string:
		if len(x) > exchangeText {
			return strings.ToValidUTF8(x[:exchangeText], "") + "…"
		}
	case bson.Binary:
		if len(x.Data) > exchangeText {
			return fmt.Sprintf("(%d bytes)", len(x.Data))
		}
	}
	return v
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}
