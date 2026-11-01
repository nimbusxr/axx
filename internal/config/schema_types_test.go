package config

import (
	"slices"
	"testing"
)

// A scalar where axx.yaml takes an object names the object's keys, which an
// agent would otherwise read the whole schema for.
func TestTypeErrorNamesTheObjectsKeys(t *testing.T) {
	keys := keysAt([]string{"services", "parcels", "ready", "http"})
	if len(keys) == 0 || keys[0] != "url" {
		t.Fatalf("keys of ready.http: %v", keys)
	}
	if got := keysAt([]string{"services", "parcels", "ready"}); !slices.Contains(got, "http") || !slices.Contains(got, "timeout") {
		t.Errorf("keys of ready: %v", got)
	}
	if got := keysAt([]string{"nothing", "here"}); len(got) != 0 {
		t.Errorf("keys of an unknown path: %v", got)
	}
}
