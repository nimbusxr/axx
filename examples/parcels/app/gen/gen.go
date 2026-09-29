// Package gen holds the Go code protoc generates from the service's protos
// in ../proto: the tracking API it serves and the rating service it calls,
// and the rating service's descriptor set, which its mock serves
// (../../infra/rating/grpc). Regenerate them, from the app's folder, with
// protoc, protoc-gen-go and protoc-gen-go-grpc on the PATH:
//
//	go generate ./gen
package gen

//go:generate protoc -I ../proto --go_out=. --go_opt=module=github.com/nimbusxr/axx/examples/parcels/app/gen --go-grpc_out=. --go-grpc_opt=module=github.com/nimbusxr/axx/examples/parcels/app/gen parcels/rating/v1/rating.proto parcels/tracking/v1/tracking.proto
//go:generate protoc -I ../proto --include_imports --descriptor_set_out=../../infra/rating/grpc/rating.dsc parcels/rating/v1/rating.proto
