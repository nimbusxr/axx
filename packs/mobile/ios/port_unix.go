// SPDX-License-Identifier: Apache-2.0

//go:build unix

package mobileios

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"
)

// serverPort is a port of the Mac for a server on a simulator, WebDriverAgent's or its screen
// stream's: one that no listener of the Mac has, on IPv6 or on IPv4.
//
// A simulator's server listens on every IPv6 address of the Mac, as the programs of macOS and of
// a simulator do that listen with Apple's Network framework. A port taken as Go's listeners take
// one, on 127.0.0.1, is held only to IPv4's listeners, so the Mac hands out one that such a
// program listens on (measured: every one of 40). On CI's Mac WebDriverAgent was given such a
// port, could not start its server on it ("bind ... Address already in use"), and ended as it
// started, each time it was launched. A port that only an IPv4 listener has is no better:
// WebDriverAgent starts on it, and what axx sends to 127.0.0.1 reaches the other program. So the
// port is taken here as a server that shares it with nobody would: on every address of IPv6,
// then of IPv4, without SO_REUSEADDR.
func serverPort() (int, error) {
	var last error
	// IPv6 first, then the same port for IPv4, which is refused it where only an IPv4 socket has
	// it; then the next.
	for range 200 {
		l, err := strict.Listen(context.Background(), "tcp6", "[::]:0")
		if err != nil {
			return 0, err
		}
		port := l.Addr().(*net.TCPAddr).Port
		// One after the other: the IPv4 listener is refused the port while the IPv6 one has it.
		_ = l.Close()
		l4, err := strict.Listen(context.Background(), "tcp4", "0.0.0.0:"+strconv.Itoa(port))
		if err != nil {
			last = err
			continue
		}
		_ = l4.Close()
		return port, nil
	}
	return 0, errors.Join(errors.New("no port of this machine is free on every address"), last)
}

// strict listens as a server that does not share its address: Go's listeners set SO_REUSEADDR,
// which this takes back before the socket is bound.
var strict = net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
	var failed error
	if err := c.Control(func(fd uintptr) {
		failed = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
	}); err != nil {
		return err
	}
	return failed
}}
