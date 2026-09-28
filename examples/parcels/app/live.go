package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// The tracking page asks, from the browser, when its parcel arrives
// (estimate), and follows the parcel's depot scans as they happen (live).

// estimate is when a parcel arrives: a window on the day it is delivered,
// or when it was delivered.
type estimate struct {
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
}

// estimateFor is the parcel's delivery window: its delivery days (see
// priceQuote) counted in working days from its registration, 09:00 to 12:00
// for EXPRESS and to 18:00 otherwise, UTC.
func estimateFor(p *Parcel) estimate {
	days := priceQuote(p.Zone, p.Recipient.Country, p.ServiceLevel, p.WeightGrams).DeliveryDays
	day := p.CreatedAt.UTC()
	for days > 0 {
		day = day.AddDate(0, 0, 1)
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
			days--
		}
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.UTC)
	to := from.Add(9 * time.Hour)
	if p.ServiceLevel == "EXPRESS" {
		to = from.Add(3 * time.Hour)
	}
	return estimate{From: &from, To: &to}
}

func (s *service) portalEstimate(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("reference")
	p, err := s.store.Get(r.Context(), ref)
	if errors.Is(err, errNotFound) {
		problem(w, r, http.StatusNotFound, "no parcel "+ref)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.tracking.Get(r.Context(), ref)
	switch {
	case err == nil && t.Delivered:
		writeJSON(w, http.StatusOK, estimate{DeliveredAt: t.LastScanAt})
	case err == nil || errors.Is(err, errNotFound):
		writeJSON(w, http.StatusOK, estimateFor(p))
	default:
		s.fail(w, r, err)
	}
}

// liveScan is what the tracking page hears of a parcel: its latest scan.
type liveScan struct {
	Reference string  `json:"reference"`
	Status    string  `json:"status"`
	Location  *string `json:"location"`
}

// portalLive is the tracking page's websocket: the page says which parcel
// it follows ({"follow": "PX-..."}), and hears its latest scan, then each
// new one.
func (s *service) portalLive(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	var follow struct {
		Follow string `json:"follow"`
	}
	_, msg, err := c.Read(r.Context())
	if err != nil || json.Unmarshal(msg, &follow) != nil || follow.Follow == "" {
		_ = c.Close(websocket.StatusPolicyViolation, `say {"follow": "<parcel reference>"} first`)
		return
	}
	// Then the page only listens.
	ctx := c.CloseRead(r.Context())
	var last *liveScan
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		now := liveScan{Reference: follow.Follow, Status: "REGISTERED"}
		t, err := s.tracking.Get(ctx, follow.Follow)
		switch {
		case err == nil:
			now.Status, now.Location = t.Status, t.LastLocation
		case !errors.Is(err, errNotFound):
			s.log.Warn("live tracking failed", "reference", follow.Follow, "err", err)
		}
		if last == nil || now.Status != last.Status || !sameLocation(now.Location, last.Location) {
			b, _ := json.Marshal(now)
			if c.Write(ctx, websocket.MessageText, b) != nil {
				return
			}
			last = &now
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func sameLocation(a, b *string) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// trackingEvent is what a parcel's event stream says of its latest scan.
type trackingEvent struct {
	Reference string     `json:"reference"`
	Status    string     `json:"status"`
	Location  *string    `json:"location"`
	ScannedAt *time.Time `json:"scannedAt"`
}

func (e trackingEvent) same(o trackingEvent) bool {
	return e.Status == o.Status && sameLocation(e.Location, o.Location) &&
		(e.ScannedAt == nil) == (o.ScannedAt == nil) && (e.ScannedAt == nil || e.ScannedAt.Equal(*o.ScannedAt))
}

// trackingEvents is a parcel's tracking as server-sent events, for the
// shops' own systems: a scan event for its latest scan and each new one,
// then a delivered event, which ends the stream. An event's id is the
// number of scans so far. A comment every 15 seconds keeps the proxies on
// the way from closing a quiet stream.
func (s *service) trackingEvents(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("reference")
	if _, err := s.store.Get(r.Context(), ref); errors.Is(err, errNotFound) {
		problem(w, r, http.StatusNotFound, "no parcel "+ref)
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	send := func(format string, args ...any) bool {
		_, err := fmt.Fprintf(w, format, args...)
		return err == nil && rc.Flush() == nil
	}
	if !send(": tracking %s\n\n", ref) {
		return
	}
	ctx := r.Context()
	var last *trackingEvent
	quiet := time.Now()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		t, err := s.tracking.Get(ctx, ref)
		switch {
		case ctx.Err() != nil:
			return
		case err == nil:
			now := trackingEvent{Reference: ref, Status: t.Status, Location: t.LastLocation, ScannedAt: t.LastScanAt}
			if last != nil && now.same(*last) {
				break
			}
			kind := "scan"
			if t.Delivered {
				kind = "delivered"
			}
			b, _ := json.Marshal(now)
			if !send("event: %s\nid: %d\ndata: %s\n\n", kind, t.ScanCount, b) || t.Delivered {
				return
			}
			last, quiet = &now, time.Now()
		case !errors.Is(err, errNotFound):
			s.log.Warn("tracking events failed", "reference", ref, "err", err)
		}
		if time.Since(quiet) >= 15*time.Second {
			if !send(": keep-alive\n\n") {
				return
			}
			quiet = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
