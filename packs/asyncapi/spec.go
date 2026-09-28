package asyncapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/iskorotkov/avro/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
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

// loader reads the documents of a contract, YAML or JSON, by URL: a
// document's $refs to other files are relative to it.
type loader struct {
	mu   sync.Mutex
	docs map[string]any
	http *http.Client
}

func newLoader() *loader {
	return &loader{docs: map[string]any{}, http: &http.Client{Timeout: 30 * time.Second}}
}

// Load reads a document (without its fragment); the jsonschema compiler
// calls it for the schemas' $refs too.
func (l *loader) Load(u string) (any, error) {
	u, _, _ = strings.Cut(u, "#")
	l.mu.Lock()
	defer l.mu.Unlock()
	if d, ok := l.docs[u]; ok {
		return d, nil
	}
	var raw []byte
	var err error
	switch {
	case strings.HasPrefix(u, "file://"):
		p, perr := url.Parse(u)
		if perr != nil {
			return nil, perr
		}
		raw, err = os.ReadFile(p.Path)
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		// The document is read once per run, whatever scenario asks first:
		// the client's timeout bounds it, not the scenario.
		var req *http.Request
		var res *http.Response
		if req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil); err == nil {
			res, err = l.http.Do(req)
		}
		if err == nil {
			raw, err = io.ReadAll(io.LimitReader(res.Body, 16<<20))
			_ = res.Body.Close()
			if err == nil && res.StatusCode != http.StatusOK {
				err = fmt.Errorf("%s answered %d", u, res.StatusCode)
			}
		}
	default:
		err = fmt.Errorf("cannot read %s", u)
	}
	if err != nil {
		return nil, err
	}
	doc, err := decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	l.docs[u] = doc
	return doc, nil
}

// decode reads YAML (or JSON, which is YAML) into what the jsonschema
// package validates with: plain maps and json.Number numbers.
func decode(raw []byte) (any, error) {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	j, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(j))
}

// node is a value of a document, and where it is: its document's URL and
// its JSON pointer.
type node struct {
	url, pointer string
	v            any
}

func (n node) ref() string { return n.url + "#" + n.pointer }

func (n node) get(key string) node {
	m, _ := n.v.(map[string]any)
	return node{url: n.url, pointer: n.pointer + "/" + escape(key), v: m[key]}
}

func (n node) str(key string) string {
	s, _ := n.get(key).v.(string)
	return s
}

func (n node) keys() []string {
	m, _ := n.v.(map[string]any)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (n node) items() []node {
	a, _ := n.v.([]any)
	out := make([]node, len(a))
	for i, v := range a {
		out[i] = node{url: n.url, pointer: n.pointer + "/" + strconv.Itoa(i), v: v}
	}
	return out
}

func escape(k string) string { return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1") }

// deref follows a node's $refs, across documents.
func (l *loader) deref(n node) (node, error) {
	for range 32 {
		m, ok := n.v.(map[string]any)
		if !ok {
			return n, nil
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return n, nil
		}
		file, pointer, _ := strings.Cut(ref, "#")
		target := n.url
		if file != "" {
			base, err := url.Parse(n.url)
			if err != nil {
				return n, err
			}
			rel, err := url.Parse(file)
			if err != nil {
				return n, fmt.Errorf("the $ref %q: %w", ref, err)
			}
			target = base.ResolveReference(rel).String()
		}
		doc, err := l.Load(target)
		if err != nil {
			return n, err
		}
		v, err := lookup(doc, pointer)
		if err != nil {
			return n, fmt.Errorf("the $ref %q: %w", ref, err)
		}
		n = node{url: target, pointer: pointer, v: v}
	}
	return n, fmt.Errorf("the $refs of %s go round in circles", n.ref())
}

func lookup(doc any, pointer string) (any, error) {
	v := doc
	if pointer == "" || pointer == "/" {
		return v, nil
	}
	for _, seg := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[seg]
			if !ok {
				return nil, fmt.Errorf("no %q", pointer)
			}
			v = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, fmt.Errorf("no %q", pointer)
			}
			v = x[i]
		default:
			return nil, fmt.Errorf("no %q", pointer)
		}
	}
	return v, nil
}

// load reads and reduces an AsyncAPI document.
func load(source, u string) (*spec, error) {
	l := newLoader()
	doc, err := l.Load(u)
	if err != nil {
		return nil, err
	}
	root := node{url: u, v: doc}
	s := &spec{source: source, version: root.str("asyncapi")}
	major, _, _ := strings.Cut(s.version, ".")
	if major != "2" && major != "3" {
		return nil, fmt.Errorf("%s is not an AsyncAPI 2.x or 3.x document (asyncapi: %q)", source, s.version)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.UseLoader(jsonschema.SchemeURLLoader{"file": l, "http": l, "https": l})
	r := &reducer{l: l, c: c, defaultContentType: root.str("defaultContentType"), servers: map[string]server{}}
	servers := root.get("servers")
	for _, name := range servers.keys() {
		n, err := l.deref(servers.get(name))
		if err != nil {
			return nil, err
		}
		srv := server{protocol: n.str("protocol"), path: n.str("pathname")}
		if major == "2" {
			srv.path = urlPath(n.str("url"))
		}
		r.servers[name], r.servers[n.ref()] = srv, srv
		r.all = append(r.all, srv)
	}
	channels := root.get("channels")
	for _, id := range channels.keys() {
		ch, err := l.deref(channels.get(id))
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
	l                  *loader
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
func (r *reducer) channel2(address string, ch node) (*channel, error) {
	c := &channel{id: address, address: address}
	var on []server
	for _, s := range ch.get("servers").items() {
		name, _ := s.v.(string)
		on = append(on, r.servers[name])
	}
	r.on(c, on)
	seen := map[string]bool{}
	for _, op := range []string{"publish", "subscribe"} {
		m := ch.get(op).get("message")
		if m.v == nil {
			continue
		}
		list := []node{m}
		if one := m.get("oneOf"); one.v != nil {
			list = one.items()
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
func (r *reducer) channel3(id string, ch node) (*channel, error) {
	address, _ := ch.get("address").v.(string)
	if address == "" {
		return nil, nil // a channel whose address is only known at run time
	}
	c := &channel{id: id, address: address}
	var on []server
	for _, s := range ch.get("servers").items() {
		srv, err := r.l.deref(s)
		if err != nil {
			return nil, err
		}
		info, ok := r.servers[srv.ref()]
		if !ok {
			info = server{protocol: srv.str("protocol"), path: srv.str("pathname")}
		}
		on = append(on, info)
	}
	r.on(c, on)
	msgs := ch.get("messages")
	for _, name := range msgs.keys() {
		msg, err := r.message(msgs.get(name), name)
		if err != nil {
			return nil, err
		}
		c.messages = append(c.messages, msg)
	}
	return c, nil
}

// message reads a message: its name, content type, payload and headers.
func (r *reducer) message(n node, name string) (*message, error) {
	m, err := r.l.deref(n)
	if err != nil {
		return nil, err
	}
	msg := &message{name: m.str("name"), contentType: m.str("contentType")}
	if msg.name == "" {
		msg.name = name
	}
	if msg.contentType == "" {
		msg.contentType = r.defaultContentType
	}
	if msg.payload, err = r.schema(m.get("payload"), m.str("schemaFormat")); err != nil {
		return nil, fmt.Errorf("the payload of %s: %w", msg.name, err)
	}
	if msg.headers, err = r.schema(m.get("headers"), ""); err != nil {
		return nil, fmt.Errorf("the headers of %s: %w", msg.name, err)
	}
	return msg, nil
}

// schema compiles a payload or headers schema; a 3.x multi-format schema
// ({schemaFormat, schema}) gives its format itself.
func (r *reducer) schema(n node, format string) (*schema, error) {
	if n.v == nil {
		return nil, nil
	}
	d, err := r.l.deref(n)
	if err != nil {
		return nil, err
	}
	if f := d.str("schemaFormat"); f != "" && d.get("schema").v != nil {
		format, d = f, d.get("schema")
		if d, err = r.l.deref(d); err != nil {
			return nil, err
		}
	}
	switch kind := formatOf(format); kind {
	case "json":
		s, err := r.c.Compile(d.ref())
		if err != nil {
			return nil, err
		}
		return &schema{format: kind, json: s, raw: d.v, where: d.ref()}, nil
	case "avro":
		raw, err := json.Marshal(d.v)
		if err != nil {
			return nil, err
		}
		s, err := avro.ParseBytesWithCache(raw, "", &avro.SchemaCache{})
		if err != nil {
			return nil, fmt.Errorf("the Avro schema at %s: %w", d.ref(), err)
		}
		return &schema{format: kind, avro: s, raw: d.v, where: d.ref()}, nil
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

// fileURL is the URL of a document of the project.
func fileURL(p string) string {
	return (&url.URL{Scheme: "file", Path: path.Clean(strings.ReplaceAll(p, `\`, "/"))}).String()
}
