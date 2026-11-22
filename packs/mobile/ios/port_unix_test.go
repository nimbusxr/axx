// SPDX-License-Identifier: Apache-2.0

//go:build unix

package mobileios

import (
	"context"
	"net"
	"testing"
)

// A port for a simulator's server is never one that another socket of the machine has: not a
// listener's, on every address or on one, of IPv6 or of IPv4, and not the port a connection goes
// out from. The sockets are held while every port the machine hands out comes by once. (A port
// taken on 127.0.0.1, as before, was each of the ports of the listeners on IPv6 only.)
func TestAServersPortIsNoOtherSocketsPort(t *testing.T) {
	held := heldPorts(t)
	for range 20000 {
		port, err := serverPort()
		if err != nil {
			t.Fatal(err)
		}
		if kind, taken := held[port]; taken {
			t.Fatalf("port %d is given, which %s has", port, kind)
		}
	}
}

// heldPorts holds sockets of each kind until the test ends, and says which port each kind has.
func heldPorts(t *testing.T) map[int]string {
	t.Helper()
	held := map[int]string{}
	listen := func(kind, network, address string) net.Listener {
		l, err := (&net.ListenConfig{}).Listen(context.Background(), network, address)
		if err != nil {
			t.Skipf("this machine has no %s: %v", kind, err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	server := listen("listener on 127.0.0.1", "tcp4", "127.0.0.1:0")
	go func() {
		for {
			conn, err := server.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	for range 40 {
		for _, l := range []struct{ kind, network, address string }{
			{"a listener on every address", "tcp", ":0"},
			{"a listener on every IPv6 address", "tcp6", "[::]:0"},
			{"a listener on every IPv4 address", "tcp4", "0.0.0.0:0"},
			{"a listener on IPv6's loopback", "tcp6", "[::1]:0"},
		} {
			held[listen(l.kind, l.network, l.address).Addr().(*net.TCPAddr).Port] = l.kind
		}
		conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp4", server.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		held[conn.LocalAddr().(*net.TCPAddr).Port] = "a connection that goes out"
	}
	return held
}
