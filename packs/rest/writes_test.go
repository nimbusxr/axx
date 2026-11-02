package rest

import "testing"

func TestWriteHint(t *testing.T) {
	body := []byte(`{"recipient": {"postcode": 10115, "name": "Ada"}, "weightGrams": "1200", "lines": [{"fragile": "true"}], "express": true}`)
	for _, c := range []struct{ key, msg, want string }{
		{"validation.request.body.schema.type", "$.recipient.postcode: got number, want string (POST request body for '/api/parcels' failed to validate schema)", `"10115"`},
		{"validation.request.body.schema.type", "$.weightGrams: got string, want integer (POST request body)", "1200"},
		{"validation.request.body.schema.type", "$.lines[0].fragile: got string, want boolean", "true"},
		{"validation.request.body.schema.type", "$.express: got boolean, want string", `"true"`},
		{"validation.request.body.schema.type", "$.recipient.name: got string, want integer", ""},
		{"validation.response.body.schema.type", "$.recipient.postcode: got number, want string", ""},
		{"validation.request.body.schema.type", "$.missing: got number, want string", ""},
	} {
		if got := writeHint(c.key, c.msg, body); got != c.want {
			t.Errorf("%s: %q, want %q", c.msg, got, c.want)
		}
	}
}
