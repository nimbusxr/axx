package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The tables live in the "parcels" schema, created by
// ../infra/postgres/init/01-schema.sql.

// Recipient is where a parcel goes.
type Recipient struct {
	Name     string `json:"name"`
	Street   string `json:"street,omitempty"`
	City     string `json:"city,omitempty"`
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
}

// Parcel is a registered parcel.
type Parcel struct {
	Reference    string          `json:"reference"`
	Sender       string          `json:"sender"`
	Status       string          `json:"status"`
	WeightGrams  int             `json:"weightGrams"`
	ServiceLevel string          `json:"serviceLevel"`
	Recipient    Recipient       `json:"recipient"`
	Zone         string          `json:"zone"`
	Source       string          `json:"source"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Details      json.RawMessage `json:"-"`
	LabelNumber  int64           `json:"-"`
}

type store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

var (
	errNotFound    = errors.New("not found")
	errDuplicate   = errors.New("duplicate reference")
	errLocked      = errors.New("locked by another process")
	errUnavailable = errors.New("database unavailable")
)

func openStore(ctx context.Context, url string, log *slog.Logger) (*store, error) {
	var pool *pgxpool.Pool
	err := retry(ctx, log, "postgres", func() error {
		p, err := pgxpool.New(ctx, url)
		if err != nil {
			return err
		}
		if err := p.Ping(ctx); err != nil {
			p.Close()
			return err
		}
		pool = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &store{pool: pool, log: log}, nil
}

func (s *store) Close() { s.pool.Close() }

const parcelColumns = `reference, sender, status, weight_grams, service_level, recipient, details, label_number, created_at, updated_at`

func scanParcel(row pgx.Row) (*Parcel, error) {
	var p Parcel
	var recipient []byte
	if err := row.Scan(&p.Reference, &p.Sender, &p.Status, &p.WeightGrams, &p.ServiceLevel, &recipient, &p.Details, &p.LabelNumber, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(recipient, &p.Recipient); err != nil {
		return nil, fmt.Errorf("recipient of %s: %w", p.Reference, err)
	}
	var d struct {
		Zone   string `json:"zone"`
		Source string `json:"source"`
	}
	if len(p.Details) > 0 {
		_ = json.Unmarshal(p.Details, &d)
	}
	p.Zone, p.Source = d.Zone, d.Source
	if p.Source == "" {
		p.Source = "api"
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return &p, nil
}

func (s *store) Get(ctx context.Context, ref string) (*Parcel, error) {
	return scanParcel(s.pool.QueryRow(ctx, `SELECT `+parcelColumns+` FROM parcels.parcels WHERE reference = $1`, ref))
}

// Insert stores a new parcel: errDuplicate when the reference exists, and
// errUnavailable when the database keeps reporting transient failures
// (serialization failures and deadlocks are retried twice).
func (s *store) Insert(ctx context.Context, p *Parcel, details map[string]any) (*Parcel, error) {
	rj, err := json.Marshal(p.Recipient)
	if err != nil {
		return nil, err
	}
	dj, err := json.Marshal(details)
	if err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		out, err := scanParcel(s.pool.QueryRow(ctx, `
INSERT INTO parcels.parcels (reference, sender, status, weight_grams, service_level, recipient, details)
VALUES ($1, $2, 'REGISTERED', $3, $4, $5, $6)
RETURNING `+parcelColumns, p.Reference, p.Sender, p.WeightGrams, p.ServiceLevel, rj, dj))
		switch sqlState(err) {
		case "23505":
			return nil, errDuplicate
		case "40001", "40P01":
			if attempt == 3 {
				return nil, fmt.Errorf("%w: %w", errUnavailable, err)
			}
			s.log.Warn("storing parcel failed, retrying", "reference", p.Reference, "attempt", attempt, "sqlstate", sqlState(err))
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		default:
			return out, err
		}
	}
}

// Change applies change to a parcel under a row lock. A parcel another
// process holds (the depot while dispatching it) is not waited for:
// errLocked.
func (s *store) Change(ctx context.Context, ref string, change func(*Parcel) error) (*Parcel, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	p, err := lockParcel(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	if err := change(p); err != nil {
		return nil, err
	}
	rj, err := json.Marshal(p.Recipient)
	if err != nil {
		return nil, err
	}
	out, err := scanParcel(tx.QueryRow(ctx, `
UPDATE parcels.parcels SET weight_grams = $2, service_level = $3, recipient = $4, updated_at = now()
WHERE reference = $1
RETURNING `+parcelColumns, p.Reference, p.WeightGrams, p.ServiceLevel, rj))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// Cancel deletes a parcel, under the same rules as Change.
func (s *store) Cancel(ctx context.Context, ref string, check func(*Parcel) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	p, err := lockParcel(ctx, tx, ref)
	if err != nil {
		return err
	}
	if err := check(p); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM parcels.parcels WHERE reference = $1`, ref); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockParcel(ctx context.Context, tx pgx.Tx, ref string) (*Parcel, error) {
	p, err := scanParcel(tx.QueryRow(ctx, `SELECT `+parcelColumns+` FROM parcels.parcels WHERE reference = $1 FOR UPDATE NOWAIT`, ref))
	if sqlState(err) == "55P03" {
		return nil, errLocked
	}
	return p, err
}

// List returns parcels (of one sender, unless sender is empty), oldest first.
func (s *store) List(ctx context.Context, sender string) ([]*Parcel, error) {
	q := `SELECT ` + parcelColumns + ` FROM parcels.parcels`
	var args []any
	if sender != "" {
		q += ` WHERE sender = $1`
		args = append(args, sender)
	}
	q += ` ORDER BY created_at, reference`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Parcel{}
	for rows.Next() {
		p, err := scanParcel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
