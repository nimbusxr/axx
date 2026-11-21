// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
)

// unverified sends the requests of a run whose axx.yaml leaves packs.rest.tls.verify unset: none
// has its certificate verified, and for a host that is not this machine the run says so, once
// for each. This machine's own names never get a word: no one stands between a run and them.
type unverified struct {
	next http.RoundTripper
	log  *slog.Logger
	said sync.Map // host -> struct{}
}

func (u *unverified) RoundTrip(req *http.Request) (*http.Response, error) {
	if host := req.URL.Hostname(); req.URL.Scheme == "https" && !thisMachine(host) && u.log != nil {
		if _, again := u.said.LoadOrStore(strings.ToLower(host), struct{}{}); !again {
			u.log.Warn(fmt.Sprintf("the REST pack does not verify %s's certificate: in axx.yaml, packs.rest.tls.verify: true verifies certificates, and false says that not verifying them is meant", host))
		}
	}
	return u.next.RoundTrip(req)
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
