//go:build integration

package webcore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"

	"github.com/nimbusxr/axx/core"
)

// Shops sign in to the portal through Dex (dexidp.io), an OpenID Connect
// provider, going through its pages in the browser as people do.

const (
	dexImage     = "ghcr.io/dexidp/dex:v2.45.1"
	portalClient = "parcels-portal"
	portalSecret = "portal-client-secret"
)

// The shops Dex knows. Their passwords are in the environment, as they are
// in a run.
var dexShops = []struct{ Email, Name, ID, Env, Password string }{
	{"orders@fjord-outdoor.example", "Fjord Outdoor", "fjord-outdoor", "FJORD_OUTDOOR_PASSWORD", `Fjord & "north" 2026/ü`},
	{"orders@maple-crafts.example", "Maple Crafts", "maple-crafts", "MAPLE_CRAFTS_PASSWORD", "maple-leaf-0417"},
}

var dexConfig = template.Must(template.New("dex").Parse(`issuer: {{.Issuer}}
storage:
  type: memory
web:
  http: 0.0.0.0:5556
staticClients:
  - id: ` + portalClient + `
    name: Parcels portal
    secret: ` + portalSecret + `
    redirectURIs: ['{{.Redirect}}']
enablePasswordDB: true
staticPasswords:
{{- range .Shops}}
  - email: {{.Email}}
    hash: '{{.Hash}}'
    username: {{.Name}}
    userID: {{.ID}}
{{- end}}
connectors:
  - type: mockCallback
    id: mock
    name: Example
`))

var dex struct {
	once sync.Once
	// issuer is Dex's address, on a port of the host: the browsers use it,
	// and so does the portal.
	issuer string
	err    error
}

// withDex starts Dex once, and sets the shops' passwords in the
// environment.
func withDex(t *testing.T) {
	t.Helper()
	environment(t)
	dex.once.Do(func() { dex.issuer, dex.err = startDex() })
	if dex.err != nil {
		t.Skipf("no Dex: %v", dex.err)
	}
	for _, s := range dexShops {
		t.Setenv(s.Env, s.Password)
	}
}

func startDex() (string, error) {
	// Dex is at the address it announces: a port of the host, fixed before
	// it starts.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	issuer := fmt.Sprintf("http://127.0.0.1:%d/dex", port)
	type shop struct{ Email, Name, ID, Hash string }
	var shops []shop
	for _, s := range dexShops {
		hash, err := bcrypt.GenerateFromPassword([]byte(s.Password), bcrypt.DefaultCost)
		if err != nil {
			return "", err
		}
		shops = append(shops, shop{s.Email, s.Name, s.ID, string(hash)})
	}
	var cfg strings.Builder
	if err := dexConfig.Execute(&cfg, map[string]any{"Issuer": issuer, "Redirect": callbackURL(), "Shops": shops}); err != nil {
		return "", err
	}
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        dexImage,
			Cmd:          []string{"dex", "serve", "/etc/dex/axx.yaml"},
			Files:        []testcontainers.ContainerFile{{Reader: strings.NewReader(cfg.String()), ContainerFilePath: "/etc/dex/axx.yaml", FileMode: 0o644}},
			ExposedPorts: []string{"5556/tcp"},
			// At the address it announces, on this machine only.
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.PortBindings = network.PortMap{network.MustParsePort("5556/tcp"): {
					{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: strconv.Itoa(port)},
				}}
			},
			WaitingFor: wait.ForHTTP("/dex/.well-known/openid-configuration").WithPort("5556/tcp").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if c != nil {
		env.cleanup = append(env.cleanup, func() { _ = c.Terminate(ctx) })
	}
	if err != nil {
		return "", fmt.Errorf("cannot start %s: %w", dexImage, err)
	}
	return issuer, nil
}

var dexClient = &http.Client{Timeout: 10 * time.Second}

func callbackURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", env.port)
}

func (p *portal) account(w http.ResponseWriter, r *http.Request) {
	who, link := "Not signed in", `<a href="/login">Sign in</a>`
	if c, err := r.Cookie("session"); err == nil {
		p.mu.Lock()
		s, ok := p.sessions[c.Value]
		p.mu.Unlock()
		if ok {
			who, link = "Signed in as "+s, `<a href="/logout">Sign out</a>`
		}
	} else if c, err := r.Cookie("shop"); err == nil {
		who = "Signed in as " + c.Value
	}
	page(w, "Account", "<p>"+html.EscapeString(who)+"</p>"+link)
}

// login sends the browser to Dex, which sends it back to callback.
func (p *portal) login(w http.ResponseWriter, r *http.Request) {
	state := token()
	http.SetCookie(w, &http.Cookie{Name: "state", Value: state, Path: "/", HttpOnly: true})
	q := url.Values{
		"client_id": {portalClient}, "redirect_uri": {callbackURL()}, "response_type": {"code"},
		"scope": {"openid email profile"}, "state": {state},
	}
	http.Redirect(w, r, dex.issuer+"/auth?"+q.Encode(), http.StatusFound)
}

func (p *portal) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if c, err := r.Cookie("state"); err != nil || c.Value != q.Get("state") {
		http.Error(w, "the sign-in does not belong to this browser", http.StatusBadRequest)
		return
	}
	who, err := exchange(r.Context(), q.Get("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	id := token()
	p.mu.Lock()
	p.sessions[id] = who
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "session", Value: id, Path: "/", HttpOnly: true})
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (p *portal) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		p.mu.Lock()
		delete(p.sessions, c.Value)
		p.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// exchange trades the code Dex gave the browser for the shop's identity.
func exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callbackURL()}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dex.issuer+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(portalClient, portalSecret)
	resp, err := dexClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tok struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.IDToken == "" {
		return "", fmt.Errorf("no ID token from Dex (%s): %v", tok.Error, err)
	}
	// The token comes straight from Dex, so its claims are read without
	// checking its signature.
	parts := strings.Split(tok.IDToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("the ID token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct{ Name, Email string }
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (%s)", claims.Name, claims.Email), nil
}

func token() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// A shop signs in through every page Dex shows: the choice of how to sign
// in, the password form and the grant of access. Dex keeps no session of
// its own, so after signing out, signing in asks for the password again.
func TestSigningInThroughDex(t *testing.T) {
	for _, engine := range bundled {
		t.Run(engine, func(t *testing.T) {
			withDex(t)
			h, app := newHarness(t, engine)
			h.OK("the portal web app with the following properties:", app)
			h.OK(`the "/account" page is opened`)
			h.OK(`the page shows "Not signed in"`)
			h.OK(`the "Sign in" link is clicked`)
			h.OK(`the page shows "Log in to dex"`)
			h.OK(`the "Log in with Email" button is clicked`)
			h.OK(`the page shows "Log in to Your Account"`)
			h.OK(`the "Email Address" field is filled with "orders@fjord-outdoor.example"`)
			h.OK(`the "Password" field is filled with "${env:FJORD_OUTDOOR_PASSWORD}"`)
			h.OK(`the "Login" button is clicked`)
			h.OK(`the page shows "Grant Access"`)
			h.OK(`the page shows "Parcels portal would like to:"`)
			h.OK(`the "Grant Access" button is clicked`)
			h.OK(`the "/account" page is shown`)
			h.OK(`the page shows "Signed in as Fjord Outdoor (orders@fjord-outdoor.example)"`)

			h.OK(`the "Sign out" link is clicked`)
			h.OK(`the page shows "Not signed in"`)
			h.OK(`the "Sign in" link is clicked`)
			h.OK(`the "Log in with Email" button is clicked`)
			h.OK(`the "Password" field has the value ""`)
			h.OK(`the "Email Address" field is filled with "orders@maple-crafts.example"`)
			h.OK(`the "Password" field is filled with "${env:MAPLE_CRAFTS_PASSWORD}"`)
			h.OK(`the "Login" button is clicked`)
			h.OK(`the "Grant Access" button is clicked`)
			h.OK(`the page shows "Signed in as Maple Crafts (orders@maple-crafts.example)"`)
			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAWrongPasswordIsTurnedDown(t *testing.T) {
	withDex(t)
	t.Setenv("STALE_PASSWORD", "maple-leaf-2025")
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/login" page is opened`)
	h.OK(`the "Log in with Email" button is clicked`)
	h.OK(`the "Email Address" field is filled with "orders@maple-crafts.example"`)
	h.OK(`the "Password" field is filled with "${env:STALE_PASSWORD}"`)
	h.OK(`the "Login" button is clicked`)
	h.OK(`the page shows "Invalid Email Address and password."`)
	h.OK(`the page does not show "Grant Access"`)
	h.OK(`the "Email Address" field has the value "orders@maple-crafts.example"`)
	h.OK(`the "Password" field is filled with "${env:MAPLE_CRAFTS_PASSWORD}"`)
	h.OK(`the "Login" button is clicked`)
	h.OK(`the "Grant Access" button is clicked`)
	h.OK(`the page shows "Signed in as Maple Crafts (orders@maple-crafts.example)"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Dex's mock connector signs in a fixed test identity without a form.
func TestTheMockConnectorSignsInStraightAway(t *testing.T) {
	withDex(t)
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/login" page is opened`)
	h.OK(`the "Log in with Example" button is clicked`)
	h.OK(`the "Cancel" button is clicked`)
	h.OK(`the page shows "Approval rejected."`)
	h.OK(`the "/login" page is opened`)
	h.OK(`the "Log in with Example" button is clicked`)
	h.OK(`the "Grant Access" button is clicked`)
	h.OK(`the page shows "Signed in as Kilgore Trout (kilgore@kilgore.trout)"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Two shops signed in at the same time, in scenarios side by side, stay
// apart.
func TestShopsSignedInSideBySide(t *testing.T) {
	withDex(t)
	h, app := newHarness(t, "chromium")
	var wg sync.WaitGroup
	errs := make([]error, len(dexShops))
	for i, shop := range dexShops {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: shop.ID, Name: "sign in"}, h.Suite, h.Sink)
			s := h.In(sc)
			if err := s.Step("the portal web app with the following properties:", app); err != nil {
				errs[i] = err
				return
			}
			for _, step := range []string{
				`the "/login" page is opened`,
				`the "Log in with Email" button is clicked`,
				`the "Email Address" field is filled with "` + shop.Email + `"`,
				`the "Password" field is filled with "${env:` + shop.Env + `}"`,
				`the "Login" button is clicked`,
				`the "Grant Access" button is clicked`,
				`the "/account" page is opened`,
				`the page shows "Signed in as ` + shop.Name + `"`,
			} {
				if err := s.Step(step); err != nil {
					errs[i] = fmt.Errorf("%s: %s: %w", shop.Name, step, err)
					return
				}
			}
			errs[i] = s.End("passed")
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

// A password typed from the environment is masked in failure messages and
// in the failed scenario's trace, which records the login form and its POST.
func TestPasswordsStayOutOfTraces(t *testing.T) {
	withDex(t)
	defer func(d time.Duration) { actionTimeout = d }(actionTimeout)
	actionTimeout = 2 * time.Second
	pw := dexShops[0].Password
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/login" page is opened`)
	h.OK(`the "Log in with Email" button is clicked`)
	h.OK(`the "Email Address" field is filled with "orders@fjord-outdoor.example"`)
	h.OK(`the "Password" field is filled with "${env:FJORD_OUTDOOR_PASSWORD}"`)
	err := h.Fails(`within 1s the "Password" field has the value "${env:MAPLE_CRAFTS_PASSWORD}"`,
		`The "Password" field has another value`+"\n  expected: \"********\"\n  actual:   \"********\"")
	if strings.Contains(err.Error(), pw) {
		t.Errorf("the failure shows the password: %v", err)
	}
	h.OK(`the "Login" button is clicked`)
	h.OK(`the "Grant Access" button is clicked`)
	_ = h.Fails(`within 1s the page shows "Signed in as ${env:FJORD_OUTDOOR_PASSWORD}"`, `The page does not show "Signed in as ********"`)
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}

	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
	if len(traces) != 1 {
		t.Fatalf("traces: %v", traces)
	}
	entries := readZip(t, traces[0])
	var all strings.Builder
	for name, b := range entries {
		for _, s := range dexShops {
			for _, f := range encodings(s.Password) {
				if strings.Contains(string(b), f) {
					t.Errorf("%s holds %s's password as %q", name, s.Name, f)
				}
			}
		}
		all.Write(b)
	}
	// The trace did record what was typed and posted.
	for _, want := range []string{"orders@fjord-outdoor.example", "login=orders%40fjord-outdoor.example&password=" + masked, masked} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("the trace lacks %q", want)
		}
	}
	for _, name := range []string{"trace.trace", "trace.network"} {
		for line := range strings.SplitSeq(strings.TrimSpace(string(entries[name])), "\n") {
			var v any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Errorf("%s: %v", name, err)
				break
			}
		}
	}
}
