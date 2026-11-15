package hostwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

type parcel struct {
	Reference string `json:"reference"`
	Grams     int    `json:"grams"`
}

// pair is a run's side and a host's side joined, the host's with ops.
func pair(t *testing.T, ops map[string]Handler, told func(string, json.RawMessage)) (*Client, *Server) {
	t.Helper()
	run, host := net.Pipe()
	srv := NewServer(host, ops)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(context.Background(), host) }()
	c := NewClient(run, told)
	t.Cleanup(func() {
		_ = c.Close()
		_ = host.Close()
		select {
		case err := <-served:
			_ = err // the pipe's end, or none
		case <-time.After(10 * time.Second):
			t.Error("the host's side did not end with the connection")
		}
	})
	return c, srv
}

func TestACallIsAnsweredWithItsResultOrItsError(t *testing.T) {
	c, _ := pair(t, map[string]Handler{
		"weigh": Op(func(_ context.Context, p parcel) (parcel, error) {
			if p.Reference == "" {
				return p, errors.New("a parcel has a reference")
			}
			p.Grams = 2400
			return p, nil
		}),
		"refuse": Op(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, fmt.Errorf("the courier app: %w",
				axxerr.New("AXX-E0501", exitcode.Environment, "the person refused").WithHint("ask them"))
		}),
		"panic": func(context.Context, json.RawMessage) (any, error) { panic("out of labels") },
	}, nil)
	ctx := context.Background()
	var got parcel
	if err := c.Call(ctx, "weigh", parcel{Reference: "PX-4101"}, &got); err != nil || got != (parcel{"PX-4101", 2400}) {
		t.Fatalf("weigh: %+v %v", got, err)
	}
	if err := c.Call(ctx, "weigh", parcel{}, &got); err == nil || err.Error() != "a parcel has a reference" {
		t.Errorf("an error crosses as its text: %v", err)
	}
	// An error of axx's own keeps its code, hint and exit code, and the
	// words it was wrapped with.
	err := c.Call(ctx, "refuse", nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != "AXX-E0501" || ae.Hint != "ask them" || ae.Exit != exitcode.Environment ||
		!strings.Contains(err.Error(), "the courier app: ") || !strings.Contains(err.Error(), "the person refused") {
		t.Errorf("axx's own error: %#v (%v)", ae, err)
	}
	if err := c.Call(ctx, "panic", nil, nil); err == nil || !strings.Contains(err.Error(), "out of labels") {
		t.Errorf("a handler that panics fails its call: %v", err)
	}
	if err := c.Call(ctx, "shell", map[string]string{"command": "rm -rf ~"}, nil); err == nil || !strings.Contains(err.Error(), `no operation "shell"`) {
		t.Errorf("an operation the host has not: %v", err)
	}
	// The connection still serves.
	if err := c.Call(ctx, "weigh", parcel{Reference: "PX-4102"}, &got); err != nil || got.Reference != "PX-4102" {
		t.Errorf("after the failures: %+v %v", got, err)
	}
}

func TestCallsAtOnceEachGetTheirOwnAnswer(t *testing.T) {
	c, _ := pair(t, map[string]Handler{
		"echo": Op(func(_ context.Context, p parcel) (parcel, error) {
			time.Sleep(time.Duration(p.Grams%5) * time.Millisecond)
			return p, nil
		}),
	}, nil)
	var wg sync.WaitGroup
	for i := range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := parcel{Reference: fmt.Sprintf("PX-%d", i), Grams: i}
			var got parcel
			if err := c.Call(context.Background(), "echo", want, &got); err != nil || got != want {
				t.Errorf("call %d: %+v %v", i, got, err)
			}
		}()
	}
	wg.Wait()
}

// A call the run no longer waits for is ended in the host; the host's
// telling reaches the run in the order it told.
func TestACallNoLongerWaitedForEndsInTheHost(t *testing.T) {
	began, ended := make(chan struct{}), make(chan struct{}, 1)
	var told []string
	var mu sync.Mutex
	c, srv := pair(t, map[string]Handler{
		"wait": func(ctx context.Context, _ json.RawMessage) (any, error) {
			close(began)
			<-ctx.Done()
			ended <- struct{}{}
			return nil, ctx.Err()
		},
	}, func(op string, params json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		told = append(told, op+" "+string(params))
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- c.Call(ctx, "wait", nil, nil) }()
	<-began
	srv.Tell("log", "the simulator boots")
	srv.Tell("log", "the app starts")
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("the call: %v", err)
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the host's handler never heard the call was no longer waited for")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(told, "; ") != `log "the simulator boots"; log "the app starts"` {
		t.Errorf("told: %v", told)
	}
}

// When the connection ends, calls that wait fail, and so does every later one.
func TestCallsFailOnceTheConnectionEnded(t *testing.T) {
	began := make(chan struct{})
	c, _ := pair(t, map[string]Handler{
		"wait": func(ctx context.Context, _ json.RawMessage) (any, error) {
			close(began)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}, nil)
	result := make(chan error, 1)
	go func() { result <- c.Call(context.Background(), "wait", nil, nil) }()
	<-began
	_ = c.Close()
	if err := <-result; !errors.Is(err, ErrClosed) {
		t.Errorf("a waiting call: %v", err)
	}
	<-c.Done()
	if err := c.Call(context.Background(), "wait", nil, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("a later call: %v", err)
	}
}
