package main

import "strings"

// homeCountry is where parcels are posted; other countries are international.
const homeCountry = "DE"

type quote struct {
	Zone         string `json:"zone"`
	ServiceLevel string `json:"serviceLevel"`
	WeightGrams  int    `json:"weightGrams"`
	PriceCents   int    `json:"priceCents"`
	Currency     string `json:"currency"`
	DeliveryDays int    `json:"deliveryDays"`
}

// priceQuote is the price list:
//
//	base            STANDARD 4.90, EXPRESS 9.90
//	weight          up to 1 kg +0, 5 kg +2.00, 10 kg +4.00, 30 kg +8.00
//	international   +8.00
//	remote zones    +3.00 (zones ending in -REMOTE)
//
// Delivery takes 2 days (STANDARD) or 1 day (EXPRESS) at home, 5 or 2 days
// abroad, and one day more in a remote zone.
func priceQuote(zone, country, level string, weightGrams int) quote {
	q := quote{Zone: zone, ServiceLevel: level, WeightGrams: weightGrams, Currency: "EUR"}
	home := country == homeCountry
	switch level {
	case "EXPRESS":
		q.PriceCents, q.DeliveryDays = 990, 1
		if !home {
			q.DeliveryDays = 2
		}
	default:
		q.PriceCents, q.DeliveryDays = 490, 2
		if !home {
			q.DeliveryDays = 5
		}
	}
	switch {
	case weightGrams <= 1000:
	case weightGrams <= 5000:
		q.PriceCents += 200
	case weightGrams <= 10000:
		q.PriceCents += 400
	default:
		q.PriceCents += 800
	}
	if !home {
		q.PriceCents += 800
	}
	if strings.HasSuffix(zone, "-REMOTE") {
		q.PriceCents += 300
		q.DeliveryDays++
	}
	return q
}
