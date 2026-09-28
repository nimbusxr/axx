package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// courierClient calls the courier company that collects express parcels
// and picks up the parcels shops plan (its contract is
// ../infra/openapi/courier.yaml):
//
//	POST /v1/collections   a JSON booking: the parcel's reference, weight and
//	                       where it goes, never who it goes to; the query has
//	                       the slot and a request ID of its own, which the
//	                       courier drops retried bookings by
//	DELETE /v1/collections the query has the reference of a parcel whose
//	                       collection is off: the shop cancelled it
//	POST /v1/pickups       a form: the parcel's reference and the day
type courierClient struct {
	base string
	http *http.Client
}

type collection struct {
	Reference   string      `json:"reference"`
	WeightGrams int         `json:"weightGrams"`
	DeliverTo   destination `json:"deliverTo"`
}

type destination struct {
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
}

// book books the collection of an express parcel.
func (c *courierClient) book(ctx context.Context, p *Parcel) error {
	body, err := json.Marshal(collection{
		Reference: p.Reference, WeightGrams: p.WeightGrams,
		DeliverTo: destination{Postcode: p.Recipient.Postcode, Country: p.Recipient.Country},
	})
	if err != nil {
		return err
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	q := url.Values{"slot": {"same-day"}, "requestId": {hex.EncodeToString(id)}}
	return c.post(ctx, "/v1/collections?"+q.Encode(), "application/json", body)
}

// cancel calls off the collection of an express parcel the shop cancelled.
func (c *courierClient) cancel(ctx context.Context, reference string) error {
	return c.send(ctx, http.MethodDelete, "/v1/collections?"+url.Values{"reference": {reference}}.Encode(), "", nil)
}

// pickup tells the courier the day a shop has a parcel picked up.
func (c *courierClient) pickup(ctx context.Context, reference, day string) error {
	form := url.Values{"reference": {reference}, "day": {day}}
	return c.post(ctx, "/v1/pickups", "application/x-www-form-urlencoded", []byte(form.Encode()))
}

func (c *courierClient) post(ctx context.Context, path, contentType string, body []byte) error {
	return c.send(ctx, http.MethodPost, path, contentType, body)
}

func (c *courierClient) send(ctx context.Context, method, path, contentType string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		answer, _ := io.ReadAll(io.LimitReader(res.Body, 1<<10))
		return fmt.Errorf("courier answered %d: %s", res.StatusCode, strings.TrimSpace(string(answer)))
	}
	return nil
}
