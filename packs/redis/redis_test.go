package redis

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

func TestServerProperties(t *testing.T) {
	h := cloudtest.New(t, Pack())
	for _, tc := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"database", "1"}}, `the redis server property "url" is required`},
		{[][]string{{"url", "http://localhost:6379"}}, "the redis server's url is redis://user:password@host:port/database"},
		{[][]string{{"url", "redis://localhost:6379"}, {"database", "one"}}, `the redis database is a number, not "one"`},
		{[][]string{{"url", "redis://localhost:6379"}, {"prefix", "parcels:"}}, `unknown redis server property "prefix" (supported: url, database)`},
	} {
		_ = h.Fails("the cache redis server with the following properties:", tc.want, tc.rows)
	}
}

func TestAStepNeedsAServer(t *testing.T) {
	h := cloudtest.New(t, Pack())
	_ = h.Fails("the quote:DE-1 redis key has the value '690'", "No Redis server is registered in this scenario")
}
