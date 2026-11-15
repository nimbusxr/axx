// Package hostwire is how a pack's side in a run talks to its side in the
// axx host (core.HostServer) when the host does the pack's machine work: the
// run calls an operation by its name, with what it needs, and waits for its
// answer; the host tells the run what it has to hear meanwhile (a log line,
// an attachment). Many calls go over one connection at once: a run's
// scenarios share it.
//
// The run is an agent's. An operation is one the pack's host side has a
// handler for, and nothing else: a call names no program and no file of the
// machine but through what that handler checks.
package hostwire

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// maxLine is the longest message: a screen of a few thousand nodes, a file
// of an app's folder. Larger files do not cross the wire.
const maxLine = 256 << 20

// message is one line of the connection.
type message struct {
	// ID pairs a call with its answer; 0 for what the host tells the run.
	ID uint64 `json:"id,omitempty"`
	// Op is the operation a call asks for, or what the host tells
	// ("cancel" from the run: the call of that ID is no longer waited for).
	Op     string          `json:"op,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	// Result or Error answer a call.
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Failure        `json:"error,omitempty"`
	Answer bool            `json:"answer,omitempty"`
}

// Failure is an error as it crosses: its text, and what makes it one of
// axx's own (its code, hint and exit code), so that a run reports a failure
// in the host as it reports its own.
type Failure struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Exit    int    `json:"exit,omitempty"`
}

func failure(err error) *Failure {
	// The text is the whole error's, as the run would print it: an error
	// of axx's own, wrapped with what it concerns, keeps both.
	f := &Failure{Message: err.Error()}
	var ae *axxerr.Error
	if errors.As(err, &ae) {
		f.Code, f.Hint, f.Exit = ae.Code, ae.Hint, int(ae.Exit)
	}
	return f
}

func (f *Failure) err() error {
	if f.Code == "" {
		return errors.New(f.Message)
	}
	e := axxerr.New(f.Code, exitcode.Code(f.Exit), "%s", f.Message)
	if f.Hint != "" {
		e = e.WithHint("%s", f.Hint)
	}
	return e
}

// ErrClosed is a call's error once the connection ended.
var ErrClosed = errors.New("the connection to the axx host ended")

type wire struct {
	w   io.Writer
	wmu sync.Mutex
}

func (w *wire) send(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_, err = w.w.Write(append(b, '\n'))
	return err
}

func scanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	return sc
}

// Client is the run's side of a connection.
type Client struct {
	wire
	conn io.Closer

	mu      sync.Mutex
	next    uint64
	waiting map[uint64]chan message
	closed  error
	told    func(op string, params json.RawMessage)
	done    chan struct{}
}

// NewClient serves the run's side of conn until it ends. told hears what
// the host tells the run, each in turn; it may be nil.
func NewClient(conn io.ReadWriteCloser, told func(op string, params json.RawMessage)) *Client {
	c := &Client{wire: wire{w: conn}, conn: conn, waiting: map[uint64]chan message{}, told: told, done: make(chan struct{})}
	go c.read(conn)
	return c
}

func (c *Client) read(r io.Reader) {
	sc := scanner(r)
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if !m.Answer {
			if c.told != nil {
				c.told(m.Op, m.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.waiting[m.ID]
		delete(c.waiting, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
	err := sc.Err()
	if err == nil {
		err = ErrClosed
	} else {
		err = fmt.Errorf("%w: %v", ErrClosed, err)
	}
	c.mu.Lock()
	c.closed = err
	for id, ch := range c.waiting {
		close(ch)
		delete(c.waiting, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// Done is closed once the connection ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection: the host stops what it started for it.
func (c *Client) Close() error { return c.conn.Close() }

// Call asks the host for an operation and waits for its answer, which it
// reads into result (nil: none is read). When ctx ends first, the host is
// told the call is no longer waited for, and Call returns ctx's error.
func (c *Client) Call(ctx context.Context, op string, params, result any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	c.mu.Lock()
	if c.closed != nil {
		err := c.closed
		c.mu.Unlock()
		return err
	}
	c.next++
	id := c.next
	ch := make(chan message, 1)
	c.waiting[id] = ch
	c.mu.Unlock()
	if err := c.send(message{ID: id, Op: op, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return fmt.Errorf("%w: %v", ErrClosed, err)
	}
	select {
	case m, ok := <-ch:
		if !ok {
			c.mu.Lock()
			err := c.closed
			c.mu.Unlock()
			return err
		}
		if m.Error != nil {
			return m.Error.err()
		}
		if result != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		_ = c.send(message{ID: id, Op: "cancel"})
		return ctx.Err()
	}
}

// Handler answers one call in the host: params are the run's, to be read
// and checked; what it returns is the answer. ctx ends when the run no
// longer waits for the call, or the connection ends.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server is the host's side of a connection.
type Server struct {
	wire
	ops map[string]Handler
}

// NewServer is the host's side of conn, with the operations it has.
func NewServer(conn io.Writer, ops map[string]Handler) *Server {
	return &Server{wire: wire{w: conn}, ops: ops}
}

// Tell tells the run something outside any call's answer.
func (s *Server) Tell(op string, params any) {
	b, err := json.Marshal(params)
	if err != nil {
		return
	}
	_ = s.send(message{Op: op, Params: b})
}

// Serve answers the run's calls, each as it comes and several at once,
// until the connection ends or ctx does; it returns once every call it
// began has returned.
func (s *Server) Serve(ctx context.Context, r io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	defer wg.Wait()
	var mu sync.Mutex
	calls := map[uint64]context.CancelFunc{}
	sc := scanner(r)
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == 0 {
			continue
		}
		if m.Op == "cancel" {
			mu.Lock()
			if stop := calls[m.ID]; stop != nil {
				stop()
			}
			mu.Unlock()
			continue
		}
		h := s.ops[m.Op]
		if h == nil {
			_ = s.send(message{ID: m.ID, Answer: true, Error: &Failure{Message: fmt.Sprintf("the axx host has no operation %q for this pack: the host and the run must be the same axx", m.Op)}})
			continue
		}
		callCtx, stop := context.WithCancel(ctx)
		mu.Lock()
		calls[m.ID] = stop
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				mu.Lock()
				delete(calls, m.ID)
				mu.Unlock()
				stop()
			}()
			answer := message{ID: m.ID, Answer: true}
			result, err := safely(callCtx, h, m.Params)
			if err != nil {
				answer.Error = failure(err)
			} else if result != nil {
				b, merr := json.Marshal(result)
				if merr != nil {
					answer.Error = &Failure{Message: merr.Error()}
				} else {
					answer.Result = b
				}
			}
			_ = s.send(answer)
		}()
	}
	cancel()
	return sc.Err()
}

// safely runs a handler: one that panics fails its call, not the host.
func safely(ctx context.Context, h Handler, params json.RawMessage) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the axx host's side of the pack failed: %v", r)
		}
	}()
	return h(ctx, params)
}

// Op makes a Handler of a function that takes its parameters decoded.
func Op[P, R any](fn func(ctx context.Context, p P) (R, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("the run's request cannot be read: %w", err)
			}
		}
		return fn(ctx, p)
	}
}
