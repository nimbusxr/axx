package main

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// shopSettings are what a shop set in the portal.
type shopSettings struct {
	Shop            string
	PickupAddress   string
	PickupDays      []string
	NotifyDelivered bool
	Logo            string
}

// Settings returns a shop's settings; a shop that set none has empty ones.
func (s *store) Settings(ctx context.Context, shop string) (*shopSettings, error) {
	st := &shopSettings{Shop: shop}
	var days string
	err := s.pool.QueryRow(ctx, `SELECT pickup_address, pickup_days, notify_delivered, logo FROM parcels.shop_settings WHERE shop = $1`, shop).
		Scan(&st.PickupAddress, &days, &st.NotifyDelivered, &st.Logo)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if days != "" {
		st.PickupDays = strings.Split(days, ",")
	}
	return st, err
}

// SaveSettings stores a shop's settings.
func (s *store) SaveSettings(ctx context.Context, st *shopSettings) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO parcels.shop_settings (shop, pickup_address, pickup_days, notify_delivered, logo)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (shop) DO UPDATE SET pickup_address = $2, pickup_days = $3, notify_delivered = $4,
    logo = CASE WHEN $5 = '' THEN parcels.shop_settings.logo ELSE $5 END, updated_at = now()`,
		st.Shop, st.PickupAddress, strings.Join(st.PickupDays, ","), st.NotifyDelivered, st.Logo)
	return err
}

// PlanPickup records the day a parcel is picked up.
func (s *store) PlanPickup(ctx context.Context, ref, day string) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO parcels.pickups (reference, day) VALUES ($1, $2)
ON CONFLICT (reference) DO UPDATE SET day = $2, planned_at = now()`, ref, day)
	return err
}
