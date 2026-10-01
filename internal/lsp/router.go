package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/packset"
)

// The router is `axx lsp` as editors start it: it serves the editor through
// one server per axx project, each `axx lsp --project` started in the
// project's directory, which prepares an axx with that project's packs. So
// every project in the editor gets its own steps, whatever packs it lists.
// When what a project's steps come from changes (its axx.yaml, its
// axx-packs.yaml, the code of a pack of its own), the router starts the
// project's server again, and moves the project's documents to it once it
// is ready; until then, the server before it answers.

// Backend is a project's server, started by RouteOptions.Start: the router
// writes to In and reads Out.
type Backend struct {
	In  io.WriteCloser
	Out io.ReadCloser
	// Wait waits for the server to end, and says why when it failed.
	Wait func() error
	// Stop ends the server at once.
	Stop func()
}

// RouteOptions configure the router.
type RouteOptions struct {
	// Version is reported to the editor.
	Version string
	// Logger receives the router's own log.
	Logger *slog.Logger
	// Start starts the server of the project in dir. For feature files in
	// no axx project, dir is the directory of the first of them.
	Start func(dir string) (*Backend, error)
}

// The ids of the router's own requests to project servers.
const (
	initID     = `"axx-router-initialize"`
	shutdownID = `"axx-router-shutdown"`
)

type router struct {
	opts RouteOptions
	out  *queue // to the editor, which the router never waits on
	log  *slog.Logger

	mu          sync.Mutex
	initParams  json.RawMessage
	watchFiles  bool
	initialized bool
	shutdown    bool
	docs        map[string]*routedDoc // open documents, by URI
	routes      map[string]*route     // by project directory ("" for no project)
	keys        map[string]string     // the project directory of a feature file's directory
}

// routedDoc is an open document, as the editor last sent it.
type routedDoc struct {
	languageID string
	version    int
	text       string
	project    string // the project directory it was opened in
}

// route is a project's server, and the one started to replace it.
type route struct {
	dir     string
	current *conn
	next    *conn
	failed  error // why the last server could not start
}

// conn is a running project server.
type conn struct {
	r       *route
	b       *Backend
	q       *queue
	ready   bool            // it answered initialize
	retired bool            // a newer server replaced it
	pending map[string]bool // the editor's requests it has not answered
}

// Route runs the router until the editor exits it or closes in.
func Route(ctx context.Context, in io.Reader, out io.Writer, opts RouteOptions) error {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	r := &router{
		opts: opts, out: newQueue(), log: opts.Logger,
		docs: map[string]*routedDoc{}, routes: map[string]*route{}, keys: map[string]string{},
	}
	go r.out.run(&writer{w: out})
	defer r.stopAll()
	br := bufio.NewReader(in)
	for ctx.Err() == nil {
		body, err := readMessage(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			r.reply(nil, nil, &rpcError{Code: codeParseError, Message: err.Error()})
			continue
		}
		if req.Method == "" {
			continue // the editor's answer to the router's own request
		}
		if done, err := r.handle(&req, body); done {
			return err
		}
	}
	return nil
}

// handle processes one message from the editor. It returns true when the
// router must stop.
func (r *router) handle(req *request, body []byte) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.Method == "exit" {
		if r.shutdown {
			return true, nil
		}
		return true, errExitWithoutShutdown
	}
	if !r.initialized && req.Method != "initialize" {
		if !req.isNotification() {
			r.reply(req.ID, nil, &rpcError{Code: codeServerNotInitialized, Message: "the server is not initialized"})
		}
		return false, nil
	}
	if r.shutdown && !req.isNotification() {
		r.reply(req.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "the server is shutting down"})
		return false, nil
	}
	switch req.Method {
	case "initialize":
		var p initializeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			r.reply(req.ID, nil, &rpcError{Code: codeInvalidParams, Message: err.Error()})
			return false, nil
		}
		r.initialized = true
		r.initParams = req.Params
		r.watchFiles = p.Capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration
		r.reply(req.ID, capabilities(r.opts.Version), nil)
	case "initialized":
		if r.watchFiles {
			r.out.push(watchRequest())
		}
	case "shutdown":
		r.shutdown = true
		r.reply(req.ID, nil, nil)
	case "workspace/didChangeWatchedFiles":
		var p didChangeWatchedFilesParams
		if err := json.Unmarshal(req.Params, &p); err == nil {
			r.filesChanged(p)
		}
		r.broadcast(body)
	case "$/cancelRequest":
		r.broadcast(body)
	default:
		r.document(req, body)
	}
	return false, nil
}

// document routes a message about a document to its project's server, and
// keeps the open documents' text for a server that starts later.
func (r *router) document(req *request, body []byte) {
	var p struct {
		TextDocument struct {
			URI        string `json:"uri"`
			LanguageID string `json:"languageId"`
			Version    int    `json:"version"`
			Text       string `json:"text"`
		} `json:"textDocument"`
		ContentChanges []contentChange `json:"contentChanges"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.TextDocument.URI == "" {
		if !req.isNotification() {
			r.reply(req.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "method not supported: " + req.Method})
		}
		return
	}
	uri := p.TextDocument.URI
	dir := r.projectOf(uri)
	switch req.Method {
	case "textDocument/didOpen":
		r.docs[uri] = &routedDoc{languageID: p.TextDocument.LanguageID, version: p.TextDocument.Version, text: p.TextDocument.Text, project: dir}
	case "textDocument/didChange":
		if d := r.docs[uri]; d != nil && len(p.ContentChanges) > 0 {
			d.version, d.text = p.TextDocument.Version, p.ContentChanges[len(p.ContentChanges)-1].Text
		}
	case "textDocument/didClose":
		delete(r.docs, uri)
	}
	rt := r.routeFor(dir)
	c := rt.current
	if c == nil {
		// The project's server could not start: say why on the document,
		// and answer for it.
		if req.Method == "textDocument/didOpen" || req.Method == "textDocument/didChange" {
			r.publishFailure(uri, rt.failed)
		}
		if !req.isNotification() {
			r.reply(req.ID, nil, nil)
		}
		return
	}
	if !req.isNotification() {
		c.pending[string(req.ID)] = true
	}
	c.q.push(json.RawMessage(body))
}

// routeFor returns the route of a project, starting its server the first
// time.
func (r *router) routeFor(dir string) *route {
	rt, ok := r.routes[dir]
	if !ok {
		rt = &route{dir: dir}
		r.routes[dir] = rt
		r.start(rt, false)
	}
	return rt
}

// start starts a server for a route: its first, or, with replace, one to
// replace the current one once it is ready.
func (r *router) start(rt *route, replace bool) {
	dir := rt.dir
	if dir == "" {
		dir = r.looseDir()
	}
	b, err := r.opts.Start(dir)
	if err != nil {
		r.log.Warn("cannot start the project's language server", "dir", dir, "error", err)
		if replace {
			r.showFailure(rt.dir, err)
		} else {
			rt.current, rt.failed = nil, err
		}
		return
	}
	c := &conn{r: rt, b: b, q: newQueue(), pending: map[string]bool{}}
	go c.q.run(&writer{w: b.In})
	c.q.push(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(initID), "method": "initialize", "params": r.initParams})
	c.q.push(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	if replace && rt.current != nil {
		if rt.next != nil {
			r.retire(rt.next)
		}
		rt.next = c
	} else {
		rt.current, rt.failed = c, nil
	}
	go r.read(c)
}

// looseDir is where the server of the feature files in no axx project
// starts: the directory of the first of them.
func (r *router) looseDir() string {
	for uri, d := range r.docs {
		if d.project == "" {
			if p := uriToPath(uri); p != "" {
				return filepath.Dir(p)
			}
		}
	}
	return ""
}

// read forwards what a project server sends to the editor, until it ends.
func (r *router) read(c *conn) {
	br := bufio.NewReader(c.b.Out)
	for {
		body, err := readMessage(br)
		if err != nil {
			break
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		r.received(c, m.ID, m.Method, body)
	}
	err := c.b.Wait()
	r.ended(c, err)
}

// received handles one message from a project server.
func (r *router) received(c *conn, id json.RawMessage, method string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case method != "" && len(id) > 0:
		// The server's own request to the editor, like watching files:
		// the router does that for every server.
		c.q.push(map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
	case method != "":
		if c.retired && method == "textDocument/publishDiagnostics" {
			return // the server that replaced it publishes them
		}
		r.forward(body)
	case string(id) == initID:
		c.ready = true
		if c.r.next == c {
			r.promote(c.r)
		}
	case string(id) == shutdownID:
	default:
		delete(c.pending, string(id))
		r.forward(body)
	}
}

// promote moves a project's documents to the server started to replace the
// current one, which is ready, and stops the current one.
func (r *router) promote(rt *route) {
	old, c := rt.current, rt.next
	rt.current, rt.next, rt.failed = c, nil, nil
	for uri, d := range r.docs {
		if d.project == rt.dir {
			c.q.push(didOpen(uri, d))
		}
	}
	if old != nil {
		r.retire(old)
	}
}

// retire shuts a server down once it has answered what it was asked.
func (r *router) retire(c *conn) {
	c.retired = true
	c.q.push(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(shutdownID), "method": "shutdown"})
	c.q.push(map[string]any{"jsonrpc": "2.0", "method": "exit"})
}

// ended handles a project server that stopped.
func (r *router) ended(c *conn, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c.q.close()
	for id := range c.pending {
		r.reply(json.RawMessage(id), nil, nil)
	}
	c.pending = map[string]bool{}
	if c.retired {
		return
	}
	rt := c.r
	if err == nil {
		err = errors.New("the language server stopped")
	}
	r.log.Warn("the project's language server stopped", "dir", rt.dir, "error", err)
	switch c {
	case rt.next:
		// Keep the server before it, and say why on its documents.
		rt.next = nil
		r.showFailure(rt.dir, err)
	case rt.current:
		rt.current, rt.failed = nil, err
		for uri, d := range r.docs {
			if d.project == rt.dir {
				r.publishFailure(uri, err)
			}
		}
	}
}

// filesChanged starts again the servers of the projects whose steps the
// changed files come from, and moves documents to a project created or
// removed around them.
func (r *router) filesChanged(p didChangeWatchedFilesParams) {
	restart := map[string]bool{}
	moved := false
	for _, ch := range p.Changes {
		path := uriToPath(ch.URI)
		if path == "" {
			continue
		}
		if isConfigFile(filepath.Base(path)) {
			r.keys = map[string]string{}
			moved = true
		}
		for dir := range r.routes {
			if dir != "" && stepsComeFrom(dir, path) {
				restart[dir] = true
			}
		}
	}
	if moved {
		r.reroute()
	}
	for dir := range restart {
		rt := r.routes[dir]
		if rt == nil {
			continue
		}
		r.log.Info("starting the project's language server again", "dir", dir)
		if rt.current != nil {
			r.start(rt, true)
			continue
		}
		// A project whose server could not start: start one, with the
		// project's documents.
		r.start(rt, false)
		if rt.current != nil {
			for uri, d := range r.docs {
				if d.project == dir {
					rt.current.q.push(didOpen(uri, d))
				}
			}
		}
	}
}

// reroute moves open documents whose project changed to the new project's
// server.
func (r *router) reroute() {
	for uri, d := range r.docs {
		dir := r.projectOf(uri)
		if dir == d.project {
			continue
		}
		if old := r.routes[d.project]; old != nil && old.current != nil {
			old.current.q.push(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{"textDocument": map[string]any{"uri": uri}}})
		}
		d.project = dir
		if c := r.routeFor(dir).current; c != nil {
			c.q.push(didOpen(uri, d))
		}
	}
}

// projectOf returns the directory of the axx project of a document: the
// axx.yaml found upward from it, as every axx command finds it, or "" when
// there is none.
func (r *router) projectOf(uri string) string {
	path := uriToPath(uri)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	if p, ok := r.keys[dir]; ok {
		return p
	}
	p := ""
	for d := dir; ; {
		if hasConfig(d) {
			p = d
			break
		}
		parent := filepath.Dir(d)
		if exists(filepath.Join(d, ".git")) || parent == d {
			break
		}
		d = parent
	}
	r.keys[dir] = p
	return p
}

// stepsComeFrom reports whether a project's steps come from a file: its
// configuration, its axx-packs.yaml, or the Go code of a pack of its own.
func stepsComeFrom(dir, path string) bool {
	if filepath.Dir(path) == dir {
		name := filepath.Base(path)
		if isConfigFile(name) || name == "axx.local.yaml" || name == packset.FileName {
			return true
		}
	}
	name := filepath.Base(path)
	if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" {
		return false
	}
	f, found, err := packset.Load(dir)
	if err != nil || !found {
		return false
	}
	entries, _ := f.Entries()
	for _, e := range entries {
		if e.Kind != packset.Local {
			continue
		}
		root := filepath.Join(dir, filepath.FromSlash(e.Path))
		if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func isConfigFile(name string) bool {
	for _, n := range config.FileNames {
		if name == n {
			return true
		}
	}
	return false
}

// broadcast sends a notification to every running project server.
func (r *router) broadcast(body []byte) {
	for _, rt := range r.routes {
		for _, c := range []*conn{rt.current, rt.next} {
			if c != nil {
				c.q.push(json.RawMessage(body))
			}
		}
	}
}

// publishFailure says on a document why its project's server could not
// start.
func (r *router) publishFailure(uri string, err error) {
	if err == nil {
		return
	}
	d := r.docs[uri]
	var lines []string
	if d != nil {
		lines = splitLines(d.text)
	}
	diags := []diagnostic{{
		Range: wholeLine(lines, 0), Severity: severityWarning, Source: "axx",
		Message: "axx cannot load this project's steps: " + err.Error(),
	}}
	r.forwardValue(notification{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: publishDiagnosticsParams{URI: uri, Diagnostics: diags}})
}

// showFailure tells the editor that a project's server could not start
// again, and keeps the one before it.
func (r *router) showFailure(dir string, err error) {
	r.forwardValue(notification{JSONRPC: "2.0", Method: "window/showMessage", Params: map[string]any{
		"type":    1,
		"message": "axx cannot load the steps of " + dir + " again, and keeps the steps it had: " + err.Error(),
	}})
}

func (r *router) forward(body []byte) { r.out.push(json.RawMessage(body)) }

func (r *router) forwardValue(v any) { r.out.push(v) }

func (r *router) reply(id json.RawMessage, result any, rerr *rpcError) {
	resp := response{JSONRPC: "2.0", ID: id, Error: rerr}
	if rerr == nil {
		b, err := json.Marshal(result)
		if err != nil {
			b = []byte("null")
		}
		resp.Result = b
	}
	if len(resp.ID) == 0 {
		resp.ID = json.RawMessage("null")
	}
	r.forwardValue(resp)
}

// stopAll stops every project server: politely, then for good.
func (r *router) stopAll() {
	r.mu.Lock()
	var all []*conn
	for _, rt := range r.routes {
		for _, c := range []*conn{rt.current, rt.next} {
			if c != nil {
				r.retire(c)
				all = append(all, c)
			}
		}
	}
	r.mu.Unlock()
	deadline := time.After(3 * time.Second)
	for _, c := range all {
		select {
		case <-c.q.done:
		case <-deadline:
		}
		c.b.Stop()
	}
	r.out.close()
	select {
	case <-r.out.done:
	case <-deadline:
	}
}

func didOpen(uri string, d *routedDoc) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": d.languageID, "version": d.version, "text": d.text},
	}}
}

// queue holds what is sent to a project server, which may still be
// preparing its packs and reading nothing, so the router never waits on it.
type queue struct {
	mu     sync.Mutex
	items  []any
	wake   chan struct{}
	closed bool
	done   chan struct{} // closed when the queue has stopped writing
}

func newQueue() *queue {
	return &queue{wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (q *queue) push(v any) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.items = append(q.items, v)
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.wake)
	}
}

// run writes what is pushed, in order, until the queue is closed or a write
// fails. It closes the server's input after an exit.
func (q *queue) run(w *writer) {
	defer close(q.done)
	for {
		q.mu.Lock()
		items := q.items
		q.items = nil
		q.mu.Unlock()
		for _, v := range items {
			if err := w.write(v); err != nil {
				return
			}
			if m, ok := v.(map[string]any); ok && m["method"] == "exit" {
				if c, ok := w.w.(io.Closer); ok {
					_ = c.Close()
				}
				return
			}
		}
		if _, ok := <-q.wake; !ok {
			q.mu.Lock()
			rest := q.items
			q.mu.Unlock()
			if len(rest) == 0 {
				return
			}
		}
	}
}
