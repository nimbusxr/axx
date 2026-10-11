package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The couriers' app (../courier) is the handheld a courier delivers with:
// the courier signs in with their courier ID and PIN, sees the parcels they
// deliver today, and confirms each delivery, which the service records as it
// records the confirmations handhelds publish over NATS (couriers.go).
//
//	POST /api/couriers/sign-in                          {"courier", "pin"}: a token
//	                                                    (a JSON Web Token signed with
//	                                                    the couriers' key, for a
//	                                                    working day) and the
//	                                                    courier's name
//	GET  /api/couriers/{courier}/deliveries             the parcels out for delivery
//	                                                    with the courier and not
//	                                                    delivered yet, oldest first
//	POST /api/couriers/{courier}/deliveries/{reference} {"signedBy", "location"}: the
//	                                                    parcel is delivered
//	POST /api/couriers/{courier}/location               {"latitude", "longitude", "speed",
//	                                                    "course"}: where the courier's van
//	                                                    is, which the tracking streams of
//	                                                    the parcels on board tell
//
// The deliveries need the courier's token: 401 without one, 403 with another
// courier's. A parcel is the courier's when its details name the courier.

// courierTokenLife is how long a courier's sign-in lasts: a working day.
const courierTokenLife = 12 * time.Hour

// courierAccount is a courier who can sign in to the app.
type courierAccount struct {
	name string
	pin  string
}

func (s *service) courierRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/couriers/sign-in", s.courierSignIn)
	mux.HandleFunc("GET /api/couriers/{courier}/deliveries", s.courierDeliveries)
	mux.HandleFunc("POST /api/couriers/{courier}/deliveries/{reference}", s.courierDelivered)
	mux.HandleFunc("POST /api/couriers/{courier}/location", s.courierLocation)
}

// vanPosition is where a courier's van is, as the courier's app last reported it: rounded to
// about 11 metres, its speed to a km/h and its heading to a degree.
type vanPosition struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	SpeedKmh  int       `json:"speedKmh"`
	Heading   int       `json:"heading"`
	At        time.Time `json:"at"`
}

// vans are the couriers' vans where their apps last reported them.
type vans struct {
	mu  sync.Mutex
	pos map[string]vanPosition
}

func (v *vans) set(courier string, p vanPosition) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pos == nil {
		v.pos = map[string]vanPosition{}
	}
	v.pos[courier] = p
}

func (v *vans) get(courier string) (vanPosition, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	p, ok := v.pos[courier]
	return p, ok
}

func (s *service) courierLocation(w http.ResponseWriter, r *http.Request) {
	courier, ok := s.courierOf(w, r)
	if !ok {
		return
	}
	var in struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
		Speed     float64  `json:"speed"`  // metres a second
		Course    float64  `json:"course"` // degrees clockwise from north
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || in.Latitude == nil || in.Longitude == nil ||
		*in.Latitude < -90 || *in.Latitude > 90 || *in.Longitude < -180 || *in.Longitude > 180 || in.Speed < 0 || in.Course < 0 || in.Course >= 360 {
		problem(w, r, http.StatusBadRequest, "a location is a latitude and a longitude, with the van's speed in metres a second and its course in degrees")
		return
	}
	round := func(v float64) float64 { return math.Round(v*1e4) / 1e4 }
	s.vans.set(courier, vanPosition{
		Latitude: round(*in.Latitude), Longitude: round(*in.Longitude),
		SpeedKmh: int(math.Round(in.Speed * 3.6)), Heading: int(math.Round(in.Course)) % 360, At: time.Now().UTC(),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) courierSignIn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Courier string `json:"courier"`
		PIN     string `json:"pin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || in.Courier == "" || in.PIN == "" {
		problem(w, r, http.StatusBadRequest, "signing in takes the courier's ID and PIN")
		return
	}
	acct, ok := s.couriers[in.Courier]
	if !ok || !hmac.Equal([]byte(in.PIN), []byte(acct.pin)) {
		problem(w, r, http.StatusUnauthorized, "the courier ID or the PIN is wrong")
		return
	}
	now := time.Now()
	token := signJWT(s.courierTokenKey, map[string]any{"courier": in.Courier, "iat": now.Unix(), "exp": now.Add(courierTokenLife).Unix()})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "courier": in.Courier, "name": acct.name, "expiresIn": int(courierTokenLife.Seconds())})
}

// courierOf checks the request's token is the courier's: false when it
// answered that it is not.
func (s *service) courierOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	courier := r.PathValue("courier")
	claims, err := verifyJWT(s.courierTokenKey, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		problem(w, r, http.StatusUnauthorized, "a courier's token is needed: "+err.Error())
		return "", false
	}
	if claims["courier"] != courier {
		problem(w, r, http.StatusForbidden, "the token is not "+courier+"'s")
		return "", false
	}
	return courier, true
}

// delivery is a parcel as the courier's app shows it: where it goes, and
// to whom.
type delivery struct {
	Reference    string    `json:"reference"`
	ServiceLevel string    `json:"serviceLevel"`
	Recipient    Recipient `json:"recipient"`
}

func (s *service) courierDeliveries(w http.ResponseWriter, r *http.Request) {
	courier, ok := s.courierOf(w, r)
	if !ok {
		return
	}
	ps, err := s.store.OutForDelivery(r.Context(), courier)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []delivery{}
	for _, p := range ps {
		if s.delivered(r, p.Reference) {
			continue
		}
		out = append(out, delivery{p.Reference, p.ServiceLevel, p.Recipient})
	}
	writeJSON(w, http.StatusOK, map[string]any{"courier": courier, "deliveries": out})
}

// delivered reports a parcel whose delivery the service recorded.
func (s *service) delivered(r *http.Request, ref string) bool {
	v, err := s.tracking.Get(r.Context(), ref)
	return err == nil && v.Delivered
}

func (s *service) courierDelivered(w http.ResponseWriter, r *http.Request) {
	courier, ok := s.courierOf(w, r)
	if !ok {
		return
	}
	var in struct {
		SignedBy string `json:"signedBy"`
		Location string `json:"location"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || strings.TrimSpace(in.SignedBy) == "" {
		problem(w, r, http.StatusBadRequest, "a delivery is signed for: signedBy is who took the parcel")
		return
	}
	ref := r.PathValue("reference")
	p, err := s.store.Get(r.Context(), ref)
	if errors.Is(err, errNotFound) || (err == nil && courierIn(p.Details) != courier) {
		problem(w, r, http.StatusNotFound, "no parcel "+ref+" is out for delivery with "+courier)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p.Status != "OUT_FOR_DELIVERY" || s.delivered(r, ref) {
		problem(w, r, http.StatusConflict, ref+" is not out for delivery")
		return
	}
	location := in.Location
	if location == "" {
		location = p.Recipient.City
	}
	sc := scan{ScanID: "DLV-" + ref, Status: "DELIVERED", Location: location, ScannedAt: time.Now().UTC()}
	if err := s.rec.record(r.Context(), ref, sc, "courier", map[string]string{"courier": courier}); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("delivery confirmed in the couriers' app", "parcel", ref, "courier", courier)
	writeJSON(w, http.StatusOK, map[string]any{"reference": ref, "status": "DELIVERED", "signedBy": in.SignedBy, "location": location})
}

// courierIn is the courier a parcel's details name.
func courierIn(details json.RawMessage) string {
	var d struct {
		Courier string `json:"courier"`
	}
	_ = json.Unmarshal(details, &d)
	return d.Courier
}
