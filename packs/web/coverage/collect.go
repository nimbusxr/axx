package coverage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	webcore "github.com/nimbusxr/axx/packs/web/core"
	"github.com/nimbusxr/axx/packs/web/coverage/internal/v8cov"
)

// Collecting V8's coverage over the Chrome DevTools Protocol, one session a
// page. V8 counts in count mode, block by block; each take returns what ran
// since the one before and starts over, so every take is kept, and a
// script's takes are merged at the end of the run.
//
// Pages of one renderer share its V8 (an isolate) and its counts: the first
// page of a renderer starts its coverage, and takes it for all of them. A
// page that goes to another site moves to another renderer; one that closes
// stops its renderer's coverage, if it started it. Before a page leaves its
// document, which takes its counts along, a hook in the page pauses it
// (beforeunload): its coverage is taken then.

// hookScript pauses a page that is about to leave its document, so that
// its coverage is taken first. Every browser context of a web app has it.
const hookScript = "addEventListener('beforeunload', function " + hookFunction + "() { debugger; });"

const hookFunction = "__axxCoverageHook"

// How long a page waits, paused before it leaves, for its coverage; and a
// scenario, at its end, for its pages' last coverage and sources, or less
// while a page shows a dialog (its scripts wait for an answer).
const (
	leaveWait   = 5 * time.Second
	settleWait  = 10 * time.Second
	blockedWait = 200 * time.Millisecond
)

// run is what a run collects: every take of each script that counts, and
// the scripts' sources, until it ends and the reports are written.
type run struct {
	suite *core.Suite
	cfg   *settings
	seq   atomic.Uint64

	mu       sync.Mutex
	scripts  map[scriptKey]*script
	sources  map[string]*source // by V8's hash of the source
	fetching sync.WaitGroup
}

// scriptKey is a script: its URL and V8's hash of its source.
type scriptKey struct{ url, hash string }

// script is a script's takes; a take that comes again (the same page does
// the same) is kept once, with the times it came.
type script struct {
	takes   [][]v8cov.Function
	times   []int
	digests map[[sha256.Size]byte]int
}

// source is a script's source and source map, once fetched.
type source struct {
	text      string
	ok        bool
	sourceMap []byte
	mapErr    error
}

// runFor is the run's coverage, which writes its reports when the run ends.
func runFor(s *core.Suite) (*run, error) {
	return core.Cached(s, Name+"/run", func() (*run, error) {
		cfg, err := settingsFor(s)
		if err != nil {
			return nil, err
		}
		r := &run{suite: s, cfg: cfg, scripts: map[scriptKey]*script{}, sources: map[string]*source{}}
		s.OnClose(r.report)
		return r, nil
	})
}

// unsupported says, once a run for each web app, that its pages run without
// coverage.
func (r *run) unsupported(sc *core.Scenario, c webcore.Context) {
	_, _ = core.Cached(r.suite, Name+"/unsupported/"+c.App(), func() (bool, error) {
		sc.Log("the %s web app runs in %s: JavaScript coverage is collected in Chromium, Chrome and Edge only", c.App(), c.Engine())
		return true, nil
	})
}

// fetch reports whether a script's source is to be fetched: the first time
// a source is seen, or again when fetching it failed.
func (r *run) fetch(key string) (*source, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[key]; ok {
		return nil, false
	}
	src := &source{}
	r.sources[key] = src
	r.fetching.Add(1)
	return src, true
}

func (r *run) fetched(key string, src *source) {
	r.mu.Lock()
	if !src.ok && r.sources[key] == src {
		delete(r.sources, key)
	}
	r.mu.Unlock()
	r.fetching.Done()
}

// add keeps a take of a script.
func (r *run) add(key scriptKey, raw json.RawMessage) {
	digest := sha256.Sum256(raw)
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.scripts[key]
	if s == nil {
		s = &script{digests: map[[sha256.Size]byte]int{}}
		r.scripts[key] = s
	}
	if i, ok := s.digests[digest]; ok {
		s.times[i]++
		return
	}
	var functions []v8cov.Function
	if err := json.Unmarshal(raw, &functions); err != nil {
		return
	}
	s.digests[digest] = len(s.takes)
	s.takes = append(s.takes, functions)
	s.times = append(s.times, 1)
}

// sourceKey is what a script's source is kept by: V8's hash of it.
func sourceKey(hash, scriptURL string) string {
	if hash == "" {
		return "url " + scriptURL
	}
	return hash
}

// appCoverage is the coverage of a web app's pages in a scenario: its
// browser context.
type appCoverage struct {
	run    *run
	ctx    webcore.Context
	origin string

	mu    sync.Mutex
	pages []*pageCoverage // every page, open or closed, in the order they opened
	// A take of the pages runs in the background, one at a time: running
	// while it does, again when another is asked for meanwhile, done closed
	// when it ends.
	running, again bool
	done           chan struct{}
	fetching       sync.WaitGroup
}

// pageCoverage is a page's session. Its fields are guarded by its app's mu.
type pageCoverage struct {
	app *appCoverage
	cdp playwright.CDPSession

	isolate string // V8's, where the page is now
	started bool   // the session started its isolate's coverage, which follows it
	closed  bool
	scripts map[string]*parsed // those that count, by scriptId
	// dialog is when a dialog opened, answered when a command sent later
	// was answered: the page's scripts wait for the dialog's answer, and so
	// do the commands to its renderer.
	dialog, answered time.Time
}

// parsed is a script a page parsed.
type parsed struct {
	url, hash string
	seq       uint64
}

func newAppCoverage(r *run, c webcore.Context) *appCoverage {
	origin := ""
	if u, err := url.Parse(c.URL()); err == nil {
		origin = originOf(u)
	}
	return &appCoverage{run: r, ctx: c, origin: origin}
}

// attach collects a page's coverage.
func (a *appCoverage) attach(pg playwright.Page) error {
	cdp, err := a.ctx.NewCDPSession(pg)
	if err != nil {
		return err
	}
	p := &pageCoverage{app: a, cdp: cdp, scripts: map[string]*parsed{}}
	// Events come on Playwright's dispatcher, which a command sent from there
	// would wait for: they send nothing themselves.
	cdp.On("Debugger.scriptParsed", p.parsed)
	cdp.On("Debugger.paused", p.paused)
	pg.OnClose(func(playwright.Page) { a.closed(p) })
	pg.OnDialog(func(playwright.Dialog) {
		a.mu.Lock()
		p.dialog = time.Now()
		a.mu.Unlock()
	})
	a.mu.Lock()
	a.pages = append(a.pages, p)
	a.mu.Unlock()
	id, err := p.locate()
	if err != nil {
		return err
	}
	// The first page of a renderer starts its coverage.
	a.mu.Lock()
	first := a.takerIn(id) == nil
	if first {
		p.started = true
	}
	a.mu.Unlock()
	if first {
		if err := p.start(); err != nil {
			a.mu.Lock()
			p.started = false
			a.mu.Unlock()
			return err
		}
	}
	_, err = p.send("Debugger.enable", nil)
	return err
}

// send sends a command to the page's renderer.
func (p *pageCoverage) send(method string, params map[string]any) (any, error) {
	sent := time.Now()
	res, err := p.cdp.Send(method, params)
	if err == nil {
		p.app.mu.Lock()
		if sent.After(p.answered) {
			p.answered = sent
		}
		p.app.mu.Unlock()
	}
	return res, err
}

// start starts the coverage of the page's renderer. Only the page that
// starts it enables its profiler: a page whose profiler is on stops its
// renderer's coverage as it closes.
func (p *pageCoverage) start() error {
	if _, err := p.send("Profiler.enable", nil); err != nil {
		return err
	}
	_, err := p.send("Profiler.startPreciseCoverage", map[string]any{"callCount": true, "detailed": true})
	return err
}

// locate finds the page's isolate: a page that went to another site is in
// another renderer.
func (p *pageCoverage) locate() (string, error) {
	res, err := p.send("Runtime.getIsolateId", nil)
	if err != nil {
		return "", err
	}
	m, _ := res.(map[string]any)
	id, _ := m["id"].(string)
	if id == "" {
		return "", errors.New("no isolate")
	}
	a := p.app
	a.mu.Lock()
	if p.started && p.isolate != "" && p.isolate != id {
		// Its session left the renderer, whose coverage stopped.
		a.stopped(p.isolate, p)
	}
	p.isolate = id
	a.mu.Unlock()
	return id, nil
}

// stopped marks an isolate's coverage stopped: a page there starts it
// again. a.mu is held.
func (a *appCoverage) stopped(isolate string, except *pageCoverage) {
	for _, q := range a.pages {
		if q != except && q.isolate == isolate {
			q.started = false
		}
	}
}

// takerIn is the open page that takes an isolate's coverage, if one does.
// a.mu is held.
func (a *appCoverage) takerIn(isolate string) *pageCoverage {
	for _, p := range a.pages {
		if !p.closed && p.started && p.isolate == isolate {
			return p
		}
	}
	return nil
}

func (a *appCoverage) closed(p *pageCoverage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p.closed = true
	if p.started {
		p.started = false
		a.stopped(p.isolate, p)
	}
}

// parsed keeps a script a page parsed, if it counts, and fetches its source
// the first time.
func (p *pageCoverage) parsed(ev map[string]any) {
	id, _ := ev["scriptId"].(string)
	u, _ := ev["url"].(string)
	hash, _ := ev["hash"].(string)
	mapURL, _ := ev["sourceMapURL"].(string)
	line, _ := ev["startLine"].(float64)
	column, _ := ev["startColumn"].(float64)
	a := p.app
	// Scripts in a page's HTML (inline scripts, event handlers) start after
	// its first character; a file's scripts at it.
	if u == "" || line != 0 || column != 0 || !a.run.cfg.counts(a.origin, u, mapURL != "") {
		return
	}
	a.mu.Lock()
	p.scripts[id] = &parsed{url: u, hash: hash, seq: a.run.seq.Add(1)}
	a.mu.Unlock()
	key := sourceKey(hash, u)
	if src, ok := a.run.fetch(key); ok {
		a.fetching.Add(1)
		go func() {
			defer a.fetching.Done()
			defer a.run.fetched(key, src)
			a.source(p, id, u, mapURL, src)
		}()
	}
}

// source fetches a script's source and its source map, while its page is
// there.
func (a *appCoverage) source(p *pageCoverage, id, scriptURL, mapURL string, src *source) {
	res, err := p.send("Debugger.getScriptSource", map[string]any{"scriptId": id})
	if err != nil {
		return
	}
	m, _ := res.(map[string]any)
	text, ok := m["scriptSource"].(string)
	if !ok {
		return
	}
	var sourceMap []byte
	var mapErr error
	if mapURL != "" {
		sourceMap, mapErr = a.sourceMap(scriptURL, mapURL)
	}
	a.run.mu.Lock()
	src.text, src.ok, src.sourceMap, src.mapErr = text, true, sourceMap, mapErr
	a.run.mu.Unlock()
}

// sourceMap fetches a script's source map: from its data: URL, or with the
// web app's cookies, relative to the script.
func (a *appCoverage) sourceMap(scriptURL, mapURL string) ([]byte, error) {
	if data, ok := strings.CutPrefix(mapURL, "data:"); ok {
		meta, body, ok := strings.Cut(data, ",")
		if !ok {
			return nil, errors.New("its data: URL has no data")
		}
		if strings.HasSuffix(meta, ";base64") {
			return base64.RawStdEncoding.DecodeString(strings.TrimRight(body, "="))
		}
		s, err := url.PathUnescape(body)
		return []byte(s), err
	}
	base, err := url.Parse(scriptURL)
	if err != nil {
		return nil, err
	}
	ref, err := url.Parse(mapURL)
	if err != nil {
		return nil, err
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return nil, fmt.Errorf("%s is not an address the pack can fetch", abs)
	}
	resp, err := a.ctx.Request().Get(abs.String(), playwright.APIRequestContextGetOptions{Timeout: playwright.Float(30000)})
	if err != nil {
		return nil, fmt.Errorf("%s could not be fetched: %s", abs, firstLine(err))
	}
	defer func() { _ = resp.Dispose() }()
	if !resp.Ok() {
		return nil, fmt.Errorf("%s answered %d", abs, resp.Status())
	}
	return resp.Body()
}

// paused takes the coverage of a page about to leave its document, paused
// by the hook, then lets it go on: any other pause (a debugger statement of
// the app) goes on at once.
func (p *pageCoverage) paused(ev map[string]any) {
	frames, _ := ev["callFrames"].([]any)
	hook := false
	if len(frames) > 0 {
		f, _ := frames[0].(map[string]any)
		hook = f["functionName"] == hookFunction
	}
	go func() {
		if hook {
			done := make(chan struct{})
			go func() {
				defer close(done)
				p.app.takeLeaving(p)
			}()
			select {
			case <-done:
			case <-time.After(leaveWait):
			}
		}
		_, _ = p.cdp.Send("Debugger.resume", nil)
	}()
}

// takeLeaving takes the coverage of a page's isolate, as the page leaves
// its document.
func (a *appCoverage) takeLeaving(p *pageCoverage) {
	id, err := p.locate()
	if err != nil {
		return
	}
	a.mu.Lock()
	t := a.takerIn(id)
	a.mu.Unlock()
	if t != nil {
		t.take()
	}
}

// trigger takes the coverage of the app's pages in the background; the
// channel closes once it is taken.
func (a *appCoverage) trigger() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		a.again = true
		return a.done
	}
	a.running = true
	a.done = make(chan struct{})
	done := a.done
	go func() {
		for {
			a.cycle()
			a.mu.Lock()
			if !a.again {
				a.running = false
				close(done)
				a.mu.Unlock()
				return
			}
			a.again = false
			a.mu.Unlock()
		}
	}()
	return done
}

// pending is the take running in the background, if one is, and how long
// to wait for it: less while a page shows a dialog.
func (a *appCoverage) pending() (<-chan struct{}, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return nil, 0
	}
	wait := settleWait
	for _, p := range a.pages {
		if !p.closed && p.dialog.After(p.answered) {
			wait = blockedWait
		}
	}
	return a.done, wait
}

// cycle takes the coverage of each isolate of the app's pages, where they
// are now, starting it where no page does.
func (a *appCoverage) cycle() {
	a.mu.Lock()
	var open []*pageCoverage
	for _, p := range a.pages {
		if !p.closed {
			open = append(open, p)
		}
	}
	a.mu.Unlock()
	for _, p := range open {
		_, _ = p.locate()
	}
	a.mu.Lock()
	var takers, starting []*pageCoverage
	seen := map[string]bool{}
	for _, p := range open {
		if p.closed || p.isolate == "" || seen[p.isolate] {
			continue
		}
		seen[p.isolate] = true
		if t := a.takerIn(p.isolate); t != nil {
			takers = append(takers, t)
			continue
		}
		p.started = true
		starting = append(starting, p)
	}
	a.mu.Unlock()
	for _, p := range starting {
		if err := p.start(); err != nil {
			a.mu.Lock()
			p.started = false
			a.mu.Unlock()
		}
	}
	for _, t := range takers {
		t.take()
	}
}

// take takes the coverage of the page's isolate.
func (p *pageCoverage) take() {
	res, err := p.send("Profiler.takePreciseCoverage", nil)
	if err != nil {
		if strings.Contains(err.Error(), "not been started") {
			p.app.mu.Lock()
			p.started = false
			p.app.mu.Unlock()
		}
		return
	}
	p.app.record(p, res)
}

// record keeps a take's scripts that count: those the pages of the isolate
// parsed (a script's id is its isolate's).
func (a *appCoverage) record(taker *pageCoverage, res any) {
	b, err := json.Marshal(res)
	if err != nil {
		return
	}
	var out struct {
		Result []struct {
			ScriptID  string          `json:"scriptId"`
			URL       string          `json:"url"`
			Functions json.RawMessage `json:"functions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return
	}
	type found struct {
		key       scriptKey
		functions json.RawMessage
	}
	var keep []found
	a.mu.Lock()
	for _, s := range out.Result {
		if s.URL == "" {
			continue
		}
		var best *parsed
		for _, p := range a.pages {
			if p.isolate != taker.isolate {
				continue
			}
			if ps := p.scripts[s.ScriptID]; ps != nil && ps.url == s.URL && (best == nil || ps.seq > best.seq) {
				best = ps
			}
		}
		if best != nil {
			keep = append(keep, found{scriptKey{s.URL, sourceKey(best.hash, s.URL)}, s.Functions})
		}
	}
	a.mu.Unlock()
	for _, f := range keep {
		a.run.add(f.key, f.functions)
	}
}

// settle takes the pages' last coverage and waits for their sources, before
// they close.
func (a *appCoverage) settle() {
	wait := settleWait
	a.mu.Lock()
	for _, p := range a.pages {
		if !p.closed && p.dialog.After(p.answered) {
			wait = blockedWait
		}
	}
	a.mu.Unlock()
	deadline := time.After(wait)
	select {
	case <-a.trigger():
	case <-deadline:
		return
	}
	fetched := make(chan struct{})
	go func() {
		a.fetching.Wait()
		close(fetched)
	}()
	select {
	case <-fetched:
	case <-deadline:
	}
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(s)
}
