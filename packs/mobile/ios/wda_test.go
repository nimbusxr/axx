package mobileios

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A run takes up a kept simulator's WebDriverAgent only when the process listening on its port
// is that simulator's: its program is in the simulator's folder, named for its UDID. Here the
// listener is this test's own process, whose program's name stands in for the folder.
func TestTheProcessOnAPortIsTheSimulators(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("lsof lists a Mac's ports here")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !listensFor(context.Background(), port, filepath.Base(exe)) {
		t.Errorf("the process on port %d is not taken for %s's", port, filepath.Base(exe))
	}
	if listensFor(context.Background(), port, "9F3B2D3E-0000-0000-0000-000000000000") {
		t.Errorf("the process on port %d is taken for another simulator's", port)
	}
	if listensFor(context.Background(), port+1, filepath.Base(exe)) {
		t.Errorf("a port nothing listens on is taken for the simulator's")
	}
}
