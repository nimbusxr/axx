package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// Shipping labels carry a barcode "PX" + the 10-digit label number + one
// Luhn (mod 10) check digit over those 10 digits, and a signature: the
// lowercase hex HMAC-SHA256 of "<reference>|<barcode>" with the label
// signing key, which depot scanners verify before accepting a parcel.
type labeler struct{ secret []byte }

type shippingLabel struct {
	Reference    string `json:"reference"`
	ServiceLevel string `json:"serviceLevel"`
	Barcode      string `json:"barcode"`
	Signature    string `json:"signature"`
}

func (l labeler) label(p *Parcel) shippingLabel {
	digits := fmt.Sprintf("%010d", p.LabelNumber%10_000_000_000)
	barcode := "PX" + digits + strconv.Itoa(luhnCheckDigit(digits))
	mac := hmac.New(sha256.New, l.secret)
	mac.Write([]byte(p.Reference + "|" + barcode))
	return shippingLabel{Reference: p.Reference, ServiceLevel: p.ServiceLevel, Barcode: barcode, Signature: hex.EncodeToString(mac.Sum(nil))}
}

// luhnCheckDigit returns the digit that makes digits+check pass the Luhn test.
func luhnCheckDigit(digits string) int {
	sum, double := 0, true
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return (10 - sum%10) % 10
}
