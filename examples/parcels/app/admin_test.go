package main

import (
	"bytes"
	"strings"
	"testing"
)

// Wrong use exits 2 with the usage, before reaching for the database.
func TestAdminUsage(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		input string
		says  string
	}{
		{nil, "", "say what to do"},
		{[]string{"reprint", "PX-1"}, "", `unknown verb "reprint"`},
		{[]string{"label"}, "", "label takes one parcel reference"},
		{[]string{"label", "PX-1", "PX-2"}, "", "label takes one parcel reference"},
		{[]string{"cancel"}, "", "cancel takes parcel references"},
		{[]string{"cancel", "-"}, "\n  \n", "cancel takes parcel references"},
		{[]string{"parcels"}, "", "parcels takes --shop <sender>"},
		{[]string{"parcels", "--shop"}, "", "parcels takes --shop <sender>"},
		{[]string{"parcels", "--shop", "maple-crafts", "extra"}, "", "parcels takes --shop <sender>"},
	} {
		var out, errOut bytes.Buffer
		if code := admin(tc.args, strings.NewReader(tc.input), &out, &errOut); code != 2 {
			t.Errorf("%q: exit code %d", tc.args, code)
		}
		if !strings.Contains(errOut.String(), tc.says) || !strings.Contains(errOut.String(), "usage: parcels admin") {
			t.Errorf("%q: %s", tc.args, errOut.String())
		}
		if out.Len() > 0 {
			t.Errorf("%q printed %q", tc.args, out.String())
		}
	}
}
