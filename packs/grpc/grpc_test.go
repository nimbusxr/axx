package grpc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcreflection "google.golang.org/grpc/reflection"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/protoload"
	"github.com/nimbusxr/axx/internal/tablevalue"
)

// tracking acts as the parcels service's tracking API does, with the
// messages of testdata/tracking.proto: a parcel's latest scan, and its
// scans as they happen.
type tracking struct {
	set     *protoload.Set
	auth    atomic.Value
	watches atomic.Int32
}

func (tr *tracking) msg(name string) *dynamicpb.Message {
	d, err := tr.set.FindDescriptorByName(protoreflect.FullName("parcels.tracking.v1." + name))
	if err != nil {
		panic(err)
	}
	return dynamicpb.NewMessage(d.(protoreflect.MessageDescriptor))
}

func set(m *dynamicpb.Message, field string, v protoreflect.Value) {
	m.Set(m.Descriptor().Fields().ByName(protoreflect.Name(field)), v)
}

func str(m *dynamicpb.Message, field string) string {
	return m.Get(m.Descriptor().Fields().ByName(protoreflect.Name(field))).String()
}

func (tr *tracking) scan(ref string, status protoreflect.EnumNumber, location string) *dynamicpb.Message {
	s := tr.msg("Scan")
	set(s, "reference", protoreflect.ValueOfString(ref))
	set(s, "status", protoreflect.ValueOfEnum(status))
	set(s, "location", protoreflect.ValueOfString(location))
	return s
}

func (tr *tracking) getParcel(_ any, ctx context.Context, dec func(any) error, _ grpclib.UnaryServerInterceptor) (any, error) {
	in := tr.msg("GetParcelRequest")
	if err := dec(in); err != nil {
		return nil, err
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		tr.auth.Store(strings.Join(md.Get("authorization"), ""))
	}
	ref := str(in, "reference")
	if ref == "PX-GRP-6199" {
		return nil, status.Error(codes.NotFound, "no parcel PX-GRP-6199")
	}
	_ = grpclib.SetHeader(ctx, metadata.Pairs("x-served-by", "tracking-v1"))
	_ = grpclib.SetTrailer(ctx, metadata.Pairs("x-scans", "2"))
	p := tr.msg("Parcel")
	set(p, "reference", protoreflect.ValueOfString(ref))
	set(p, "status", protoreflect.ValueOfEnum(2))
	set(p, "last_scan", protoreflect.ValueOfMessage(tr.scan(ref, 2, "Leipzig")))
	weight := in.Get(in.Descriptor().Fields().ByName("weight_grams")).Int()
	if weight == 0 {
		weight = 1200
	}
	set(p, "weight_grams", protoreflect.ValueOfInt64(weight))
	set(p, "postcode", protoreflect.ValueOfString(str(in, "postcode")))
	set(p, "weight_kg", protoreflect.ValueOfFloat64(6))
	customs := tr.msg("CustomsDeclaration")
	set(customs, "grams", protoreflect.ValueOfInt64(1200))
	set(customs, "value_eur", protoreflect.ValueOfFloat64(25))
	declared, err := proto.Marshal(customs)
	if err != nil {
		return nil, err
	}
	anyField := p.Descriptor().Fields().ByName("customs")
	held := dynamicpb.NewMessage(anyField.Message())
	set(held, "type_url", protoreflect.ValueOfString("type.googleapis.com/parcels.tracking.v1.CustomsDeclaration"))
	set(held, "value", protoreflect.ValueOfBytes(declared))
	p.Set(anyField, protoreflect.ValueOfMessage(held))
	return p, nil
}

func (tr *tracking) watchParcel(_ any, stream grpclib.ServerStream) error {
	in := tr.msg("WatchParcelRequest")
	if err := stream.RecvMsg(in); err != nil {
		return err
	}
	tr.watches.Add(1)
	defer tr.watches.Add(-1)
	ref := str(in, "reference")
	if err := stream.SendMsg(tr.scan(ref, 2, "Leipzig")); err != nil {
		return err
	}
	switch ref {
	case "PX-GRP-6198":
		return status.Error(codes.FailedPrecondition, "the parcel was returned")
	case "PX-GRP-6197": // a stream that goes on until the client leaves
		<-stream.Context().Done()
		return nil
	}
	time.Sleep(200 * time.Millisecond)
	return stream.SendMsg(tr.scan(ref, 3, "Leipzig"))
}

// serve starts the tracking service, with server reflection, and returns
// it and its address.
func serve(t *testing.T) (*tracking, string) {
	t.Helper()
	set, err := protoload.Compile("tracking.proto", []string{"testdata"})
	if err != nil {
		t.Fatal(err)
	}
	tr := &tracking{set: set}
	s := grpclib.NewServer()
	s.RegisterService(&grpclib.ServiceDesc{
		ServiceName: "parcels.tracking.v1.Tracking",
		HandlerType: (*any)(nil),
		Methods:     []grpclib.MethodDesc{{MethodName: "GetParcel", Handler: tr.getParcel}},
		Streams:     []grpclib.StreamDesc{{StreamName: "WatchParcel", Handler: tr.watchParcel, ServerStreams: true}},
	}, struct{}{})
	rpb.RegisterServerReflectionServer(s, grpcreflection.NewServerV1(grpcreflection.ServerOptions{Services: s, DescriptorResolver: set}))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	return tr, lis.Addr().String()
}

func TestUnaryCalls(t *testing.T) {
	_, addr := serve(t)
	h := cloudtest.New(t, Pack())
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}})
	h.OK("the parcels.tracking.v1.Tracking/GetParcel method is called on the tracking grpc service with the following fields:",
		[][]string{{"reference", "PX-GRP-6101"}, {"postcode", `"10115"`}})
	h.OK("the tracking grpc service answered OK")
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{
		{"reference", "PX-GRP-6101"},
		{"status", "OUT_FOR_DELIVERY"},
		{"lastScan.location", "Leipzig"},
		{"weightGrams", "1200"},
		{"signature", "false"},
		{"postcode", `"10115"`},
		{"lastScan.scannedAt", "undefined"},
	})
	h.OK("the tracking grpc service's answer has the following metadata:", [][]string{
		{"X-Served-By", "tracking-v1"}, {"x-scans", `"2"`}, {"x-missing", "undefined"},
	})
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "status", [][]string{{"status", "DELIVERED"}})
	// Checks are strict: a 64-bit integer is a number, not its text.
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "weightGrams is the integer 1200: write 1200",
		[][]string{{"weightGrams", `"1200"`}})
	_ = h.Fails("the tracking grpc service's answer has the following metadata:", `x-served-by: expected "tracking-v2", got "tracking-v1"`,
		[][]string{{"x-served-by", "tracking-v2"}})
	_ = h.Fails("the tracking grpc service's answer has the following metadata:", `x-served-by: expected none, got "tracking-v1"`,
		[][]string{{"x-served-by", "undefined"}})
	_ = h.Fails("the tracking grpc service's answer has the following metadata:", `x-missing: expected "tracking-v1", got none`,
		[][]string{{"x-missing", "tracking-v1"}})
	// Metadata is text: a value that reads as a number is in double quotes.
	_ = h.Fails("the tracking grpc service's answer has the following metadata:", `x-scans: 2 reads as an integer, and only text fits here: write "2" for the text`,
		[][]string{{"x-scans", "2"}})
	_ = h.Fails("the tracking grpc service's answer has the following metadata:", `x-scans: the cell is empty: write "" for the empty text, or undefined for none`,
		[][]string{{"x-scans", ""}})

	// A status other than OK is the call's, for the checks.
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6199"}})
	h.OK("the tracking grpc service answered NOT_FOUND with a message containing 'PX-GRP-6199'")
	h.OK("the tracking grpc service answered 5")
	_ = h.Fails("the tracking grpc service answered OK",
		`The tracking grpc service answered NOT_FOUND ("no parcel PX-GRP-6199") to parcels.tracking.v1.Tracking/GetParcel, not OK`)
	_ = h.Fails("the tracking grpc service answered NOT_FOUND with a message containing 'returned'", `whose message does not contain "returned"`)
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "with no answer to check", [][]string{{"status", "DELIVERED"}})
	_ = h.Fails("the tracking grpc service answered LOST", `"LOST" is not a gRPC status; the statuses are ABORTED, ALREADY_EXISTS`)
}

// A double in an answer is a decimal, a whole one too, and an Any's fields are
// numbers as a message's are.
func TestAnswersKeepTheirNumbers(t *testing.T) {
	_, addr := serve(t)
	h := cloudtest.New(t, Pack())
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}})
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6111"}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{
		{"weightKg", "6.0"}, {"customs.grams", "1200"}, {"customs.valueEur", "25.0"},
	})
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "weightKg is the decimal 6.0: write 6.0", [][]string{{"weightKg", "6"}})
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "customs.grams is the integer 1200: write 1200", [][]string{{"customs.grams", `"1200"`}})
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "customs.valueEur is the decimal 25.0: write 25.0", [][]string{{"customs.valueEur", "25"}})
}

// typedNumbers follows a message's descriptors everywhere proto JSON holds a
// number: fields, nested messages, lists, maps, wrappers and Anys.
func TestProtoJSONNumbers(t *testing.T) {
	set, err := protoload.Compile("numbers.proto", []string{"testdata"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := set.FindDescriptorByName("parcels.numbers.v1.Weighing")
	if err != nil {
		t.Fatal(err)
	}
	const anyOf = `"@type":"type.googleapis.com/`
	for _, c := range []struct{ in, want string }{
		{`{"kilos":6}`, `"kilos":6.0`},
		{`{"kilos":6.5}`, `"kilos":6.5`},
		{`{"kilos":1e21}`, `"kilos":1e+21`},
		{`{"kilos":-3}`, `"kilos":-3.0`},
		{`{"kilos":"NaN"}`, `"kilos":"NaN"`},
		{`{"kilos":"Infinity"}`, `"kilos":"Infinity"`},
		{`{"kilos":"-Infinity"}`, `"kilos":"-Infinity"`},
		{`{}`, `"kilos":0.0,"pounds":0.0,"grams":0,`},
		{`{"pounds":2}`, `"pounds":2.0`},
		{`{"pounds":1.1}`, `"pounds":1.1`},
		{`{"grams":"1200"}`, `"grams":1200`},
		{`{"pieces":3}`, `"pieces":3`},
		{`{"readings":[1,2.5,"NaN"]}`, `"readings":[1.0,2.5,"NaN"]`},
		{`{"parts":[{"kilos":1,"grams":"2"}]}`, `{"kilos":1.0,"pounds":0.0,"grams":2,`},
		{`{"byDepot":{"LEJ":4}}`, `"byDepot":{"LEJ":4.0}`},
		{`{"byParcel":{"PX-1":{"kilos":2}}}`, `"byParcel":{"PX-1":{"kilos":2.0,`},
		{`{"byId":{"12":7}}`, `"byId":{"12":7.0}`},
		{`{"grams_by_depot":{"LEJ":"12"}}`, `"gramsByDepot":{"LEJ":12}`},
		{`{"tare":1}`, `"tare":1.0`},
		{`{"margin":2}`, `"margin":2.0`},
		{`{"total":"12"}`, `"total":12`},
		{`{"extra":{"kilos":6,"fields":{"kilos":7},"grams":"12"}}`, `"extra":{"fields":{"kilos":7},"grams":"12","kilos":6}`},
		{`{"detail":{` + anyOf + `parcels.numbers.v1.Weighing","kilos":3,"grams":"4"}}`, `"detail":{` + anyOf + `parcels.numbers.v1.Weighing","kilos":3.0,"pounds":0.0,"grams":4,`},
		{`{"detail":{` + anyOf + `google.protobuf.Int64Value","value":"12"}}`, `"detail":{` + anyOf + `google.protobuf.Int64Value","value":12}`},
		{`{"detail":{` + anyOf + `google.protobuf.DoubleValue","value":6}}`, `"detail":{` + anyOf + `google.protobuf.DoubleValue","value":6.0}`},
		{`{"detail":{` + anyOf + `google.protobuf.Struct","value":{"kilos":6}}}`, `"detail":{` + anyOf + `google.protobuf.Struct","value":{"kilos":6}}`},
		{
			`{"detail":{` + anyOf + `google.protobuf.Any","value":{` + anyOf + `google.protobuf.Int64Value","value":"12"}}}`,
			`"detail":{` + anyOf + `google.protobuf.Any","value":{` + anyOf + `google.protobuf.Int64Value","value":12}}`,
		},
		{`{"details":[{` + anyOf + `parcels.numbers.v1.Reading","value":6}]}`, `"details":[{` + anyOf + `parcels.numbers.v1.Reading","value":6.0}]`},
	} {
		m := dynamicpb.NewMessage(d.(protoreflect.MessageDescriptor))
		if err := (protojson.UnmarshalOptions{Resolver: set.Types()}).Unmarshal([]byte(c.in), m); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		got, err := marshal(m, set)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !strings.Contains(string(got), c.want) {
			t.Errorf("%s:\n got  %s\n want %s", c.in, got, c.want)
		}
		if !json.Valid(got) {
			t.Errorf("%s: not JSON: %s", c.in, got)
		}
	}
}

func TestRequests(t *testing.T) {
	_, addr := serve(t)
	h := cloudtest.New(t, Pack())
	if err := os.CopyFS(h.Dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	h.File("grpc/get-parcel.json", `{"reference": "PX-GRP-6102", "postcode": "01067"}`)
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}, {"proto", "tracking.proto"}})
	h.OK("the Tracking/GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message")
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "PX-GRP-6102"}, {"postcode", `"01067"`}})
	h.OK("the GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:",
		[][]string{{"reference", "PX-GRP-6103"}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "PX-GRP-6103"}, {"postcode", `"01067"`}})
	// Text in double quotes is that text: the postcode keeps its zero.
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:",
		[][]string{{"reference", "PX-GRP-6106"}, {"postcode", `"01067"`}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "PX-GRP-6106"}, {"postcode", `"01067"`}})
	// Proto names do as well as JSON names.
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", `"6104"`}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", `"6104"`}})
	// A 64-bit integer is a number, in the request as in the answer.
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:",
		[][]string{{"reference", "PX-GRP-6107"}, {"weight_grams", "2400"}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"weightGrams", "2400"}})
	// A field takes only its type: nothing converts a value to it.
	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the following fields:",
		`postcode is a string field: write "10115"`, [][]string{{"reference", "PX-GRP-6105"}, {"postcode", "10115"}})
	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:",
		`weightGrams is an int64 field: write 2400`, [][]string{{"weightGrams", `"2400"`}})
	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the following fields:",
		`postcode: 01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`,
		[][]string{{"reference", "PX-GRP-6105"}, {"postcode", "01067"}})

	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the following fields:",
		`the request is not a parcels.tracking.v1.GetParcelRequest: `, [][]string{{"referenc", "PX-GRP-6105"}})
	_ = h.Fails("the GetParcels method is called on the tracking grpc service",
		"the service has no method GetParcels; it has parcels.tracking.v1.Tracking/GetParcel, parcels.tracking.v1.Tracking/WatchParcel")
	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:",
		"the fields are missing")
}

// A row's value must be of its field's type, at any path: a field of a
// message, an item of a list, a value of a map.
func TestFieldsTakeTheirTypes(t *testing.T) {
	set, err := protoload.Compile("tracking.proto", []string{"testdata"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := set.FindDescriptorByName("parcels.tracking.v1.Parcel")
	if err != nil {
		t.Fatal(err)
	}
	md := d.(protoreflect.MessageDescriptor)
	for _, c := range []struct {
		path, value, want string
	}{
		{"reference", `"PX-GRP-6110"`, ""},
		{"reference", "PX-GRP-6110", ""},
		{"reference", "null", ""},
		{"reference", "undefined", ""},
		{"postcode", "10115", `postcode is a string field: write "10115"`},
		{"postcode", "true", `postcode is a string field: write "true"`},
		{"status", "OUT_FOR_DELIVERY", ""},
		{"status", "2", ""},
		{"status", "true", "status is a parcels.tracking.v1.Status field: write the name of one of its values"},
		{"weightGrams", "1200", ""},
		{"weight_grams", "1200L", ""},
		{"weightGrams", `"1200"`, "weightGrams is an int64 field: write 1200"},
		{"weightGrams", "12.5", "weightGrams is an int64 field: write a whole number"},
		{"signature", "false", ""},
		{"signature", `"true"`, "signature is a bool field: write true"},
		{"signature", "1", "signature is a bool field: write true or false"},
		{"lastScan.location", "Leipzig", ""},
		{"lastScan.location", "4", `lastScan.location is a string field: write "4"`},
		{"lastScan", `{"location": 4}`, `lastScan.location is a string field: write "4"`},
		{"lastScan", "Leipzig", "lastScan is a parcels.tracking.v1.Scan field: write a JSON object, or set its fields (lastScan.<field>)"},
		{"lastScan.scannedAt", "2026-10-01T10:00:00Z", ""},
		{"$.lastScan.location", "4", `lastScan.location is a string field: write "4"`},
		{"unknown", "4", ""},
	} {
		err := checkRows(md, []tablevalue.Row{{Path: c.path, Value: c.value}})
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s = %s: %v", c.path, c.value, err)
		case c.want != "" && (err == nil || err.Error() != c.want):
			t.Errorf("%s = %s: got %v, want %s", c.path, c.value, err, c.want)
		}
	}
}

func TestReflectionAndProtoAgree(t *testing.T) {
	_, addr := serve(t)
	for _, rows := range [][][]string{{{"address", addr}}, {{"address", addr}, {"proto", "tracking.proto"}}} {
		h := cloudtest.New(t, Pack())
		if err := os.CopyFS(h.Dir, os.DirFS("testdata")); err != nil {
			t.Fatal(err)
		}
		h.OK("the tracking grpc service with the following properties:", rows)
		h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6106"}})
		h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"lastScan.status", "OUT_FOR_DELIVERY"}})
	}
}

func TestStreams(t *testing.T) {
	tr, addr := serve(t)
	h := cloudtest.New(t, Pack())
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}})
	h.OK("the WatchParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6107"}})
	h.OK("within 5s the tracking grpc service streamed a message where:", [][]string{{"reference", "PX-GRP-6107"}, {"status", "OUT_FOR_DELIVERY"}})
	h.OK("within 5s the tracking grpc service streamed a message where:", [][]string{{"status", "DELIVERED"}})
	h.OK("within 5s the tracking grpc service answered OK")
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "is a server stream", [][]string{{"status", "DELIVERED"}})

	// A stream that ended fails a check at once, with how it ended.
	h.OK("the WatchParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6198"}})
	start := time.Now()
	_ = h.Fails("within 1m the tracking grpc service streamed a message where:",
		`the stream ended with FAILED_PRECONDITION ("the parcel was returned")`, [][]string{{"status", "DELIVERED"}})
	if time.Since(start) > 10*time.Second {
		t.Errorf("the check waited %s for a stream that had ended", time.Since(start))
	}
	h.OK("the tracking grpc service answered FAILED_PRECONDITION")

	// A stream belongs to its scenario: it ends with it.
	h.OK("the WatchParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6197"}})
	h.OK("within 5s the tracking grpc service streamed a message where:", [][]string{{"reference", "PX-GRP-6197"}})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for tr.watches.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := tr.watches.Load(); n != 0 {
		t.Errorf("%d streams still open after their scenario ended", n)
	}
}

func TestHeadersAndSecrets(t *testing.T) {
	tr, addr := serve(t)
	t.Setenv("TRACKING_TOKEN", "tk-4711-secret")
	h := cloudtest.New(t, Pack())
	h.OK("the tracking grpc service with the following properties:", [][]string{
		{"address", addr}, {"header.Authorization", "Bearer ${env:TRACKING_TOKEN}"}, {"timeout", "2s"},
	})
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6108"}})
	if got := tr.auth.Load(); got != "Bearer tk-4711-secret" {
		t.Errorf("authorization: %v", got)
	}
	for _, l := range h.Sink.Logs {
		if strings.Contains(l, "tk-4711-secret") {
			t.Errorf("the token is in the logs: %s", l)
		}
	}
	_ = h.Fails("the tracking grpc service with the following properties:", `unknown grpc service property "host"`, [][]string{{"host", addr}})
	_ = h.Fails("the other grpc service with the following properties:", `the other grpc service's tls is true or false, not "yes"`,
		[][]string{{"address", addr}, {"tls", "yes"}})
	_ = h.Fails("the GetParcel method is called on the other grpc service", `no grpc service named "other"`)
}

func TestAServiceThatIsNotThere(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()
	h := cloudtest.New(t, Pack())
	if err := os.CopyFS(h.Dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}, {"proto", "tracking.proto"}, {"timeout", "2s"}})
	h.OK("the GetParcel method is called on the tracking grpc service")
	_ = h.Fails("the tracking grpc service answered OK", "The tracking grpc service answered UNAVAILABLE")
	h.OK("the other grpc service with the following properties:", [][]string{{"address", addr}, {"timeout", "2s"}})
	_ = h.Fails("the GetParcel method is called on the other grpc service", "through its server reflection (give the service a proto instead)")
}
