package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Claim statuses.
const (
	AwaitingEvidence = "AWAITING_EVIDENCE"
	Approved         = "APPROVED"
	Referred         = "REFERRED"
	Rejected         = "REJECTED"
	Paid             = "PAID"
	RefundFailed     = "REFUND_FAILED"
)

// Claim reasons.
const (
	Damaged = "DAMAGED"
	Lost    = "LOST"
)

// Parcel is an insured parcel.
type Parcel struct {
	Reference     string  `dynamodbav:"reference" json:"reference"`
	Shop          string  `dynamodbav:"shop" json:"shop"`
	Carrier       string  `dynamodbav:"carrier" json:"carrier"`
	DeclaredValue float64 `dynamodbav:"declaredValue" json:"declaredValue"`
	Currency      string  `dynamodbav:"currency" json:"currency"`
	DeliveredAt   string  `dynamodbav:"deliveredAt,omitempty" json:"deliveredAt,omitempty"`
}

// Claim is a shop's claim for a parcel.
type Claim struct {
	ID          string  `dynamodbav:"id" json:"id"`
	Parcel      string  `dynamodbav:"parcel" json:"parcel"`
	Reason      string  `dynamodbav:"reason" json:"reason"`
	Description string  `dynamodbav:"description,omitempty" json:"description,omitempty"`
	Status      string  `dynamodbav:"status" json:"status"`
	Amount      float64 `dynamodbav:"amount" json:"amount"`
	Currency    string  `dynamodbav:"currency" json:"currency"`
	Note        string  `dynamodbav:"note,omitempty" json:"note,omitempty"`
	Source      string  `dynamodbav:"source" json:"source"`
	FiledAt     string  `dynamodbav:"filedAt" json:"filedAt"`
	DecidedAt   string  `dynamodbav:"decidedAt,omitempty" json:"decidedAt,omitempty"`
	PaidAt      string  `dynamodbav:"paidAt,omitempty" json:"paidAt,omitempty"`
}

// claimID is the claim for a parcel: one claim per parcel, CLM-4101 for
// PX-4101.
func claimID(parcel string) string { return "CLM-" + strings.TrimPrefix(parcel, "PX-") }

type service struct {
	aws              clients
	names            names
	queues           map[string]string // queue URLs by name
	log              *slog.Logger
	autoApproveLimit float64
	now              func() time.Time
}

var (
	errNotInsured = errors.New("not insured")
	errExists     = errors.New("already claimed")
)

// file files a shop's claim. A damaged parcel waits for the evidence
// photo; a lost one is decided at once: rejected when the parcel was
// delivered, approved otherwise.
func (s *service) file(ctx context.Context, parcel, reason, description string) (Claim, error) {
	p, ok, err := s.parcel(ctx, parcel)
	if err != nil {
		return Claim{}, err
	}
	if !ok {
		return Claim{}, errNotInsured
	}
	c := Claim{
		ID: claimID(parcel), Parcel: parcel, Reason: reason, Description: description,
		Status: AwaitingEvidence, Currency: p.Currency, Source: "SHOP", FiledAt: s.stamp(),
	}
	if err := s.createClaim(ctx, c); err != nil {
		return Claim{}, err
	}
	s.log.Info("claim filed", "claim", c.ID, "parcel", parcel, "reason", reason)
	if err := s.putEvent(ctx, s.names.ClaimsBus, "Claim Filed", map[string]any{
		"claim": c.ID, "parcel": parcel, "reason": reason, "shop": p.Shop,
	}); err != nil {
		return Claim{}, err
	}
	if reason == Lost {
		if p.DeliveredAt != "" {
			return s.reject(ctx, c, "delivered on "+p.DeliveredAt)
		}
		return s.approve(ctx, c, p)
	}
	return c, nil
}

// assess decides a damaged parcel's claim once its evidence arrived: within
// the auto-approval limit it is approved, above it referred to a person,
// with the evidence copied for the reviewer.
func (s *service) assess(ctx context.Context, bucket, key string) error {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[0] != "claims" {
		s.log.Warn("evidence ignored: not claims/<claim>/<file>", "key", key)
		return nil
	}
	c, ok, err := s.claim(ctx, parts[1])
	if err != nil {
		return err
	}
	if !ok || c.Status != AwaitingEvidence {
		s.log.Info("evidence ignored: no claim awaits it", "key", key)
		return nil
	}
	p, _, err := s.parcel(ctx, c.Parcel)
	if err != nil {
		return err
	}
	s.log.Info("evidence received", "claim", c.ID, "key", key)
	if p.DeclaredValue <= s.autoApproveLimit {
		_, err := s.approve(ctx, c, p)
		return err
	}
	if err := s.copyObject(ctx, bucket, key, s.names.Reviews, c.ID+"/"+parts[2]); err != nil {
		return err
	}
	c.Status, c.Note, c.DecidedAt = Referred, fmt.Sprintf("declared value %.2f above the %.2f auto-approval limit", p.DeclaredValue, s.autoApproveLimit), s.stamp()
	if err := s.saveClaim(ctx, c); err != nil {
		return err
	}
	s.log.Info("claim referred", "claim", c.ID, "declaredValue", p.DeclaredValue)
	return s.announce(ctx, c)
}

// carrierReport opens an approved claim when a carrier reports damage to an
// insured parcel: the carrier admitted it, no evidence is needed.
func (s *service) carrierReport(ctx context.Context, parcel, carrier, note string) error {
	p, ok, err := s.parcel(ctx, parcel)
	if err != nil || !ok {
		return err
	}
	c, exists, err := s.claim(ctx, claimID(parcel))
	if err != nil {
		return err
	}
	if exists && c.Status != AwaitingEvidence {
		s.log.Info("carrier report ignored: the claim is decided", "claim", c.ID)
		return nil
	}
	if !exists {
		c = Claim{
			ID: claimID(parcel), Parcel: parcel, Reason: Damaged, Description: note,
			Currency: p.Currency, Source: "CARRIER:" + carrier, FiledAt: s.stamp(),
		}
	}
	s.log.Info("carrier reported damage", "claim", c.ID, "carrier", carrier)
	_, err = s.approve(ctx, c, p)
	return err
}

// approve settles a claim for the parcel's declared value: a letter for the
// shop, a refund request for payments, and the decision.
func (s *service) approve(ctx context.Context, c Claim, p Parcel) (Claim, error) {
	c.Status, c.Amount, c.Currency, c.DecidedAt = Approved, p.DeclaredValue, p.Currency, s.stamp()
	if err := s.saveClaim(ctx, c); err != nil {
		return c, err
	}
	letter := map[string]any{
		"claim": c.ID, "parcel": c.Parcel, "shop": p.Shop, "decision": Approved,
		"amount": c.Amount, "currency": c.Currency, "reason": c.Reason,
	}
	if err := s.putJSON(ctx, s.names.Letters, c.ID+".json", letter); err != nil {
		return c, err
	}
	if err := s.send(ctx, s.names.RefundRequests, map[string]any{
		"claim": c.ID, "shop": p.Shop, "amount": c.Amount, "currency": c.Currency,
	}, map[string]string{"reason": c.Reason}); err != nil {
		return c, err
	}
	s.log.Info("claim approved", "claim", c.ID, "amount", c.Amount)
	return c, s.announce(ctx, c)
}

func (s *service) reject(ctx context.Context, c Claim, note string) (Claim, error) {
	c.Status, c.Note, c.DecidedAt = Rejected, note, s.stamp()
	if err := s.saveClaim(ctx, c); err != nil {
		return c, err
	}
	s.log.Info("claim rejected", "claim", c.ID, "note", note)
	return c, s.announce(ctx, c)
}

// announce publishes a decision to the shops and to the claims bus.
func (s *service) announce(ctx context.Context, c Claim) error {
	decision := map[string]any{"claim": c.ID, "parcel": c.Parcel, "decision": c.Status, "amount": c.Amount, "currency": c.Currency}
	if c.Note != "" {
		decision["note"] = c.Note
	}
	if err := s.publish(ctx, s.names.Decisions, decision, map[string]string{"eventType": "ClaimDecided"}); err != nil {
		return err
	}
	return s.putEvent(ctx, s.names.ClaimsBus, "Claim Decided", decision)
}

// refundResult records what payments did with a refund request: paid, or
// failed with a reason.
func (s *service) refundResult(ctx context.Context, claim, status, paidAt, failure string) error {
	c, ok, err := s.claim(ctx, claim)
	if err != nil || !ok || c.Status != Approved {
		return err
	}
	switch status {
	case "PAID":
		c.Status, c.PaidAt = Paid, paidAt
		s.log.Info("claim paid", "claim", c.ID)
	case "FAILED":
		c.Status, c.Note = RefundFailed, failure
		s.log.Warn("refund failed", "claim", c.ID, "reason", failure)
	default:
		return nil
	}
	return s.saveClaim(ctx, c)
}

func (s *service) stamp() string { return s.now().UTC().Format(time.RFC3339) }

// ---- HTTP ----

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})
	mux.HandleFunc("POST /api/claims", s.fileClaim)
	mux.HandleFunc("GET /api/claims/{id}", s.getClaim)
	return mux
}

func (s *service) fileClaim(w http.ResponseWriter, r *http.Request) {
	var in struct{ Parcel, Reason, Description string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Parcel == "" {
		problem(w, http.StatusBadRequest, "a claim needs a parcel")
		return
	}
	if in.Reason != Damaged && in.Reason != Lost {
		problem(w, http.StatusBadRequest, "the reason is DAMAGED or LOST")
		return
	}
	c, err := s.file(r.Context(), in.Parcel, in.Reason, in.Description)
	switch {
	case errors.Is(err, errNotInsured):
		problem(w, http.StatusUnprocessableEntity, "parcel "+in.Parcel+" is not insured")
	case errors.Is(err, errExists):
		problem(w, http.StatusConflict, "parcel "+in.Parcel+" is already claimed")
	case err != nil:
		s.log.Error("filing failed", "parcel", in.Parcel, "error", err)
		problem(w, http.StatusServiceUnavailable, "try again later")
	default:
		writeJSON(w, http.StatusCreated, c)
	}
}

func (s *service) getClaim(w http.ResponseWriter, r *http.Request) {
	c, ok, err := s.claim(r.Context(), r.PathValue("id"))
	switch {
	case err != nil:
		problem(w, http.StatusServiceUnavailable, "try again later")
	case !ok:
		problem(w, http.StatusNotFound, "no claim "+r.PathValue("id"))
	default:
		writeJSON(w, http.StatusOK, c)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "title": http.StatusText(status), "detail": detail})
}
