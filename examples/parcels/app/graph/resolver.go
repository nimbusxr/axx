package graph

import (
	"context"
	"errors"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/nimbusxr/axx/examples/parcels/app/graph/model"
)

// Parcels is what the subgraph reads and changes: the service's parcels,
// which the main package gives it.
type Parcels interface {
	// Parcel is a parcel, nil for one the service does not know.
	Parcel(ctx context.Context, reference string) (*model.Parcel, error)
	// Hold holds a parcel at its depot until a day (2026-10-05).
	Hold(ctx context.Context, reference, until string, reason *string) (*model.Parcel, error)
	// Scans sends the parcel's latest scan, then each new one, and closes
	// once it is delivered or ctx ends.
	Scans(ctx context.Context, reference string) (<-chan *model.Scan, error)
}

// ErrNoParcel is a parcel the service does not know.
var ErrNoParcel = errors.New("no parcel")

// ErrBadDay is a day that is not one.
var ErrBadDay = errors.New("until is a day, like 2026-10-05")

// NotHoldableError is a parcel its depot can no longer hold.
type NotHoldableError struct{ Reference, Status string }

func (e *NotHoldableError) Error() string {
	return e.Reference + " is " + e.Status + " and can no longer be held"
}

// Resolver resolves the subgraph's fields from the service's parcels.
type Resolver struct {
	Parcels Parcels
}

// graphError is how the graph answers a parcels error: with its code in
// the error's extensions.
func graphError(reference string, err error) error {
	var nh *NotHoldableError
	switch {
	case errors.As(err, &nh):
		return &gqlerror.Error{Message: nh.Error(), Extensions: map[string]any{"code": "NOT_HOLDABLE", "status": nh.Status}}
	case errors.Is(err, ErrNoParcel):
		return &gqlerror.Error{Message: "no parcel " + reference, Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, ErrBadDay):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	}
	return err
}
