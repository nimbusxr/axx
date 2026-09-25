// Package cloudstep holds what the cloud service packs share: waiting for
// something the services under test do asynchronously, matching rows,
// items, documents and messages against a step's table with the text
// comparison of the other packs' JSON property steps, and reading seed
// files.
package cloudstep

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
	"github.com/nimbusxr/axx/internal/compat/jyaml"
)

// DefaultWait is how long a check waits when its step does not say
// `within {duration}`: the services under test act asynchronously.
const DefaultWait = 10 * time.Second

const pollEvery = 200 * time.Millisecond

// Wait is the optional `within {duration}` argument i, or DefaultWait.
func Wait(a core.Args, i int) time.Duration {
	if a.Present(i) {
		return a.Value(i).(time.Duration)
	}
	return DefaultWait
}

// Poll calls check until it reports done or d has passed; check's last
// message explains the failure. Errors from check end the wait.
func Poll(sc *core.Scenario, d time.Duration, check func() (bool, string, error)) error {
	deadline := time.Now().Add(d)
	for {
		done, msg, err := check()
		if err != nil || done {
			return err
		}
		if !time.Now().Before(deadline) {
			return core.Failf("%s", msg)
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(pollEvery):
		}
	}
}

// Rows are the conditions of a "where:" table: each is a path into the JSON
// of a row, item, document or message (a column or field name, a dotted
// path, or a JSONPath) and the text its value must have. `null` means null
// and `undefined` means absent, as in the JSON property steps.
type Rows []core.Pair

// Conditions reads a "where:" table.
func Conditions(t *core.Table) (Rows, error) {
	if t == nil {
		return nil, fmt.Errorf("the step needs a table of conditions (| field | value |)")
	}
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	return pairs, nil
}

// Match reports whether the JSON document meets every condition.
func (rs Rows) Match(doc string) (bool, error) {
	for _, r := range rs {
		ok, err := matchValue(doc, r)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func matchValue(doc string, r core.Pair) (bool, error) {
	want := r.Value
	if r.Null {
		want = "null"
	}
	ok, err := jvalue.PostgresPropertyIs(doc, r.Key, want)
	if err != nil {
		return false, fmt.Errorf("%s: %w", r.Key, err)
	}
	return ok, nil
}

// Message is a message as the checks see it: its body and the named
// values sent with it (attributes, application properties).
type Message struct {
	Body   []byte
	Fields map[string]string
	// Received is when axx received it.
	Received time.Time
}

// MatchMessage reports whether a message meets every condition. A
// condition on "<field> <name>" ("attribute eventType") is on a named
// value sent with the message; any other is a path into the body, which
// must then be JSON.
func (rs Rows) MatchMessage(m Message, field string) (bool, error) {
	for _, r := range rs {
		if name, ok := strings.CutPrefix(r.Key, field+" "); ok {
			v, present := m.Fields[strings.TrimSpace(name)]
			switch {
			case strings.EqualFold(r.Value, "undefined"):
				if present {
					return false, nil
				}
			case !present || v != r.Value:
				return false, nil
			}
			continue
		}
		if !json.Valid(m.Body) {
			return false, nil
		}
		ok, err := matchValue(string(m.Body), r)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// Describe renders a message for a failure report: its named values and
// its body.
func (m Message) Describe(field string) string {
	var b strings.Builder
	names := make([]string, 0, len(m.Fields))
	for k := range m.Fields {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s %s=%s ", field, k, m.Fields[k])
	}
	b.Write(Compact(m.Body))
	return b.String()
}

// Compact is JSON on one line, or the text as it is.
func Compact(body []byte) []byte {
	var buf bytes.Buffer
	if json.Compact(&buf, body) == nil {
		return buf.Bytes()
	}
	return bytes.TrimSpace(body)
}

// Shown lists up to max candidates (rows, items, messages) for a failure
// report, the most recent last, saying how many were left out.
func Shown(what string, items []string, max int) string {
	if len(items) == 0 {
		return "no " + what
	}
	var b strings.Builder
	skipped := 0
	if len(items) > max {
		skipped = len(items) - max
		items = items[skipped:]
	}
	fmt.Fprintf(&b, "%d %s", len(items)+skipped, what)
	if skipped > 0 {
		fmt.Fprintf(&b, " (the last %d):", max)
	} else {
		b.WriteString(":")
	}
	for _, it := range items {
		b.WriteString("\n  " + it)
	}
	return b.String()
}

// Seed is a seed file: names (tables, collections) in file order, each
// with its rows, items or documents.
type Seed struct {
	Names []string
	Items map[string]any
}

// ReadSeed reads a YAML or JSON seed file whose top level maps names to
// their content.
func ReadSeed(sc *core.Scenario, file string) (*Seed, error) {
	path, err := sc.Suite().ResolvePath(file)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := jyaml.Unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	obj, ok := doc.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("%s: the seed must map names to their content", file)
	}
	s := &Seed{Items: map[string]any{}}
	for _, k := range obj.Keys() {
		v, _ := obj.Get(k)
		plain, err := Plain(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", file, k, err)
		}
		s.Names = append(s.Names, k)
		s.Items[k] = plain
	}
	return s, nil
}

// Plain converts a compat JSON value to plain Go values: maps, slices,
// strings, bools, nil, and json.Number for numbers.
func Plain(v any) (any, error) {
	text := jsonx.MarshalValue(v)
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// JSON renders a value as compact JSON.
func JSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// Inbox collects the messages a listener receives during a run, for the
// "has a message where:" checks of every scenario.
type Inbox struct {
	mu   sync.Mutex
	msgs []Message
	err  error
}

// Add records a received message.
func (b *Inbox) Add(m Message) {
	if m.Received.IsZero() {
		m.Received = time.Now()
	}
	b.mu.Lock()
	b.msgs = append(b.msgs, m)
	b.mu.Unlock()
}

// Fail records why the listener stopped.
func (b *Inbox) Fail(err error) {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
}

// Since returns the messages received at or after t, or why the listener
// stopped.
func (b *Inbox) Since(t time.Time) ([]Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Message
	for _, m := range b.msgs {
		if !m.Received.Before(t) {
			out = append(out, m)
		}
	}
	return out, b.err
}

// ExpectMessage waits until the inbox has a message, received since the
// scenario started, that meets the conditions. where names what was
// listened to ("the invoice-events pubsub topic"); field is the word for the
// named values messages carry ("attribute", "property").
func ExpectMessage(sc *core.Scenario, d time.Duration, in *Inbox, rs Rows, field, where string) error {
	return Poll(sc, d, func() (bool, string, error) {
		msgs, err := in.Since(sc.Started())
		if err != nil {
			return false, "", fmt.Errorf("listening to %s failed: %w", where, err)
		}
		shown := make([]string, 0, len(msgs))
		for _, m := range msgs {
			ok, err := rs.MatchMessage(m, field)
			if err != nil {
				return false, "", err
			}
			if ok {
				return true, "", nil
			}
			shown = append(shown, m.Describe(field))
		}
		return false, fmt.Sprintf("No message on %s met the conditions within %s. It received %s",
			where, d, Shown(plural(len(shown), "message")+" since the scenario started", shown, 10)), nil
	})
}

// Listeners are the listeners one pack runs during a run, one per key
// (connection and topic, say), started once and stopped when the run ends.
type Listeners struct {
	mu sync.Mutex
	m  map[string]*listener
}

type listener struct {
	once  sync.Once
	inbox *Inbox
	err   error
	stop  func(context.Context) error
}

// RunListeners returns the pack's listeners for the run.
func RunListeners(s *core.Suite, pack string) *Listeners {
	l, _ := core.Cached(s, pack+"/listeners", func() (*Listeners, error) {
		ls := &Listeners{m: map[string]*listener{}}
		s.OnClose(ls.stopAll)
		return ls, nil
	})
	return l
}

// Start runs start once for key and returns the inbox it fills. start
// receives the context of the whole run; stop undoes what it set up
// (a subscription it created, say).
func (l *Listeners) Start(key string, start func(ctx context.Context, in *Inbox) (stop func(context.Context) error, err error)) (*Inbox, error) {
	l.mu.Lock()
	ln, ok := l.m[key]
	if !ok {
		ln = &listener{inbox: &Inbox{}}
		l.m[key] = ln
	}
	l.mu.Unlock()
	ln.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		stop, err := start(ctx, ln.inbox)
		ln.err = err
		ln.stop = func(c context.Context) error {
			cancel()
			if stop != nil {
				return stop(c)
			}
			return nil
		}
	})
	return ln.inbox, ln.err
}

// Started reports whether key's listener was started.
func (l *Listeners) Started(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.m[key]
	return ok
}

func (l *Listeners) stopAll(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var errs []error
	for _, ln := range l.m {
		if ln.stop != nil {
			errs = append(errs, ln.stop(ctx))
		}
	}
	return errors.Join(errs...)
}

// RunID names what a run creates in the cloud (subscriptions, queues), so
// runs do not collide and leftovers are recognizable: "axx-" and 8 hex
// digits.
func RunID(s *core.Suite) string {
	id, _ := core.Cached(s, "cloudstep/run-id", func() (string, error) {
		var b [4]byte
		_, _ = rand.Read(b[:])
		return "axx-" + hex.EncodeToString(b[:]), nil
	})
	return id
}

// Deferred holds what a pack plans before the apps start (Prepare) and does
// once they run (Init): the listeners of a run's checks need the topics and
// queues the environment creates.
type Deferred struct {
	mu    sync.Mutex
	order []string
	fns   map[string]func(context.Context) error
}

// DeferredFor returns the pack's deferred work for the run.
func DeferredFor(s *core.Suite, pack string) *Deferred {
	d, _ := core.Cached(s, pack+"/deferred", func() (*Deferred, error) {
		return &Deferred{fns: map[string]func(context.Context) error{}}, nil
	})
	return d
}

// Add plans fn under key (planning a key twice keeps the first).
func (d *Deferred) Add(key string, fn func(context.Context) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.fns[key]; ok {
		return
	}
	d.order = append(d.order, key)
	d.fns[key] = fn
}

// Run does the planned work, in the order planned, and forgets it.
func (d *Deferred) Run(ctx context.Context) error {
	d.mu.Lock()
	order, fns := d.order, d.fns
	d.order, d.fns = nil, map[string]func(context.Context) error{}
	d.mu.Unlock()
	for _, k := range order {
		if err := fns[k](ctx); err != nil {
			return err
		}
	}
	return nil
}

// ExpectRecords waits until the records (rows, items, documents, as JSON)
// fetch returns include one that meets the conditions, or, with count >=
// 0, exactly count of them. what names them ("item", "row") and where names
// what holds them ("the claims dynamodb table").
func ExpectRecords(sc *core.Scenario, d time.Duration, fetch func() ([]string, error), rs Rows, count int, what, where string) error {
	return Poll(sc, d, func() (bool, string, error) {
		recs, err := fetch()
		if err != nil {
			return false, "", err
		}
		matched := 0
		for _, r := range recs {
			ok, err := rs.Match(r)
			if err != nil {
				return false, "", err
			}
			if ok {
				matched++
			}
		}
		if count < 0 && matched > 0 || count >= 0 && matched == count {
			return true, "", nil
		}
		want := article(what) + " " + what
		if count >= 0 {
			want = fmt.Sprintf("%d %s", count, plural(count, what))
		}
		return false, fmt.Sprintf("%s did not have %s that met the conditions within %s: %d did. It has %s",
			strings.ToUpper(where[:1])+where[1:], want, d, matched, Shown(plural(len(recs), what), recs, 10)), nil
	})
}

func article(what string) string {
	if strings.ContainsRune("aeiou", rune(what[0])) {
		return "an"
	}
	return "a"
}

func plural(n int, what string) string {
	if n == 1 {
		return what
	}
	return what + "s"
}
