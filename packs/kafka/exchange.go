package kafka

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// exchangeBodyLimit is where the payload of an exchange is cut.
const exchangeBodyLimit = 2048

// published says that the scenario published the event to the topic
// (core.Exchange), for a pack that shows the run: its key and its payload,
// as the step attached it. It says no headers.
func (tc *TopicClient) published(sc *core.Scenario, ev *Event) {
	sent := &core.Message{Title: keyTitle(ev.Key)}
	sent.MediaType, sent.Body = payloadOf(ev.Payload)
	sc.Exchanged(core.Exchange{Peer: tc.Topic, Kind: "topic", Sent: sent})
}

// consumed says that a step found a record of the topic that meets a label's
// expectations: its key, where it is on the topic, and its value as the
// steps read it. It says a record once, however many of the scenario's steps
// and labels it meets.
func (tc *TopicClient) consumed(sc *core.Scenario, r *record, d *decoded) {
	tc.mu.Lock()
	said := tc.said[r]
	if !said {
		if tc.said == nil {
			tc.said = map[*record]bool{}
		}
		tc.said[r] = true
	}
	tc.mu.Unlock()
	if said {
		return
	}
	seen := &core.Message{Title: fmt.Sprintf("%s, partition %d, offset %d", keyTitle(d.Key), r.Partition, r.Offset)}
	if d.Value != nil {
		seen.MediaType, seen.Body = payloadOf(*d.Value)
	}
	sc.Exchanged(core.Exchange{Peer: tc.Topic, Kind: "topic", Seen: seen})
}

func keyTitle(key *string) string {
	if key == nil {
		return "no key"
	}
	return "key " + *key
}

// payloadOf is a payload as an exchange has it: JSON when it is JSON, else
// text, and cut where it is long.
func payloadOf(text string) (mediaType, body string) {
	if text == "" {
		return "", ""
	}
	mediaType = "text/plain"
	if json.Valid([]byte(text)) {
		mediaType = core.MimeJSON
	}
	if len(text) > exchangeBodyLimit {
		text = strings.ToValidUTF8(text[:exchangeBodyLimit], "") + fmt.Sprintf("... (%d more bytes)", len(text)-exchangeBodyLimit)
	}
	return mediaType, text
}
