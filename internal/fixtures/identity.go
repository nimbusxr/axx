package fixtures

import (
	"crypto/sha1" //nolint:gosec // RFC 4122 version 5 UUIDs are defined over SHA-1
	"encoding/hex"
	"strings"
)

// Identity declares a field whose values must be unique across every
// fixture of every factory.
type Identity struct {
	// Path is dotted with optional indices, e.g. payments[0].id.
	Path string
	// Prefix is prepended to derived values.
	Prefix string
	// Derive is "fixture-key" (derived, the default) or "authored".
	Derive string
	// Qualifier disambiguates several identities of one class in a fixture.
	Qualifier string
	// Format is "literal" (the default) or "uuid-name-based".
	Format string
}

// namespace is the fixed RFC 4122 namespace of uuid-name-based identities
// (ace5fac7-0000-5000-8000-ace5fac70000): same name, same value, forever.
var namespace = [16]byte{0xac, 0xe5, 0xfa, 0xc7, 0x00, 0x00, 0x50, 0x00, 0x80, 0x00, 0xac, 0xe5, 0xfa, 0xc7, 0x00, 0x00}

// identities guarantees identity uniqueness across a module.
type identities struct {
	claims map[string]string // value -> owner description
}

func newIdentities() *identities { return &identities{claims: map[string]string{}} }

// derive computes the value for a fixture: prefix + key [+ "-" + qualifier],
// or its name-based UUID.
func (r *identities) derive(id Identity, fixtureKey string) string {
	name := id.Prefix + fixtureKey
	if strings.TrimSpace(id.Qualifier) != "" {
		name += "-" + id.Qualifier
	}
	if id.Format == "uuid-name-based" {
		return nameBasedUUID(name)
	}
	return name
}

// claim registers a value, failing with both owners on a collision.
func (r *identities) claim(value, factory, fixtureKey, path string) error {
	owner := factory + " -> fixtures." + fixtureKey + " (" + path + ")"
	if prev, ok := r.claims[value]; ok {
		if prev != owner {
			return genError("identity collision: value \"%s\" is claimed by both\n  %s\n  %s\nEvery fixture must own unique identity values.", value, prev, owner)
		}
		return nil
	}
	r.claims[value] = owner
	return nil
}

// nameBasedUUID is an RFC 4122 version 5 UUID in the factory namespace.
func nameBasedUUID(name string) string {
	h := sha1.New() //nolint:gosec // see import
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)
	sum[6] = sum[6]&0x0f | 0x50
	sum[8] = sum[8]&0x3f | 0x80
	s := hex.EncodeToString(sum[:16])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
