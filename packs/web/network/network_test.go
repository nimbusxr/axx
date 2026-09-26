package network

import "testing"

func TestAddressesMatch(t *testing.T) {
	const app = "http://localhost:8400/portal"
	for _, c := range []struct {
		pattern, address string
		want             bool
	}{
		{"/pickups", "http://localhost:8400/portal/pickups", true},
		{"pickups", "http://localhost:8400/portal/pickups", true},
		{"/pickups", "http://localhost:8400/portal/pickups?day=Friday", true},
		{"/pickups", "http://localhost:8400/portal/pickups/today", false},
		{"/pickups", "http://localhost:8400/pickups", false},
		{"/track/*/estimate", "http://localhost:8400/portal/track/PX-4101/estimate", true},
		{"/track/*/estimate", "http://localhost:8400/portal/track/a/b/estimate", false},
		{"/track/**", "http://localhost:8400/portal/track/a/b/estimate", true},
		{"/pickups?day=Friday", "http://localhost:8400/portal/pickups?day=Friday", true},
		{"/pickups?day=Friday", "http://localhost:8400/portal/pickups?day=Monday", false},
		{"/pickups?day=Friday", "http://localhost:8400/portal/pickups", false},
		{"https://maps.example.com/tiles/**", "https://maps.example.com/tiles/12/2200/1343.png", true},
		{"https://maps.example.com/tiles/**", "https://maps.example.com/style.json", false},
		{"https://maps.example.com/*.json", "https://maps.example.com/style.json", true},
		{"/a.b", "http://localhost:8400/portal/aXb", false},
	} {
		matches, err := matcher(app, c.pattern)
		if err != nil {
			t.Fatal(err)
		}
		if got := matches(c.address); got != c.want {
			t.Errorf("%q matches %q: %v, want %v", c.pattern, c.address, got, c.want)
		}
	}
}
