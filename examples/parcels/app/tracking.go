package main

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// The tracking read model lives in MongoDB. Depot scans (from the
// depot-scans topic, see events.go) land in the "scans" collection:
//
//	{scanId, parcelRef, status, location, scannedAt}
//
// and the projector keeps one summary per parcel in "tracking" (_id = the
// parcel reference): the status and location of the latest scan (by
// scannedAt; scans arrive out of order), the number of distinct scans
// (scanners resend scans with the same scanId), whether the parcel was
// delivered, and when the latest scan happened.
type trackingStore struct {
	client *mongo.Client
	db     *mongo.Database
	log    *slog.Logger
}

type trackingView struct {
	ParcelRef    string     `json:"parcelRef" bson:"parcelRef"`
	Status       string     `json:"status" bson:"status"`
	LastLocation *string    `json:"lastLocation" bson:"lastLocation"`
	LastScanAt   *time.Time `json:"lastScanAt" bson:"lastScanAt"`
	ScanCount    int        `json:"scanCount" bson:"scanCount"`
	Delivered    bool       `json:"delivered" bson:"delivered"`
}

type scan struct {
	ScanID    string
	Status    string
	Location  string
	ScannedAt time.Time
}

func openTracking(ctx context.Context, uri, dbName string, log *slog.Logger) (*trackingStore, error) {
	var client *mongo.Client
	err := retry(ctx, log, "mongodb", func() error {
		c, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(3 * time.Second))
		if err != nil {
			return err
		}
		if err := c.Ping(ctx, nil); err != nil {
			_ = c.Disconnect(context.WithoutCancel(ctx))
			return err
		}
		client = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	t := &trackingStore{client: client, db: client.Database(dbName), log: log}
	_, err = t.db.Collection("scans").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "parcelRef", Value: 1}}},
		{Keys: bson.D{{Key: "projected", Value: 1}}},
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (t *trackingStore) Close(ctx context.Context) { _ = t.client.Disconnect(ctx) }

func (t *trackingStore) Get(ctx context.Context, ref string) (*trackingView, error) {
	var v trackingView
	err := t.db.Collection("tracking").FindOne(ctx, bson.D{{Key: "_id", Value: ref}}).Decode(&v)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.LastScanAt != nil {
		u := v.LastScanAt.UTC()
		v.LastScanAt = &u
	}
	return &v, nil
}

// AddScan stores a depot scan for the projector.
func (t *trackingStore) AddScan(ctx context.Context, parcelRef string, s scan) error {
	_, err := t.db.Collection("scans").InsertOne(ctx, bson.D{
		{Key: "scanId", Value: s.ScanID},
		{Key: "parcelRef", Value: parcelRef},
		{Key: "status", Value: s.Status},
		{Key: "location", Value: s.Location},
		{Key: "scannedAt", Value: s.ScannedAt},
	})
	return err
}

func (t *trackingStore) runProjector(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := t.project(ctx); err != nil && ctx.Err() == nil {
			t.log.Warn("tracking projection failed", "err", err)
		}
	}
}

func (t *trackingStore) project(ctx context.Context) error {
	scans := t.db.Collection("scans")
	cur, err := scans.Find(ctx, bson.D{{Key: "projected", Value: bson.D{{Key: "$ne", Value: true}}}},
		options.Find().SetLimit(500).SetProjection(bson.D{{Key: "parcelRef", Value: 1}}))
	if err != nil {
		return err
	}
	var pending []bson.M
	if err := cur.All(ctx, &pending); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	refs := map[string]bool{}
	ids := make([]any, 0, len(pending))
	for _, d := range pending {
		ids = append(ids, d["_id"])
		if ref, ok := d["parcelRef"].(string); ok && ref != "" {
			refs[ref] = true
		}
	}
	for ref := range refs {
		if err := t.rebuild(ctx, ref); err != nil {
			return err
		}
	}
	_, err = scans.UpdateMany(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "projected", Value: true}}}})
	return err
}

// rebuild recomputes one parcel's summary from all of its scans.
func (t *trackingStore) rebuild(ctx context.Context, ref string) error {
	cur, err := t.db.Collection("scans").Find(ctx, bson.D{{Key: "parcelRef", Value: ref}})
	if err != nil {
		return err
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return err
	}
	all := make([]scan, 0, len(docs))
	for _, d := range docs {
		var s scan
		s.ScanID, _ = d["scanId"].(string)
		s.Status, _ = d["status"].(string)
		s.Location, _ = d["location"].(string)
		s.ScannedAt = asTime(d["scannedAt"])
		all = append(all, s)
	}
	v := summarize(ref, all)
	doc := bson.D{
		{Key: "parcelRef", Value: v.ParcelRef},
		{Key: "status", Value: v.Status},
		{Key: "lastLocation", Value: v.LastLocation},
		{Key: "lastScanAt", Value: v.LastScanAt},
		{Key: "scanCount", Value: v.ScanCount},
		{Key: "delivered", Value: v.Delivered},
		{Key: "updatedAt", Value: time.Now().UTC()},
	}
	_, err = t.db.Collection("tracking").UpdateOne(ctx, bson.D{{Key: "_id", Value: ref}},
		bson.D{{Key: "$set", Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

// summarize is the projection rule.
func summarize(ref string, all []scan) trackingView {
	distinct := make([]scan, 0, len(all))
	seen := map[string]bool{}
	for _, s := range all {
		if s.ScanID != "" {
			if seen[s.ScanID] {
				continue
			}
			seen[s.ScanID] = true
		}
		distinct = append(distinct, s)
	}
	sort.SliceStable(distinct, func(i, j int) bool {
		if !distinct[i].ScannedAt.Equal(distinct[j].ScannedAt) {
			return distinct[i].ScannedAt.Before(distinct[j].ScannedAt)
		}
		return distinct[i].ScanID < distinct[j].ScanID
	})
	v := trackingView{ParcelRef: ref, Status: "REGISTERED", ScanCount: len(distinct)}
	if len(distinct) == 0 {
		return v
	}
	latest := distinct[len(distinct)-1]
	v.Status = latest.Status
	loc := latest.Location
	at := latest.ScannedAt.UTC()
	v.LastLocation, v.LastScanAt = &loc, &at
	for _, s := range distinct {
		if s.Status == "DELIVERED" {
			v.Delivered = true
		}
	}
	return v
}

func asTime(v any) time.Time {
	switch t := v.(type) {
	case bson.DateTime:
		return t.Time().UTC()
	case time.Time:
		return t.UTC()
	case string:
		if p, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return p.UTC()
		}
	}
	return time.Time{}
}
