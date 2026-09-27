package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// portal is a fake of a shop portal: it answers its registration form
// with 303 See Other to the parcel's page (setting a cookie on the way), and
// its old registration paths with other redirects.
type portal struct {
	mu   sync.Mutex
	seen []string // "METHOD /path cookie=<Cookie> type=<Content-Type>"
}

func (p *portal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.seen = append(p.seen, fmt.Sprintf("%s %s cookie=%s type=%s", r.Method, r.URL.RequestURI(),
		strings.Join(r.Header.Values("Cookie"), ";"), r.Header.Get("Content-Type")))
	p.mu.Unlock()
	switch {
	case r.URL.Path == "/portal/parcels" && r.Method == http.MethodPost:
		http.SetCookie(w, &http.Cookie{Name: "shop", Value: r.FormValue("sender"), Path: "/portal"})
		http.Redirect(w, r, "/portal/parcels/"+r.FormValue("reference"), http.StatusSeeOther)
	case strings.HasPrefix(r.URL.Path, "/portal/parcels/") && r.Method == http.MethodGet:
		respond(w, http.StatusOK, "text/html; charset=utf-8",
			"<h1>Parcel "+strings.TrimPrefix(r.URL.Path, "/portal/parcels/")+" registered</h1>")
	case strings.HasPrefix(r.URL.Path, "/portal/v1/"):
		var status int
		_, _ = fmt.Sscanf(r.URL.Query().Get("status"), "%d", &status)
		http.Redirect(w, r, "/portal/parcels", status)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (p *portal) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.seen
	p.seen = nil
	return out
}

func TestRedirects(t *testing.T) {
	p := &portal{}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	h := newHarness(t)
	h.service("portal", srv.URL, "")

	// A form answered with 303 See Other: the page it leads to is the response.
	h.ok("a POST request to /portal/parcels")
	h.ok("a request payload using an application/x-www-form-urlencoded empty content template")
	h.ok("the request payload properties are:", []string{"sender", "kestrel-books"}, []string{"reference", "PX-1001"})
	h.ok("the request is executed")
	h.ok("the response status code is 200")
	h.ok("the response body contains 'Parcel PX-1001 registered'")
	h.ok("the response header Location is missing")
	h.ok("the response header Set-Cookie is missing")
	// The page is asked for with a GET, without the form's Content-Type and
	// without the cookie the 303 set.
	want := []string{
		"POST /portal/parcels cookie= type=application/x-www-form-urlencoded",
		"GET /portal/parcels/PX-1001 cookie= type=",
	}
	if got := p.requests(); !slices.Equal(got, want) {
		t.Errorf("the portal got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A Cookie header the scenario sets goes along to the same host.
	h.ok("a 2nd ordered POST request to /portal/parcels")
	h.ok("the request header Cookie is 'shop=kestrel-books' for 2nd ordered request")
	h.ok("a request payload using an application/x-www-form-urlencoded empty content template for 2nd ordered request")
	h.ok("the request payload properties for 2nd ordered request are:", []string{"sender", "kestrel-books"}, []string{"reference", "PX-1002"})
	h.ok("the 2nd ordered request is executed")
	h.ok("the 2nd ordered response status code is 200")
	want = []string{
		"POST /portal/parcels cookie=shop=kestrel-books type=application/x-www-form-urlencoded",
		"GET /portal/parcels/PX-1002 cookie=shop=kestrel-books type=",
	}
	if got := p.requests(); !slices.Equal(got, want) {
		t.Errorf("the portal got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Other redirects of other methods are the response; the scenario checks
	// where they lead, and follows them itself.
	n := 3
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, status := range []int{301, 302, 307, 308} {
			ord := ordinal(n)
			h.ok(fmt.Sprintf("a %s ordered %s request to /portal/v1/parcels?status=%d", ord, method, status))
			h.ok(fmt.Sprintf("the %s ordered request is executed", ord))
			h.ok(fmt.Sprintf("the %s ordered response status code is %d", ord, status))
			h.ok(fmt.Sprintf("the response header Location is '/portal/parcels' for %s ordered response", ord))
			if got := p.requests(); len(got) != 1 {
				t.Errorf("%s answered with %d: the portal got %q", method, status, got)
			}
			n++
		}
	}
	ord := ordinal(n)
	h.ok(fmt.Sprintf("a %s ordered GET request to /portal/parcels/PX-1001", ord))
	h.ok(fmt.Sprintf("the %s ordered request is executed", ord))
	h.ok(fmt.Sprintf("the %s ordered response status code is 200", ord))

	// GET and HEAD follow every redirect.
	for _, method := range []string{"GET", "HEAD"} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			n++
			ord := ordinal(n)
			h.ok(fmt.Sprintf("a %s ordered %s request to /portal/v1/parcels/PX-1001?status=%d", ord, method, status))
			h.ok(fmt.Sprintf("the %s ordered request is executed", ord))
			// /portal/parcels answers GET and HEAD with 404: the redirect was followed.
			h.ok(fmt.Sprintf("the %s ordered response status code is 404", ord))
		}
	}
}

func ordinal(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return fmt.Sprintf("%dth", n)
	case n%10 == 1:
		return fmt.Sprintf("%dst", n)
	case n%10 == 2:
		return fmt.Sprintf("%dnd", n)
	case n%10 == 3:
		return fmt.Sprintf("%drd", n)
	}
	return fmt.Sprintf("%dth", n)
}
