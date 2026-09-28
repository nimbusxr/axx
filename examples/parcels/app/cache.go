package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// The service keeps in Valkey (or any Redis) what it is asked for again and
// again:
//
//	tracking:<reference>          a parcel's tracking view, for a minute;
//	                              dropped when a scan changes it
//	idempotency:<shop>:<key>      the parcel a registration made, for a day,
//	                              under the Idempotency-Key its shop sent: a
//	                              retry of the registration is answered with it
type cache struct {
	c   *redis.Client
	log *slog.Logger
}

const (
	trackingTTL    = time.Minute
	idempotencyTTL = 24 * time.Hour
)

func openCache(ctx context.Context, url string, log *slog.Logger) (*cache, error) {
	o, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(o)
	if err := retry(ctx, log, "valkey", func() error { return c.Ping(ctx).Err() }); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &cache{c: c, log: log}, nil
}

func (c *cache) Close() { _ = c.c.Close() }

// tracking is a parcel's cached tracking view, if there is one.
func (c *cache) tracking(ctx context.Context, ref string) (*trackingView, bool) {
	b, err := c.c.Get(ctx, "tracking:"+ref).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			c.log.Warn("reading the tracking cache failed", "reference", ref, "err", err)
		}
		return nil, false
	}
	var v trackingView
	if json.Unmarshal(b, &v) != nil {
		return nil, false
	}
	return &v, true
}

func (c *cache) keepTracking(ctx context.Context, v *trackingView) {
	b, _ := json.Marshal(v)
	if err := c.c.Set(ctx, "tracking:"+v.ParcelRef, b, trackingTTL).Err(); err != nil {
		c.log.Warn("caching a tracking view failed", "reference", v.ParcelRef, "err", err)
	}
}

func (c *cache) dropTracking(ctx context.Context, ref string) {
	if err := c.c.Del(ctx, "tracking:"+ref).Err(); err != nil {
		c.log.Warn("dropping a cached tracking view failed", "reference", ref, "err", err)
	}
}

// registered is the parcel a shop's registration with that idempotency key
// made, if one did.
func (c *cache) registered(ctx context.Context, shop, key string) (string, bool) {
	ref, err := c.c.Get(ctx, "idempotency:"+shop+":"+key).Result()
	return ref, err == nil
}

func (c *cache) keepRegistered(ctx context.Context, shop, key, ref string) {
	if err := c.c.SetNX(ctx, "idempotency:"+shop+":"+key, ref, idempotencyTTL).Err(); err != nil {
		c.log.Warn("keeping an idempotency key failed", "shop", shop, "err", err)
	}
}
