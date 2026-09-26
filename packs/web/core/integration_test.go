//go:build integration

package webcore

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// The portal is a parcel shop's web portal, served by the test on the host,
// where the browsers run.
type portal struct {
	mu      sync.Mutex
	parcels map[string]parcel
	// sessions are the shops signed in through Dex, by session cookie.
	sessions map[string]string
}

type parcel struct{ recipient, date, notes, invoice, status string }

// bundled are Playwright's own browsers; chrome and msedge are those
// installed on the machine, when they are.
var bundled = []string{"chromium", "firefox", "webkit"}

// env is what the package's tests share: the portal, started on first use
// and stopped after the tests, and the services the tests start.
var env struct {
	once    sync.Once
	port    int
	tlsPort int // the portal over HTTPS, with a certificate no one trusts
	err     error
	cleanup []func()
}

func TestMain(m *testing.M) {
	code := m.Run()
	for i := len(env.cleanup) - 1; i >= 0; i-- {
		env.cleanup[i]()
	}
	os.Exit(code)
}

// environment starts the portal once, and returns its address.
func environment(t *testing.T) (portalURL string) {
	t.Helper()
	env.once.Do(func() { env.port, env.err = startPortal() })
	if env.err != nil {
		t.Fatalf("cannot start the portal: %v", env.err)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", env.port)
}

func startPortal() (int, error) {
	p := &portal{parcels: map[string]parcel{"PX-4101": {recipient: "Anna Weber", status: "IN_TRANSIT"}}, sessions: map[string]string{}}
	mux := http.NewServeMux()
	mux.Handle("GET /{page}", http.FileServer(http.Dir("testdata/site")))
	// The site's icon, which browsers ask for.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		icon := image.NewRGBA(image.Rect(0, 0, 16, 16))
		draw.Draw(icon, icon.Bounds(), &image.Uniform{color.RGBA{0x1f, 0x5f, 0xa8, 0xff}}, image.Point{}, draw.Src)
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, icon)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		page(w, "Parcels", `<h1>Parcels</h1><a href="/quote.html">Get a quote</a> <a href="/parcels">Track a parcel</a>`)
	})
	mux.HandleFunc("POST /parcels", p.register)
	mux.HandleFunc("GET /parcels", p.list)
	mux.HandleFunc("GET /parcels/{ref}", p.show)
	mux.HandleFunc("GET /signin", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "shop", Value: r.URL.Query().Get("shop"), Path: "/"})
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /account", p.account)
	mux.HandleFunc("GET /login", p.login)
	mux.HandleFunc("GET /callback", p.callback)
	mux.HandleFunc("GET /logout", p.logout)
	mux.HandleFunc("GET /whoami", func(w http.ResponseWriter, r *http.Request) {
		who := map[string]string{"cookie": "none", "authorization": r.Header.Get("Authorization"), "user": "none"}
		if c, err := r.Cookie("session"); err == nil {
			who["cookie"] = c.Value
		}
		if u, _, ok := r.BasicAuth(); ok {
			who["user"] = u
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(who)
	})
	mux.HandleFunc("GET /protected", func(w http.ResponseWriter, r *http.Request) {
		if u, pw, ok := r.BasicAuth(); !ok || u != "depot" || pw != "depot-staff" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Depot"`)
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		page(w, "Depot", "<p>Welcome to the depot</p>")
	})
	mux.HandleFunc("POST /api/board", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /invoice.pdf", func(w http.ResponseWriter, r *http.Request) {
		// A customs invoice Chromium printed, read by the files' reader too.
		w.Header().Set("Content-Disposition", `attachment; filename="INV-6001.pdf"`)
		http.ServeFile(w, r, "../../../internal/filecontent/testdata/customs-invoice.pdf")
	})
	mux.HandleFunc("GET /labels/{ref}", p.label)
	mux.HandleFunc("GET /parcels.csv", p.export)
	mux.HandleFunc("GET /broken", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the pricing service is down", http.StatusInternalServerError)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln) //nolint:errcheck
	env.cleanup = append(env.cleanup, func() { _ = srv.Close() })
	tlsSrv := httptest.NewUnstartedServer(mux)
	tlsSrv.StartTLS()
	env.cleanup = append(env.cleanup, tlsSrv.Close)
	env.tlsPort = tlsSrv.Listener.Addr().(*net.TCPAddr).Port
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func page(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s</title></head><body>%s</body></html>`, title, body)
}

func (p *portal) register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pc := parcel{recipient: r.FormValue("recipient"), date: r.FormValue("date"), notes: r.FormValue("notes"), status: "REGISTERED"}
	if f, h, err := r.FormFile("invoice"); err == nil {
		n, _ := io.Copy(io.Discard, f)
		_ = f.Close()
		pc.invoice = fmt.Sprintf("%s (%d bytes)", h.Filename, n)
	}
	ref := r.FormValue("reference")
	p.mu.Lock()
	p.parcels[ref] = pc
	p.mu.Unlock()
	http.Redirect(w, r, "/parcels/"+ref, http.StatusSeeOther)
}

func (p *portal) show(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	p.mu.Lock()
	pc, ok := p.parcels[ref]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	page(w, "Parcel "+ref, fmt.Sprintf(`<h1>Parcel %s registered</h1><p>Recipient: %s</p><p>Delivery date: %s</p><p>Notes: %s</p><p>Invoice: %s</p><a href="/parcels">All parcels</a>`,
		html.EscapeString(ref), html.EscapeString(pc.recipient), html.EscapeString(pc.date), html.EscapeString(pc.notes), html.EscapeString(pc.invoice)))
}

func (p *portal) list(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	refs := make([]string, 0, len(p.parcels))
	for ref := range p.parcels {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	var rows strings.Builder
	for _, ref := range refs {
		pc := p.parcels[ref]
		fmt.Fprintf(&rows, "<tr><td>%s</td><td>%s</td><td>%s</td></tr>", html.EscapeString(ref), html.EscapeString(pc.recipient), pc.status)
	}
	p.mu.Unlock()
	page(w, "Parcels", `<h1>Parcels</h1>
<table><thead><tr><th>Zone</th><th>Price</th></tr></thead><tbody><tr><td>DE-1</td><td>6.90 EUR</td></tr></tbody></table>
<table><thead><tr><th>Parcel</th><th>Recipient</th><th>Status</th></tr></thead><tbody>`+rows.String()+`</tbody></table>`)
}

// label is a parcel's shipping label, for a label printer (ZPL).
func (p *portal) label(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	p.mu.Lock()
	pc, ok := p.parcels[ref]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/zpl")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", ref+"-label.zpl"))
	fmt.Fprint(w, zplLabel(ref, pc.recipient))
}

func zplLabel(ref, recipient string) string {
	return fmt.Sprintf("^XA\n^FO50,50^A0N,40,40^FD%s^FS\n^FO50,110^A0N,30,30^FD%s^FS\n^XZ\n", ref, recipient)
}

// export is the shop's parcels as CSV.
func (p *portal) export(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	refs := make([]string, 0, len(p.parcels))
	for ref := range p.parcels {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	var b strings.Builder
	b.WriteString("Parcel,Recipient,Status\n")
	for _, ref := range refs {
		fmt.Fprintf(&b, "%s,%s,%s\n", ref, p.parcels[ref].recipient, p.parcels[ref].status)
	}
	p.mu.Unlock()
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="parcels.csv"`)
	fmt.Fprint(w, b.String())
}

func newHarness(t *testing.T, engine string) (*cloudtest.Harness, [][]string) {
	t.Helper()
	return newConfigured(t, engine, nil)
}

// newConfigured is newHarness with the pack's section of axx.yaml.
func newConfigured(t *testing.T, engine string, config *Config) (*cloudtest.Harness, [][]string) {
	t.Helper()
	portalURL := environment(t)
	var cfg map[string]any
	if config != nil {
		cfg = map[string]any{Name: config}
	}
	h := cloudtest.NewWith(t, cfg, Pack())
	return h, [][]string{{"url", portalURL}, {"engine", engine}}
}

// Every step, in every engine, through a shop's day on the portal.
func TestThePortalInEveryEngine(t *testing.T) {
	for i, engine := range bundled {
		t.Run(engine, func(t *testing.T) {
			h, app := newHarness(t, engine)
			ref := fmt.Sprintf("PX-700%d", i+1)
			h.OK("the portal web app with the following properties:", app)

			// A quote: every kind of field, a validation message, a price that takes a moment.
			h.OK(`the "/quote.html" page is opened`)
			h.OK(`the "Get a quote" button is clicked`)
			h.OK(`the page shows "Enter a weight"`)
			h.OK(`the "Weight (grams)" field is filled with "1200"`)
			h.OK(`"Germany" is chosen in the "Destination country" field`)
			h.OK(`the "Express" option is chosen`)
			h.OK(`the "Insure this parcel" checkbox is checked`)
			h.OK(`the "Leave with a neighbour" checkbox is unchecked`)
			h.OK(`the "Get a quote" button is clicked`)
			h.OK(`within 5s the page shows "Price: 13.40 EUR (signature required)"`)
			h.OK(`the page does not show "Enter a weight"`)
			h.OK(`the page does not show "Calculating"`)
			h.OK(`the "Weight (grams)" field has the value "1200"`)
			h.OK(`the "Destination country" field has the value "Germany"`)

			// A registration: a placeholder-only field, a date, a text area, an upload,
			// a button enabled once the form is valid, a redirect and a table.
			h.OK(`the "/register.html" page is opened`)
			h.OK(`the "Register the parcel" button is disabled`)
			h.OK(`the "Reference" field is filled with "` + ref + `"`)
			h.OK(`the "Recipient name" field is filled with "Anna Weber"`)
			h.OK(`the "Delivery date" field is filled with "2026-10-01"`)
			h.OK(`the "Delivery notes" field is filled with "Ring twice"`)
			h.File("invoices/INV-2041.pdf", "%PDF-1.4 invoice")
			h.OK(`the invoices/INV-2041.pdf file is uploaded in the "Customs invoice" field`)
			h.OK(`the "Register the parcel" button is enabled`)
			h.OK(`the "Register the parcel" button is clicked`)
			h.OK(`the "/parcels/` + ref + `" page is shown`)
			h.OK(`the page shows "Parcel ` + ref + ` registered"`)
			h.OK(`the page shows "Delivery date: 2026-10-01"`)
			h.OK(`the page shows "Invoice: INV-2041.pdf (16 bytes)"`)
			h.OK(`the "All parcels" link is clicked`)
			h.OK(`the "/parcels" page is shown`)
			h.OK("the page shows a table row where:", [][]string{{"Parcel", ref}, {"Recipient", "Anna Weber"}, {"Status", "REGISTERED"}})
			h.OK("within 5s the page shows a table row where:", [][]string{{"Parcel", "PX-4101"}, {"Status", "IN_TRANSIT"}})

			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}
			if traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip")); len(traces) != 0 {
				t.Errorf("a passing scenario kept traces: %v", traces)
			}
		})
	}
}

// Failures say what is on the page, attach a screenshot, and a failed
// scenario keeps a trace.
func TestFailuresShowWhatIsThere(t *testing.T) {
	defer func(d time.Duration) { actionTimeout = d }(actionTimeout)
	actionTimeout = 2 * time.Second
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)

	_ = h.Fails(`the "Weight (grams)" field is filled with "1200"`, "no page is open")
	_ = h.Fails(`the "/quote.html" page of the backoffice web app is opened`, "backoffice")
	h.OK(`the "/quote.html" page is opened`)

	err := h.Fails(`the "Weight" field is filled with "1200"`, `No field named "Weight" on the page; 2 fields:`)
	for _, name := range []string{`"Weight (grams)"`, `"Destination country"`} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the fields listed lack %s: %v", name, err)
		}
	}
	_ = h.Fails(`the "Save" button is clicked`, `No button named "Save" on the page; 1 buttons:`+"\n  "+`"Get a quote"`)
	_ = h.Fails(`the "Leave at the door" checkbox is checked`, `No checkbox named "Leave at the door"`)
	_ = h.Fails(`"Spain" is chosen in the "Destination country" field`, `The "Destination country" field has no option "Spain"; 3 options:`)
	_ = h.Fails(`within 1s the page shows "Price: 99.00 EUR"`, `The page does not show "Price: 99.00 EUR"`)
	_ = h.Fails(`within 1s the page does not show "Get a quote"`, `The page shows "Get a quote"`)
	_ = h.Fails(`within 1s the "/quotes" page is shown`, `actual:   "/quote.html"`)
	h.OK(`the "Weight (grams)" field is filled with "1200"`)
	_ = h.Fails(`within 1s the "Weight (grams)" field has the value "900"`, `expected: "900"`+"\n  "+`actual:   "1200"`)
	_ = h.Fails(`within 1s the "Get a quote" button is disabled`, `The "Get a quote" button is enabled`)

	h.OK(`the "/parcels" page is opened`)
	_ = h.Fails("within 1s the page shows a table row where:", `No table row on the page has Parcel=PX-9999; the rows are:`, [][]string{{"Parcel", "PX-9999"}})
	_ = h.Fails("within 1s the page shows a table row where:", `no table on the page has the columns Carrier`, [][]string{{"Carrier", "Kestrel"}})

	_ = h.Fails(`the "/nowhere.html" page is opened`, "404")
	_ = h.Fails(`the "/broken" page is opened`, "500")
	h.OK(`the "/duplicates.html" page is opened`)
	_ = h.Fails(`the "Weight" field is filled with "1"`, `2 fields on the page are named "Weight"`)
	_ = h.Fails(`the "Save" button is clicked`, `2 buttons on the page are named "Save"`)

	shots := 0
	for _, a := range h.Sink.Attachments {
		if a.MediaType == "image/png" && len(a.Body) > 0 {
			shots++
		}
	}
	if shots < 15 {
		t.Errorf("%d screenshots for the failed steps on a page", shots)
	}

	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
	if len(traces) != 1 {
		t.Fatalf("traces: %v", traces)
	}
	if st, err := os.Stat(traces[0]); err != nil || st.Size() == 0 {
		t.Errorf("the trace is empty: %v", err)
	}
	if logs := strings.Join(h.Sink.Logs, "\n"); !strings.Contains(logs, ".axx/web/traces/") || !strings.Contains(logs, "show-trace") {
		t.Errorf("the log does not point at the trace: %s", logs)
	}
}

// Scenarios run side by side, each in a browser context of its own: one
// shop's session never shows in another's.
func TestScenariosAreIsolated(t *testing.T) {
	h, app := newHarness(t, "chromium")
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: fmt.Sprint("shop-", i), Name: "shop"}, h.Suite, h.Sink)
			s := h.In(sc)
			shop := fmt.Sprintf("SHOP-%d", i+1)
			for _, step := range []struct {
				text string
				arg  []any
			}{
				{"the portal web app with the following properties:", []any{app}},
				{`the "/signin?shop=` + shop + `" page is opened`, nil},
				{`the "/account" page is opened`, nil},
				{`the page shows "Signed in as ` + shop + `"`, nil},
			} {
				if err := s.Step(step.text, step.arg...); err != nil {
					errs[i] = fmt.Errorf("%s: %s: %w", shop, step.text, err)
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
