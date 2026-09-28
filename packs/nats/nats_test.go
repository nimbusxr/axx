package nats

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

func TestServerProperties(t *testing.T) {
	h := cloudtest.New(t, Pack())
	for _, tc := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"token", "t"}}, `the nats server property "url" is required`},
		{[][]string{{"url", "nats://localhost"}, {"token", "t"}, {"username", "axx"}}, "give the nats server one way to sign in"},
		{[][]string{{"url", "nats://localhost"}, {"stream", "TRACKING"}}, `unknown nats server property "stream" (supported: url, token, username, password, creds, asyncapi)`},
	} {
		_ = h.Fails("the tracking nats server with the following properties:", tc.want, tc.rows)
	}
}

func TestURLsAreRedacted(t *testing.T) {
	got := redact("nats://axx:tracking-pass@nats-1:4222, nats://tracking-token@nats-2:4222,nats://nats-3:4222")
	if strings.Contains(got, "tracking-pass") || strings.Contains(got, "tracking-token") || !strings.Contains(got, "nats-3:4222") {
		t.Errorf("redact: %s", got)
	}
}

func TestPublishingNeedsASubject(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.OK("the tracking nats server with the following properties:", [][]string{{"url", "nats://127.0.0.1:1"}})
	_ = h.Fails("a message is published to the tracking.> nats subject:", "not to one with wildcards: tracking.>", `{}`)
}
