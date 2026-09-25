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

// schema is applied on every start (idempotent). The tables live in the
// "parcels" schema so `parcels reset` can drop everything at once.
const schema = `
CREATE SCHEMA IF NOT EXISTS parcels;
CREATE SEQUENCE IF NOT EXISTS parcels.label_numbers START 100000001;
CREATE TABLE IF NOT EXISTS parcels.parcels (
  reference     text PRIMARY KEY,
  sender        text NOT NULL,
  status        text NOT NULL DEFAULT 'REGISTERED',
  weight_grams  integer NOT NULL,
  service_level text NOT NULL DEFAULT 'STANDARD',
  recipient     jsonb NOT NULL,
  details       jsonb NOT NULL DEFAULT '{}'::jsonb,
  label_number  bigint NOT NULL DEFAULT nextval('parcels.label_numbers'),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS parcels_sender_idx ON parcels.parcels (sender, created_at);
CREATE TABLE IF NOT EXISTS parcels.manifest_lines (
  id               text PRIMARY KEY,
  manifest_id      text NOT NULL,
  reference        text NOT NULL,
  sender           text NOT NULL,
  weight_grams     integer NOT NULL,
  service_level    text NOT NULL DEFAULT 'STANDARD',
  recipient        jsonb NOT NULL,
  status           text NOT NULL DEFAULT 'PENDING',
  error            text,
  parcel_reference text,
  received_at      timestamptz NOT NULL DEFAULT now(),
  processed_at     timestamptz
);
CREATE INDEX IF NOT EXISTS manifest_lines_status_idx ON parcels.manifest_lines (status, received_at);
`

// Recipient is where a parcel goes.
type Recipient struct {
	Name     string `json:"name"`
	Street   string `json:"street,omitempty"`
	City     string `json:"city,omitempty"`
	Postcode string `json:"postcode,omitempty"`
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
	Zone         *string         `json:"zone"`
	Source       string          `json:"source"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Details      json.RawMessage `json:"-"`
	LabelNumber  int64           `json:"-"`
}

type store struct {
	pool *pgxpool.Pool
}

var (
	errNotFound  = errors.New("not found")
	errDuplicate = errors.New("duplicate reference")
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
	s := &store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *store) Close() { s.pool.Close() }

func (s *store) migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize concurrent starts (two instances migrating at once).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4242)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, schema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func resetStore(ctx context.Context, url string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	_, err = conn.Exec(ctx, `DROP SCHEMA IF EXISTS parcels CASCADE`)
	return err
}

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
		Zone   *string `json:"zone"`
		Source string  `json:"source"`
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

// Insert stores a new parcel; errDuplicate when the reference exists.
func (s *store) Insert(ctx context.Context, p *Parcel, recipient any, details map[string]any) (*Parcel, error) {
	rj, err := json.Marshal(recipient)
	if err != nil {
		return nil, err
	}
	dj, err := json.Marshal(details)
	if err != nil {
		return nil, err
	}
	out, err := scanParcel(s.pool.QueryRow(ctx, `
INSERT INTO parcels.parcels (reference, sender, status, weight_grams, service_level, recipient, details)
VALUES ($1, $2, 'REGISTERED', $3, $4, $5, $6)
RETURNING `+parcelColumns, p.Reference, p.Sender, p.WeightGrams, p.ServiceLevel, rj, dj))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, errDuplicate
	}
	return out, err
}

// Update replaces the mutable fields of a parcel.
func (s *store) Update(ctx context.Context, p *Parcel) (*Parcel, error) {
	rj, err := json.Marshal(p.Recipient)
	if err != nil {
		return nil, err
	}
	return scanParcel(s.pool.QueryRow(ctx, `
UPDATE parcels.parcels SET weight_grams = $2, service_level = $3, recipient = $4, updated_at = now()
WHERE reference = $1
RETURNING `+parcelColumns, p.Reference, p.WeightGrams, p.ServiceLevel, rj))
}

func (s *store) Delete(ctx context.Context, ref string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM parcels.parcels WHERE reference = $1`, ref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
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

func retry(ctx context.Context, log *slog.Logger, what string, f func() error) error {
	delay := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		err := f()
		if err == nil {
			return nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Info("waiting for "+what, "err", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w (last error: %w)", what, ctx.Err(), err)
		case <-time.After(delay):
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}
