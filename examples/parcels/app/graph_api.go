package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"

	"github.com/nimbusxr/axx/examples/parcels/app/graph"
	"github.com/nimbusxr/axx/examples/parcels/app/graph/model"
)

// graphHandler serves the parcels subgraph on /graphql: queries and
// mutations over HTTP, subscriptions over graphql-ws or server-sent events.
func graphHandler(g *parcelsGraph) http.Handler {
	h := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{Parcels: g}}))
	h.AddTransport(transport.Websocket{KeepAlivePingInterval: 10 * time.Second})
	h.AddTransport(transport.SSE{})
	h.AddTransport(transport.POST{})
	h.Use(extension.Introspection{})
	return h
}

// parcelsGraph gives the subgraph the service's parcels.
type parcelsGraph struct {
	store    *store
	tracking *trackingStore
	log      *slog.Logger
}

func (g *parcelsGraph) Parcel(ctx context.Context, ref string) (*model.Parcel, error) {
	p, err := g.store.Get(ctx, ref)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	until, err := g.store.HeldUntil(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := toGraph(p, until)
	t, err := g.tracking.Get(ctx, ref)
	switch {
	case err == nil:
		out.LastScan = graphScan(t)
	case !errors.Is(err, errNotFound):
		g.log.Warn("tracking failed", "reference", ref, "err", err)
	}
	return out, nil
}

func (g *parcelsGraph) Hold(ctx context.Context, ref, day string, reason *string) (*model.Parcel, error) {
	until, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, graph.ErrBadDay
	}
	var refused *graph.NotHoldableError
	p, err := g.store.Hold(ctx, ref, until, func(p *Parcel) error {
		if !holdable[p.Status] {
			refused = &graph.NotHoldableError{Reference: p.Reference, Status: p.Status}
			return errNotChangeable
		}
		return nil
	})
	switch {
	case refused != nil:
		return nil, refused
	case errors.Is(err, errNotFound):
		return nil, graph.ErrNoParcel
	case err != nil:
		return nil, err
	}
	why := ""
	if reason != nil {
		why = *reason
	}
	g.log.Info("parcel held", "reference", ref, "until", day, "reason", why)
	return toGraph(p, &until), nil
}

// Scans sends the parcel's latest scan, then each new one, and closes once
// it is delivered or ctx ends.
func (g *parcelsGraph) Scans(ctx context.Context, ref string) (<-chan *model.Scan, error) {
	if _, err := g.store.Get(ctx, ref); errors.Is(err, errNotFound) {
		return nil, graph.ErrNoParcel
	} else if err != nil {
		return nil, err
	}
	out := make(chan *model.Scan, 1)
	go func() {
		defer close(out)
		var last *trackingEvent
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			t, err := g.tracking.Get(ctx, ref)
			switch {
			case ctx.Err() != nil:
				return
			case err == nil:
				now := trackingEvent{Reference: ref, Status: t.Status, Location: t.LastLocation, ScannedAt: t.LastScanAt}
				if last == nil || !now.same(*last) {
					select {
					case out <- graphScan(t):
					case <-ctx.Done():
						return
					}
					if t.Delivered {
						return
					}
					last = &now
				}
			case !errors.Is(err, errNotFound):
				g.log.Warn("tracking failed", "reference", ref, "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return out, nil
}

func toGraph(p *Parcel, heldUntil *time.Time) *model.Parcel {
	out := &model.Parcel{
		Reference:    p.Reference,
		Status:       p.Status,
		ServiceLevel: model.ServiceLevel(p.ServiceLevel),
		WeightGrams:  p.WeightGrams,
		Shop:         &model.Shop{ID: p.Sender},
	}
	if heldUntil != nil {
		day := heldUntil.Format(time.DateOnly)
		out.HeldUntil = &day
	}
	return out
}

func graphScan(t *trackingView) *model.Scan {
	s := &model.Scan{Reference: t.ParcelRef, Status: t.Status, Location: t.LastLocation}
	if t.LastScanAt != nil {
		at := t.LastScanAt.UTC().Format(time.RFC3339)
		s.ScannedAt = &at
	}
	return s
}
