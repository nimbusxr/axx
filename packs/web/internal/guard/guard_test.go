package guard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// portalRules are a few of the web packs' commands, as rules.json has them.
func portalRules() *Rules {
	return &Rules{
		Commands: map[string][]string{
			"Root.initialize":                {"sdkLanguage"},
			"Browser.newContext":             {"locale", "recordVideo.size.height", "recordVideo.size.width", "timezoneId", "viewport.height", "viewport.width"},
			"Tracing.harStart":               {"options.content", "options.mode"},
			"Frame.goto":                     {"url", "waitUntil"},
			"Frame.click":                    {"selector", "strict"},
			"BrowserContext.createTempFiles": {"items[].lastModifiedMs", "items[].name", "rootDirName"},
			"CDPSession.send":                {"method", "params"},
		},
		DevTools: []string{"Network.emulateNetworkConditions"},
	}
}

// created is a guard that heard Playwright's server create a browser, a
// page's frame and a DevTools session.
func created(t *testing.T) *Guard {
	t.Helper()
	g := New(portalRules(), nil)
	for guid, typ := range map[string]string{"browser@1": "Browser", "frame@7": "Frame", "cdp@9": "CDPSession", "artifact@3": "Artifact", "context@2": "BrowserContext", "tracing@4": "Tracing"} {
		g.fromServer([]byte(`{"guid":"","method":"__create__","params":{"type":"` + typ + `","guid":"` + guid + `","initializer":{}}}`))
	}
	return g
}

func refusal(t *testing.T, msg []byte) string {
	t.Helper()
	var m struct {
		ID    int `json:"id"`
		Error struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		t.Fatalf("the refusal is not an answer: %s", msg)
	}
	return m.Error.Error.Message
}

func TestTheStepsCommandsPass(t *testing.T) {
	g := created(t)
	for _, msg := range []string{
		`{"id":1,"guid":"","method":"initialize","params":{"sdkLanguage":"go"},"metadata":{}}`,
		`{"id":2,"guid":"browser@1","method":"newContext","params":{"locale":"en-GB","timezoneId":"Europe/London","viewport":{"width":1280,"height":720},"userAgent":null}}`,
		`{"id":9,"guid":"tracing@4","method":"harStart","params":{"options":{"content":"attach","mode":"minimal"}}}`,
		`{"id":3,"guid":"frame@7","method":"goto","params":{"url":"http://127.0.0.1:8400/held","waitUntil":"load"}}`,
		`{"id":4,"guid":"frame@7","method":"goto","params":{"url":"https://portal.parcels.example/quote"}}`,
		`{"id":5,"guid":"frame@7","method":"goto","params":{"url":"about:blank"}}`,
		`{"id":6,"guid":"frame@7","method":"click","params":{"selector":"internal:role=button[name=\"Get a quote\"i]","strict":true}}`,
		`{"id":7,"guid":"cdp@9","method":"send","params":{"method":"Network.emulateNetworkConditions","params":{"offline":true}}}`,
		`{"id":8,"guid":"context@2","method":"createTempFiles","params":{"items":[{"name":"invoice.pdf","lastModifiedMs":1},{"name":"labels/px-4101.png"}],"rootDirName":"labels"}}`,
	} {
		pass, refused := g.fromRun([]byte(msg))
		if refused != nil {
			t.Errorf("%s\n  was refused: %s", msg, refusal(t, refused))
		} else if string(pass) != msg {
			t.Errorf("%s\n  reached the server as %s", msg, pass)
		}
	}
}

// What a run in a sandbox must not have the machine do, each a command
// Playwright's server would obey.
func TestWhatReachesTheMachineIsRefused(t *testing.T) {
	g := created(t)
	for name, c := range map[string]struct{ msg, why string }{
		"a file opened as a page": {
			`{"id":1,"guid":"frame@7","method":"goto","params":{"url":"file:///Users/dana/.ssh/id_ed25519"}}`,
			"not file:",
		},
		"a file opened as a page, in capitals": {
			`{"id":1,"guid":"frame@7","method":"goto","params":{"url":"FILE:///etc/hosts"}}`,
			"not file:",
		},
		"the browser's own pages": {
			`{"id":1,"guid":"frame@7","method":"goto","params":{"url":"chrome://settings"}}`,
			"not chrome:",
		},
		"a file's source": {
			`{"id":1,"guid":"frame@7","method":"goto","params":{"url":"view-source:file:///etc/hosts"}}`,
			"not view-source:",
		},
		"the browser's settings, in Firefox": {
			`{"id":1,"guid":"frame@7","method":"goto","params":{"url":"about:config"}}`,
			"not about:config",
		},
		"an upload kept outside the server's folder": {
			`{"id":1,"guid":"context@2","method":"createTempFiles","params":{"items":[{"name":"../../../Users/dana/.zshrc","lastModifiedMs":0}]}}`,
			"is neither",
		},
		"an upload kept at a path of the machine": {
			`{"id":1,"guid":"context@2","method":"createTempFiles","params":{"items":[{"name":"/etc/hosts"}]}}`,
			"is neither",
		},
		"a download saved where the run says": {
			`{"id":1,"guid":"artifact@3","method":"saveAs","params":{"path":"/Users/dana/.zshrc"}}`,
			"Artifact.saveAs: it is not a command",
		},
		"a network of its own for a context": {
			`{"id":1,"guid":"browser@1","method":"newContext","params":{"proxy":{"server":"http://203.0.113.9:8080"}}}`,
			"its proxy.server is not a parameter",
		},
		"a parameter of another shape than the steps'": {
			`{"id":1,"guid":"browser@1","method":"newContext","params":{"viewport":{"width":800,"height":600,"dir":"/tmp"}}}`,
			"its viewport.dir is not a parameter",
		},
		"a recording at a path the run names": {
			`{"id":1,"guid":"browser@1","method":"newContext","params":{"recordHar":{"path":"/Users/dana/Library/x.har"}}}`,
			"its recordHar.path is not a parameter",
		},
		"a file of the machine uploaded": {
			`{"id":1,"guid":"frame@7","method":"setInputFiles","params":{"selector":"input","localPaths":["/Users/dana/.aws/credentials"]}}`,
			"Frame.setInputFiles: it is not a command",
		},
		"a DevTools command that reads files": {
			`{"id":1,"guid":"cdp@9","method":"send","params":{"method":"DOM.setFileInputFiles","params":{"files":["/Users/dana/.aws/credentials"]}}}`,
			"the DevTools command DOM.setFileInputFiles",
		},
		"a DevTools command that says where downloads go": {
			`{"id":1,"guid":"cdp@9","method":"send","params":{"method":"Browser.setDownloadBehavior","params":{"behavior":"allow","downloadPath":"/Users/dana"}}}`,
			"the DevTools command Browser.setDownloadBehavior",
		},
		"a DevTools session of the whole browser": {
			`{"id":1,"guid":"browser@1","method":"newBrowserCDPSession","params":{}}`,
			"Browser.newBrowserCDPSession: it is not a command",
		},
		"the recorder, which writes what it records to a file": {
			`{"id":1,"guid":"context@2","method":"enableRecorder","params":{"outputFile":"/Users/dana/.zshrc"}}`,
			"BrowserContext.enableRecorder: it is not a command",
		},
		"an object the server never made": {
			`{"id":1,"guid":"browser-type@chromium","method":"launch","params":{"executablePath":"/bin/sh"}}`,
			"no object browser-type@chromium",
		},
		"an object the run says the server made": {
			`{"guid":"","method":"__create__","params":{"type":"Frame","guid":"mine"}}`,
			"not one",
		},
		"a message that is no command": {
			`[1,2,3]`,
			"not one",
		},
		"a command with more than a command has": {
			`{"id":1,"guid":"frame@7","method":"click","params":{"selector":"a"},"also":{"method":"goto"}}`,
			"not one",
		},
	} {
		pass, refused := g.fromRun([]byte(c.msg))
		if pass != nil || refused == nil {
			t.Errorf("%s: it reached the server: %s", name, pass)
			continue
		}
		if got := refusal(t, refused); !strings.Contains(got, c.why) {
			t.Errorf("%s: refused with %q, want it to say %q", name, got, c.why)
		}
	}
}

func TestOnlyTheServerSaysWhatAnObjectIs(t *testing.T) {
	g := created(t)
	// A run that sends what the server sends changes nothing of what the
	// guard knows.
	_, _ = g.fromRun([]byte(`{"guid":"","method":"__create__","params":{"type":"Frame","guid":"artifact@3"}}`))
	if _, refused := g.fromRun([]byte(`{"id":2,"guid":"artifact@3","method":"goto","params":{"url":"http://127.0.0.1/"}}`)); refused == nil {
		t.Error("an artifact took a frame's command")
	}
	// An object the server disposed of takes no more commands.
	g.fromServer([]byte(`{"guid":"frame@7","method":"__dispose__","params":{}}`))
	if _, refused := g.fromRun([]byte(`{"id":3,"guid":"frame@7","method":"goto","params":{"url":"http://127.0.0.1/"}}`)); refused == nil {
		t.Error("a frame the server disposed of took a command")
	}
	// An answer of the server that quotes those words is no creation: only
	// its events are (they have no id).
	g.fromServer([]byte(`{"id":4,"result":{"value":"__create__"},"method":"__create__","params":{"type":"Frame","guid":"quoted"}}`))
	if _, refused := g.fromRun([]byte(`{"id":5,"guid":"quoted","method":"goto","params":{"url":"http://127.0.0.1/"}}`)); refused == nil {
		t.Error("an answer of the server created an object")
	}
}

func TestWhatAPageAskedForIsRecordedInTheServersFolder(t *testing.T) {
	g := created(t)
	pass, refused := g.fromRun([]byte(`{"id":1,"guid":"tracing@4","method":"harStart","params":{"options":{"content":"attach","harPath":"/Users/dana/.zshrc","resourcesDir":"/Users/dana"}}}`))
	if refused != nil {
		t.Fatalf("refused: %s", refusal(t, refused))
	}
	if got, want := string(pass), `{"id":1,"guid":"tracing@4","method":"harStart","params":{"options":{"content":"attach"}}}`; got != want {
		t.Errorf("the server got %s, want %s", got, want)
	}
}

func TestAVideoIsKeptInTheServersFolder(t *testing.T) {
	g := created(t)
	pass, refused := g.fromRun([]byte(`{"id":1,"guid":"browser@1","method":"newContext","params":{"locale":"en-GB","recordVideo":{"dir":"/Users/dana/Movies","size":{"width":800,"height":600}}}}`))
	if refused != nil {
		t.Fatalf("refused: %s", refusal(t, refused))
	}
	var c struct {
		Params struct {
			Locale      string         `json:"locale"`
			RecordVideo map[string]any `json:"recordVideo"`
		} `json:"params"`
	}
	if err := json.Unmarshal(pass, &c); err != nil {
		t.Fatal(err)
	}
	if _, named := c.Params.RecordVideo["dir"]; named || c.Params.RecordVideo["size"] == nil || c.Params.Locale != "en-GB" {
		t.Errorf("the server got %s: want the video's size and the rest, and no folder", pass)
	}
}

// The guard between two WebSocket connections, as it stands between a run
// and Playwright's server.
func TestARefusedCommandIsAnsweredAndNeverSent(t *testing.T) {
	got := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"guid":"","method":"__create__","params":{"type":"Frame","guid":"frame@7","initializer":{}}}`))
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			got <- string(msg)
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"id":2,"result":{}}`))
		}
	}))
	defer server.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = run.CloseNow() }()
		up, resp, err := websocket.Dial(r.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			return
		}
		defer func() { _ = up.CloseNow() }()
		_ = New(portalRules(), nil).Relay(r.Context(), run, up)
	}))
	defer front.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http"), nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = run.CloseNow() }()
	read := func() string {
		_, msg, err := run.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return string(msg)
	}
	if first := read(); !strings.Contains(first, "__create__") {
		t.Fatalf("the run did not hear of the frame: %s", first)
	}
	_ = run.Write(ctx, websocket.MessageText, []byte(`{"id":1,"guid":"frame@7","method":"goto","params":{"url":"file:///etc/hosts"}}`))
	if answer := read(); !strings.Contains(answer, `"id":1`) || !strings.Contains(answer, "not file:") {
		t.Errorf("the refused command's answer: %s", answer)
	}
	ok := `{"id":2,"guid":"frame@7","method":"goto","params":{"url":"http://127.0.0.1:8400/"}}`
	_ = run.Write(ctx, websocket.MessageText, []byte(ok))
	if answer := read(); answer != `{"id":2,"result":{}}` {
		t.Errorf("the command's answer: %s", answer)
	}
	select {
	case msg := <-got:
		if msg != ok {
			t.Errorf("the server got %s", msg)
		}
	case <-ctx.Done():
		t.Fatal("the server got no command")
	}
	select {
	case msg := <-got:
		t.Errorf("the server also got %s", msg)
	default:
	}
}

func TestTheRulesNameNoCommandThatReachesTheMachine(t *testing.T) {
	r := Default()
	// What a client that is not on the server's machine never needs, and a
	// run must never have: Playwright's commands with a path, a program or a
	// network of the client's choosing.
	for _, never := range []string{
		"Artifact.saveAs", "Artifact.pathAfterFinished", "BrowserType.launch", "BrowserType.launchPersistentContext",
		"BrowserType.connectOverCDP", "Browser.newBrowserCDPSession", "BrowserContext.enableRecorder",
		"Playwright.newRequest", "Electron.launch", "Android.devices", "LocalUtils.zip", "LocalUtils.connect",
		"BrowserContext.harStart", "Browser.startTracing",
	} {
		if _, ok := r.Commands[never]; ok {
			t.Errorf("rules.json allows %s", never)
		}
	}
	for command, params := range r.Commands {
		for _, p := range params {
			for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '.' || r == '[' || r == ']' }) {
				switch part {
				case "localPaths", "localDirectory", "proxy", "recordHar", "path", "dir", "harPath", "resourcesDir", "executablePath",
					"downloadsPath", "tracesDir", "artifactsDir", "userDataDir", "args", "env", "clientCertificates", "outputFile":
					t.Errorf("rules.json allows %s its %s", command, p)
				}
			}
		}
	}
	for _, d := range r.DevTools {
		for _, domain := range []string{"Browser.", "DOM.setFileInputFiles", "Page.setDownloadBehavior", "Page.navigate", "IO.", "Target.", "Storage.", "FileSystem.", "SystemInfo.", "Tracing.", "Extensions."} {
			if strings.HasPrefix(d, domain) {
				t.Errorf("rules.json allows the DevTools command %s", d)
			}
		}
	}
}
