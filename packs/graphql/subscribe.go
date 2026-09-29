package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/stream"
)

// subscription is a subscription a scenario started: the messages it
// received, as they came.
type subscription struct {
	op *operation
	in *cloudstep.Inbox

	mu   sync.Mutex
	ws   *stream.WebSocket
	stop func()
}

func (s *subscription) close() {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// add records a message's data.
func (s *subscription) add(payload json.RawMessage) {
	var p struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	m := cloudstep.Message{Body: p.Data}
	if len(p.Errors) > 0 {
		errs, _ := json.Marshal(p.Errors)
		m.Meta = map[string]string{"errors": string(errs)}
	}
	s.in.Add(m)
}

// subscribe starts a subscription, which lives until the scenario ends.
func (s *service) subscribe(op *operation) (*subscription, error) {
	sub := &subscription{op: op, in: &cloudstep.Inbox{}}
	if s.subProtocol == sse {
		body, err := json.Marshal(op.body())
		if err != nil {
			return nil, err
		}
		url := s.subURL
		if url == "" {
			url = s.url
		}
		es, err := stream.PostEvents(url, s.headers, body, func(ev stream.Event) {
			switch ev.Type {
			case "next", "":
				sub.add(json.RawMessage(ev.Data))
			case "complete":
				sub.in.End("the service completed the subscription")
			}
		}, func(err error) {
			if err != nil {
				sub.in.End("the stream broke: " + err.Error())
				return
			}
			sub.in.End("the service ended the stream")
		})
		if err != nil {
			return nil, fmt.Errorf("cannot subscribe on the %s graphql service: %w", s.name, err)
		}
		sub.stop = es.Close
		return sub, nil
	}
	url := s.subURL
	if url == "" {
		url = "ws" + strings.TrimPrefix(s.url, "http")
	}
	acked := make(chan struct{})
	var once sync.Once
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		sub.mu.Lock()
		ws := sub.ws
		sub.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		defer cancel()
		return ws.Send(ctx, b)
	}
	ws, err := stream.DialWebSocket(url, s.headers, []string{"graphql-transport-ws"}, func(r stream.Received) {
		var m struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(r.Data, &m) != nil {
			return
		}
		switch m.Type {
		case "connection_ack":
			once.Do(func() { close(acked) })
		case "ping":
			go func() { _ = send(map[string]string{"type": "pong"}) }()
		case "next":
			sub.add(m.Payload)
		case "error":
			sub.in.End("the subscription failed: " + string(m.Payload))
		case "complete":
			sub.in.End("the service completed the subscription")
		}
	}, func(code int, reason string) {
		sub.in.End(fmt.Sprintf("the service closed the connection: %d %s", code, reason))
	})
	if err != nil {
		return nil, fmt.Errorf("cannot subscribe on the %s graphql service at %s: %w", s.name, url, err)
	}
	sub.ws, sub.stop = ws, ws.Close
	if err := send(map[string]any{"type": "connection_init", "payload": map[string]any{}}); err != nil {
		ws.Close()
		return nil, err
	}
	select {
	case <-acked:
	case <-time.After(s.timeout):
		ws.Close()
		return nil, fmt.Errorf("the %s graphql service did not acknowledge the graphql-ws connection within %s", s.name, s.timeout)
	}
	if err := send(map[string]any{"id": "1", "type": "subscribe", "payload": op.body()}); err != nil {
		ws.Close()
		return nil, err
	}
	sub.stop = func() {
		_ = send(map[string]string{"id": "1", "type": "complete"})
		ws.Close()
	}
	return sub, nil
}
