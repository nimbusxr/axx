package grpc

import (
	"context"
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
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/protoload"
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
	set(p, "weight_grams", protoreflect.ValueOfInt64(1200))
	set(p, "postcode", protoreflect.ValueOfString(str(in, "postcode")))
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
		[][]string{{"reference", "PX-GRP-6101"}, {"postcode", "10115"}})
	h.OK("the tracking grpc service answered OK")
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{
		{"reference", "PX-GRP-6101"},
		{"status", "OUT_FOR_DELIVERY"},
		{"lastScan.location", "Leipzig"},
		{"weightGrams", "1200"},
		{"signature", "false"},
		{"postcode", "10115"},
		{"lastScan.scannedAt", "undefined"},
	})
	h.OK("the tracking grpc service's answer has the following metadata:", [][]string{
		{"X-Served-By", "tracking-v1"}, {"x-scans", "2"}, {"x-missing", "undefined"},
	})
	_ = h.Fails("the tracking grpc service's answer has the following fields:", "status", [][]string{{"status", "DELIVERED"}})
	_ = h.Fails("the tracking grpc service's answer has the following metadata:", `x-served-by: expected "tracking-v2", got "tracking-v1"`,
		[][]string{{"x-served-by", "tracking-v2"}})

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

func TestRequests(t *testing.T) {
	_, addr := serve(t)
	h := cloudtest.New(t, Pack())
	if err := os.CopyFS(h.Dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	h.File("grpc/get-parcel.json", `{"reference": "PX-GRP-6102", "postcode": "01067"}`)
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}, {"proto", "tracking.proto"}})
	h.OK("the Tracking/GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message")
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "PX-GRP-6102"}, {"postcode", "01067"}})
	h.OK("the GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:",
		[][]string{{"reference", "PX-GRP-6103"}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "PX-GRP-6103"}, {"postcode", "01067"}})
	// A string field is its text as written: the postcode keeps its zero.
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:",
		[][]string{{"reference", "PX-GRP-6106"}, {"postcode", "01067"}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "PX-GRP-6106"}, {"postcode", "01067"}})
	// Proto names do as well as JSON names.
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", `"6104"`}})
	h.OK("the tracking grpc service's answer has the following fields:", [][]string{{"reference", "6104"}})

	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the following fields:",
		`the request is not a parcels.tracking.v1.GetParcelRequest: `, [][]string{{"referenc", "PX-GRP-6105"}})
	_ = h.Fails("the GetParcels method is called on the tracking grpc service",
		"the service has no method GetParcels; it has parcels.tracking.v1.Tracking/GetParcel, parcels.tracking.v1.Tracking/WatchParcel")
	_ = h.Fails("the GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:",
		"the fields are missing")
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
