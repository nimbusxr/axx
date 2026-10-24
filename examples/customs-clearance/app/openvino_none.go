//go:build !openvino

package main

import "errors"

// loadContentsModel: a build without OpenVINO (the image builds the service
// with it, -tags openvino) checks no contents.
func loadContentsModel(string) (contentsChecker, error) {
	return nil, errors.New("the service is built without OpenVINO, which runs the contents check's model: build it with -tags openvino")
}
