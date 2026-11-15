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
	// Approve asks the person who runs the host whether the pack may start,
	// for runs, what a describes. It returns nil once they approved it: now,
	// or before, with the same text. Otherwise its error says why not, for
	// the run: they refused, they have not answered yet (the question stays
	// open where the host runs), or no one can answer there. They are asked
	// where the host runs, never over the connection.
	Approve func(ctx context.Context, a HostApproval) error
	// Suite is a suite of the project's packs for the pack's side to do
	// its work with: its settings, its caches, scenarios of its own. The
	// host loads it as the run loaded its own (the same axx.yaml, profile,
	// properties and settings), confined to the project ([Suite.Confine]),
	// when it is first asked for, and closes it when the connection ends.
	Suite func() (*Suite, error)
}

// HostApproval is what a pack's side in the axx host asks the person to
// approve: something that runs on their machine for an agent's runs, past
// the agent's sandbox, which the agent has a say in (an app it builds).
type HostApproval struct {
	// Subject names it among the pack's: "the courier app". A later
	// approval of the subject shows what changed since this one.
	Subject string
	// Text is all they approve, as lines: what starts, from which files of
	// the project, with which of its settings. A text that differs in
	// anything asks again.
	Text string
	// Warnings say what it reaches past the sandbox, each a sentence.
	Warnings []string
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
