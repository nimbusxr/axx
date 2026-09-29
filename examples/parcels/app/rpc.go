package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openrpcJSON describes the depots' JSON-RPC API; rpc.discover answers it.
//
//go:embed openrpc.json
var openrpcJSON []byte

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// The service's own errors (openrpc.json declares them) and the JSON-RPC
// ones it answers.
const (
	rpcNoParcel      = 4004
	rpcNotHoldable   = 4009
	rpcParseError    = -32700
	rpcInvalidReq    = -32600
	rpcNoMethod      = -32601
	rpcInvalidParams = -32602
	rpcInternal      = -32603
)

// holdable are the statuses of the parcels a depot can still hold.
var holdable = map[string]bool{"REGISTERED": true, "IN_TRANSIT": true, "ON_HOLD": true}

// depotRPC is the depots' JSON-RPC 2.0 API, on POST /rpc: parcel.get,
// parcel.hold, and rpc.discover, which answers the API's OpenRPC document.
func (s *service) depotRPC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      json.RawMessage `json:"id"`
	}
	answer := func(id json.RawMessage, result any, e *rpcError) {
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		out := map[string]any{"jsonrpc": "2.0", "id": id}
		if e != nil {
			out["error"] = e
		} else {
			out["result"] = result
		}
		writeJSON(w, http.StatusOK, out)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(body, &req) != nil {
		answer(nil, nil, &rpcError{Code: rpcParseError, Message: "Parse error"})
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		answer(req.ID, nil, &rpcError{Code: rpcInvalidReq, Message: "Invalid Request"})
		return
	}
	var result any
	var e *rpcError
	switch req.Method {
	case "rpc.discover":
		result = json.RawMessage(openrpcJSON)
	case "parcel.get":
		result, e = s.rpcGet(r, req.Params)
	case "parcel.hold":
		result, e = s.rpcHold(r, req.Params)
	default:
		e = &rpcError{Code: rpcNoMethod, Message: "Method not found: " + req.Method}
	}
	answer(req.ID, result, e)
}

// rpcParcel is a parcel as the depots' API answers it.
type rpcParcel struct {
	Reference    string `json:"reference"`
	Status       string `json:"status"`
	ServiceLevel string `json:"serviceLevel"`
	WeightGrams  int    `json:"weightGrams"`
	HeldUntil    string `json:"heldUntil,omitempty"`
}

func toRPC(p *Parcel, heldUntil *time.Time) rpcParcel {
	out := rpcParcel{Reference: p.Reference, Status: p.Status, ServiceLevel: p.ServiceLevel, WeightGrams: p.WeightGrams}
	if heldUntil != nil {
		out.HeldUntil = heldUntil.Format(time.DateOnly)
	}
	return out
}

// params reads params by name, or by position in the order of names.
func params(raw json.RawMessage, byPosition bool, names ...string) (map[string]string, *rpcError) {
	out := map[string]string{}
	var byName map[string]any
	var list []any
	switch {
	case len(raw) == 0:
	case json.Unmarshal(raw, &byName) == nil:
		for k, v := range byName {
			s, ok := v.(string)
			if !ok {
				return nil, &rpcError{Code: rpcInvalidParams, Message: "Invalid params: " + k + " is text"}
			}
			out[k] = s
		}
	case byPosition && json.Unmarshal(raw, &list) == nil:
		for i, v := range list {
			s, ok := v.(string)
			if !ok || i >= len(names) {
				return nil, &rpcError{Code: rpcInvalidParams, Message: "Invalid params: takes " + strings.Join(names, ", ")}
			}
			out[names[i]] = s
		}
	default:
		return nil, &rpcError{Code: rpcInvalidParams, Message: "Invalid params: by name, in an object"}
	}
	return out, nil
}

func (s *service) rpcGet(r *http.Request, raw json.RawMessage) (any, *rpcError) {
	ps, e := params(raw, true, "reference")
	if e != nil {
		return nil, e
	}
	ref := ps["reference"]
	if ref == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "Invalid params: reference is required"}
	}
	p, err := s.store.Get(r.Context(), ref)
	if errors.Is(err, errNotFound) {
		return nil, &rpcError{Code: rpcNoParcel, Message: "No parcel " + ref}
	}
	if err != nil {
		return nil, s.rpcFailed(err)
	}
	until, err := s.store.HeldUntil(r.Context(), ref)
	if err != nil {
		return nil, s.rpcFailed(err)
	}
	return toRPC(p, until), nil
}

func (s *service) rpcHold(r *http.Request, raw json.RawMessage) (any, *rpcError) {
	ps, e := params(raw, false)
	if e != nil {
		return nil, e
	}
	ref, day := ps["reference"], ps["until"]
	var missing []string
	for _, n := range []string{"reference", "until"} {
		if ps[n] == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "Invalid params: " + strings.Join(missing, " and ") + " required"}
	}
	until, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "Invalid params: until is a day, like 2026-10-02"}
	}
	var refused *rpcError
	p, err := s.store.Hold(r.Context(), ref, until, func(p *Parcel) error {
		if !holdable[p.Status] {
			refused = &rpcError{
				Code: rpcNotHoldable, Message: fmt.Sprintf("%s is %s and can no longer be held", p.Reference, p.Status),
				Data: map[string]string{"status": p.Status},
			}
			return errNotChangeable
		}
		return nil
	})
	switch {
	case refused != nil:
		return nil, refused
	case errors.Is(err, errNotFound):
		return nil, &rpcError{Code: rpcNoParcel, Message: "No parcel " + ref}
	case err != nil:
		return nil, s.rpcFailed(err)
	}
	s.log.Info("parcel held", "reference", ref, "until", day, "reason", ps["reason"])
	return toRPC(p, &until), nil
}

func (s *service) rpcFailed(err error) *rpcError {
	s.log.Error("depot call failed", "err", err)
	return &rpcError{Code: rpcInternal, Message: "Internal error"}
}
