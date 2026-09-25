package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// The manifest importer registers the parcels that shops upload in bulk.
// Their systems insert rows into parcels.manifest_lines (status PENDING);
// every poll the importer registers each pending line as a parcel (same rules
// as the API) and marks the line IMPORTED with the parcel reference, or
// REJECTED with the reason:
//
//	weight exceeds 30000 g | weight must be at least 1 g |
//	unknown service level | invalid reference | invalid recipient address |
//	duplicate reference | address not deliverable
type manifestLine struct {
	ID           string
	ManifestID   string
	Reference    string
	Sender       string
	WeightGrams  int
	ServiceLevel string
	Recipient    Recipient
}

func (s *service) runImporter(ctx context.Context, every time.Duration) {
	var handled sync.Map // lines handled while a mutant keeps them PENDING
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.importPending(ctx, &handled); err != nil && ctx.Err() == nil {
			s.log.Warn("manifest import failed", "err", err)
		}
	}
}

func (s *service) importPending(ctx context.Context, handled *sync.Map) error {
	rows, err := s.store.pool.Query(ctx, `
SELECT id, manifest_id, reference, sender, weight_grams, service_level, recipient
FROM parcels.manifest_lines WHERE status = 'PENDING' ORDER BY received_at, id LIMIT 100`)
	if err != nil {
		return err
	}
	lines, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (manifestLine, error) {
		var l manifestLine
		var rec []byte
		if err := row.Scan(&l.ID, &l.ManifestID, &l.Reference, &l.Sender, &l.WeightGrams, &l.ServiceLevel, &rec); err != nil {
			return l, err
		}
		if err := json.Unmarshal(rec, &l.Recipient); err != nil {
			l.Recipient = Recipient{}
		}
		return l, nil
	})
	if err != nil {
		return err
	}
	for _, l := range lines {
		if _, done := handled.Load(l.ID); done {
			continue
		}
		status, reason, ref := s.importLine(ctx, l)
		if status == "" {
			continue // retry on the next poll (address service unavailable)
		}
		if status == "IMPORTED" && s.mut.On("import-leaves-line-pending") {
			handled.Store(l.ID, true)
			continue
		}
		if _, err := s.store.pool.Exec(ctx, `
UPDATE parcels.manifest_lines SET status = $2, error = $3, parcel_reference = $4, processed_at = now()
WHERE id = $1 AND status = 'PENDING'`, l.ID, status, nullable(reason), nullable(ref)); err != nil {
			return fmt.Errorf("line %s: %w", l.ID, err)
		}
		s.log.Info("manifest line processed", "line", l.ID, "status", status, "reason", reason)
	}
	return nil
}

// importLine returns the new line status ("" to retry later), the rejection
// reason and the parcel reference.
func (s *service) importLine(ctx context.Context, l manifestLine) (status, reason, ref string) {
	switch {
	case l.WeightGrams > maxWeightGrams && !s.mut.On("import-accepts-overweight"):
		return "REJECTED", fmt.Sprintf("weight exceeds %d g", maxWeightGrams), ""
	case l.WeightGrams < 1:
		return "REJECTED", "weight must be at least 1 g", ""
	case !serviceLevels[l.ServiceLevel]:
		return "REJECTED", "unknown service level", ""
	case !referencePattern.MatchString(l.Reference):
		return "REJECTED", "invalid reference", ""
	case l.Recipient.Name == "" || l.Recipient.Postcode == "" || !countryPattern.MatchString(l.Recipient.Country):
		return "REJECTED", "invalid recipient address", ""
	}
	_, err := s.register(ctx, registration{
		Reference: l.Reference, Sender: l.Sender, WeightGrams: l.WeightGrams, ServiceLevel: l.ServiceLevel,
		Recipient: l.Recipient, Source: "manifest", ManifestID: l.ManifestID, LineID: l.ID,
	})
	var und *undeliverableError
	var up *upstreamError
	switch {
	case err == nil:
		return "IMPORTED", "", l.Reference
	case errors.Is(err, errDuplicate):
		return "REJECTED", "duplicate reference", ""
	case errors.As(err, &und):
		return "REJECTED", "address not deliverable", ""
	case errors.As(err, &up):
		s.log.Warn("manifest line waits for the address service", "line", l.ID, "err", err)
		return "", "", ""
	default:
		s.log.Error("manifest line failed", "line", l.ID, "err", err)
		return "", "", ""
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
