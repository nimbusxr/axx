package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/protoload"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/tablevalue"
)

// call is a call a scenario made: a unary call's answer, or a server
// stream's messages as they come.
type call struct {
	method  protoreflect.MethodDescriptor
	request []byte // proto JSON

	mu      sync.Mutex
	status  *status.Status // nil while a stream runs
	answer  []byte         // a unary call's answer, as proto JSON
	header  metadata.MD
	trailer metadata.MD

	stream *cloudstep.Inbox // a server stream's messages
	cancel context.CancelFunc
}

func (c *call) name() string { return methodName(c.method) }

// result is the call's status, and whether it has one yet.
func (c *call) result() (*status.Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.status != nil
}

func (c *call) stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *call) describe(sc *core.Scenario) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := map[string]any{"method": c.name(), "request": secrets.Mask(sc, string(c.request))}
	if c.status != nil {
		d["status"] = codeName(c.status.Code())
		if m := c.status.Message(); m != "" {
			d["message"] = secrets.Mask(sc, m)
		}
	}
	if c.answer != nil {
		d["answer"] = secrets.Mask(sc, string(c.answer))
	}
	return d
}

// findMethod finds the method a step names: package.Service/Method, or a
// shorter name that only one of the services' methods has.
func findMethod(set *protoload.Set, name string) (protoreflect.MethodDescriptor, error) {
	svcPart, method, qualified := strings.Cut(name, "/")
	if !qualified {
		svcPart, method = "", name
	}
	var found, all []protoreflect.MethodDescriptor
	for _, svc := range set.Services() {
		if strings.HasPrefix(string(svc.FullName()), "grpc.reflection.") {
			continue
		}
		ms := svc.Methods()
		for i := range ms.Len() {
			m := ms.Get(i)
			all = append(all, m)
			if string(m.Name()) != method {
				continue
			}
			full := string(svc.FullName())
			if svcPart == "" || full == svcPart || strings.HasSuffix(full, "."+svcPart) {
				found = append(found, m)
			}
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, fmt.Errorf("the service has no method %s; it has %s", name, methodList(all))
	}
	return nil, fmt.Errorf("%s names several methods (%s): name it as package.Service/Method", name, methodList(found))
}

func methodList(ms []protoreflect.MethodDescriptor) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = methodName(m)
	}
	sort.Strings(names)
	if len(names) > 20 {
		names = append(names[:20], "...")
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// methodName is package.Service/Method.
func methodName(m protoreflect.MethodDescriptor) string {
	return string(m.Parent().FullName()) + "/" + string(m.Name())
}

// request builds a method's request: the file's JSON, or an empty one,
// with the table's rows set into it, as the REST pack's request properties
// are. Scalars take the types of their fields, and a string field's value
// is its text as written (01067 keeps its zero).
func request(sc *core.Scenario, md protoreflect.MessageDescriptor, set *protoload.Set, file string, t *core.Table) (*dynamicpb.Message, []byte, error) {
	var v any = map[string]any{}
	if file != "" {
		p, err := sc.Suite().ResolvePath(file)
		if err != nil {
			return nil, nil, err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		text, err := secrets.Resolve(sc, string(raw))
		if err != nil {
			return nil, nil, err
		}
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, nil, fmt.Errorf("%s is not JSON: %w", file, err)
		}
	}
	if base, ok := v.(map[string]any); ok && t != nil {
		pairs, err := t.Pairs()
		if err != nil {
			return nil, nil, err
		}
		rows := make([]tablevalue.Row, len(pairs))
		for i, p := range pairs {
			rows[i] = tablevalue.Row{Path: p.Key, Null: p.Null}
			if !p.Null {
				if rows[i].Value, err = secrets.Resolve(sc, p.Value); err != nil {
					return nil, nil, err
				}
			}
		}
		if v, err = tablevalue.Apply(base, rows, func(path string) bool { return stringField(md, path) }); err != nil {
			return nil, nil, err
		}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		b, _ := json.Marshal(v)
		return nil, nil, fmt.Errorf("a %s is a JSON object, not %s", md.FullName(), b)
	}
	conform(md, obj)
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, err
	}
	msg := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{Resolver: set.Types()}).Unmarshal(body, msg); err != nil {
		return nil, nil, fmt.Errorf("the request is not a %s: %s", md.FullName(), protoError(err))
	}
	return msg, body, nil
}

// protoError is a protojson error without its package's prefix.
func protoError(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"proto: ", "proto: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

// stringField reports whether a path into a message (`recipient.postcode`,
// `lines[0].reference`) is a string field.
func stringField(md protoreflect.MessageDescriptor, path string) bool {
	var fd protoreflect.FieldDescriptor
	for _, seg := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if md == nil {
			return false
		}
		name, _, _ := strings.Cut(seg, "[")
		fields := md.Fields()
		if fd = fields.ByJSONName(name); fd == nil {
			fd = fields.ByName(protoreflect.Name(name))
		}
		if fd == nil {
			return false
		}
		md = fd.Message()
	}
	return fd != nil && fd.Kind() == protoreflect.StringKind
}

// conform gives a JSON object's scalars the types of the message's
// fields: the text of a number for a string field, a boolean for "true".
func conform(md protoreflect.MessageDescriptor, obj map[string]any) {
	fields := md.Fields()
	for k, v := range obj {
		fd := fields.ByJSONName(k)
		if fd == nil {
			fd = fields.ByName(protoreflect.Name(k))
		}
		if fd == nil {
			continue // protojson says it is not a field
		}
		switch {
		case fd.IsList():
			if arr, ok := v.([]any); ok {
				for i, e := range arr {
					arr[i] = conformValue(fd, e)
				}
			}
		case fd.IsMap():
			if m, ok := v.(map[string]any); ok {
				for mk, mv := range m {
					m[mk] = conformValue(fd.MapValue(), mv)
				}
			}
		default:
			obj[k] = conformValue(fd, v)
		}
	}
}

func conformValue(fd protoreflect.FieldDescriptor, v any) any {
	switch fd.Kind() {
	case protoreflect.StringKind:
		switch x := v.(type) {
		case json.Number:
			return x.String()
		case bool:
			return strconv.FormatBool(x)
		}
	case protoreflect.BoolKind:
		if s, ok := v.(string); ok {
			if b, err := strconv.ParseBool(s); err == nil {
				return b
			}
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if m, ok := v.(map[string]any); ok && !strings.HasPrefix(string(fd.Message().FullName()), "google.protobuf.") {
			conform(fd.Message(), m)
		}
	}
	return v
}

// invoke makes a call and keeps it as the service's last. A status other
// than OK is the call's, which the checks read; only what keeps the call
// from being made fails the step.
func (s *service) invoke(sc *core.Scenario, methodName, file string, t *core.Table) error {
	set, err := s.descriptors(sc)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	m, err := findMethod(set, methodName)
	if err != nil {
		return err
	}
	if m.IsStreamingClient() {
		return fmt.Errorf("%s is a client-streaming method: the grpc pack makes unary calls and server streams", methodName)
	}
	req, body, err := request(sc, m.Input(), set, file, t)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	conn, err := s.conn(sc.Suite())
	if err != nil {
		return secrets.Hide(sc, err)
	}
	c := &call{method: m, request: body}
	s.mu.Lock()
	if s.last != nil {
		s.last.stop()
	}
	s.last = c
	s.mu.Unlock()
	sc.Attach("application/json", []byte(secrets.Mask(sc, string(body))), methodName+" request")
	path := "/" + string(m.Parent().FullName()) + "/" + string(m.Name())
	if m.IsStreamingServer() {
		return s.stream(sc, c, conn, path, req, set)
	}
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, s.headers)
	out := dynamicpb.NewMessage(m.Output())
	var header, trailer metadata.MD
	err = conn.Invoke(ctx, path, req, out, grpclib.Header(&header), grpclib.Trailer(&trailer))
	st := status.New(codes.OK, "")
	if err != nil {
		st = status.Convert(err)
	}
	var answer []byte
	if st.Code() == codes.OK {
		if answer, err = marshal(out, set); err != nil {
			return err
		}
		sc.Attach("application/json", []byte(secrets.Mask(sc, string(answer))), methodName+" answer")
	}
	c.mu.Lock()
	c.status, c.answer, c.header, c.trailer = st, answer, header, trailer
	c.mu.Unlock()
	sc.Log("called %s on the %s grpc service: %s", methodName, s.name, secrets.Mask(sc, statusText(st)))
	return nil
}

// stream starts a server-streaming call, whose messages come in the
// background until it ends or the scenario does.
func (s *service) stream(sc *core.Scenario, c *call, conn *grpclib.ClientConn, path string, req *dynamicpb.Message, set *protoload.Set) error {
	// The stream lives for the scenario, not for the step that starts it.
	ctx, cancel := context.WithCancel(metadata.NewOutgoingContext(context.Background(), s.headers))
	c.cancel, c.stream = cancel, &cloudstep.Inbox{}
	cs, err := conn.NewStream(ctx, &grpclib.StreamDesc{ServerStreams: true}, path)
	if err == nil {
		if err = cs.SendMsg(req); err == nil {
			err = cs.CloseSend()
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		st := status.Convert(err)
		c.finish(st, nil)
		sc.Log("could not start %s on the %s grpc service: %s", c.name(), s.name, secrets.Mask(sc, statusText(st)))
		return nil
	}
	sc.Log("started %s on the %s grpc service", c.name(), s.name)
	go func() {
		header, _ := cs.Header()
		c.mu.Lock()
		c.header = header
		c.mu.Unlock()
		for {
			m := dynamicpb.NewMessage(c.method.Output())
			if err := cs.RecvMsg(m); err != nil {
				st := status.New(codes.OK, "")
				if !errors.Is(err, io.EOF) {
					st = status.Convert(err)
				}
				c.finish(st, cs.Trailer())
				return
			}
			body, err := marshal(m, set)
			if err != nil {
				c.stream.Fail(err)
				return
			}
			c.stream.Add(cloudstep.Message{Body: body})
		}
	}()
	return nil
}

// finish records how a stream ended.
func (c *call) finish(st *status.Status, trailer metadata.MD) {
	c.mu.Lock()
	c.status, c.trailer = st, trailer
	c.mu.Unlock()
	if c.stream != nil {
		c.stream.End("the stream ended with " + statusText(st))
	}
}

// marshal writes a message as proto JSON, with its fields' default values.
func marshal(m *dynamicpb.Message, set *protoload.Set) ([]byte, error) {
	b, err := protojson.MarshalOptions{EmitDefaultValues: true, Resolver: set.Types()}.Marshal(m)
	if err != nil {
		return nil, err
	}
	// protojson varies its spacing on purpose; the checks and the reports
	// read compact JSON.
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// statusText is a status as the steps name it, with its message.
func statusText(st *status.Status) string {
	if m := st.Message(); m != "" {
		return fmt.Sprintf("%s (%q)", codeName(st.Code()), m)
	}
	return codeName(st.Code())
}
