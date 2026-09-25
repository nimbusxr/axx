package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
)

//go:embed openapi.yaml
var openapiYAML []byte

const maxWeightGrams = 30000

var (
	referencePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,39}$`)
	countryPattern   = regexp.MustCompile(`^[A-Z]{2}$`)
	serviceLevels    = map[string]bool{"STANDARD": true, "EXPRESS": true}
)

type service struct {
	store    *store
	tracking *trackingStore
	address  *addressClient
	events   *events
	labels   labeler
	log      *slog.Logger
}

// registration is a parcel to register, from the API or a manifest line.
type registration struct {
	Reference    string
	Sender       string
	WeightGrams  int
	ServiceLevel string
	Recipient    Recipient
	Source       string // api | manifest
	ManifestID   string
	LineID       string
}

type undeliverableError struct{ reason string }

func (e *undeliverableError) Error() string { return "address not deliverable: " + e.reason }

type upstreamError struct{ err error }

func (e *upstreamError) Error() string { return "address service unavailable: " + e.err.Error() }

var errNotChangeable = errors.New("not changeable")

// register runs the rules shared by the API and the manifest importer:
// duplicate check, address check, storage and the ParcelRegistered event.
func (s *service) register(ctx context.Context, r registration) (*Parcel, error) {
	if existing, err := s.store.Get(ctx, r.Reference); err == nil {
		return existing, errDuplicate
	} else if !errors.Is(err, errNotFound) {
		return nil, err
	}
	zone, err := s.zone(ctx, r.Recipient.Country, r.Recipient.Postcode)
	if err != nil {
		return nil, err
	}
	details := map[string]any{"source": r.Source, "zone": zone}
	if r.Source == "manifest" {
		details["manifestId"] = r.ManifestID
		details["lineId"] = r.LineID
	}
	p, err := s.store.Insert(ctx, &Parcel{
		Reference: r.Reference, Sender: r.Sender, WeightGrams: r.WeightGrams, ServiceLevel: r.ServiceLevel, Recipient: r.Recipient,
	}, details)
	if err != nil {
		return nil, err
	}
	ev := parcelRegistered{
		Reference: p.Reference, Sender: p.Sender, WeightGrams: p.WeightGrams, ServiceLevel: p.ServiceLevel,
		Zone: zone, Source: r.Source, RegisteredAt: p.CreatedAt,
	}
	if err := s.events.publishRegistered(ctx, ev); err != nil {
		s.log.Error("publishing ParcelRegistered failed", "reference", p.Reference, "err", err)
	}
	return p, nil
}

// zone asks the address service where a postcode is delivered from.
func (s *service) zone(ctx context.Context, country, postcode string) (string, error) {
	v, err := s.address.check(ctx, country, postcode)
	if err != nil {
		return "", &upstreamError{err}
	}
	if !v.Deliverable {
		reason := v.Reason
		if reason == "" {
			reason = "rejected by the address service"
		}
		return "", &undeliverableError{reason}
	}
	return v.Zone, nil
}

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
	})
	openapiJSON := sync.OnceValues(func() ([]byte, error) { return yaml.YAMLToJSON(openapiYAML) })
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		b, err := openapiJSON()
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("POST /api/quotes", s.quote)
	mux.HandleFunc("POST /api/parcels", s.create)
	mux.HandleFunc("GET /api/parcels", s.list)
	mux.HandleFunc("GET /api/parcels/{reference}", s.get)
	mux.HandleFunc("PATCH /api/parcels/{reference}", s.update)
	mux.HandleFunc("DELETE /api/parcels/{reference}", s.remove)
	mux.HandleFunc("GET /api/parcels/{reference}/tracking", s.trackingView)
	mux.HandleFunc("GET /api/parcels/{reference}/label", s.label)
	return s.logRequests(mux)
}

func (s *service) logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		h.ServeHTTP(rw, r)
		if r.URL.Path != "/health" {
			s.log.Info("request", "method", r.Method, "path", r.URL.RequestURI(), "status", rw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *service) quote(w http.ResponseWriter, r *http.Request) {
	body, err := decodeObject(r)
	if err != nil {
		problem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	var errs []string
	level := "STANDARD"
	weight, errs := weightOf(body, true, errs)
	if lvl, ok := body["serviceLevel"]; ok {
		level, errs = serviceLevel(lvl, errs)
	}
	var country, postcode string
	if m, ok := body["recipient"].(map[string]any); ok {
		var e []string
		postcode, e = str(m, "postcode", true, nil)
		errs = append(errs, inRecipient(e)...)
		country, e = str(m, "country", true, nil)
		errs = append(errs, inRecipient(e)...)
	} else {
		errs = append(errs, "recipient is required")
	}
	if len(errs) > 0 {
		problem(w, r, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	zone, err := s.zone(r.Context(), country, postcode)
	if !s.addressFailed(w, r, err) {
		writeJSON(w, http.StatusOK, priceQuote(zone, country, level, weight))
	}
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := decodeObject(r)
	if err != nil {
		problem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	reg := registration{Source: "api", ServiceLevel: "STANDARD"}
	var errs []string
	reg.Reference, errs = str(body, "reference", true, errs)
	if reg.Reference != "" && !referencePattern.MatchString(reg.Reference) {
		errs = append(errs, "reference must match "+referencePattern.String())
	}
	reg.Sender, errs = str(body, "sender", true, errs)
	if _, ok := body["sender"]; ok && strings.TrimSpace(reg.Sender) == "" {
		errs = append(errs, "sender must not be empty")
	}
	reg.WeightGrams, errs = weightOf(body, true, errs)
	if lvl, ok := body["serviceLevel"]; ok {
		reg.ServiceLevel, errs = serviceLevel(lvl, errs)
	}
	if rec, ok := body["recipient"]; ok {
		reg.Recipient, errs = recipient(rec, errs)
	} else {
		errs = append(errs, "recipient is required")
	}
	if len(errs) > 0 {
		problem(w, r, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	p, err := s.register(r.Context(), reg)
	switch {
	case errors.Is(err, errDuplicate):
		s.refused(reg.Reference, "already registered")
		problem(w, r, http.StatusConflict, fmt.Sprintf("parcel %s is already registered", reg.Reference))
	case errors.Is(err, errUnavailable):
		s.refused(reg.Reference, "the database keeps failing")
		w.Header().Set("Retry-After", "5")
		problem(w, r, http.StatusServiceUnavailable, "the parcel could not be stored; try again later")
	case s.addressFailed(w, r, err):
		var und *undeliverableError
		if errors.As(err, &und) {
			s.refused(reg.Reference, und.Error())
		} else {
			s.refused(reg.Reference, "the address service is unavailable")
		}
	case err != nil:
		s.fail(w, r, err)
	default:
		w.Header().Set("Location", "/api/parcels/"+p.Reference)
		writeJSON(w, http.StatusCreated, p)
	}
}

// refused logs a registration the service turned down: it is not stored
// and not announced.
func (s *service) refused(reference, reason string) {
	s.log.Info("registration refused; not announced", "reference", reference, "reason", reason)
}

// addressFailed answers the address check's errors (422, 502) and reports
// whether it did.
func (s *service) addressFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	var und *undeliverableError
	var up *upstreamError
	switch {
	case errors.As(err, &und):
		problem(w, r, http.StatusUnprocessableEntity, und.Error())
	case errors.As(err, &up):
		s.log.Warn("address check failed", "err", up.err)
		problem(w, r, http.StatusBadGateway, "the address service is unavailable; try again later")
	default:
		return false
	}
	return true
}

func (s *service) get(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Get(r.Context(), r.PathValue("reference"))
	if s.notFound(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *service) list(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.List(r.Context(), r.URL.Query().Get("sender"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *service) update(w http.ResponseWriter, r *http.Request) {
	body, err := decodeObject(r)
	if err != nil {
		problem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	var errs []string
	for k := range body {
		if k != "weightGrams" && k != "serviceLevel" && k != "recipient" {
			errs = append(errs, k+" cannot be changed")
		}
	}
	if len(errs) > 0 {
		problem(w, r, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	out, err := s.store.Change(r.Context(), r.PathValue("reference"), func(p *Parcel) error {
		if _, ok := body["weightGrams"]; ok {
			p.WeightGrams, errs = weightOf(body, false, errs)
		}
		if lvl, ok := body["serviceLevel"]; ok {
			p.ServiceLevel, errs = serviceLevel(lvl, errs)
		}
		if rec, ok := body["recipient"]; ok {
			p.Recipient, errs = recipient(rec, errs)
		}
		if len(errs) > 0 {
			return errInvalid
		}
		return changeable(p)
	})
	if errors.Is(err, errInvalid) {
		problem(w, r, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	if s.notFound(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *service) remove(w http.ResponseWriter, r *http.Request) {
	err := s.store.Cancel(r.Context(), r.PathValue("reference"), changeable)
	if s.notFound(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errInvalid = errors.New("invalid change")

// changeable allows changes until the parcel is picked up.
func changeable(p *Parcel) error {
	if p.Status != "REGISTERED" {
		return fmt.Errorf("%w: parcel %s is %s", errNotChangeable, p.Reference, p.Status)
	}
	return nil
}

func (s *service) trackingView(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("reference")
	t, err := s.tracking.Get(r.Context(), ref)
	if errors.Is(err, errNotFound) {
		if _, perr := s.store.Get(r.Context(), ref); perr == nil {
			writeJSON(w, http.StatusOK, trackingView{ParcelRef: ref, Status: "REGISTERED"})
			return
		}
		problem(w, r, http.StatusNotFound, fmt.Sprintf("no tracking for parcel %s", ref))
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *service) label(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Get(r.Context(), r.PathValue("reference"))
	if s.notFound(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, s.labels.label(p))
}

// notFound answers the store's errors and reports whether it did.
func (s *service) notFound(w http.ResponseWriter, r *http.Request, err error) bool {
	ref := r.PathValue("reference")
	switch {
	case err == nil:
		return false
	case errors.Is(err, errNotFound):
		problem(w, r, http.StatusNotFound, fmt.Sprintf("parcel %s not found", ref))
	case errors.Is(err, errLocked):
		problem(w, r, http.StatusConflict, fmt.Sprintf("parcel %s is being dispatched and cannot be changed now", ref))
	case errors.Is(err, errNotChangeable):
		problem(w, r, http.StatusConflict, fmt.Sprintf("parcel %s can no longer be changed", ref))
	default:
		s.fail(w, r, err)
	}
	return true
}

func (s *service) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "path", r.URL.Path, "err", err)
	problem(w, r, http.StatusInternalServerError, "internal error")
}

func decodeObject(r *http.Request) (map[string]any, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(http.MaxBytesReader(nil, r.Body, 1<<20)); err != nil {
		return nil, fmt.Errorf("cannot read the request body: %w", err)
	}
	dec := json.NewDecoder(&buf)
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil || body == nil {
		return nil, errors.New("the request body is not a JSON object")
	}
	return body, nil
}

func str(body map[string]any, key string, required bool, errs []string) (string, []string) {
	v, ok := body[key]
	if !ok || v == nil {
		if required {
			errs = append(errs, key+" is required")
		}
		return "", errs
	}
	s, ok := v.(string)
	if !ok {
		return "", append(errs, key+" must be a string")
	}
	return s, errs
}

func weightOf(body map[string]any, required bool, errs []string) (int, []string) {
	v, ok := body["weightGrams"]
	if !ok || v == nil {
		if required {
			errs = append(errs, "weightGrams is required")
		}
		return 0, errs
	}
	n, ok := v.(json.Number)
	if !ok {
		return 0, append(errs, "weightGrams must be an integer")
	}
	w, err := n.Int64()
	switch {
	case err != nil:
		return 0, append(errs, "weightGrams must be an integer")
	case w < 1:
		return 0, append(errs, "weightGrams must be at least 1")
	case w > maxWeightGrams:
		return 0, append(errs, fmt.Sprintf("weightGrams must be at most %d", maxWeightGrams))
	}
	return int(w), errs
}

func serviceLevel(v any, errs []string) (string, []string) {
	lvl, ok := v.(string)
	if !ok {
		return "", append(errs, "serviceLevel must be a string")
	}
	if !serviceLevels[lvl] {
		return "", append(errs, "serviceLevel must be one of STANDARD, EXPRESS")
	}
	return lvl, errs
}

func recipient(v any, errs []string) (Recipient, []string) {
	m, ok := v.(map[string]any)
	if !ok {
		return Recipient{}, append(errs, "recipient must be an object")
	}
	var rec Recipient
	var e []string
	rec.Name, e = str(m, "name", true, nil)
	errs = append(errs, inRecipient(e)...)
	rec.Street, e = str(m, "street", false, nil)
	errs = append(errs, inRecipient(e)...)
	rec.City, e = str(m, "city", false, nil)
	errs = append(errs, inRecipient(e)...)
	rec.Postcode, e = str(m, "postcode", true, nil)
	errs = append(errs, inRecipient(e)...)
	rec.Country, e = str(m, "country", true, nil)
	errs = append(errs, inRecipient(e)...)
	if rec.Country != "" && !countryPattern.MatchString(rec.Country) {
		errs = append(errs, "recipient.country must be an ISO 3166 alpha-2 code")
	}
	return rec, errs
}

// inRecipient prefixes the recipient's field errors.
func inRecipient(errs []string) []string {
	for i := range errs {
		errs[i] = "recipient." + errs[i]
	}
	return errs
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// problem answers with RFC 9457 problem details.
func problem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail, "instance": r.URL.Path,
	})
}
