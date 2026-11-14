package core

import (
	"context"
	"errors"
	"log/slog"
	"net"
)

// HostServer is implemented by packs with a side in the axx host: what a
// run cannot do in an agent's sandbox, the pack does there, on the person's
// machine, for the run (web-core starts the browser a sandbox keeps from
// starting). The pack's side in the run opens a connection with
// [Suite.HostDial]; the host hands it to ServeHost, and the two sides speak
// the pack's own protocol over it.
//
// The run is an agent's: nothing it sends may be run as a command, name a
// file of this machine to read or write, or reach an address its sandbox
// keeps from it. A pack's host side decides what it starts, and takes from
// the connection only what it checks.
type HostServer interface {
	// HostServes says what the pack does in the host for runs, for the
	// person who starts it: "the browsers of web scenarios".
	HostServes() string
	// ServeHost serves one connection until the run closes it or ctx ends,
	// and then stops what it started for it.
	ServeHost(ctx context.Context, run HostRun, conn net.Conn) error
}

// HostRun is what a pack's side in the axx host knows of the run a
// connection is from.
type HostRun struct {
	// ProjectDir is the project the host serves.
	ProjectDir string
	// Command is what the run is, for the person: "axx run features/board".
	Command string
	Logger  *slog.Logger
	// Tell says in the host's terminal, or the editor that started it, what
	// the pack does for the run: "started chromium".
	Tell func(text string)
}

// HostDialer opens a connection to a pack's side in the axx host that
// serves the project.
type HostDialer func(ctx context.Context, pack string) (net.Conn, error)

// UseHost gives the suite the axx host that serves the project's runs: the
// program that runs the suite calls it once it found one.
func (s *Suite) UseHost(dial HostDialer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.host = dial
}

// Hosted reports whether an axx host serves the project's runs: a pack with
// a side there ([HostServer]) can have it do what the run cannot.
func (s *Suite) Hosted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host != nil
}

// ErrNoHost is HostDial's error when no axx host serves the project.
var ErrNoHost = errors.New("no axx host serves the project")

// HostDial opens a connection to pack's side in the axx host that serves
// the project, which serves it with [HostServer.ServeHost]. The host must
// run the same axx, with the pack.
func (s *Suite) HostDial(ctx context.Context, pack string) (net.Conn, error) {
	s.mu.Lock()
	dial := s.host
	s.mu.Unlock()
	if dial == nil {
		return nil, ErrNoHost
	}
	return dial(ctx, pack)
}
