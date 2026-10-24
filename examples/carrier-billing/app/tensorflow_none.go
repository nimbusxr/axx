//go:build !tensorflow

package main

import "errors"

// loadPhotoModel: a build without TensorFlow (the image builds the service
// with it, -tags tensorflow) checks no repacks' photos.
func loadPhotoModel(string) (photoChecker, error) {
	return nil, errors.New("the service is built without TensorFlow, which runs the repack check's model: build it with -tags tensorflow")
}
