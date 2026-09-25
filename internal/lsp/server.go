// Package lsp is axx's language server (`axx lsp`). Editors start it over
// stdio and get, for feature files: diagnostics for undefined, ambiguous and
// misused steps and for Gherkin syntax errors; completion of step text; step
// documentation on hover; highlighting of step parameters; going to a step's
// definition; and, for the files that steps name (seeds, payloads, schemas,
// logs), links, path completion and a warning when a file is missing. It
// uses the same step registry as `axx validate`, `axx explain` and the MCP
// server, including a project's custom packs.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/fixtures"
	"github.com/nimbusxr/axx/internal/match"
)

// Options configure the server.
type Options struct {
	// Version is reported to the editor.
	Version string
	// Logger receives the server's own log (never stdout, which carries the
	// protocol).
	Logger *slog.Logger
	// Load returns the axx project that contains a feature file's
	// directory. It defaults to finding axx.yaml upward from the directory
	// and loading the project's packs.
	Load func(dir string) Project
	// CacheDir holds reference pages written for go-to-definition; it
	// defaults to the user cache directory.
	CacheDir string
}

// Project is the axx project a feature file belongs to.
type Project struct {
	// Dir is the directory of its axx.yaml (or the file's directory).
	Dir string
	// Registry holds its steps; nil when Err is set.
	Registry *match.Registry
	// Manifests describe its packs, by name (for reference pages).
	Manifests map[string]core.Manifest
	// Roots are the directories, in order, where steps look for the files
	// they name: the `resources` roots, then Dir.
	Roots []string
	// Generated are the files `axx fixtures generate` writes.
	Generated []string
	// Err says why the project's steps could not be loaded.
	Err error
}

// project is Project as the server uses it.
type project struct {
	dir       string
	reg       *match.Registry
	manifests map[string]core.Manifest
	roots     []string
	generated map[string]bool
	err       error
}

type server struct {
	opts        Options
	out         *writer
	log         *slog.Logger
	docs        map[string]*document
	projects    map[string]*project // by the directory of a feature file
	pages       map[string][]string // reference pages written for go-to-definition
	initialized bool
	shutdown    bool
	// watchFiles is set when the editor tells the server about changed
	// files on request, so missing-file warnings follow the files.
	watchFiles bool
}

// errExitWithoutShutdown is returned when the editor exits the server
// without asking it to shut down first.
var errExitWithoutShutdown = errors.New("exit without shutdown")

// Serve runs the language server until the editor exits it or closes in.
func Serve(ctx context.Context, in io.Reader, out io.Writer, opts Options) error {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Load == nil {
		opts.Load = LoadProject
	}
	s := &server{
		opts: opts, out: &writer{w: out}, log: opts.Logger,
		docs: map[string]*document{}, projects: map[string]*project{}, pages: map[string][]string{},
	}
	r := bufio.NewReader(in)
	for ctx.Err() == nil {
		body, err := readMessage(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			s.reply(nil, nil, &rpcError{Code: codeParseError, Message: err.Error()})
			continue
		}
		if req.Method == "" {
			continue // a response; the server sends no requests
		}
		if done, err := s.handle(&req); done {
			return err
		}
	}
	return nil
}

// LoadProject finds the axx.yaml that governs dir and loads its steps.
func LoadProject(dir string) Project {
	cfg, err := config.Load(config.LoadOptions{WorkDir: dir})
	if err != nil {
		return Project{Dir: dir, Err: err}
	}
	e, err := engine.New(engine.Options{Config: cfg})
	if err != nil {
		return Project{Dir: cfg.Dir, Err: err}
	}
	var generated []string
	if m, err := fixtures.LoadManifest(cfg.FixturesDir()); err == nil {
		for _, f := range m.Entries() {
			generated = append(generated, filepath.Join(cfg.FixturesDir(), filepath.FromSlash(f.Path)))
		}
	}
	return Project{
		Dir: cfg.Dir, Registry: e.Registry, Manifests: e.Manifests(),
		Roots: e.ResourceRoots(), Generated: generated,
	}
}

// handle processes one request or notification. It returns true when the
// server must stop.
func (s *server) handle(req *request) (bool, error) {
	if req.Method == "exit" {
		if s.shutdown {
			return true, nil
		}
		return true, errExitWithoutShutdown
	}
	if !s.initialized && req.Method != "initialize" {
		if !req.isNotification() {
			s.reply(req.ID, nil, &rpcError{Code: codeServerNotInitialized, Message: "the server is not initialized"})
		}
		return false, nil
	}
	if s.shutdown && !req.isNotification() {
		s.reply(req.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "the server is shutting down"})
		return false, nil
	}
	switch req.Method {
	case "initialize":
		var p initializeParams
		if s.decode(req, &p) {
			s.initialized = true
			s.watchFiles = p.Capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration
			s.reply(req.ID, s.capabilities(), nil)
		}
	case "initialized":
		if s.watchFiles {
			s.watch()
		}
	case "workspace/didChangeWatchedFiles":
		var p didChangeWatchedFilesParams
		if s.decode(req, &p) {
			s.filesChanged(p)
		}
	case "shutdown":
		s.shutdown = true
		s.reply(req.ID, nil, nil)
	case "textDocument/didOpen":
		var p didOpenParams
		if s.decode(req, &p) {
			s.open(p.TextDocument.URI, p.TextDocument.Version, p.TextDocument.Text)
		}
	case "textDocument/didChange":
		var p didChangeParams
		if s.decode(req, &p) && len(p.ContentChanges) > 0 {
			s.open(p.TextDocument.URI, p.TextDocument.Version, p.ContentChanges[len(p.ContentChanges)-1].Text)
		}
	case "textDocument/didClose":
		var p didCloseParams
		if s.decode(req, &p) {
			delete(s.docs, p.TextDocument.URI)
			s.publish(p.TextDocument.URI, nil, []diagnostic{})
		}
	case "textDocument/completion":
		var p completionParams
		if s.decode(req, &p) {
			list := completionList{IsIncomplete: true, Items: []completionItem{}}
			if d, pr := s.doc(p.TextDocument.URI); d != nil {
				list = d.completionsAt(pr, p.Position, p.Context.TriggerKind == triggerCharacter && p.Context.TriggerCharacter == "/")
			}
			s.reply(req.ID, list, nil)
		}
	case "textDocument/hover":
		var p textDocumentPositionParams
		if s.decode(req, &p) {
			var h *hover
			if d, pr := s.doc(p.TextDocument.URI); d != nil {
				h = d.hoverAt(pr.reg, p.Position)
			}
			s.reply(req.ID, h, nil)
		}
	case "textDocument/definition":
		var p textDocumentPositionParams
		if s.decode(req, &p) {
			locs := []location{}
			if d, pr := s.doc(p.TextDocument.URI); d != nil {
				locs = append(locs, s.definitionAt(d, pr, p.Position)...)
			}
			s.reply(req.ID, locs, nil)
		}
	case "textDocument/documentLink":
		var p documentLinkParams
		if s.decode(req, &p) {
			links := []documentLink{}
			if d, pr := s.doc(p.TextDocument.URI); d != nil {
				links = append(links, d.documentLinks(pr)...)
			}
			s.reply(req.ID, links, nil)
		}
	case "textDocument/semanticTokens/full":
		var p semanticTokensParams
		if s.decode(req, &p) {
			toks := semanticTokens{Data: []uint32{}}
			if d, pr := s.doc(p.TextDocument.URI); d != nil {
				toks.Data = d.tokens(pr.reg)
			}
			s.reply(req.ID, toks, nil)
		}
	default:
		if !req.isNotification() {
			s.reply(req.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "method not supported: " + req.Method})
		}
	}
	return false, nil
}

func (s *server) capabilities() map[string]any {
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding":     "utf-16",
			"textDocumentSync":     map[string]any{"openClose": true, "change": 1},
			"completionProvider":   map[string]any{"triggerCharacters": []string{" ", "/"}},
			"hoverProvider":        true,
			"definitionProvider":   true,
			"documentLinkProvider": map[string]any{"resolveProvider": false},
			"semanticTokensProvider": map[string]any{
				"legend": map[string]any{"tokenTypes": tokenTypes, "tokenModifiers": []string{}},
				"full":   true,
			},
		},
		"serverInfo": map[string]any{"name": "axx", "version": s.opts.Version},
	}
}

// open stores a document's new text and publishes its diagnostics.
func (s *server) open(uri string, version int, text string) {
	d := newDocument(uri, version, text)
	s.docs[uri] = d
	pr := s.projectFor(uri)
	v := version
	s.publish(uri, &v, d.diagnostics(pr))
}

// doc returns an open document and its project.
func (s *server) doc(uri string) (*document, *project) {
	d, ok := s.docs[uri]
	if !ok {
		return nil, nil
	}
	return d, s.projectFor(uri)
}

// projectFor loads (once per directory) the project of a document.
func (s *server) projectFor(uri string) *project {
	dir := ""
	if p := uriToPath(uri); p != "" {
		dir = filepath.Dir(p)
	} else if wd, err := os.Getwd(); err == nil {
		dir = wd
	}
	if pr, ok := s.projects[dir]; ok {
		return pr
	}
	loaded := s.opts.Load(dir)
	pr := &project{dir: loaded.Dir, reg: loaded.Registry, manifests: loaded.Manifests, roots: loaded.Roots, generated: map[string]bool{}, err: loaded.Err}
	for _, g := range loaded.Generated {
		pr.generated[g] = true
	}
	if pr.err != nil {
		s.log.Warn("cannot load the axx project", "dir", dir, "error", pr.err)
	}
	s.projects[dir] = pr
	return pr
}

func (s *server) decode(req *request, v any) bool {
	if err := json.Unmarshal(req.Params, v); err != nil {
		if !req.isNotification() {
			s.reply(req.ID, nil, &rpcError{Code: codeInvalidParams, Message: err.Error()})
		}
		return false
	}
	return true
}

func (s *server) reply(id json.RawMessage, result any, rerr *rpcError) {
	resp := response{JSONRPC: "2.0", ID: id, Error: rerr}
	if id == nil {
		resp.ID = json.RawMessage("null")
	}
	if rerr == nil {
		b, err := json.Marshal(result)
		if err != nil {
			resp.Error = &rpcError{Code: codeInvalidRequest, Message: err.Error()}
		} else {
			resp.Result = b
		}
	}
	if err := s.out.write(resp); err != nil {
		s.log.Warn("cannot write a response", "error", err)
	}
}

func (s *server) publish(uri string, version *int, diags []diagnostic) {
	n := notification{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: publishDiagnosticsParams{URI: uri, Version: version, Diagnostics: diags}}
	if err := s.out.write(n); err != nil {
		s.log.Warn("cannot publish diagnostics", "error", err)
	}
}

// watch asks the editor to report changes to files, so missing-file
// warnings follow the files as they are created and removed.
func (s *server) watch() {
	req := map[string]any{
		"jsonrpc": "2.0", "id": "axx-watch-files", "method": "client/registerCapability",
		"params": map[string]any{"registrations": []map[string]any{{
			"id": "axx-watch-files", "method": "workspace/didChangeWatchedFiles",
			"registerOptions": map[string]any{"watchers": []map[string]any{{"globPattern": "**/*"}}},
		}}},
	}
	if err := s.out.write(req); err != nil {
		s.log.Warn("cannot ask to watch files", "error", err)
	}
}

// filesChanged publishes again the diagnostics of the open documents that
// name a changed file.
func (s *server) filesChanged(p didChangeWatchedFilesParams) {
	for uri, d := range s.docs {
		pr := s.projectFor(uri)
		for _, c := range p.Changes {
			if path := uriToPath(c.URI); path != "" && d.names(pr, path) {
				v := d.version
				s.publish(uri, &v, d.diagnostics(pr))
				break
			}
		}
	}
}
