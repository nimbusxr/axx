//go:build integration

package redis

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

const cacheSeed = `
"quote:DE-1:EXPRESS:1200":
  value: {"priceCents": 1490, "currency": "EUR", "zone": "DE-1"}
  ttl: 10m
"label-printer:LEJ-3":
  value: online
"shop:hawthorn-home":
  hash: {name: Hawthorn Home, tier: gold, parcels: 42}
"depot:LEJ:sorting":
  list: [PX-RDS-9601, PX-RDS-9602]
"parcels:express":
  set: [PX-RDS-9612, PX-RDS-9611]
"couriers:deliveries":
  sorted set: {CR-LEJ-12: 42, CR-LEJ-7: 17.5}
"tracking:PX-RDS-9603":
  value: {"status": "IN_TRANSIT"}
  ttl: 1s
`

func run(t *testing.T, image string, cmd []string, url string) {
	t.Helper()
	addr := cloudtest.Server(t, image, "6379", cmd, nil)
	server := [][]string{{"url", strings.Replace(url, "HOST", addr, 1)}}
	h := cloudtest.New(t, Pack())
	h.File("seeds/cache.yaml", cacheSeed)
	h.OK("the cache redis server with the following properties:", server)
	h.OK("a seeds/cache.yaml redis seed")

	h.OK("the label-printer:LEJ-3 redis key has the value 'online'")
	h.OK("the quote:DE-1:EXPRESS:1200 redis key has the following properties:", [][]string{{"priceCents", "1490"}, {"zone", "DE-1"}, {"surcharge", "undefined"}})
	h.OK("the shop:hawthorn-home redis key has the following properties:", [][]string{{"tier", "gold"}, {"parcels", "42"}})
	h.OK("the depot:LEJ:sorting redis key has the following properties:", [][]string{{"[0]", "PX-RDS-9601"}, {"[1]", "PX-RDS-9602"}})
	h.OK("the parcels:express redis key has the following properties:", [][]string{{"[0]", "PX-RDS-9611"}})
	h.OK("the couriers:deliveries redis key has the following properties:", [][]string{{"CR-LEJ-7", "17.5"}})
	h.OK("within 5s the tracking:PX-RDS-9603 redis key does not exist")
	h.OK("the quote:DE-2:EXPRESS:1200 redis key does not exist")

	_ = h.Fails("within 1s the label-printer:LEJ-3 redis key has the value 'offline'", `has the value "online", not "offline"`)
	_ = h.Fails("within 1s the shop:hawthorn-home redis key has the value 'gold'", "The shop:hawthorn-home redis key is a hash, not a string")
	_ = h.Fails("within 1s the shop:hawthorn-home redis key has the following properties:", `does not have those properties: {"name":"Hawthorn Home"`,
		[][]string{{"tier", "silver"}})
	_ = h.Fails("within 1s the label-printer:LEJ-3 redis key does not exist", "The label-printer:LEJ-3 redis key still exists")
	_ = h.Fails("within 1s the quote:FR-1:EXPRESS:1200 redis key has the value '990'", "The quote:FR-1:EXPRESS:1200 redis key does not exist")

	// Seeding again replaces the keys.
	h.File("seeds/printer.yaml", "\"label-printer:LEJ-3\":\n  hash: {state: jammed}\n")
	h.OK("a seeds/printer.yaml redis seed")
	h.OK("the label-printer:LEJ-3 redis key has the following properties:", [][]string{{"state", "jammed"}})
	h.File("seeds/bad.yaml", "\"label-printer:LEJ-4\":\n  value: online\n  list: [a]\n")
	_ = h.Fails("a seeds/bad.yaml redis seed", "a key is one of value, hash, list, set, sorted set, not both")
}

func TestValkey(t *testing.T) {
	run(t, "valkey/valkey:9.1.2-alpine", nil, "redis://HOST/0")
}

// Redis itself, with a password the failures never show.
func TestRedis(t *testing.T) {
	run(t, "redis:8.8.3-alpine", []string{"redis-server", "--requirepass", "cache-pass-cache"}, "redis://:cache-pass-cache@HOST/2")
	h := cloudtest.New(t, Pack())
	err := h.Fails("the cache redis server with the following properties:", "the redis server's url is",
		[][]string{{"url", "redis://:cache-pass-cache@HOST:x/0"}})
	if strings.Contains(err.Error(), "cache-pass-cache") {
		t.Errorf("the password shows: %v", err)
	}
}
