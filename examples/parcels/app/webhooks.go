package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The courier calls the service back as it collects and delivers parcels,
// and the service tells each shop of its parcels' deliveries:
//
//	POST /api/courier/callbacks    the courier's status callbacks:
//	                               {"reference", "status", "location", "at"},
//	                               signed in X-Courier-Signature:
//	                               sha256=<hex HMAC-SHA256 of the body>
//	POST <shop webhooks>/<shop>    a Standard Webhook (standardwebhooks.com)
//	                               for each delivered parcel:
//	                               {"type": "parcel.delivered", "reference",
//	                               "location", "deliveredAt"}
//
// A delivery is told to the shop before the service answers the callback,
// and before it tells the other services (see couriers.go).

// courierCallback records a status the courier reports, when the courier
// signed it.
func (s *service) courierCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		problem(w, r, http.StatusBadRequest, "the body could not be read")
		return
	}
	if !s.courierSigned(r.Header.Get("X-Courier-Signature"), body) {
		problem(w, r, http.StatusUnauthorized, "the X-Courier-Signature is not the courier's signature of the body")
		return
	}
	var cb struct {
		Reference string    `json:"reference"`
		Status    string    `json:"status"`
		Location  string    `json:"location"`
		At        time.Time `json:"at"`
	}
	if err := json.Unmarshal(body, &cb); err != nil || cb.Reference == "" || cb.Status == "" {
		problem(w, r, http.StatusBadRequest, "a callback has a reference and a status")
		return
	}
	if _, err := s.store.Get(r.Context(), cb.Reference); errors.Is(err, errNotFound) {
		problem(w, r, http.StatusNotFound, "no parcel "+cb.Reference)
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	if cb.At.IsZero() {
		cb.At = time.Now()
	}
	sc := scan{ScanID: "CB-" + cb.Reference + "-" + cb.Status, Status: cb.Status, Location: cb.Location, ScannedAt: cb.At.UTC()}
	if err := s.rec.record(r.Context(), cb.Reference, sc, "courier", nil); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) courierSigned(header string, body []byte) bool {
	got, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	mac := hmac.New(sha256.New, s.courierKey)
	mac.Write(body)
	return hmac.Equal([]byte(got), []byte(hex.EncodeToString(mac.Sum(nil))))
}

// shopWebhooks tells shops of their parcels' deliveries.
type shopWebhooks struct {
	base string
	key  []byte // the Standard Webhooks key, decoded
	http *http.Client
	log  *slog.Logger
}

func newShopWebhooks(base, key string, log *slog.Logger) (*shopWebhooks, error) {
	k, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(key, "whsec_"))
	if err != nil {
		return nil, fmt.Errorf("PARCELS_SHOP_WEBHOOK_KEY is whsec_ and the key in base64: %w", err)
	}
	return &shopWebhooks{base: strings.TrimRight(base, "/"), key: k, http: &http.Client{Timeout: 5 * time.Second}, log: log}, nil
}

// delivered tells a parcel's shop that it was delivered.
func (w *shopWebhooks) delivered(ctx context.Context, p *Parcel, s scan) error {
	body, err := json.Marshal(map[string]any{
		"type": "parcel.delivered", "reference": p.Reference, "location": s.Location, "deliveredAt": s.ScannedAt,
	})
	if err != nil {
		return err
	}
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	id, ts := "msg_"+hex.EncodeToString(raw[:]), strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/"+p.Sender, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", ts)
	req.Header.Set("webhook-signature", standardSignature(w.key, id, ts, body))
	res, err := w.http.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("the shop's webhook answered %d", res.StatusCode)
	}
	return nil
}

// standardSignature is a Standard Webhook's signature: "v1," and the base64
// HMAC-SHA256 of "<id>.<timestamp>.<body>".
func standardSignature(key []byte, id, ts string, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
