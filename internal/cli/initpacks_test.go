package cli

import (
	"strings"
	"testing"
)

// axx init suggests the packs a repository points to, with what points to
// each, and leaves out those it uses already.
func TestInitSuggestsPacks(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"compose.yaml":             "services:\n  db:\n    image: postgres:16-alpine\n  cache:\n    image: valkey/valkey:8\n",
		"app/go.mod":               "module x\n\nrequire github.com/twmb/franz-go v1.22.0\n",
		"schemas/event.avsc":       "{}",
		"README.md":                "Stores parcels in MongoDB.",
		"node_modules/x/README.md": "Kafka MQTT NATS",
	})
	got := map[string]string{}
	for _, s := range suggestPacks(dir, []string{"rest", "redis"}) {
		got[s.Pack] = strings.Join(s.Reasons, "; ")
	}
	for pack, want := range map[string]string{
		"sql":   "compose.yaml runs a database image",
		"kafka": "app/go.mod uses a Kafka client; schemas/event.avsc is an Avro schema",
		"mongo": "README.md mentions MongoDB",
	} {
		if !strings.Contains(got[pack], want) {
			t.Errorf("%s: %q, want %q", pack, got[pack], want)
		}
	}
	if _, ok := got["redis"]; ok {
		t.Error("redis is used already")
	}
	if _, ok := got["mqtt"]; ok {
		t.Error("node_modules was read")
	}
}
