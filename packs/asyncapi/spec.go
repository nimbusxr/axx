package asyncapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/iskorotkov/avro/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nimbusxr/axx/internal/schemadoc"
)

// spec is an AsyncAPI document (2.6 or 3.0), reduced to what checking
// messages needs: its channels, their addresses and protocols, and their
// messages' payload and header schemas.
type spec struct {
	source   string // what the registration named, for messages
	version  string
	channels []*channel
}

type channel struct {
	id        string
	address   string           // with its {parameters}
	res       []*regexp.Regexp // its address, and with its servers' paths before it
	protocols []string         // the protocols of its servers; empty: any
	messages  []*message
}

type message struct {
	name        string
	contentType string
	payload     *schema // nil: any payload
	headers     *schema // nil: any headers
}

// schema is a message's payload or headers schema, in the format the
// document gives it in.
type schema struct {
	format string // "json" or "avro"
	json   *jsonschema.Schema
	avro   avro.Schema
	raw    any // the schema as the document gives it
	where  string
}

// load reads and reduces an AsyncAPI document.
func load(source, u string) (*spec, error) {
	l := schemadoc.NewLoader()
	doc, err := l.Load(u)
	if err != nil {
		return nil, err
	}
	root := schemadoc.Node{URL: u, V: doc}
	s := &spec{source: source, version: root.Str("asyncapi")}
	major, _, _ := strings.Cut(s.version, ".")
	if major != "2" && major != "3" {
		return nil, fmt.Errorf("%s is not an AsyncAPI 2.x or 3.x document (asyncapi: %q)", source, s.version)
	}
	c := l.Compiler(jsonschema.Draft7)
	r := &reducer{l: l, c: c, defaultContentType: root.Str("defaultContentType"), servers: map[string]server{}}
	servers := root.Get("servers")
	for _, name := range servers.Keys() {
		n, err := l.Deref(servers.Get(name))
		if err != nil {
			return nil, err
		}
		srv := server{protocol: n.Str("protocol"), path: n.Str("pathname")}
		if major == "2" {
			srv.path = urlPath(n.Str("url"))
		}
		r.servers[name], r.servers[n.Ref()] = srv, srv
		r.all = append(r.all, srv)
	}
	channels := root.Get("channels")
	for _, id := range channels.Keys() {
		ch, err := l.Deref(channels.Get(id))
		if err != nil {
			return nil, err
		}
		var parsed *channel
		if major == "2" {
			parsed, err = r.channel2(id, ch)
		} else {
			parsed, err = r.channel3(id, ch)
		}
		if err != nil {
			return nil, fmt.Errorf("the channel %s: %w", id, err)
		}
		if parsed != nil {
			s.channels = append(s.channels, parsed)
		}
	}
	return s, nil
}

type reducer struct {
	l                  *schemadoc.Loader
	c                  *jsonschema.Compiler
	defaultContentType string
	servers            map[string]server // by name, and by a server's ref
	all                []server
}

type server struct {
	protocol string
	path     string // the path its channels' addresses are under, for WebSockets and HTTP
}

// urlPath is the path of an AsyncAPI 2.x server's url, which may have no
// scheme: localhost:8400/portal.
func urlPath(u string) string {
	if _, rest, ok := strings.Cut(u, "://"); ok {
		u = rest
	}
	if i := strings.IndexByte(u, '/'); i >= 0 {
		return u[i:]
	}
	return ""
}

// on puts a channel on its servers: their protocols, and the addresses
// under their paths. A channel that names none is on every server.
func (r *reducer) on(c *channel, servers []server) {
	c.res = []*regexp.Regexp{addressPattern(c.address)}
	for _, s := range servers {
		c.protocols = append(c.protocols, s.protocol)
	}
	if len(servers) == 0 {
		servers = r.all
	}
	seen := map[string]bool{}
	for _, s := range servers {
		if p := strings.TrimRight(s.path, "/"); p != "" && !seen[p] {
			seen[p] = true
			c.res = append(c.res, addressPattern(p+"/"+strings.TrimLeft(c.address, "/")))
		}
	}
}

// channel2 reads an AsyncAPI 2.x channel: its key is its address, and its
// messages are its operations'.
func (r *reducer) channel2(address string, ch schemadoc.Node) (*channel, error) {
	c := &channel{id: address, address: address}
	var on []server
	for _, s := range ch.Get("servers").Items() {
		name, _ := s.V.(string)
		on = append(on, r.servers[name])
	}
	r.on(c, on)
	seen := map[string]bool{}
	for _, op := range []string{"publish", "subscribe"} {
		m := ch.Get(op).Get("message")
		if m.V == nil {
			continue
		}
		list := []schemadoc.Node{m}
		if one := m.Get("oneOf"); one.V != nil {
			list = one.Items()
		}
		for _, mn := range list {
			msg, err := r.message(mn, "")
			if err != nil {
				return nil, err
			}
			if key := msg.name + "|" + msg.contentType; !seen[key] || msg.name == "" {
				seen[key] = true
				c.messages = append(c.messages, msg)
			}
		}
	}
	return c, nil
}

// channel3 reads an AsyncAPI 3.x channel: its address, and its messages.
func (r *reducer) channel3(id string, ch schemadoc.Node) (*channel, error) {
	address, _ := ch.Get("address").V.(string)
	if address == "" {
		return nil, nil // a channel whose address is only known at run time
	}
	c := &channel{id: id, address: address}
	var on []server
	for _, s := range ch.Get("servers").Items() {
		srv, err := r.l.Deref(s)
		if err != nil {
			return nil, err
		}
		info, ok := r.servers[srv.Ref()]
		if !ok {
			info = server{protocol: srv.Str("protocol"), path: srv.Str("pathname")}
		}
		on = append(on, info)
	}
	r.on(c, on)
	msgs := ch.Get("messages")
	for _, name := range msgs.Keys() {
		msg, err := r.message(msgs.Get(name), name)
		if err != nil {
			return nil, err
		}
		c.messages = append(c.messages, msg)
	}
	return c, nil
}

// message reads a message: its name, content type, payload and headers.
func (r *reducer) message(n schemadoc.Node, name string) (*message, error) {
	m, err := r.l.Deref(n)
	if err != nil {
		return nil, err
	}
	msg := &message{name: m.Str("name"), contentType: m.Str("contentType")}
	if msg.name == "" {
		msg.name = name
	}
	if msg.contentType == "" {
		msg.contentType = r.defaultContentType
	}
	if msg.payload, err = r.schema(m.Get("payload"), m.Str("schemaFormat")); err != nil {
		return nil, fmt.Errorf("the payload of %s: %w", msg.name, err)
	}
	if msg.headers, err = r.schema(m.Get("headers"), ""); err != nil {
		return nil, fmt.Errorf("the headers of %s: %w", msg.name, err)
	}
	return msg, nil
}

// schema compiles a payload or headers schema; a 3.x multi-format schema
// ({schemaFormat, schema}) gives its format itself.
func (r *reducer) schema(n schemadoc.Node, format string) (*schema, error) {
	if n.V == nil {
		return nil, nil
	}
	d, err := r.l.Deref(n)
	if err != nil {
		return nil, err
	}
	if f := d.Str("schemaFormat"); f != "" && d.Get("schema").V != nil {
		format, d = f, d.Get("schema")
		if d, err = r.l.Deref(d); err != nil {
			return nil, err
		}
	}
	switch kind := formatOf(format); kind {
	case "json":
		s, err := r.c.Compile(d.Ref())
		if err != nil {
			return nil, err
		}
		return &schema{format: kind, json: s, raw: d.V, where: d.Ref()}, nil
	case "avro":
		raw, err := json.Marshal(d.V)
		if err != nil {
			return nil, err
		}
		s, err := avro.ParseBytesWithCache(raw, "", &avro.SchemaCache{})
		if err != nil {
			return nil, fmt.Errorf("the Avro schema at %s: %w", d.Ref(), err)
		}
		return &schema{format: kind, avro: s, raw: d.V, where: d.Ref()}, nil
	}
	return nil, nil // a format the checks do not read: Protobuf, RAML...
}

// formatOf is how a schemaFormat's schemas are read: "json", "avro", or
// "" for formats the checks do not read.
func formatOf(format string) string {
	mt, _, _ := strings.Cut(strings.ToLower(format), ";")
	switch strings.TrimSpace(mt) {
	case "", "application/vnd.aai.asyncapi", "application/vnd.aai.asyncapi+json", "application/vnd.aai.asyncapi+yaml",
		"application/schema+json", "application/schema+yaml":
		return "json"
	case "application/vnd.apache.avro", "application/vnd.apache.avro+json", "application/vnd.apache.avro+yaml":
		return "avro"
	}
	return ""
}

var param = regexp.MustCompile(`\{[^{}]+\}`)

// addressPattern matches the addresses a channel's address stands for: a
// {parameter} is any text without a slash.
func addressPattern(address string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	last := 0
	for _, loc := range param.FindAllStringIndex(address, -1) {
		b.WriteString(regexp.QuoteMeta(address[last:loc[0]]))
		b.WriteString("[^/]+")
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(address[last:]) + "$")
	return regexp.MustCompile(b.String())
}
