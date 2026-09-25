package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// Filing is a broker's customs declaration for a parcel.
type Filing struct {
	Declaration string `json:"declaration"`
	Parcel      string `json:"parcel"`
	// Invoice is the commercial invoice's blob in the invoices container.
	Invoice string `json:"invoice"`
}

// Invoice is a commercial invoice: what the parcel contains and its value.
type Invoice struct {
	Currency string  `json:"currency"`
	Total    float64 `json:"total"`
}

type service struct {
	azure     *clients
	names     names
	log       *slog.Logger
	deMinimis float64 // parcels worth up to this are cleared without duties
	dutyRate  float64
	now       func() time.Time
}

// clear decides a declaration from its commercial invoice: parcels worth
// up to the de minimis value are cleared, others owe duties first, and a
// declaration without its invoice is held.
func (s *service) clear(ctx context.Context, f Filing, broker string) error {
	raw, found, err := s.download(ctx, s.names.Invoices, f.Invoice)
	if err != nil {
		return err
	}
	if !found {
		s.log.Warn("declaration held", "declaration", f.Declaration, "reason", "MISSING_INVOICE")
		return s.announce(ctx, "DeclarationHeld", map[string]any{"declaration": f.Declaration, "parcel": f.Parcel, "reason": "MISSING_INVOICE"})
	}
	var inv Invoice
	if err := json.Unmarshal(raw, &inv); err != nil {
		return s.announce(ctx, "DeclarationHeld", map[string]any{"declaration": f.Declaration, "parcel": f.Parcel, "reason": "UNREADABLE_INVOICE"})
	}
	if err := s.upload(ctx, s.names.Archive, f.Declaration+"/invoice.json", "application/json", raw); err != nil {
		return err
	}
	if inv.Total > s.deMinimis {
		duties := math.Round(inv.Total*s.dutyRate*100) / 100
		payment, _ := json.Marshal(map[string]any{"declaration": f.Declaration, "parcel": f.Parcel, "amount": duties, "currency": inv.Currency})
		if err := s.send(ctx, s.names.Duties, payment, map[string]any{"broker": broker}); err != nil {
			return err
		}
		s.log.Info("declaration held", "declaration", f.Declaration, "reason", "DUTIES_DUE", "duties", duties)
		return s.announce(ctx, "DeclarationHeld", map[string]any{"declaration": f.Declaration, "parcel": f.Parcel, "reason": "DUTIES_DUE", "duties": duties})
	}
	certificate, _ := json.MarshalIndent(map[string]any{
		"declaration": f.Declaration, "parcel": f.Parcel, "status": "CLEARED", "duties": 0,
		"currency": inv.Currency, "value": inv.Total, "clearedAt": s.now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err := s.upload(ctx, s.names.Clearances, f.Declaration+".json", "application/json", certificate); err != nil {
		return err
	}
	s.log.Info("declaration cleared", "declaration", f.Declaration)
	return s.announce(ctx, "DeclarationCleared", map[string]any{"declaration": f.Declaration, "parcel": f.Parcel})
}

// arrived releases a parcel that reaches the border cleared, and holds one
// that is not.
func (s *service) arrived(ctx context.Context, declaration, parcel string) error {
	_, cleared, err := s.download(ctx, s.names.Clearances, declaration+".json")
	if err != nil {
		return err
	}
	event := "HeldAtBorder"
	if cleared {
		event = "ReleasedForDelivery"
	}
	s.log.Info("parcel at the border", "declaration", declaration, "event", event)
	return s.announce(ctx, event, map[string]any{"declaration": declaration, "parcel": parcel})
}

// ---- Blob Storage ----

func (s *service) download(ctx context.Context, container, name string) ([]byte, bool, error) {
	if name == "" {
		return nil, false, nil
	}
	out, err := s.azure.blob.DownloadStream(ctx, container, name, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	return b, true, err
}

func (s *service) upload(ctx context.Context, container, name, contentType string, body []byte) error {
	_, err := s.azure.blob.UploadStream(ctx, container, name, bytes.NewReader(body), &azblob.UploadStreamOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: to.Ptr(contentType)},
	})
	return err
}

// ---- Service Bus ----

func (s *service) send(ctx context.Context, entity string, body []byte, props map[string]any) error {
	snd, err := s.azure.bus.NewSender(entity, nil)
	if err != nil {
		return err
	}
	defer snd.Close(ctx)
	return snd.SendMessage(ctx, &azservicebus.Message{Body: body, ApplicationProperties: props, ContentType: to.Ptr("application/json")}, nil)
}

// announce publishes a customs event, typed by its eventType property.
func (s *service) announce(ctx context.Context, eventType string, body map[string]any) error {
	b, _ := json.Marshal(body)
	return s.send(ctx, s.names.Events, b, map[string]any{"eventType": eventType})
}

func (s *service) startReceivers(ctx context.Context) {
	go s.receive(ctx, "the filings queue", func() (*azservicebus.Receiver, error) {
		return s.azure.bus.NewReceiverForQueue(s.names.Filings, nil)
	}, s.filed)
	go s.receive(ctx, "the border subscription", func() (*azservicebus.Receiver, error) {
		return s.azure.bus.NewReceiverForSubscription(s.names.Border, s.names.BorderSub, nil)
	}, s.borderEvent)
}

// receive handles messages until ctx ends; a message whose handling fails
// is abandoned, to be delivered again.
func (s *service) receive(ctx context.Context, what string, open func() (*azservicebus.Receiver, error), handle func(context.Context, *azservicebus.ReceivedMessage) error) {
	for ctx.Err() == nil {
		r, err := open()
		if err != nil {
			s.log.Warn("cannot receive; retrying", "from", what, "error", err)
			time.Sleep(time.Second)
			continue
		}
		for ctx.Err() == nil {
			ms, err := r.ReceiveMessages(ctx, 10, nil)
			if err != nil {
				if ctx.Err() == nil {
					s.log.Warn("receive failed; reconnecting", "from", what, "error", err)
					time.Sleep(time.Second)
				}
				break
			}
			for _, m := range ms {
				if err := handle(ctx, m); err != nil {
					s.log.Error("message failed; it will come back", "from", what, "error", err)
					_ = r.AbandonMessage(ctx, m, nil)
					continue
				}
				_ = r.CompleteMessage(ctx, m, nil)
			}
		}
		_ = r.Close(context.Background())
	}
}

// decode reads a message's JSON body, reporting whether it was JSON.
func decode(m *azservicebus.ReceivedMessage, v any) bool { return json.Unmarshal(m.Body, v) == nil }

func (s *service) filed(ctx context.Context, m *azservicebus.ReceivedMessage) error {
	var f Filing
	if !decode(m, &f) || f.Declaration == "" {
		s.log.Warn("filing ignored: not a declaration")
		return nil
	}
	broker, _ := m.ApplicationProperties["broker"].(string)
	s.log.Info("declaration filed", "declaration", f.Declaration, "broker", broker)
	return s.clear(ctx, f, broker)
}

func (s *service) borderEvent(ctx context.Context, m *azservicebus.ReceivedMessage) error {
	var e struct{ Declaration, Parcel string }
	if !decode(m, &e) || e.Declaration == "" {
		s.log.Warn("border event ignored: no declaration")
		return nil
	}
	return s.arrived(ctx, e.Declaration, e.Parcel)
}
