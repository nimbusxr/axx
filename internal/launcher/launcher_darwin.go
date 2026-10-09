//go:build darwin

// Package launcher starts programs on macOS under axx's launcher (ADR 0013).
//
// macOS asks for a permission (Accessibility, Screen Recording, Automation)
// on behalf of a process's responsible process: the app at the top of its
// tree, such as the terminal, IDE or CI agent that started axx. Start makes a
// program responsible for itself and for every program it starts. A signed
// launcher started that way is the one identity a person allows once, and
// axx and the apps it runs as its children ask nothing more.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
)

var (
	loadOnce sync.Once
	loadErr  error

	spawnattrInit    func(attr *uintptr) int32
	spawnattrDestroy func(attr *uintptr) int32
	setDisclaim      func(attr *uintptr, disclaim int32) int32
	posixSpawn       func(pid *int32, path *byte, actions uintptr, attr *uintptr, argv, envp **byte) int32
	responsibleFor   func(pid int32) int32
)

// load binds libSystem's functions, once. The responsibility functions are
// private to macOS: Chromium and VS Code start their helpers with them, and
// a macOS without them fails here rather than at a call.
func load() error {
	loadOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("cannot load libSystem: %w", err)
			return
		}
		for name, fn := range map[string]any{
			"posix_spawnattr_init":                       &spawnattrInit,
			"posix_spawnattr_destroy":                    &spawnattrDestroy,
			"responsibility_spawnattrs_setdisclaim":      &setDisclaim,
			"posix_spawn":                                &posixSpawn,
			"responsibility_get_pid_responsible_for_pid": &responsibleFor,
		} {
			if _, err := purego.Dlsym(lib, name); err != nil {
				loadErr = fmt.Errorf("this macOS has no %s: %w", name, err)
				return
			}
			purego.RegisterLibFunc(fn, lib, name)
		}
	})
	return loadErr
}

// Start starts the program at path with args (args[0] its name) and env as
// the process macOS holds responsible for itself and the programs it
// starts. Its standard input and output are the caller's.
func Start(path string, args, env []string) (pid int, err error) {
	if err := load(); err != nil {
		return 0, err
	}
	var attr uintptr
	if rc := spawnattrInit(&attr); rc != 0 {
		return 0, fmt.Errorf("posix_spawnattr_init: %w", syscall.Errno(rc))
	}
	defer spawnattrDestroy(&attr)
	if rc := setDisclaim(&attr, 1); rc != 0 {
		return 0, fmt.Errorf("responsibility_spawnattrs_setdisclaim: %w", syscall.Errno(rc))
	}
	cpath, cargs, cenv := cstring(path), cstrings(args), cstrings(env)
	var p int32
	rc := posixSpawn(&p, &cpath[0], 0, &attr, &cargs.ptrs[0], &cenv.ptrs[0])
	runtime.KeepAlive(cpath)
	runtime.KeepAlive(cargs)
	runtime.KeepAlive(cenv)
	if rc != 0 {
		return 0, fmt.Errorf("cannot start %s: %w", path, syscall.Errno(rc))
	}
	return int(p), nil
}

// ResponsibleFor is the process macOS holds responsible for pid: the one
// whose permissions pid has.
func ResponsibleFor(pid int) (int, error) {
	if err := load(); err != nil {
		return 0, err
	}
	r := responsibleFor(int32(pid)) //nolint:gosec // a process's number
	if r <= 0 {
		return 0, fmt.Errorf("no responsible process for %d", pid)
	}
	return int(r), nil
}

// Wait waits for the process Start started, and is its exit status: 128
// plus the signal's number for one a signal stopped.
func Wait(pid int) (int, error) {
	var ws syscall.WaitStatus
	for {
		_, err := syscall.Wait4(pid, &ws, 0, nil)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EINTR) {
			return 0, err
		}
	}
	if ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return ws.ExitStatus(), nil
}

// Run runs the program at path with args (args[0] its name) as the
// launcher's child, and is its exit status, as Wait's: its standard input
// and output are the launcher's, and the stop signals the launcher gets
// (interrupt, terminate, hang-up) pass on to it.
func Run(path string, args []string) (int, error) {
	// The program runs as long as it runs: the launcher sets it no deadline.
	cmd := exec.CommandContext(context.Background(), path, args[1:]...) //nolint:gosec // the program the launcher is asked to run
	cmd.Args = args
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-stop:
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return exit.ExitCode(), nil
	default:
		return 0, err
	}
}

// cstrs is a NUL-terminated list of C strings, kept alive with their bytes.
type cstrs struct {
	bytes [][]byte
	ptrs  []*byte
}

func cstring(s string) []byte { return append([]byte(s), 0) }

func cstrings(ss []string) *cstrs {
	c := &cstrs{}
	for _, s := range ss {
		b := cstring(s)
		c.bytes = append(c.bytes, b)
		c.ptrs = append(c.ptrs, &b[0])
	}
	c.ptrs = append(c.ptrs, nil)
	return c
}
