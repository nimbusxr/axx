package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	ratingv1 "github.com/nimbusxr/axx/examples/parcels/app/gen/rating/v1"
)

// ratingClient asks the partner carrier's rating service, over gRPC, what
// it adds to the price of a parcel going beyond the EU, and how long it
// takes (../infra/rating mocks it).
type ratingClient struct {
	conn  *grpc.ClientConn
	rates ratingv1.RatesClient
}

func openRating(addr string) (*ratingClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("the rating service at %s: %w", addr, err)
	}
	return &ratingClient{conn: conn, rates: ratingv1.NewRatesClient(conn)}, nil
}

func (c *ratingClient) Close() { _ = c.conn.Close() }

// noCarrierError is a country the partner carrier does not deliver to.
type noCarrierError struct{ country, reason string }

func (e *noCarrierError) Error() string { return "no delivery to " + e.country + ": " + e.reason }

// ratingUnavailableError is a rating service that did not answer.
type ratingUnavailableError struct{ err error }

func (e *ratingUnavailableError) Error() string {
	return "rating service unavailable: " + e.err.Error()
}

func (c *ratingClient) rate(ctx context.Context, country string, weightGrams int, level string) (*ratingv1.Rate, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := c.rates.Quote(ctx, &ratingv1.QuoteRequest{
		Country:      country,
		WeightGrams:  int32(min(weightGrams, maxWeightGrams)), //nolint:gosec // at most maxWeightGrams
		ServiceLevel: ratingv1.ServiceLevel(ratingv1.ServiceLevel_value[level]),
	})
	switch {
	case status.Code(err) == codes.NotFound:
		return nil, &noCarrierError{country: country, reason: status.Convert(err).Message()}
	case err != nil:
		return nil, &ratingUnavailableError{err: err}
	}
	return r, nil
}

// euCountries are where the price list's international rate applies.
var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "CY": true, "CZ": true, "DK": true, "EE": true, "ES": true, "FI": true,
	"FR": true, "GR": true, "HR": true, "HU": true, "IE": true, "IT": true, "LT": true, "LU": true, "LV": true,
	"MT": true, "NL": true, "PL": true, "PT": true, "RO": true, "SE": true, "SI": true, "SK": true,
}

// priceFor prices a parcel: from the price list within the EU, and with
// the partner carrier's rate beyond it.
func (s *service) priceFor(ctx context.Context, zone, country, level string, weightGrams int) (quote, error) {
	if country == homeCountry || euCountries[country] {
		return priceQuote(zone, country, level, weightGrams), nil
	}
	r, err := s.rating.rate(ctx, country, weightGrams, level)
	if err != nil {
		return quote{}, err
	}
	return priceAbroad(zone, level, weightGrams, int(r.GetSurchargeCents()), int(r.GetDeliveryDays())), nil
}

// ratingMessage is what a quote says of a rating error, "" for none.
func ratingMessage(err error) (string, bool) {
	var nc *noCarrierError
	var down *ratingUnavailableError
	switch {
	case errors.As(err, &nc):
		return "We do not deliver to " + nc.country + ".", true
	case errors.As(err, &down):
		return "Parcels abroad cannot be priced right now; try again later.", false
	}
	return "", false
}
