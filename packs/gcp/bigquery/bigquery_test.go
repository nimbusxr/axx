package gcpbigquery

import (
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

func TestColumns(t *testing.T) {
	rows := func(keys ...string) cloudstep.Rows {
		var rs cloudstep.Rows
		for _, k := range keys {
			rs = append(rs, core.Pair{Key: k, Value: "x"})
		}
		return rs
	}
	for _, c := range []struct {
		keys []string
		want string
	}{
		{[]string{"invoice", "status"}, "invoice, status"},
		{[]string{"route.to", "route.from", "$.billed"}, "route, billed"},
		{[]string{"from", "to"}, "`from`, `to`"},
		{[]string{"$['route']['to']"}, "route"},
		{[]string{"lines[0].parcel"}, "lines"},
		{[]string{"invoice", "$..parcel"}, "*"},
		{[]string{"weird col"}, "*"},
	} {
		if got := columns(rows(c.keys...)); got != c.want {
			t.Errorf("%v: %s, want %s", c.keys, got, c.want)
		}
	}
}
