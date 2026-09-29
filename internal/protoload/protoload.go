// Package protoload reads protobuf descriptors: .proto files compiled in
// process, descriptor sets (protoc --descriptor_set_out), and the file
// descriptors a gRPC server's reflection returns. The fixtures' protobuf
// family and the grpc pack read their schemas with it.
package protoload

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Set is a set of proto files, with every file they import.
type Set struct {
	files *protoregistry.Files
	// roots are the files the set was read for, not their imports.
	roots []protoreflect.FileDescriptor
}

// Compile compiles source, a .proto file found under importPaths, or one of
// the standard imports such as google/protobuf/type.proto, with its imports.
func Compile(source string, importPaths []string) (*Set, error) {
	c := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: importPaths})}
	files, err := c.Compile(context.Background(), source)
	if err != nil {
		return nil, err
	}
	s := &Set{files: new(protoregistry.Files)}
	for _, f := range files {
		if err := s.register(f); err != nil {
			return nil, err
		}
		s.roots = append(s.roots, f)
	}
	return s, nil
}

// register registers a file after its imports.
func (s *Set) register(f protoreflect.FileDescriptor) error {
	if _, err := s.files.FindFileByPath(f.Path()); err == nil {
		return nil
	}
	imports := f.Imports()
	for i := range imports.Len() {
		if err := s.register(imports.Get(i).FileDescriptor); err != nil {
			return err
		}
	}
	return s.files.RegisterFile(f)
}

// DescriptorSet reads a FileDescriptorSet file; name names it in errors.
func DescriptorSet(path, name string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read descriptor set %s: %w", name, err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("cannot read descriptor set %s: %w", name, err)
	}
	return FromProtos(set.GetFile(), name, "regenerate with --include_imports")
}

// FromProtos builds a set from file descriptor protos, each after the
// files it imports; name names where they came from in errors, and fix
// says how to supply a missing import. The well-known types need not be
// among them.
func FromProtos(fds []*descriptorpb.FileDescriptorProto, name, fix string) (*Set, error) {
	protos := map[string]*descriptorpb.FileDescriptorProto{}
	for _, f := range fds {
		protos[f.GetName()] = f
	}
	s := &Set{files: new(protoregistry.Files)}
	var build func(n string) error
	build = func(n string) error {
		if _, err := s.files.FindFileByPath(n); err == nil {
			return nil
		}
		fp, ok := protos[n]
		if !ok {
			if _, err := protoregistry.GlobalFiles.FindFileByPath(n); err == nil {
				return nil // a well-known import the set omitted
			}
			return fmt.Errorf("%s: dependency '%s' missing - %s", name, n, fix)
		}
		for _, dep := range fp.GetDependency() {
			if err := build(dep); err != nil {
				return err
			}
		}
		fd, err := protodesc.NewFile(fp, chain{s.files, protoregistry.GlobalFiles})
		if err != nil {
			return fmt.Errorf("%s is not a valid descriptor set: %w", name, err)
		}
		return s.files.RegisterFile(fd)
	}
	for _, f := range fds {
		if err := build(f.GetName()); err != nil {
			return nil, err
		}
		fd, _ := s.files.FindFileByPath(f.GetName())
		s.roots = append(s.roots, fd)
	}
	return s, nil
}

// FindDescriptorByName finds a message, an enum, a service or a method by
// its full name, in the set or among the well-known types.
func (s *Set) FindDescriptorByName(n protoreflect.FullName) (protoreflect.Descriptor, error) {
	return chain{s.files, protoregistry.GlobalFiles}.FindDescriptorByName(n)
}

// FindFileByPath finds a file of the set, or a well-known one, by its path.
func (s *Set) FindFileByPath(p string) (protoreflect.FileDescriptor, error) {
	return chain{s.files, protoregistry.GlobalFiles}.FindFileByPath(p)
}

// Services are the services the set's files define, by full name.
func (s *Set) Services() []protoreflect.ServiceDescriptor {
	var out []protoreflect.ServiceDescriptor
	s.files.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		svcs := f.Services()
		for i := range svcs.Len() {
			out = append(out, svcs.Get(i))
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].FullName() < out[j].FullName() })
	return out
}

// Types resolves the set's message types, for google.protobuf.Any values
// in proto JSON.
func (s *Set) Types() *dynamicpb.Types { return dynamicpb.NewTypes(s.files) }

// chain resolves from the set first, then the well-known types.
type chain []*protoregistry.Files

func (c chain) FindFileByPath(p string) (protoreflect.FileDescriptor, error) {
	for _, f := range c {
		if d, err := f.FindFileByPath(p); err == nil {
			return d, nil
		}
	}
	return nil, protoregistry.NotFound
}

func (c chain) FindDescriptorByName(n protoreflect.FullName) (protoreflect.Descriptor, error) {
	for _, f := range c {
		if d, err := f.FindDescriptorByName(n); err == nil {
			return d, nil
		}
	}
	return nil, protoregistry.NotFound
}
