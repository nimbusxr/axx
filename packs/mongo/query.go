package mongo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

const filterDoc = "Each row is a `field | value` condition (dotted field paths reach into nested documents). " +
	"Values are read as JSON when they parse as JSON (`3`, `true`, `null`, `\"3\"`, `{\"$oid\": \"...\"}`) and as plain strings otherwise."

const docDoc = "Documents are compared as JSON: ObjectIds become their hex string, dates become ISO-8601 UTC strings, " +
	"and every scalar is compared as text. `null` means null and `undefined` means the field is absent."

// Selection is the result of a find: documents in natural (or _id) order.
type Selection struct {
	Collection string
	Filter     bson.D
	Docs       []bson.D
}

func queryParams() []core.ParamType {
	return []core.ParamType{{
		Name: "mongoService", Regexps: []string{`([^\s]+)`},
		Doc: "The name of a MongoDB database registered in the scenario.",
		Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
			return stateKey.Of(sc).services.Get(name)
		},
	}}
}

func querySteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mongo.find", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "a[[ {ordinal}]] selection of documents is retrieved from the {word} collection[[ on {mongoService}]] where:",
			Doc: "Find documents and keep them as the next selection for later assertions, like the SQL selection steps. " +
				"Selections are numbered in the order they are retrieved; `the selection` means the first. " + filterDoc,
			Examples: []string{"Then a selection of documents is retrieved from the scans collection where:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return find(sc, a, 2, a.String(1), a.Table, 0, 0)
			},
		},
		{
			ID: "mongo.find.poll", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "within {duration} a[[ {ordinal}]] selection of at least {int} document(s) is retrieved from the {word} collection[[ on {mongoService}]] where:",
			Doc: "Poll every 500ms until the find returns at least the given number of documents or the time is up. " +
				"On timeout the last result (possibly empty) is kept, so assert on it with a document-count step. " + filterDoc,
			Examples: []string{"Then within 10s a selection of at least 1 document is retrieved from the tracking collection where:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return find(sc, a, 4, a.String(3), a.Table, a.Value(0).(time.Duration), a.Int(2))
			},
		},
		docCount("mongo.docs.eq", "has {int} document(s)", "exactly", func(got, want int) bool { return got == want }),
		docCount("mongo.docs.gt", "has more than {int} document(s)", "more than", func(got, want int) bool { return got > want }),
		docCount("mongo.docs.lt", "has fewer than {int} document(s)", "fewer than", func(got, want int) bool { return got < want }),
		{
			ID: "mongo.doc.are", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr:     "the {ordinal} document for the[[ {ordinal}]] selection[[ on {mongoService}]] properties are:",
			Doc:      "Assert properties (JSONPath, e.g. `lastLocation` or `scans[0].status`) of one document of a selection. " + docDoc,
			Examples: []string{"Then the 1st document for the selection properties are:"},
			Run:      func(sc *core.Scenario, a core.Args) error { return docProperties(sc, a, false) },
		},
		{
			ID: "mongo.doc.match", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr:     "the {ordinal} document for the[[ {ordinal}]] selection[[ on {mongoService}]] properties match:",
			Doc:      "Like the properties step, but every value is a regular expression (Java syntax) that must match the whole text.",
			Examples: []string{"Then the 1st document for the 2nd selection on tracking-db properties match:"},
			Run:      func(sc *core.Scenario, a core.Args) error { return docProperties(sc, a, true) },
		},
	}
}

func docCount(id, tail, words string, ok func(got, want int) bool) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then", Since: "0.1.0",
		Expr:     "the[[ {ordinal}]] selection[[ on {mongoService}]] " + tail,
		Doc:      fmt.Sprintf("Assert that a selection of documents has %s the given number of documents.", words),
		Examples: []string{"Then the selection " + strings.ReplaceAll(tail, "{int} document(s)", "2 documents")},
		Run: func(sc *core.Scenario, a core.Args) error {
			svc, err := service(sc, a, 1)
			if err != nil {
				return err
			}
			sel, err := svc.selection(a.IntOr(0, 1) - 1)
			if err != nil {
				return err
			}
			if want := a.Int(2); !ok(len(sel.Docs), want) {
				return core.Fail(fmt.Sprintf("expected the selection from %s to have %s %d document(s)", sel.Collection, words, want), want, len(sel.Docs))
			}
			return nil
		},
	}
}

// service returns the service from argument i, or the default one.
func service(sc *core.Scenario, a core.Args, i int) (*Service, error) {
	if a.Present(i) {
		return a.Value(i).(*Service), nil
	}
	return stateKey.Of(sc).services.Default()
}

func (svc *Service) selection(i int) (*Selection, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.selections) == 0 {
		return nil, fmt.Errorf("no selections set for MongoDB service %s", svc.Name)
	}
	if i < 0 || i >= len(svc.selections) {
		return nil, fmt.Errorf("selection %d does not exist for MongoDB service %s; %d selection(s) were retrieved in this scenario", i+1, svc.Name, len(svc.selections))
	}
	return svc.selections[i], nil
}

func find(sc *core.Scenario, a core.Args, svcArg int, collection string, t *core.Table, within time.Duration, atLeast int) error {
	svc, err := service(sc, a, svcArg)
	if err != nil {
		return err
	}
	filter := bson.D{}
	if t != nil {
		if filter, err = filterOf(sc, t); err != nil {
			return err
		}
	}
	coll := svc.db.Collection(collection)
	deadline := time.Now().Add(within)
	var lastErr error
	for {
		var docs []bson.D
		cur, err := coll.Find(sc.Context(), filter)
		if err == nil {
			err = cur.All(sc.Context(), &docs)
		}
		if err == nil && (within == 0 || len(docs) >= atLeast || time.Now().Add(500*time.Millisecond).After(deadline)) {
			svc.addSelection(&Selection{Collection: collection, Filter: filter, Docs: docs})
			return nil
		}
		if err != nil {
			lastErr = err
			if within == 0 || time.Now().Add(500*time.Millisecond).After(deadline) {
				return fmt.Errorf("could not retrieve documents from %s: %w", collection, lastErr)
			}
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (svc *Service) addSelection(sel *Selection) {
	svc.mu.Lock()
	svc.selections = append(svc.selections, sel)
	svc.mu.Unlock()
}

// filterOf builds an equality filter; values that parse as (Extended) JSON
// keep their type, anything else is a string.
func filterOf(sc *core.Scenario, t *core.Table) (bson.D, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("No conditions provided") //nolint:staticcheck // same message as the SQL selection steps
	}
	filter := make(bson.D, 0, len(pairs))
	for _, p := range pairs {
		if p.Null {
			filter = append(filter, bson.E{Key: p.Key, Value: nil})
			continue
		}
		filter = append(filter, bson.E{Key: p.Key, Value: typedValue(sc.Suite().Interpolate(p.Value))})
	}
	return filter, nil
}

func typedValue(s string) any {
	var wrapped bson.D
	if err := bson.UnmarshalExtJSON([]byte(`{"v":`+s+`}`), false, &wrapped); err == nil && len(wrapped) == 1 {
		return wrapped[0].Value
	}
	return s
}

func docProperties(sc *core.Scenario, a core.Args, regex bool) error {
	doc, err := selectedDoc(sc, a, 0, 1, 2)
	if err != nil {
		return err
	}
	return jsonassert.Properties(doc, a.Table, regex)
}

// selectedDoc returns the JSON of document (arg docArg) in selection (arg
// selArg) of the service (arg svcArg).
func selectedDoc(sc *core.Scenario, a core.Args, docArg, selArg, svcArg int) (string, error) {
	svc, err := service(sc, a, svcArg)
	if err != nil {
		return "", err
	}
	sel, err := svc.selection(a.IntOr(selArg, 1) - 1)
	if err != nil {
		return "", err
	}
	i := a.Int(docArg) - 1
	if i < 0 || i >= len(sel.Docs) {
		return "", fmt.Errorf("document %d does not exist; the selection from %s has %d document(s)", i+1, sel.Collection, len(sel.Docs))
	}
	var buf bytes.Buffer
	writeJSON(&buf, sel.Docs[i])
	return buf.String(), nil
}

// writeJSON renders BSON as plain JSON, keeping field order: ObjectIds are
// hex strings, dates ISO-8601 UTC, decimals and other exotic types strings.
func writeJSON(buf *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil, bson.Null, bson.Undefined:
		buf.WriteString("null")
	case bson.D:
		buf.WriteByte('{')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeString(buf, e.Key)
			buf.WriteByte(':')
			writeJSON(buf, e.Value)
		}
		buf.WriteByte('}')
	case bson.A:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSON(buf, e)
		}
		buf.WriteByte(']')
	case string:
		writeString(buf, x)
	case bool:
		buf.WriteString(strconv.FormatBool(x))
	case int32:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b, err := json.Marshal(x)
		if err != nil { // NaN/Inf
			writeString(buf, strconv.FormatFloat(x, 'g', -1, 64))
			return
		}
		buf.Write(b)
	case bson.ObjectID:
		writeString(buf, x.Hex())
	case bson.DateTime:
		writeString(buf, x.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
	case bson.Binary:
		writeString(buf, base64.StdEncoding.EncodeToString(x.Data))
	case fmt.Stringer:
		writeString(buf, x.String())
	default:
		writeString(buf, fmt.Sprint(x))
	}
}

func writeString(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}
