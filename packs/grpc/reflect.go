package grpc

import (
	"context"
	"fmt"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	rpbalpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/nimbusxr/axx/internal/protoload"
)

// reflect reads the descriptors of every service a server's reflection
// lists, and of the files they import.
func reflect(ctx context.Context, conn *grpclib.ClientConn, address string) (*protoload.Set, error) {
	r, err := openReflection(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("cannot read the services of %s through its server reflection (give the service a proto instead): %w", address, err)
	}
	defer r.close()
	names, err := r.services()
	if err != nil {
		return nil, fmt.Errorf("cannot list the services of %s through its server reflection: %w", address, err)
	}
	files := map[string]*descriptorpb.FileDescriptorProto{}
	var order []*descriptorpb.FileDescriptorProto
	add := func(raw [][]byte) error {
		for _, b := range raw {
			fd := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(b, fd); err != nil {
				return err
			}
			if _, ok := files[fd.GetName()]; !ok {
				files[fd.GetName()] = fd
				order = append(order, fd)
			}
		}
		return nil
	}
	for _, name := range names {
		raw, err := r.fileContaining(name)
		if err != nil {
			return nil, fmt.Errorf("cannot read the %s service of %s through its server reflection: %w", name, address, err)
		}
		if err := add(raw); err != nil {
			return nil, err
		}
	}
	// The imports a server left out of its answers.
	for i := 0; i < len(order); i++ {
		for _, dep := range order[i].GetDependency() {
			if _, ok := files[dep]; ok {
				continue
			}
			if _, err := protoregistry.GlobalFiles.FindFileByPath(dep); err == nil {
				continue
			}
			raw, err := r.fileByName(dep)
			if err != nil {
				return nil, fmt.Errorf("cannot read %s from %s through its server reflection: %w", dep, address, err)
			}
			if err := add(raw); err != nil {
				return nil, err
			}
		}
	}
	return protoload.FromProtos(order, "the server reflection of "+address, "the server does not serve it")
}

// reflection asks a server's reflection service, v1 or v1alpha.
type reflection interface {
	services() ([]string, error)
	fileContaining(symbol string) ([][]byte, error)
	fileByName(name string) ([][]byte, error)
	close()
}

func openReflection(ctx context.Context, conn *grpclib.ClientConn) (reflection, error) {
	ctx, cancel := context.WithCancel(ctx)
	stream, err := rpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err == nil {
		r := &reflectionV1{stream: stream, cancel: cancel}
		// Servers that only have v1alpha answer Unimplemented to the first request.
		if _, err = r.services(); err == nil {
			return r, nil
		}
		if status.Code(err) != codes.Unimplemented {
			cancel()
			return nil, err
		}
	}
	return openAlpha(ctx, conn, cancel)
}

type reflectionV1 struct {
	stream rpb.ServerReflection_ServerReflectionInfoClient
	cancel context.CancelFunc
}

func (r *reflectionV1) close() { r.cancel() }

func (r *reflectionV1) ask(req *rpb.ServerReflectionRequest) (*rpb.ServerReflectionResponse, error) {
	if err := r.stream.Send(req); err != nil {
		return nil, err
	}
	res, err := r.stream.Recv()
	if err != nil {
		return nil, err
	}
	if e := res.GetErrorResponse(); e != nil {
		return nil, status.Error(codes.Code(e.GetErrorCode()), e.GetErrorMessage())
	}
	return res, nil
}

func (r *reflectionV1) services() ([]string, error) {
	res, err := r.ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_ListServices{}})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range res.GetListServicesResponse().GetService() {
		names = append(names, s.GetName())
	}
	return names, nil
}

func (r *reflectionV1) fileContaining(symbol string) ([][]byte, error) {
	res, err := r.ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: symbol}})
	return res.GetFileDescriptorResponse().GetFileDescriptorProto(), err
}

func (r *reflectionV1) fileByName(name string) ([][]byte, error) {
	res, err := r.ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_FileByFilename{FileByFilename: name}})
	return res.GetFileDescriptorResponse().GetFileDescriptorProto(), err
}

// reflectionAlpha is the older reflection service, v1alpha, which some
// servers still speak alone.
//
//nolint:staticcheck // v1alpha is deprecated, and what those servers have
type reflectionAlpha struct {
	stream rpbalpha.ServerReflection_ServerReflectionInfoClient
	cancel context.CancelFunc
}

//nolint:staticcheck // see reflectionAlpha
func openAlpha(ctx context.Context, conn *grpclib.ClientConn, cancel context.CancelFunc) (reflection, error) {
	stream, err := rpbalpha.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	return &reflectionAlpha{stream: stream, cancel: cancel}, nil
}

func (r *reflectionAlpha) close() { r.cancel() }

//nolint:staticcheck // see reflectionAlpha
func (r *reflectionAlpha) ask(req *rpbalpha.ServerReflectionRequest) (*rpbalpha.ServerReflectionResponse, error) {
	if err := r.stream.Send(req); err != nil {
		return nil, err
	}
	res, err := r.stream.Recv()
	if err != nil {
		return nil, err
	}
	if e := res.GetErrorResponse(); e != nil {
		return nil, status.Error(codes.Code(e.GetErrorCode()), e.GetErrorMessage())
	}
	return res, nil
}

//nolint:staticcheck // see reflectionAlpha
func (r *reflectionAlpha) services() ([]string, error) {
	res, err := r.ask(&rpbalpha.ServerReflectionRequest{MessageRequest: &rpbalpha.ServerReflectionRequest_ListServices{}})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range res.GetListServicesResponse().GetService() {
		names = append(names, s.GetName())
	}
	return names, nil
}

//nolint:staticcheck // see reflectionAlpha
func (r *reflectionAlpha) fileContaining(symbol string) ([][]byte, error) {
	res, err := r.ask(&rpbalpha.ServerReflectionRequest{MessageRequest: &rpbalpha.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: symbol}})
	return res.GetFileDescriptorResponse().GetFileDescriptorProto(), err
}

//nolint:staticcheck // see reflectionAlpha
func (r *reflectionAlpha) fileByName(name string) ([][]byte, error) {
	res, err := r.ask(&rpbalpha.ServerReflectionRequest{MessageRequest: &rpbalpha.ServerReflectionRequest_FileByFilename{FileByFilename: name}})
	return res.GetFileDescriptorResponse().GetFileDescriptorProto(), err
}
