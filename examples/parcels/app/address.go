package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// addressClient calls the downstream address service (its contract is
// ../infra/openapi/address-service.yaml):
//
//	GET /v1/postcodes/{country}/{postcode}
//	X-Api-Key: <key>
//
// It answers {"deliverable": true, "zone": "DE-1"} or
// {"deliverable": false, "reason": "..."}.
type addressClient struct {
	base   string
	apiKey string
	http   *http.Client
}

type addressVerdict struct {
	Deliverable bool   `json:"deliverable"`
	Zone        string `json:"zone"`
	Reason      string `json:"reason"`
}

func (c *addressClient) check(ctx context.Context, country, postcode string) (*addressVerdict, error) {
	u := c.base + "/v1/postcodes/" + url.PathEscape(country) + "/" + url.PathEscape(postcode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusNotFound {
		return &addressVerdict{Deliverable: false, Reason: "unknown postcode"}, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("address service answered %d: %s", res.StatusCode, string(body))
	}
	var v addressVerdict
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("address service: invalid JSON: %w", err)
	}
	return &v, nil
}
