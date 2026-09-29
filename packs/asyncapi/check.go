package asyncapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"regexp"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/internal/avrojson"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/schemadoc"
)

// finding is a way a message breaks its contract, keyed for levels.
type finding struct {
	key     string
	message string
}

// protocols are the AsyncAPI protocols each pack's messages travel over,
// by what the pack reports: a server of any of them serves the pack.
var protocols = map[string][]string{
	"kafka":        {"kafka", "kafka-secure"},
	"amqp":         {"amqp", "amqps"},
	"amqp1":        {"amqp1", "amqp", "amqps"},
	"mqtt":         {"mqtt", "mqtt5", "secure-mqtt", "mqtts", "ws", "wss"},
	"nats":         {"nats", "tls"},
	"sqs":          {"sqs"},
	"sns":          {"sns"},
	"googlepubsub": {"googlepubsub"},
	"servicebus":   {"amqp1", "amqp", "amqps", "servicebus", "azureservicebus"},
	"ws":           {"ws", "wss"},
	"http":         {"http", "https"},
}

// channelFor finds the channel of a message: the first of its addresses a
// channel over its protocol has.
func (s *spec) channelFor(m contract.Message) (*channel, *finding) {
	accept := protocols[m.Protocol]
	if accept == nil {
		accept = []string{m.Protocol}
	}
	var elsewhere []string
	for _, addr := range m.Addresses {
		for _, c := range s.channels {
			if !slices.ContainsFunc(c.res, func(re *regexp.Regexp) bool { return re.MatchString(addr) }) {
				continue
			}
			if len(c.protocols) == 0 || slices.ContainsFunc(c.protocols, func(p string) bool {
				return slices.Contains(accept, strings.ToLower(p))
			}) {
				return c, nil
			}
			elsewhere = append(elsewhere, c.id+" ("+strings.Join(c.protocols, ", ")+")")
		}
	}
	msg := fmt.Sprintf("%s has no channel with the address %s", s.source, quoteAll(m.Addresses))
	if len(elsewhere) > 0 {
		msg += fmt.Sprintf(" over %s; the channels %s are on other servers", m.Protocol, strings.Join(elsewhere, ", "))
	} else if near := s.nearest(m.Addresses[0]); near != "" {
		msg += "; the closest is " + near
	}
	return nil, &finding{key: "validation.channel.unknown", message: msg}
}

// nearest is the channel address closest to addr, for a hint.
func (s *spec) nearest(addr string) string {
	best, cost := "", len(addr)/2+1
	for _, c := range s.channels {
		if d := levenshtein(addr, c.address); d < cost {
			best, cost = c.address, d
		}
	}
	return best
}

func quoteAll(addrs []string) string {
	q := make([]string, len(addrs))
	for i, a := range addrs {
		q[i] = fmt.Sprintf("%q", a)
	}
	return strings.Join(q, " or ")
}

// check checks a message against the document: it must be on one of its
// channels and be one of that channel's messages. When the channel has
// several, the findings are those of the message it comes closest to.
func (s *spec) check(m contract.Message) []finding {
	c, f := s.channelFor(m)
	if f != nil {
		return []finding{*f}
	}
	msgs := c.messages
	if m.Name != "" {
		if named := slices.DeleteFunc(slices.Clone(msgs), func(msg *message) bool { return msg.name != m.Name }); len(named) > 0 {
			msgs = named
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	var best []finding
	var bestMsg *message
	bestShared := -1
	keys := payloadKeys(m.Payload)
	for _, msg := range msgs {
		found := msg.check(m)
		if len(found) == 0 {
			return nil
		}
		// The closest message declares the most of the payload's
		// properties, then breaks the fewest rules.
		shared := msg.payload.declares(keys)
		if shared > bestShared || (shared == bestShared && len(found) < len(best)) {
			best, bestMsg, bestShared = found, msg, shared
		}
	}
	what := "the " + c.id + " channel's message"
	if len(msgs) > 1 {
		names := make([]string, len(msgs))
		for i, msg := range msgs {
			names[i] = msg.label()
		}
		what = fmt.Sprintf("the %s channel's messages (%s); it comes closest to %s", c.id, strings.Join(names, ", "), bestMsg.label())
	} else if bestMsg.name != "" {
		what = fmt.Sprintf("the %s message of the %s channel", bestMsg.name, c.id)
	}
	for i := range best {
		best[i].message = best[i].message + " (" + what + ")"
	}
	return best
}

// payloadKeys are the properties of a JSON object payload.
func payloadKeys(payload []byte) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(payload, &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return keys
}

// declares counts the keys the schema declares as its properties (or its
// Avro record's fields).
func (s *schema) declares(keys []string) int {
	if s == nil {
		return 0
	}
	raw, _ := s.raw.(map[string]any)
	declared := map[string]bool{}
	props, _ := raw["properties"].(map[string]any)
	for k := range props {
		declared[k] = true
	}
	fields, _ := raw["fields"].([]any)
	for _, f := range fields {
		if fm, ok := f.(map[string]any); ok {
			if name, ok := fm["name"].(string); ok {
				declared[name] = true
			}
		}
	}
	n := 0
	for _, k := range keys {
		if declared[k] {
			n++
		}
	}
	return n
}

func (msg *message) label() string {
	if msg.name == "" {
		return "an unnamed message"
	}
	return msg.name
}

// check checks a message against one of a channel's messages.
func (msg *message) check(m contract.Message) []finding {
	if msg.contentType != "" && m.ContentType != "" && !sameMediaType(msg.contentType, m.ContentType) {
		// A payload of another type cannot be read by the message's schema.
		return []finding{{
			key:     "validation.message.contentType",
			message: fmt.Sprintf("the content type is %q, not %q", m.ContentType, msg.contentType),
		}}
	}
	var out []finding
	if msg.payload != nil {
		out = append(out, msg.payload.check("payload", m.Payload, firstOf(m.ContentType, msg.contentType))...)
	}
	if msg.headers != nil && msg.headers.json != nil {
		out = append(out, msg.headers.checkValue("headers", msg.headers.headerValues(m.Headers))...)
	}
	return out
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func sameMediaType(a, b string) bool {
	ma, _, errA := mime.ParseMediaType(a)
	mb, _, errB := mime.ParseMediaType(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(a, b)
	}
	return ma == mb
}

// check reads a payload by its content type and checks it.
func (s *schema) check(part string, payload []byte, contentType string) []finding {
	if s.format == "avro" {
		return s.checkAvro(part, payload)
	}
	v, err := instance(payload, contentType)
	if err != nil {
		return []finding{{key: "validation.message." + part + ".format", message: fmt.Sprintf("the %s is not %s: %v", part, err.kind, err.err)}}
	}
	return s.checkValue(part, v)
}

type readError struct {
	kind string
	err  error
}

// instance reads a payload as JSON or YAML, when its content type says so
// or it has none; any other payload is text.
func instance(payload []byte, contentType string) (any, *readError) {
	mt, _, _ := mime.ParseMediaType(contentType)
	switch {
	case mt == "" || mt == "application/json" || strings.HasSuffix(mt, "+json"):
		v, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
		if err != nil {
			return nil, &readError{"JSON", err}
		}
		return v, nil
	case mt == "application/yaml" || mt == "application/x-yaml" || mt == "text/yaml" || strings.HasSuffix(mt, "+yaml"):
		v, err := schemadoc.Decode(payload)
		if err != nil {
			return nil, &readError{"YAML", err}
		}
		return v, nil
	}
	return string(payload), nil
}

// checkValue validates a value against a JSON Schema.
func (s *schema) checkValue(part string, v any) []finding {
	var out []finding
	for _, f := range schemadoc.Validate(s.json, v, "validation.message."+part+".schema", part) {
		out = append(out, finding{key: f.Key, message: f.Message})
	}
	return out
}

// headerValues is the object a message's headers are checked as: a header
// the schema says is not a string is read as JSON.
func (s *schema) headerValues(headers map[string]string) map[string]any {
	props, _ := s.raw.(map[string]any)
	props, _ = props["properties"].(map[string]any)
	out := make(map[string]any, len(headers))
	for name, value := range headers {
		out[name] = value
		p, _ := props[name].(map[string]any)
		if t, ok := p["type"].(string); ok && t != "string" {
			if v, err := jsonschema.UnmarshalJSON(strings.NewReader(value)); err == nil {
				out[name] = v
			}
		}
	}
	return out
}

// checkAvro checks a payload in Avro's JSON encoding against the schema.
func (s *schema) checkAvro(part string, payload []byte) []finding {
	if _, err := avrojson.Decode(s.avro, payload, avrojson.Options{LenientUnions: true}); err != nil {
		var ae *avrojson.Error
		if errors.As(err, &ae) {
			return []finding{{key: "validation.message." + part + ".schema.avro", message: fmt.Sprintf("%s %s: %s", part, ae.Path, ae.Msg)}}
		}
		return []finding{{key: "validation.message." + part + ".format", message: fmt.Sprintf("the %s is not JSON: %v", part, err)}}
	}
	return nil
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
