package mongo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// filterTable is the table of a step that finds documents by their fields.
var filterTable = &core.TableDoc{
	Columns: []string{"field", "value"},
	Note: "Each row names a field, dotted (`recipient.city`) for one inside a document, and the value it holds, of its type as a check compares it: " +
		"`6` selects an int32 or an int64 of 6, but not a double 6.0, which is `6.0`; `null` selects a null, and `undefined` a document without the field. " +
		"An object is Extended JSON, like `{\"$oid\": \"...\"}` for an ObjectId.",
	Values:    core.JSONValues,
	Undefined: true,
}

// numbered is how selections are numbered, for the steps that find one.
const numbered = "Selections are numbered in the order they are retrieved, whatever ordinal the step says; `the selection` means the first."

// Selection is the result of a find: documents in natural (or _id) order.
type Selection struct {
	Collection string
	Filter     bson.D
	Docs       []bson.D

	// dropped says why documents MongoDB selected are not in it: a value of
	// another type than the step's (keepStrict).
	dropped []string
}

// hints completes the failure of a check of the selection's documents.
func (s *Selection) hints() string {
	if len(s.dropped) == 0 {
		return ""
	}
	return " (" + strings.Join(s.dropped, "; ") + ")"
}

func queryParams() []core.ParamType {
	return []core.ParamType{{
		Name: "mongoService", Regexps: []string{`([^\s]+)`},
		Doc:      "the name of a MongoDB database registered in the scenario",
		Examples: []string{"tracking-db"},
		Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
			return stateKey.Of(sc).services.Get(name)
		},
	}}
}

func querySteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mongo.find", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr:  "a[[ {ordinal}]] selection of documents is retrieved from the {word} collection[[ on {mongoService}]] where:",
			Doc:   "Find the documents that match and keep them as the next selection, for the steps that check it. " + numbered,
			Table: filterTable,
			Examples: []string{"Then a selection of documents is retrieved from the scans collection where:\n" +
				"  | parcelRef | PX-TRK-3001 |\n" +
				"  | status    | IN_TRANSIT  |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return find(sc, a, 2, a.String(1), a.Table, 0, 0)
			},
		},
		{
			ID: "mongo.find.poll", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "within {duration} a[[ {ordinal}]] selection of at least {int} document(s) is retrieved from the {word} collection[[ on {mongoService}]] where:",
			Doc: "Find the documents that match, again every 500ms, until at least that many come back, and keep them as the next selection.\n\n" +
				"- The step fails when the time is up with fewer documents.\n" +
				"- An error from MongoDB fails the step only when the time is up.\n" +
				"- " + numbered,
			Table: filterTable,
			Examples: []string{"Then within 10s a selection of at least 1 document is retrieved from the tracking collection where:\n" +
				"  | _id       | PX-TRK-3001 |\n" +
				"  | scanCount | 2           |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return find(sc, a, 4, a.String(3), a.Table, a.Value(0).(time.Duration), a.Int(2))
			},
		},
		docCount("mongo.docs.eq", "has {int} document(s)", "exactly", func(got, want int) bool { return got == want }),
		docCount("mongo.docs.gt", "has more than {int} document(s)", "more than", func(got, want int) bool { return got > want }),
		docCount("mongo.docs.lt", "has fewer than {int} document(s)", "fewer than", func(got, want int) bool { return got < want }),
		{
			ID: "mongo.doc.are", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "the {ordinal} document for the[[ {ordinal}]] selection[[ on {mongoService}]] properties are:",
			Doc: "Check the fields of one document of a selection, read as JSON: an ObjectId is its hex string, " +
				"a date is in ISO-8601 UTC, like `2026-05-05T06:40:00.000Z`, a double is a decimal, like `6.0`, and a Decimal128 a number.",
			Table: &core.TableDoc{
				Columns:   []string{"JSONPath", "value"},
				Note:      "Each row names a JSONPath, like `lastLocation` or `scans[0].status`, and the value there.",
				Values:    core.JSONValues,
				Undefined: true,
			},
			Examples: []string{"Then the 1st document for the selection properties are:\n" +
				"  | status       | DELIVERED |\n" +
				"  | lastLocation | Leipzig   |\n" +
				"  | delivered    | true      |"},
			Run: func(sc *core.Scenario, a core.Args) error { return docProperties(sc, a, false) },
		},
		{
			ID: "mongo.doc.match", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "the {ordinal} document for the[[ {ordinal}]] selection[[ on {mongoService}]] properties match:",
			Doc: "Check the fields of one document of a selection, like the `properties are` step, " +
				"with regular expressions (Java syntax) that must match the whole value, as text: a double's text is a decimal's, like `6.0`, " +
				"and a Decimal128's its digits, like `1.50`.",
			Table: &core.TableDoc{
				Columns: []string{"JSONPath", "pattern"},
				Note:    "Each row names a JSONPath, like `_id` or `scans[0].status`, and a regular expression its value must match.",
			},
			Examples: []string{"Then the 1st document for the 2nd selection on tracking-db properties match:\n" +
				"  | _id          | PX-TRK-\\d{4} |\n" +
				"  | lastLocation | Hamburg.*    |"},
			Run: func(sc *core.Scenario, a core.Args) error { return docProperties(sc, a, true) },
		},
	}
}

func docCount(id, tail, words string, ok func(got, want int) bool) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then", Since: "0.1.0",
		Expr:     "the[[ {ordinal}]] selection[[ on {mongoService}]] " + tail,
		Doc:      fmt.Sprintf("Check that a selection of documents has %s the given number of documents. `the selection` means the first selection of the scenario.", words),
		Examples: []string{"Then the selection " + strings.ReplaceAll(tail, "{int} document(s)", "2 documents"), "Then the 2nd selection on tracking-db " + strings.ReplaceAll(tail, "{int} document(s)", "1 document")},
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
				return core.Fail(fmt.Sprintf("expected the selection from %s to have %s %d document(s)%s", sel.Collection, words, want, sel.hints()), want, len(sel.Docs))
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
	var conditions []condition
	if t != nil {
		if filter, conditions, err = filterOf(sc, t); err != nil {
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
		if err == nil {
			var dropped []string
			docs, dropped = keepStrict(docs, conditions)
			if within == 0 || len(docs) >= atLeast || time.Now().Add(500*time.Millisecond).After(deadline) {
				sel := &Selection{Collection: collection, Filter: filter, Docs: docs, dropped: dropped}
				svc.addSelection(sel)
				if within > 0 && len(docs) < atLeast {
					return core.Fail(fmt.Sprintf("expected at least %d document(s) from %s within %s%s", atLeast, collection, within, sel.hints()), atLeast, len(docs))
				}
				return nil
			}
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

// filterOf builds an equality filter of the values as the table writes them
// (core.ParseValue, ADR 0014), and the conditions keepStrict holds what it
// selects to.
func filterOf(sc *core.Scenario, t *core.Table) (bson.D, []condition, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, nil, err
	}
	if len(pairs) == 0 {
		return nil, nil, fmt.Errorf("No conditions provided") //nolint:staticcheck // same message as the SQL selection steps
	}
	filter := make(bson.D, 0, len(pairs))
	conditions := make([]condition, 0, len(pairs))
	for _, p := range pairs {
		text := sc.Suite().Interpolate(p.Value)
		v, err := core.ParseValue(text, p.Null, true)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", p.Key, err)
		}
		if v.Undefined() {
			filter = append(filter, bson.E{Key: p.Key, Value: bson.D{{Key: "$exists", Value: false}}})
			continue
		}
		typed, err := typedValue(text, p.Null)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", p.Key, err)
		}
		filter = append(filter, bson.E{Key: p.Key, Value: wideNumber(v, typed)})
		conditions = append(conditions, newCondition(p.Key, v, typed))
	}
	return filter, conditions, nil
}

// wideNumber is what a number asks MongoDB for: the BSON number it is, or the
// Decimal128 of the same digits, which is the number a check says it is. MongoDB
// compares a double with a Decimal128 by their exact values, and the double 1.1
// is not quite 1.1.
func wideNumber(v core.Value, typed any) any {
	if !jsonassert.IsNumber(v) {
		return typed
	}
	d, err := bson.ParseDecimal128(v.JSON())
	if err != nil {
		return typed
	}
	return bson.D{{Key: "$in", Value: bson.A{typed, d}}}
}

// condition is a row of a filter, as keepStrict holds a document to it.
type condition struct {
	field string
	path  []string
	// want is the value as a check of a document's JSON compares it: Extended
	// JSON is read for what it means, so `{"$oid": "..."}` is its hex string
	// (writeDoc). A value a check has no way to compare is left to MongoDB:
	// strict is false.
	want   core.Value
	strict bool
}

func newCondition(field string, v core.Value, typed any) condition {
	c := condition{field: field, path: strings.Split(field, "."), want: v, strict: true}
	if j := v.JSON(); strings.HasPrefix(j, "{") || strings.HasPrefix(j, "[") {
		var buf bytes.Buffer
		writeDoc(&buf, typed, false)
		want, err := core.ParseValue(buf.String(), false, false)
		c.want, c.strict = want, err == nil
	}
	return c
}

// keepStrict drops the documents MongoDB selected whose value is not the
// condition's, type and all, as a check compares them: MongoDB's own equality
// takes an int32 of 6 for a double of 6.0 or a Decimal128 of 6, and null for a
// field that is not there. What it selects is every document that could be meant,
// and a query's $type could not tell a Decimal128's 6 from its 6.0, so this goes
// through them after it: what it compares is what a check compares (writeDoc,
// jsonassert.Strict). dropped says what a document that differs only in type
// holds, and what to write for it.
func keepStrict(docs []bson.D, conditions []condition) (kept []bson.D, dropped []string) {
	if len(conditions) == 0 {
		return docs, nil
	}
	hint := func(h string) {
		if h != "" && !slices.Contains(dropped, h) {
			dropped = append(dropped, h)
		}
	}
	for _, doc := range docs {
		keep := true
		for _, c := range conditions {
			same, h := c.holds(doc)
			if !same {
				keep = false
				hint(h)
			}
		}
		if keep {
			kept = append(kept, doc)
		}
	}
	return kept, dropped
}

// holds says whether a document has the condition's value on its path, and what
// to write if it has it but for its type.
func (c condition) holds(doc bson.D) (same bool, hint string) {
	if !c.strict {
		return true, ""
	}
	values := valuesAt(doc, c.path)
	if len(values) == 0 && c.want.IsNull() {
		return false, "a document has no " + c.field + ", which is not null"
	}
	for _, v := range values {
		var buf bytes.Buffer
		writeDoc(&buf, v, false)
		ok, h := jsonassert.Strict(c.field, buf.String(), c.want)
		if ok {
			return true, ""
		}
		if hint == "" {
			hint = h
		}
	}
	return false, hint
}

// valuesAt are the values a dotted field names in a document, as MongoDB reads
// them: a name goes into each document of an array, a number is also a position
// in one, and an array is itself and each of its elements.
func valuesAt(v any, path []string) []any {
	if len(path) == 0 {
		if a, ok := v.(bson.A); ok {
			return append([]any{a}, a...)
		}
		return []any{v}
	}
	switch x := v.(type) {
	case bson.D:
		for _, e := range x {
			if e.Key == path[0] {
				return valuesAt(e.Value, path[1:])
			}
		}
	case bson.A:
		var out []any
		if i, err := strconv.Atoi(path[0]); err == nil && i >= 0 && i < len(x) {
			out = valuesAt(x[i], path[1:])
		}
		for _, e := range x {
			if _, isDoc := e.(bson.D); isDoc {
				out = append(out, valuesAt(e, path)...)
			}
		}
		return out
	}
	return nil
}

// typedValue is a value of the table as BSON: `"3"` and other text a string,
// `3` an int32 (an int64 past its range), `3L` an int64, `1.5` a double,
// `true` a boolean, `null` null, and `{...}` and `[...]` relaxed Extended
// JSON, like `{"$oid": "..."}` for an ObjectId. empty says the cell was empty.
func typedValue(text string, empty bool) (any, error) {
	v, err := core.ParseValue(text, empty, false)
	if err != nil {
		return nil, err
	}
	if s, ok := v.Str(); ok {
		return s, nil
	}
	if v.IsLong() {
		n, err := strconv.ParseInt(v.JSON(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s is past the range of a long", v.Text())
		}
		return n, nil
	}
	var wrapped bson.D
	if err := bson.UnmarshalExtJSON([]byte(`{"v":`+v.JSON()+`}`), false, &wrapped); err != nil || len(wrapped) != 1 {
		return nil, fmt.Errorf("%s is not MongoDB's Extended JSON: %w", v.Text(), err)
	}
	return wrapped[0].Value, nil
}

func docProperties(sc *core.Scenario, a core.Args, regex bool) error {
	doc, err := selectedDoc(sc, a, 0, 1, 2, regex)
	if err != nil {
		return err
	}
	return jsonassert.Properties(doc, a.Table, regex)
}

// selectedDoc returns the JSON of document (arg docArg) in selection (arg
// selArg) of the service (arg svcArg), for a pattern step's text when
// forPatterns says so (writeDoc).
func selectedDoc(sc *core.Scenario, a core.Args, docArg, selArg, svcArg int, forPatterns bool) (string, error) {
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
	writeDoc(&buf, sel.Docs[i], forPatterns)
	return buf.String(), nil
}

// writeJSON renders BSON as plain JSON, keeping field order and the types
// of numbers (ADR 0014): int32 and int64 are integers, a double a decimal
// (6.0), a Decimal128 the number it is (1.50); ObjectIds are hex strings,
// dates ISO-8601 UTC, and other exotic types strings.
func writeJSON(buf *bytes.Buffer, v any) { writeDoc(buf, v, false) }

// writeDoc is writeJSON, or with forPatterns the document of a pattern step,
// which matches a value's text. A Decimal128's text is its digits (1.50, and
// all 34 of them), so there it is a string of them: the reader of a pattern's
// document reads every JSON number as a double (1.5). A whole double's text
// is 6.0.
func writeDoc(buf *bytes.Buffer, v any, forPatterns bool) {
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
			writeDoc(buf, e.Value, forPatterns)
		}
		buf.WriteByte('}')
	case bson.A:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeDoc(buf, e, forPatterns)
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
		writeDouble(buf, x)
	case bson.Decimal128:
		// Its text keeps its digits (1.50, 1E+3); NaN and the infinities,
		// which JSON lacks, are strings.
		if s := x.String(); !forPatterns && json.Valid([]byte(s)) {
			buf.WriteString(s)
		} else {
			writeString(buf, s)
		}
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

// writeDouble writes a double as a decimal, a whole one too (6.0, not 6),
// and NaN and the infinities, which JSON lacks, as Extended JSON names them.
func writeDouble(buf *bytes.Buffer, f float64) {
	switch {
	case math.IsNaN(f):
		writeString(buf, "NaN")
		return
	case math.IsInf(f, 1):
		writeString(buf, "Infinity")
		return
	case math.IsInf(f, -1):
		writeString(buf, "-Infinity")
		return
	}
	b, _ := json.Marshal(f)
	buf.Write(b)
	if !bytes.ContainsAny(b, ".eE") {
		buf.WriteString(".0")
	}
}

func writeString(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}
