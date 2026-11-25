package mock

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// exchangeBodyLimit is where the body of an exchange is cut.
const exchangeBodyLimit = 2048

// say says that the system called the mocked service (core.Exchange), for a
// pack that shows the run: what a step found in the requests WireMock
// received. It says a thing once, by its key, however many of the scenario's
// steps check it after: the named request's headers, its payload, its count.
// The scenario's secrets are masked, as its failures mask them.
func (s *Service) say(sc *core.Scenario, key any, title, mediaType, body string) {
	if s.said[key] {
		return
	}
	if s.said == nil {
		s.said = map[any]bool{}
	}
	s.said[key] = true
	m := &core.Message{Title: secrets.Mask(sc, title)}
	if body != "" {
		body = secrets.Mask(sc, body)
		if len(body) > exchangeBodyLimit {
			body = strings.ToValidUTF8(body[:exchangeBodyLimit], "") + fmt.Sprintf("... (%d more bytes)", len(body)-exchangeBodyLimit)
		}
		m.MediaType, m.Body = mediaType, body
	}
	sc.Exchanged(core.Exchange{Peer: s.Name, Kind: "mock", From: core.FromSystem, Sent: m})
}

// received says that the mocked service received the requests a step counted
// for a pattern: its method and URL, as the step wrote them, and how many
// when there are several. A count is all a step has of them.
func (s *Service) received(sc *core.Scenario, p *pattern, got int) {
	if got > 0 {
		s.say(sc, p, counted(got, p.Method+" "+p.URL, "requests matching "+p.Method+" "+p.URL), "", "")
	}
}

// counted is the title of n things: one's own, or the number and many's.
func counted(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf("%d %s", n, many)
}
