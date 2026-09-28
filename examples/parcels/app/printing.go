package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// The depots print shipping labels on printers that talk AMQP, through
// RabbitMQ:
//
//	labels                          a topic exchange: a print job for each
//	                                registered parcel, routed by its service
//	                                level (print.express, print.standard),
//	                                its sender in a header
//	printers                        a topic exchange the printers report each
//	                                label they print to, as printed.<depot>
//	parcels.label-printed           the service's queue of reports, bound to
//	                                printers with printed.#; older printers
//	                                send their reports straight to it
//	parcels.label-printed.rejected  the reports the service cannot use, with
//	                                the reason in a header
//
// A report is JSON: {"reference", "printer", "printedAt"}; a printer header
// names the printer too.
const (
	labelsExchange   = "labels"
	printersExchange = "printers"
	reportsQueue     = "parcels.label-printed"
	rejectedQueue    = "parcels.label-printed.rejected"
)

type printing struct {
	conn   *amqp.Connection
	mu     sync.Mutex // guards pub: a channel publishes one message at a time
	pub    *amqp.Channel
	store  *store
	labels labeler
	log    *slog.Logger
}

func openPrinting(ctx context.Context, url string, store *store, labels labeler, log *slog.Logger) (*printing, error) {
	var conn *amqp.Connection
	err := retry(ctx, log, "rabbitmq", func() error {
		c, err := amqp.Dial(url)
		if err == nil {
			conn = c
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	pub, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	declare := []func() error{
		func() error { return pub.ExchangeDeclare(labelsExchange, "topic", true, false, false, false, nil) },
		func() error { return pub.ExchangeDeclare(printersExchange, "topic", true, false, false, false, nil) },
		func() error { _, err := pub.QueueDeclare(reportsQueue, true, false, false, false, nil); return err },
		func() error { _, err := pub.QueueDeclare(rejectedQueue, true, false, false, false, nil); return err },
		func() error { return pub.QueueBind(reportsQueue, "printed.#", printersExchange, false, nil) },
	}
	for _, d := range declare {
		if err := d(); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("declaring the label printing exchanges and queues: %w", err)
		}
	}
	return &printing{conn: conn, pub: pub, store: store, labels: labels, log: log}, nil
}

func (p *printing) Close() { _ = p.conn.Close() }

func (p *printing) publish(ctx context.Context, exchange, key string, m amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	m.DeliveryMode = amqp.Persistent
	return p.pub.PublishWithContext(ctx, exchange, key, false, false, m)
}

// printJob sends a registered parcel's label to the printers of its
// service level.
func (p *printing) printJob(ctx context.Context, parcel *Parcel) error {
	l := p.labels.label(parcel)
	body, err := json.Marshal(map[string]any{
		"reference": parcel.Reference, "serviceLevel": parcel.ServiceLevel, "barcode": l.Barcode, "zpl": zpl(parcel, l),
	})
	if err != nil {
		return err
	}
	return p.publish(ctx, labelsExchange, "print."+strings.ToLower(parcel.ServiceLevel), amqp.Publishing{
		Headers: amqp.Table{"sender": parcel.Sender}, ContentType: "application/json", Body: body,
	})
}

// run handles the printers' reports until ctx ends.
func (p *printing) run(ctx context.Context) {
	ch, err := p.conn.Channel()
	if err != nil {
		p.log.Error("label reports", "err", err)
		return
	}
	defer func() { _ = ch.Close() }()
	reports, err := ch.ConsumeWithContext(ctx, reportsQueue, "parcels", false, false, false, false, nil)
	if err != nil {
		p.log.Error("label reports", "err", err)
		return
	}
	for d := range reports {
		p.handle(ctx, d)
		_ = d.Ack(false)
	}
}

type labelReport struct {
	Reference string    `json:"reference"`
	Printer   string    `json:"printer"`
	PrintedAt time.Time `json:"printedAt"`
}

// handle records the printing of a label, or sets the report aside.
func (p *printing) handle(ctx context.Context, d amqp.Delivery) {
	var r labelReport
	if json.Unmarshal(d.Body, &r) != nil || r.Reference == "" {
		p.reject(ctx, d, "not a label report")
		return
	}
	if printer, ok := d.Headers["printer"].(string); ok && printer != "" {
		r.Printer = printer
	}
	if r.PrintedAt.IsZero() {
		r.PrintedAt = time.Now()
	}
	err := p.store.LabelPrinted(ctx, r.Reference, r.Printer, r.PrintedAt)
	switch {
	case errors.Is(err, errNotFound):
		p.reject(ctx, d, "unknown parcel")
	case err != nil:
		p.log.Error("recording a printed label failed", "reference", r.Reference, "err", err)
	default:
		p.log.Info("label printed", "reference", r.Reference, "printer", r.Printer)
	}
}

// reject sets a report aside, as it came, with the reason.
func (p *printing) reject(ctx context.Context, d amqp.Delivery, reason string) {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers["reason"] = reason
	if err := p.publish(ctx, "", rejectedQueue, amqp.Publishing{Headers: headers, ContentType: d.ContentType, Body: d.Body}); err != nil {
		p.log.Error("setting a label report aside failed", "err", err)
		return
	}
	p.log.Warn("label report set aside", "reason", reason)
}
