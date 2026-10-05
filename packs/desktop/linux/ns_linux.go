//go:build linux

package desktoplinux

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// A desktop's own view of the file system: with more than one desktop, a
// scenario's session runs in a user and mount namespace of its own, in
// which the project's .axx/desktop/linux is the desktop's own folder. An
// app's home is then at one path on every desktop, and a feature never
// knows which desktop it ran on. The namespace's first process is axx
// itself, run again as a helper: it mounts the folder, and starts the
// session's processes as axx asks it, so they share its view.

// helperEnv tells axx, run again, to be a desktop's helper: its value is the
// helper's spec.
const helperEnv = "AXX_DESKTOP_LINUX_HELPER"

// helperSpec is the folder the helper mounts, and where.
type helperSpec struct {
	Host string `json:"host"` // the desktop's own folder
	View string `json:"view"` // where its processes see it
}

func init() {
	spec := os.Getenv(helperEnv)
	if spec == "" {
		return
	}
	os.Exit(runHelper(spec))
}

// starter starts a session's processes: here, or in a desktop's namespace.
type starter interface {
	// start starts a program in a process group of its own, and returns its
	// process and, when firstLine is true, the first line it writes.
	start(req startRequest) (pid int, line string, err error)
	// exited reports whether a process it started has ended.
	exited(pid int) bool
	close()
}

type startRequest struct {
	Path      string   `json:"path"`
	Args      []string `json:"args"`
	Env       []string `json:"env"`
	Dir       string   `json:"dir"`
	FirstLine bool     `json:"firstLine"`
}

type startReply struct {
	PID   int    `json:"pid"`
	Line  string `json:"line"`
	Error string `json:"error"`
}

// direct starts processes here, as axx's children.
type direct struct {
	mu   sync.Mutex
	done map[int]chan struct{}
}

func (d *direct) start(req startRequest) (int, string, error) {
	cmd, line, err := launch(req)
	if err != nil {
		return 0, "", err
	}
	done := make(chan struct{})
	d.mu.Lock()
	if d.done == nil {
		d.done = map[int]chan struct{}{}
	}
	d.done[cmd.Process.Pid] = done
	d.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return cmd.Process.Pid, line, nil
}

func (d *direct) exited(pid int) bool {
	d.mu.Lock()
	done, ok := d.done[pid]
	d.mu.Unlock()
	if !ok {
		return syscall.Kill(pid, 0) != nil
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (d *direct) close() {}

// launch starts a program in a process group of its own; with FirstLine, it
// reads the first line it writes.
func launch(req startRequest) (*exec.Cmd, string, error) {
	// It outlives the request: the scenario's end stops it.
	cmd := exec.CommandContext(context.Background(), req.Path, req.Args...)
	cmd.Env, cmd.Dir = req.Env, req.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out io.ReadCloser
	if req.FirstLine {
		var err error
		if out, err = cmd.StdoutPipe(); err != nil {
			return nil, "", err
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	line := ""
	if out != nil {
		l, _ := bufio.NewReader(out).ReadString('\n')
		line = l
	}
	return cmd, line, nil
}

// helper is a desktop's namespace, its first process axx's helper.
type helper struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// newHelper starts a helper in a new user and mount namespace, with the
// desktop's folder at view.
func newHelper(spec helperSpec) (*helper, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.Background(), self)
	cmd.Env = append(os.Environ(), helperEnv+"="+string(b))
	cmd.Stderr = os.Stderr
	uid, gid := os.Getuid(), os.Getgid()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: uid, HostID: uid, Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: gid, HostID: gid, Size: 1}},
		Setpgid:     true,
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("this host does not give user and mount namespaces (%w): run one Linux desktop at a time (packs.%s.desktops: 1)", err, Name)
	}
	h := &helper{cmd: cmd, in: in, out: bufio.NewReader(out)}
	// Its first line says the folder is mounted, or why not.
	var ready startReply
	if err := h.read(&ready); err != nil || ready.Error != "" {
		h.close()
		if ready.Error != "" {
			return nil, fmt.Errorf("a desktop's view of the file system: %s", ready.Error)
		}
		return nil, fmt.Errorf("a desktop's helper did not start: %w", err)
	}
	return h, nil
}

func (h *helper) read(v any) error {
	line, err := h.out.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, v)
}

func (h *helper) start(req startRequest) (int, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := json.Marshal(req)
	if err != nil {
		return 0, "", err
	}
	if _, err := h.in.Write(append(b, '\n')); err != nil {
		return 0, "", fmt.Errorf("the desktop's helper is gone: %w", err)
	}
	var r startReply
	if err := h.read(&r); err != nil {
		return 0, "", fmt.Errorf("the desktop's helper is gone: %w", err)
	}
	if r.Error != "" {
		return 0, "", errors.New(r.Error)
	}
	return r.PID, r.Line, nil
}

// exited: the helper waits for its children, so a process that has ended
// is gone.
func (h *helper) exited(pid int) bool { return syscall.Kill(pid, 0) != nil }

// close ends the helper, which stops what it started.
func (h *helper) close() {
	_ = h.in.Close()
	_ = h.cmd.Wait()
}

// runHelper is axx run as a desktop's helper, in its namespace: it mounts
// the desktop's folder, says so, and then starts what it is asked to, until
// its input ends; then it stops them.
func runHelper(raw string) int {
	out := json.NewEncoder(os.Stdout)
	var spec helperSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		_ = out.Encode(startReply{Error: err.Error()})
		return 1
	}
	if err := mountView(spec); err != nil {
		_ = out.Encode(startReply{Error: err.Error()})
		return 1
	}
	_ = out.Encode(startReply{})
	var groups []int
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var req startRequest
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			_ = out.Encode(startReply{Error: err.Error()})
			continue
		}
		cmd, line, err := launch(req)
		if err != nil {
			_ = out.Encode(startReply{Error: err.Error()})
			continue
		}
		groups = append(groups, cmd.Process.Pid)
		go func() { _ = cmd.Wait() }()
		_ = out.Encode(startReply{PID: cmd.Process.Pid, Line: line})
	}
	for _, g := range groups {
		_ = syscall.Kill(-g, syscall.SIGKILL)
	}
	return 0
}

// mountView makes the namespace's mounts its own, and mounts the desktop's
// folder at view.
func mountView(spec helperSpec) error {
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("making the mounts private: %w", err)
	}
	if err := unix.Mount(spec.Host, spec.View, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("mounting %s at %s: %w", spec.Host, spec.View, err)
	}
	return nil
}

// probeNamespaces starts a helper that mounts a folder on itself, and ends
// it: whether this host gives user and mount namespaces.
func probeNamespaces() error {
	dir, err := os.MkdirTemp("", "axx-ns-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := newHelper(helperSpec{Host: dir, View: dir})
	if err != nil {
		return err
	}
	h.close()
	return nil
}
