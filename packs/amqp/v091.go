package amqp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"github.com/nimbusxr/axx/internal/cloudstep"
)

// v091 is a connection to a broker over AMQP 0-9-1, with a channel of its
// own for each send and each listener.
type v091 struct {
	conn *amqp091.Connection
	run  string
}

func dial091(url, run string) (*v091, error) {
	conn, err := amqp091.DialConfig(url, amqp091.Config{
		Properties: amqp091.Table{"connection_name": "axx " + run},
		Dial:       amqp091.DefaultDial(10 * time.Second),
	})
	if err != nil {
		return nil, err
	}
	return &v091{conn: conn, run: run}, nil
}

func (c *v091) close() error { return c.conn.Close() }

// exists checks that the queue or the exchange is there, on a channel of
// its own: the broker closes the channel that asks for one that is not.
func (c *v091) exists(kind, name string) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if kind == "queue" {
		_, err = ch.QueueDeclarePassive(name, false, false, false, false, nil)
	} else {
		err = ch.ExchangeDeclarePassive(name, "topic", false, false, false, false, nil)
	}
	var amqpErr *amqp091.Error
	if errors.As(err, &amqpErr) && amqpErr.Code == amqp091.NotFound {
		return fmt.Errorf("no amqp %s named %s", kind, name)
	}
	return err
}

func (c *v091) send(ctx context.Context, kind, target string, m outgoing) error {
	if err := c.exists(kind, target); err != nil {
		return err
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if err := ch.Confirm(false); err != nil {
		return err
	}
	returned := ch.NotifyReturn(make(chan amqp091.Return, 1))
	exchange, key := target, m.routingKey
	if kind == "queue" {
		exchange, key = "", target
	}
	headers := amqp091.Table{}
	for k, v := range m.headers {
		headers[k] = v
	}
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, key, true, false, amqp091.Publishing{
		Headers: headers, ContentType: m.contentType, CorrelationId: m.correlationID, MessageId: m.messageID,
		ReplyTo: m.replyTo, DeliveryMode: amqp091.Persistent, Timestamp: time.Now(), Body: m.body,
	})
	if err != nil {
		return err
	}
	acked, err := confirm.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !acked {
		return errors.New("the broker refused the message")
	}
	// The broker returns an unroutable message before it confirms it.
	select {
	case r := <-returned:
		return fmt.Errorf("no queue is bound to the %s amqp exchange for the routing key %q, so the message went nowhere (%s)",
			target, r.RoutingKey, r.ReplyText)
	default:
		return nil
	}
}

func (c *v091) listen(ctx context.Context, kind, target string, keys []string, in *cloudstep.Inbox) (func(context.Context) error, error) {
	if err := c.exists(kind, target); err != nil {
		return nil, err
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, err
	}
	queue, ack := target, true
	if kind == "exchange" {
		q, err := ch.QueueDeclare(c.run+"."+target, false, true, true, false, nil)
		if err != nil {
			_ = ch.Close()
			return nil, err
		}
		for _, key := range append([]string{"#"}, keys...) {
			if err := ch.QueueBind(q.Name, key, target, false, nil); err != nil {
				_ = ch.Close()
				return nil, fmt.Errorf("cannot bind a queue to the %s amqp exchange: %w", target, err)
			}
		}
		queue, ack = q.Name, false
	}
	deliveries, err := ch.ConsumeWithContext(ctx, queue, c.run, !ack, kind == "exchange", false, false, nil)
	if err != nil {
		_ = ch.Close()
		return nil, err
	}
	go func() {
		for d := range deliveries {
			in.Add(incoming091(d))
			if ack {
				_ = d.Ack(false)
			}
		}
		if ctx.Err() == nil {
			in.Fail(fmt.Errorf("the broker stopped the delivery of the %s amqp %s's messages", target, kind))
		}
	}()
	return func(context.Context) error { return ch.Close() }, nil
}

func incoming091(d amqp091.Delivery) cloudstep.Message {
	m := cloudstep.Message{Body: d.Body, Fields: map[string]string{}, Meta: map[string]string{"routing key": d.RoutingKey}}
	for k, v := range d.Headers {
		m.Fields[k] = fmt.Sprint(v)
	}
	for k, v := range map[string]string{
		"content type": d.ContentType, "correlation id": d.CorrelationId, "message id": d.MessageId, "reply to": d.ReplyTo,
	} {
		if v != "" {
			m.Meta[k] = v
		}
	}
	return m
}

func isJSON(b []byte) bool { return json.Valid(b) }
