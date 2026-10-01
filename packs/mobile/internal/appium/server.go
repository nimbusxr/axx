package appium

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/proc"
)

// Install is an installed Appium: the Node.js that runs it, its main script,
// and its home, where its drivers are.
type Install struct {
	Node string
	Main string
	Home string
}

// Server is an Appium server axx runs, for one device.
type Server struct {
	*Client
	cmd   *exec.Cmd
	group *proc.Group
	log   *os.File
	done  chan struct{}

	mu   sync.Mutex
	exit error
}

// Start runs an Appium server on a free port of 127.0.0.1, logging to
// logPath, and waits until it answers.
func (in Install) Start(ctx context.Context, logPath string) (*Server, error) {
	port, err := FreePort()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, err
	}
	log, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	// The server outlives this step: it runs until the run ends.
	cmd := exec.CommandContext(context.Background(), in.Node, in.Main, //nolint:gosec // the Appium axx installed
		"--address", "127.0.0.1", "--port", strconv.Itoa(port), "--log-no-colors", "--log-timestamp")
	cmd.Env = append(os.Environ(), "APPIUM_HOME="+in.Home)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = time.Second
	proc.Setup(cmd)
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return nil, fmt.Errorf("cannot run Appium: %w", err)
	}
	s := &Server{Client: &Client{URL: "http://127.0.0.1:" + strconv.Itoa(port), HTTP: &http.Client{Timeout: 5 * time.Minute}}, cmd: cmd, log: log, done: make(chan struct{})}
	if g, err := proc.NewGroup(cmd); err == nil {
		s.group = g
	}
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.exit = err
		s.mu.Unlock()
		close(s.done)
	}()
	ready := make(chan error, 1)
	// Node starts Appium in seconds, and in more than a minute on a busy CI
	// runner: a slow start is not a hung one.
	go func() { ready <- s.WaitReady(ctx, 3*time.Minute) }()
	select {
	case err := <-ready:
		if err != nil {
			s.Stop()
			return nil, fmt.Errorf("%w; its log: %s", err, logPath)
		}
	case <-s.done:
		if s.exit != nil {
			return nil, fmt.Errorf("appium stopped as it started: %w; its log: %s", s.exit, logPath)
		}
		return nil, fmt.Errorf("appium stopped as it started; its log: %s", logPath)
	}
	return s, nil
}

// Stop stops the server and whatever it started.
func (s *Server) Stop() {
	if s.group != nil {
		_ = s.group.KillAndWait(5 * time.Second)
		s.group.Release()
	} else if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.done
	_ = s.log.Close()
}

// FreePort is a port of 127.0.0.1 nothing listens on.
func FreePort() (int, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
