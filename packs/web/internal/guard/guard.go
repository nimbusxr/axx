// Package guard stands between a run and the browser the axx host started
// for it: it passes Playwright's server only the commands the web packs'
// steps use, and refuses every other one.
//
// The run is an agent's, in a sandbox; the browser and Playwright's server
// are outside it, on the person's machine. Playwright's own protocol lets a
// client have its server save a file at a path the client names, open a
// file as a page, and send the browser raw DevTools commands that read and
// write files. None of that may reach the server. So a command passes only
// when its object's type and its name are in the rules ([Rules]), with no
// parameter the rules do not name, however deep in its parameters it is,
// and what its parameters say is checked where they could reach this
// machine (an address to open, a DevTools command). The rules are the
// commands the web packs' own tests send (rules.json, recorded from them):
// a command or a parameter Playwright adds in a later version is refused
// until someone looked at it.
package guard

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
)

// Rules are the commands a run may send.
type Rules struct {
	// Commands are the commands by "Type.method" (Frame.goto), each with
	// the parameters it may have: their names, and the names within them
	// (viewport.width; storageState.cookies[].name for those of a list's
	// items).
	Commands map[string][]string `json:"commands"`
	// DevTools are the DevTools (CDP) commands a run may send a page
	// through a session.
	DevTools []string `json:"devtools"`
}

//go:embed rules.json
var rulesJSON []byte

// Default are the rules of the web packs: the commands their steps use.
func Default() *Rules {
	var r Rules
	if err := json.Unmarshal(rulesJSON, &r); err != nil {
		panic("guard: rules.json: " + err.Error())
	}
	return &r
}

// recordEnv names, for a test binary only, the file the commands of the
// packs' tests are recorded in, a line each: how rules.json is made (see
// TestTheRulesAreTheCommandsThePacksUse). No other program records: there
// it would pass every command.
const recordEnv = "AXX_WEB_GUARD_RECORD"

var recording sync.Mutex

// ForRun is the guard of a run's connection: the web packs' rules, or, in
// a test binary told to record, one that passes and records every command.
func ForRun(log *slog.Logger) *Guard {
	g := New(Default(), log)
	file := ""
	if testing.Testing() {
		file = os.Getenv(recordEnv)
	}
	if file == "" {
		return g
	}
	g.Record(func(command string, params []string, devtools string) {
		line, _ := json.Marshal(map[string]any{"command": command, "params": params, "devtools": devtools})
		recording.Lock()
		defer recording.Unlock()
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.Write(append(line, '\n'))
	})
	return g
}

// maxMessage bounds a message either way: a page's screenshot, a trace, a
// file a step uploads.
const maxMessage = 512 << 20

// Guard relays one run's connection.
type Guard struct {
	rules *Rules
	log   *slog.Logger
	// record, when set, passes every command and hears each: how the rules
	// are recorded from the packs' tests.
	record func(command string, params []string, devtools string)

	mu    sync.Mutex
	types map[string]string // an object's type, by its guid
}

// New is a guard with rules. A nil logger discards.
func New(rules *Rules, log *slog.Logger) *Guard {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Guard{rules: rules, log: log, types: map[string]string{"": "Root"}}
}

// Record makes the guard pass every command and tell heard each one, with
// its parameters' names and, for a DevTools session's send, the DevTools
// command: what the rules are recorded with.
func (g *Guard) Record(heard func(command string, params []string, devtools string)) {
	g.record = heard
}

// Relay passes messages between the run's connection and Playwright's
// server's until either ends or ctx does.
func (g *Guard) Relay(ctx context.Context, run, server *websocket.Conn) error {
	run.SetReadLimit(maxMessage)
	server.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			typ, msg, err := run.Read(ctx)
			if err != nil {
				cancel(err)
				return
			}
			pass, refusal := g.fromRun(msg)
			if refusal != nil {
				if err := run.Write(ctx, websocket.MessageText, refusal); err != nil {
					cancel(err)
					return
				}
				continue
			}
			if err := server.Write(ctx, typ, pass); err != nil {
				cancel(err)
				return
			}
		}
	})
	wg.Go(func() {
		for {
			typ, msg, err := server.Read(ctx)
			if err != nil {
				cancel(err)
				return
			}
			g.fromServer(msg)
			if err := run.Write(ctx, typ, msg); err != nil {
				cancel(err)
				return
			}
		}
	})
	wg.Wait()
	if err := context.Cause(ctx); err != nil && !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
		return err
	}
	return nil
}

// fromServer keeps what the server says of its objects: the type of each it
// creates, which a command's rules go by. Only the server says so.
func (g *Guard) fromServer(msg []byte) {
	if !bytes.Contains(msg, []byte(`"__create__"`)) && !bytes.Contains(msg, []byte(`"__dispose__"`)) {
		return
	}
	var m struct {
		ID     *int64 `json:"id"`
		GUID   string `json:"guid"`
		Method string `json:"method"`
		Params struct {
			Type string `json:"type"`
			GUID string `json:"guid"`
		} `json:"params"`
	}
	if json.Unmarshal(msg, &m) != nil || m.ID != nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch m.Method {
	case "__create__":
		if m.Params.GUID != "" && m.Params.Type != "" {
			g.types[m.Params.GUID] = m.Params.Type
		}
	case "__dispose__":
		delete(g.types, m.GUID)
	}
}

// call is a command a client sends Playwright's server.
type call struct {
	ID       json.RawMessage            `json:"id"`
	GUID     string                     `json:"guid"`
	Method   string                     `json:"method"`
	Params   map[string]json.RawMessage `json:"params,omitempty"`
	Metadata json.RawMessage            `json:"metadata,omitempty"`
}

// fromRun is the message to pass the server for one of the run's, or, for a
// command the rules refuse, the answer the run gets in the server's place.
func (g *Guard) fromRun(msg []byte) (pass, refusal []byte) {
	var c call
	dec := json.NewDecoder(bytes.NewReader(msg))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil || len(c.ID) == 0 || c.Method == "" {
		// Nothing the server could answer by its number: the run hears it
		// as an error of the connection's first command.
		return nil, refuse(json.RawMessage("0"), "the axx host's browser takes Playwright's commands only, and this message is not one")
	}
	g.mu.Lock()
	typ, known := g.types[c.GUID]
	g.mu.Unlock()
	if !known {
		return nil, refuse(c.ID, "the axx host's browser has no object "+c.GUID)
	}
	command := typ + "." + c.Method
	if g.record != nil {
		devtools := ""
		if command == "CDPSession.send" {
			_ = json.Unmarshal(c.Params["method"], &devtools)
			devtools = "+" + devtools
		}
		// What the rules are made of is what reaches the server.
		changed, _ := g.check(command, c.Params)
		g.record(command, names(c.Params), strings.TrimPrefix(devtools, "+"))
		if changed {
			if out, err := json.Marshal(c); err == nil {
				return out, nil
			}
		}
		return msg, nil
	}
	allowed, ok := g.rules.Commands[command]
	if !ok {
		return nil, g.refused(c.ID, command, "it is not a command the web packs' steps use")
	}
	changed, why := g.check(command, c.Params)
	if why != "" {
		return nil, g.refused(c.ID, command, why)
	}
	for _, n := range names(c.Params) {
		if !slices.Contains(allowed, n) {
			return nil, g.refused(c.ID, command, "its "+n+" is not a parameter the web packs' steps use")
		}
	}
	if !changed {
		return msg, nil
	}
	out, err := json.Marshal(c)
	if err != nil {
		return nil, g.refused(c.ID, command, err.Error())
	}
	return out, nil
}

// values are the parameters that hold a value for the page (an argument of
// a script, what a check expects) or for the browser's DevTools (a
// command's own parameters, whose command the rules name): data, of any
// shape, that Playwright's server passes on and never reads as a name.
var values = []string{"arg", "result", "expectedValue", "params"}

// names are the names in a command's parameters, sorted: each parameter's,
// and those within it, to any depth (viewport.width), with [] for a list's
// items (storageState.cookies[].name). A parameter that is a value for the
// page is named and not looked into, and one that is null or empty has
// nothing to name.
func names(params map[string]json.RawMessage) []string {
	seen := map[string]bool{}
	var walk func(path string, raw json.RawMessage)
	walk = func(path string, raw json.RawMessage) {
		raw = bytes.TrimSpace(raw)
		switch {
		case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
		case raw[0] == '{':
			var obj map[string]json.RawMessage
			if json.Unmarshal(raw, &obj) != nil {
				seen[path] = true
				return
			}
			for k, v := range obj {
				walk(path+"."+k, v)
			}
		case raw[0] == '[':
			var list []json.RawMessage
			if json.Unmarshal(raw, &list) != nil {
				seen[path] = true
				return
			}
			for _, v := range list {
				walk(path+"[]", v)
			}
		default:
			seen[path] = true
		}
	}
	for k, v := range params {
		if slices.Contains(values, k) {
			if raw := bytes.TrimSpace(v); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
				seen[k] = true
			}
			continue
		}
		walk(k, v)
	}
	return slices.Sorted(maps.Keys(seen))
}

// pageSchemes are the addresses a page may be sent to: sites, and what a
// page holds itself. Never a file of this machine, nor the browser's own
// pages.
var pageSchemes = []string{"http", "https", "about", "data", "blob"}

// check looks at what a command's parameters say, where they could reach
// this machine. It reports whether it changed them, and why it refuses the
// command, if it does.
func (g *Guard) check(command string, params map[string]json.RawMessage) (changed bool, why string) {
	switch command {
	case "Frame.goto":
		var address string
		if err := json.Unmarshal(params["url"], &address); err != nil {
			return false, "its url is not text"
		}
		if why := pageAddress(address); why != "" {
			return false, why
		}
	case "Browser.newContext", "Browser.newContextForReuse":
		// A video is kept in the server's own folder, and the run takes it
		// from there: never in a folder the run names.
		if raw, ok := params["recordVideo"]; ok {
			var video map[string]json.RawMessage
			if err := json.Unmarshal(raw, &video); err != nil {
				return false, "its recordVideo is not an object"
			}
			if _, named := video["dir"]; named {
				delete(video, "dir")
				params["recordVideo"], _ = json.Marshal(video)
				changed = true
			}
		}
	case "Tracing.harStart":
		// What a page asked for is recorded in the server's own folder, and
		// the run takes it from there: never at a path the run names.
		if raw, ok := params["options"]; ok {
			var options map[string]json.RawMessage
			if err := json.Unmarshal(raw, &options); err != nil {
				return false, "its options are not an object"
			}
			for _, path := range []string{"harPath", "resourcesDir"} {
				if _, named := options[path]; named {
					delete(options, path)
					changed = true
				}
			}
			if changed {
				params["options"], _ = json.Marshal(options)
			}
		}
	case "BrowserContext.createTempFiles":
		var items []struct {
			Name string `json:"name"`
		}
		if raw, ok := params["items"]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return false, "its items are not files"
			}
		}
		for _, it := range items {
			if why := uploadName(it.Name); why != "" {
				return false, why
			}
		}
		if raw, ok := params["rootDirName"]; ok {
			var root string
			if err := json.Unmarshal(raw, &root); err != nil || root == "" || strings.ContainsAny(root, `/\`) || root == "." || root == ".." {
				return false, "the folder a scenario uploads has a name, not a path"
			}
		}
	case "CDPSession.send":
		var method string
		if err := json.Unmarshal(params["method"], &method); err != nil {
			return false, "its method is not text"
		}
		if !slices.Contains(g.rules.DevTools, method) {
			return false, "the DevTools command " + method + " is not one the web packs' steps use"
		}
	}
	return changed, ""
}

// pageAddress says why a page may not be sent to an address, "" when it may.
func pageAddress(address string) string {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return "its url is not an address"
	}
	scheme := strings.ToLower(u.Scheme)
	if !slices.Contains(pageSchemes, scheme) {
		return fmt.Sprintf("a page opens http and https addresses there, not %s: a browser outside the sandbox does not open this machine's files or its own pages", u.Scheme+":")
	}
	// about:blank is an empty page; a browser's other about: pages are its
	// own (its settings, in Firefox).
	if scheme == "about" && !strings.EqualFold(u.Opaque, "blank") {
		return "a page opens http and https addresses there, not " + address + ": a browser outside the sandbox does not open its own pages"
	}
	return ""
}

// uploadName says why a file a run uploads may not have a name, "" when it
// may: the server keeps the file under that name in a folder of its own,
// and nowhere else.
func uploadName(name string) string {
	if name == "" || strings.ContainsRune(name, 0) || !filepath.IsLocal(filepath.FromSlash(strings.ReplaceAll(name, `\`, "/"))) {
		return "a file a scenario uploads has a name, or a path in the folder it is uploaded with: " + fmt.Sprintf("%q", name) + " is neither"
	}
	return ""
}

func (g *Guard) refused(id json.RawMessage, command, why string) []byte {
	g.log.Warn("refused a run's command to the browser", "command", command, "why", why)
	return refuse(id, fmt.Sprintf("the axx host's browser refused %s: %s. A run in a sandbox drives a browser the axx host started, which takes the commands of the web packs' steps and no other", command, why))
}

// refuse is Playwright's answer to command id with an error.
func refuse(id json.RawMessage, message string) []byte {
	out, _ := json.Marshal(map[string]any{
		"id": id,
		"error": map[string]any{"error": map[string]any{
			"name": "Error", "message": message, "stack": "",
		}},
	})
	return out
}
