// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Verify is packs.rest.tls.verify: what a TLS certificate that does not verify does to a request.
type Verify string

const (
	// VerifyWarn lets the request go on, and the run says once for each host that its
	// certificate does not verify. It is the default.
	VerifyWarn Verify = "warn"
	// VerifyError fails the request.
	VerifyError Verify = "error"
	// VerifyIgnore verifies nothing.
	VerifyIgnore Verify = "ignore"
)

// UnmarshalJSON reads warn, error or ignore, and true and false, the setting's earlier values,
// as error and ignore.
func (v *Verify) UnmarshalJSON(b []byte) error {
	switch text := strings.TrimSpace(string(b)); text {
	case `"warn"`:
		*v = VerifyWarn
	case `"error"`, "true":
		*v = VerifyError
	case `"ignore"`, "false":
		*v = VerifyIgnore
	default:
		return fmt.Errorf("tls.verify is %s: it is warn, error or ignore", text)
	}
	return nil
}

// orWarn is the setting, or what it is when axx.yaml does not say.
func (v Verify) orWarn() Verify {
	if v == "" {
		return VerifyWarn
	}
	return v
}

// warned sends the requests of a run that warns (packs.rest.tls.verify: warn): each goes on
// whatever its host's certificate is, and the run says, once for each host, when it does not
// verify. This machine's own names never get a word: no one stands between a run and them, and
// a service a run starts there has a certificate of its own as a rule.
type warned struct {
	next   http.RoundTripper
	log    *slog.Logger
	verify func(state *tls.ConnectionState, host string) error
	seen   sync.Map // host -> struct{}
}

func (w *warned) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := w.next.RoundTrip(req)
	if resp == nil || resp.TLS == nil || w.log == nil {
		return resp, err
	}
	host := req.URL.Hostname()
	if thisMachine(host) {
		return resp, err
	}
	if _, again := w.seen.LoadOrStore(strings.ToLower(host), struct{}{}); !again {
		if why := w.verify(resp.TLS, host); why != nil {
			w.log.Warn(fmt.Sprintf("%s's certificate does not verify (%v): the request went on. In axx.yaml, packs.rest.tls.verify: error fails such a request, and ignore says nothing of it", host, why))
		}
	}
	return resp, err
}

// verifies checks the certificate a host showed as a client that verifies would have: its chain
// to the system's roots, and its name.
func verifies(state *tls.ConnectionState, host string) error { return verifiesFrom(nil, state, host) }

// verifiesFrom is verifies with the roots to trust (nil: the system's).
func verifiesFrom(roots *x509.CertPool, state *tls.ConnectionState, host string) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("the server showed none")
	}
	opts := x509.VerifyOptions{DNSName: host, Roots: roots, Intermediates: x509.NewCertPool()}
	for _, c := range state.PeerCertificates[1:] {
		opts.Intermediates.AddCert(c)
	}
	_, err := state.PeerCertificates[0].Verify(opts)
	return err
}

// thisMachine says whether a host names this machine: localhost, a name under .localhost, or a
// loopback address.
func thisMachine(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
