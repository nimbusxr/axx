package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// The shops' own systems read their parcels with a bearer token: a JSON Web
// Token signed with HS256 and the shops' token key, whose shop claim names
// the shop. A shop's system gets one from the token endpoint with its client
// credentials, or its platform signs one:
//
//	POST /oauth/token                 client credentials (a form: grant_type,
//	                                  client_id, client_secret); the client
//	                                  ID is the shop's name. The token lasts
//	                                  an hour.
//	GET  /api/shops/{shop}/parcels    the shop's parcels, oldest first: 401
//	                                  without a valid token, 403 with another
//	                                  shop's

// shopTokenLife is how long a token the service issues lasts.
const shopTokenLife = time.Hour

func (s *service) issueToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	shop, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	if want, ok := s.shopClients[shop]; !ok || !hmac.Equal([]byte(secret), []byte(want)) {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	now := time.Now()
	token := signJWT(s.shopTokenKey, map[string]any{"shop": shop, "iat": now.Unix(), "exp": now.Add(shopTokenLife).Unix()})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(shopTokenLife.Seconds())})
}

func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// shopParcels lists a shop's parcels for the shop's own system.
func (s *service) shopParcels(w http.ResponseWriter, r *http.Request) {
	shop := r.PathValue("shop")
	claims, err := verifyJWT(s.shopTokenKey, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		problem(w, r, http.StatusUnauthorized, "a shop's token is needed: "+err.Error())
		return
	}
	if claims["shop"] != shop {
		problem(w, r, http.StatusForbidden, "the token is not for "+shop)
		return
	}
	ps, err := s.store.List(r.Context(), shop)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type row struct {
		Reference    string `json:"reference"`
		Status       string `json:"status"`
		ServiceLevel string `json:"serviceLevel"`
		WeightGrams  int    `json:"weightGrams"`
	}
	list := struct {
		Shop    string `json:"shop"`
		Parcels []row  `json:"parcels"`
	}{Shop: shop, Parcels: []row{}}
	for _, p := range ps {
		list.Parcels = append(list.Parcels, row{p.Reference, p.Status, p.ServiceLevel, p.WeightGrams})
	}
	writeJSON(w, http.StatusOK, list)
}

func signJWT(key []byte, claims map[string]any) string {
	h, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	c, _ := json.Marshal(claims)
	signed := b64url(h) + "." + b64url(c)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signed))
	return signed + "." + b64url(mac.Sum(nil))
}

// verifyJWT checks an HS256 token's signature and expiry, and returns its
// claims.
func verifyJWT(key []byte, token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("no bearer token")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if h, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil || json.Unmarshal(h, &header) != nil || header.Alg != "HS256" {
		return nil, errors.New("the token is not signed with HS256")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal([]byte(parts[2]), []byte(b64url(mac.Sum(nil)))) {
		return nil, errors.New("the token's signature is not the service's")
	}
	var claims map[string]any
	if c, err := base64.RawURLEncoding.DecodeString(parts[1]); err != nil || json.Unmarshal(c, &claims) != nil {
		return nil, errors.New("the token's claims are not JSON")
	}
	if exp, ok := claims["exp"].(float64); !ok || time.Now().Unix() >= int64(exp) {
		return nil, errors.New("the token has expired")
	}
	return claims, nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
