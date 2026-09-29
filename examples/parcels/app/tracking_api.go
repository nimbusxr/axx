package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	trackingv1 "github.com/nimbusxr/axx/examples/parcels/app/gen/tracking/v1"
)

// trackingAPI is the tracking API the shops' systems call over gRPC:
// where a parcel is, and its scans as they happen. It serves its
// descriptors through server reflection.
type trackingAPI struct {
	trackingv1.UnimplementedTrackingServer
	store    *store
	tracking *trackingStore
	log      *slog.Logger
}

// serveTrackingAPI serves the tracking API on addr until ctx ends.
func serveTrackingAPI(ctx context.Context, addr string, api *trackingAPI) (func(), error) {
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := grpc.NewServer()
	trackingv1.RegisterTrackingServer(srv, api)
	reflection.Register(srv)
	go func() {
		if err := srv.Serve(lis); err != nil {
			api.log.Error("the tracking API stopped", "err", err)
		}
	}()
	// Streams go on until their clients leave: they get a moment to.
	return func() {
		done := make(chan struct{})
		go func() {
			srv.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			srv.Stop()
		}
	}, nil
}

func (a *trackingAPI) GetParcel(ctx context.Context, req *trackingv1.GetParcelRequest) (*trackingv1.Parcel, error) {
	p, err := a.store.Get(ctx, req.GetReference())
	if errors.Is(err, errNotFound) {
		return nil, status.Errorf(codes.NotFound, "no parcel %s", req.GetReference())
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "the parcels are unavailable; try again later")
	}
	out := &trackingv1.Parcel{
		Reference:    p.Reference,
		Status:       p.Status,
		ServiceLevel: trackingv1.ServiceLevel(trackingv1.ServiceLevel_value[p.ServiceLevel]),
		WeightGrams:  int32(p.WeightGrams), //nolint:gosec // at most maxWeightGrams
	}
	t, err := a.tracking.Get(ctx, p.Reference)
	switch {
	case err == nil:
		out.Status, out.LastScan = t.Status, lastScan(t)
	case !errors.Is(err, errNotFound):
		a.log.Warn("tracking failed", "reference", p.Reference, "err", err)
	}
	return out, nil
}

// WatchParcel sends the parcel's latest scan, then each new one, and ends
// once the parcel is delivered.
func (a *trackingAPI) WatchParcel(req *trackingv1.WatchParcelRequest, stream grpc.ServerStreamingServer[trackingv1.Scan]) error {
	ctx, ref := stream.Context(), req.GetReference()
	if _, err := a.store.Get(ctx, ref); errors.Is(err, errNotFound) {
		return status.Errorf(codes.NotFound, "no parcel %s", ref)
	} else if err != nil {
		return status.Error(codes.Unavailable, "the parcels are unavailable; try again later")
	}
	var last *trackingEvent
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		t, err := a.tracking.Get(ctx, ref)
		switch {
		case ctx.Err() != nil:
			return nil
		case err == nil:
			now := trackingEvent{Reference: ref, Status: t.Status, Location: t.LastLocation, ScannedAt: t.LastScanAt}
			if last == nil || !now.same(*last) {
				if err := stream.Send(lastScan(t)); err != nil {
					return err
				}
				if t.Delivered {
					return nil
				}
				last = &now
			}
		case !errors.Is(err, errNotFound):
			a.log.Warn("tracking failed", "reference", ref, "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// lastScan is a tracking view's latest scan.
func lastScan(t *trackingView) *trackingv1.Scan {
	s := &trackingv1.Scan{Reference: t.ParcelRef, Status: t.Status}
	if t.LastLocation != nil {
		s.Location = *t.LastLocation
	}
	if t.LastScanAt != nil {
		s.ScannedAt = timestamppb.New(*t.LastScanAt)
	}
	return s
}
