package fixtures

import (
	"crypto/sha1" //nolint:gosec // RFC 4122 version 5 UUIDs are defined over SHA-1
	"encoding/hex"
	"strings"
)

// Identity declares a field whose values must be unique across every
// fixture of every factory: a value never repeats in a field of the same
// name (the path's last field), or of the same namespace when one is given.
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
	// Namespace, when set, is the group of identities whose values are
	// compared, instead of the fields of the same name.
	Namespace string
}

// group is where the identity's values must be unique: its namespace, or
// the field its path ends in (payments[0].id: id).
func (id Identity) group() string {
	if id.Namespace != "" {
		return "namespace " + id.Namespace
	}
	return "field " + fieldName(id.Path)
}

// fieldName is the last field of a dotted path, without indices.
func fieldName(path string) string {
	if dot := strings.LastIndexByte(path, '.'); dot >= 0 {
		path = path[dot+1:]
	}
	if i := strings.IndexByte(path, '['); i >= 0 {
		path = path[:i]
	}
	return path
}

// namespace is the fixed RFC 4122 namespace of uuid-name-based identities
// (ace5fac7-0000-5000-8000-ace5fac70000): same name, same value, forever.
var namespace = [16]byte{0xac, 0xe5, 0xfa, 0xc7, 0x00, 0x00, 0x50, 0x00, 0x80, 0x00, 0xac, 0xe5, 0xfa, 0xc7, 0x00, 0x00}

// identities guarantees identity uniqueness across a module.
type identities struct {
	claims map[string]string // group + value -> owner description
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

// claim registers an identity's value, failing with both owners when the
// value is already claimed in the identity's group.
func (r *identities) claim(id Identity, value, factory, fixtureKey string) error {
	owner := factory + " -> fixtures." + fixtureKey + " (" + id.Path + ")"
	group := id.group()
	if prev, ok := r.claims[group+"\x00"+value]; ok {
		if prev != owner {
			if id.Namespace != "" {
				return genError("identity collision: value \"%s\" in namespace %s is claimed by both\n  %s\n  %s\nIdentities of one namespace own unique values across every factory.", value, id.Namespace, prev, owner)
			}
			field := fieldName(id.Path)
			return genError("identity collision: %s \"%s\" is claimed by both\n  %s\n  %s\nIdentity fields named %s own unique values across every factory. If these identify different things, give the identities different namespaces (namespace: in identity:).", field, value, prev, owner, field)
		}
		return nil
	}
	r.claims[group+"\x00"+value] = owner
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
