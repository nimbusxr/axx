package amqp

import (
	"context"
	"fmt"
	"time"

	goamqp "github.com/Azure/go-amqp"

	"github.com/nimbusxr/axx/internal/cloudstep"
)

// v10 is a connection to a broker over AMQP 1.0, with a session of its own
// for each send and each listener. A queue is an anycast address and an
// exchange a multicast one, as ActiveMQ Artemis and Qpid have them; the
// routing key is the message's subject.
type v10 struct {
	conn *goamqp.Conn
}

func dial10(url string) (*v10, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := goamqp.Dial(ctx, url, &goamqp.ConnOptions{ContainerID: "axx"})
	if err != nil {
		return nil, err
	}
	return &v10{conn: conn}, nil
}

func (c *v10) close() error { return c.conn.Close() }

// capability is how an AMQP 1.0 broker tells a queue from a topic.
func capability(kind string) []string {
	if kind == "exchange" {
		return []string{"topic"}
	}
	return []string{"queue"}
}

func (c *v10) send(ctx context.Context, kind, target string, m outgoing) error {
	sess, err := c.conn.NewSession(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close(context.WithoutCancel(ctx)) }()
	snd, err := sess.NewSender(ctx, target, &goamqp.SenderOptions{TargetCapabilities: capability(kind)})
	if err != nil {
		return err
	}
	defer func() { _ = snd.Close(context.WithoutCancel(ctx)) }()
	msg := goamqp.NewMessage(m.body)
	msg.Properties = &goamqp.MessageProperties{ContentType: &m.contentType}
	if m.routingKey != "" {
		msg.Properties.Subject = &m.routingKey
	}
	if m.correlationID != "" {
		msg.Properties.CorrelationID = m.correlationID
	}
	if m.messageID != "" {
		msg.Properties.MessageID = m.messageID
	}
	if m.replyTo != "" {
		msg.Properties.ReplyTo = &m.replyTo
	}
	if len(m.headers) > 0 {
		msg.ApplicationProperties = map[string]any{}
		for k, v := range m.headers {
			msg.ApplicationProperties[k] = v
		}
	}
	msg.Header = &goamqp.MessageHeader{Durable: true}
	return snd.Send(ctx, msg, nil)
}

func (c *v10) listen(ctx context.Context, kind, target string, _ []string, in *cloudstep.Inbox) (func(context.Context) error, error) {
	sess, err := c.conn.NewSession(ctx, nil)
	if err != nil {
		return nil, err
	}
	rcv, err := sess.NewReceiver(ctx, target, &goamqp.ReceiverOptions{SourceCapabilities: capability(kind), Credit: 100})
	if err != nil {
		_ = sess.Close(context.Background())
		return nil, fmt.Errorf("cannot receive from the %s amqp %s: %w", target, kind, err)
	}
	go func() {
		for {
			msg, err := rcv.Receive(ctx, nil)
			if err != nil {
				if ctx.Err() == nil {
					in.Fail(fmt.Errorf("the broker stopped the delivery of the %s amqp %s's messages: %w", target, kind, err))
				}
				return
			}
			in.Add(incoming10(msg))
			_ = rcv.AcceptMessage(ctx, msg)
		}
	}()
	return func(c context.Context) error {
		_ = rcv.Close(c)
		return sess.Close(c)
	}, nil
}

func incoming10(msg *goamqp.Message) cloudstep.Message {
	m := cloudstep.Message{Body: body10(msg), Fields: map[string]string{}, Meta: map[string]string{}}
	for k, v := range msg.ApplicationProperties {
		m.Fields[k] = fmt.Sprint(v)
	}
	if p := msg.Properties; p != nil {
		set := func(name string, v *string) {
			if v != nil {
				m.Meta[name] = *v
			}
		}
		set("routing key", p.Subject)
		set("content type", p.ContentType)
		set("reply to", p.ReplyTo)
		if p.CorrelationID != nil {
			m.Meta["correlation id"] = fmt.Sprint(p.CorrelationID)
		}
		if p.MessageID != nil {
			m.Meta["message id"] = fmt.Sprint(p.MessageID)
		}
	}
	return m
}

// body10 is a message's body: its data sections, or its value, which JMS
// producers send text messages as.
func body10(msg *goamqp.Message) []byte {
	if len(msg.Data) > 0 {
		var b []byte
		for _, d := range msg.Data {
			b = append(b, d...)
		}
		return b
	}
	switch v := msg.Value.(type) {
	case nil:
		return nil
	case string:
		return []byte(v)
	case []byte:
		return v
	default:
		return fmt.Appendf(nil, "%v", v)
	}
}
