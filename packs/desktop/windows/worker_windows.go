//go:build windows

package desktopwindows

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/nimbusxr/axx/packs/desktop/internal/jab"
	"github.com/nimbusxr/axx/packs/desktop/internal/uia"
)

// worker makes the pack's calls to UI Automation and the Java Access Bridge
// on one OS thread: their clients belong to the thread that made them, and
// the bridge needs that thread to handle its window messages while it waits.
// Between calls, it handles them.
type worker struct {
	jobs chan func()
	uia  *uia.Client
	// jab is the bridge's client, loaded for the first Java app, of its
	// Java's DLL.
	jab *jab.Client
}

var (
	theWorker *worker
	workerErr error
	workerOne sync.Once
)

// work is the run's worker, started once.
func work() (*worker, error) {
	workerOne.Do(func() {
		w := &worker{jobs: make(chan func())}
		ready := make(chan error)
		go func() {
			runtime.LockOSThread()
			uia.DPIAware()
			c, err := uia.New()
			ready <- err
			if err != nil {
				return
			}
			w.uia = c
			for {
				select {
				case job := <-w.jobs:
					job()
				case <-time.After(20 * time.Millisecond):
					if w.jab != nil {
						w.jab.Pump(5 * time.Millisecond)
					}
				}
			}
		}()
		if workerErr = <-ready; workerErr == nil {
			theWorker = w
		}
	})
	return theWorker, workerErr
}

// do runs fn on the worker's thread.
func (w *worker) do(fn func() error) error {
	done := make(chan error, 1)
	w.jobs <- func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("the accessibility call failed: %v", r)
			}
		}()
		done <- fn()
	}
	return <-done
}

// pump handles the bridge's messages for a while, on the worker's thread, as
// an app takes its time: the bridge tells of changes by messages.
func (w *worker) pump(d time.Duration) {
	if w.jab == nil {
		time.Sleep(d)
		return
	}
	w.jab.Pump(d)
}

// java loads the Java Access Bridge's client from dll, once.
func (w *worker) java(dll string) (*jab.Client, error) {
	if w.jab != nil {
		return w.jab, nil
	}
	c, err := jab.New(dll)
	if err != nil {
		return nil, err
	}
	w.jab = c
	return c, nil
}
