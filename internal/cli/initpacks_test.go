package cli

import (
	"fmt"
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

// A manifest's text counts for what it depends on, not for a name that
// starts like a client's or a dependency of a dependency.
func TestInitSuggestsPacksFromWhatManifestsUse(t *testing.T) {
	for _, c := range []struct {
		file, text, pack string
		want             bool
	}{
		{"requirements.txt", "flask==3.0\nredis==5.0.1\n", "redis", true},
		{"requirements.txt", "flask==3.0\nredis\n", "redis", true},
		{"requirements.txt", "redis[hiredis]>=5\n", "redis", true},
		{"pyproject.toml", "dependencies = [\n  \"redis>=5\",\n]\n", "redis", true},
		{"requirements.txt", "hiredis==2.3\nredisearch==2.0\n", "redis", false},
		{"requirements.txt", "pika==1.3.2\n", "amqp", true},
		{"package.json", `{"dependencies": {"pikaday": "^1.8.2"}}`, "amqp", false},
		{"requirements.txt", "pika-stubs==0.1\n", "amqp", false},
		{"requirements.txt", "motor==3.5\n", "mongo", true},
		{"pyproject.toml", "[tool.poetry.dependencies]\nmotor = \"^3.5\"\n", "mongo", true},
		{"package.json", `{"dependencies": {"motorcycle-parts": "1.0.0"}}`, "mongo", false},
		{"go.mod", "module x\n\nrequire github.com/acme/motor v1.0.0\n", "mongo", false},
		{"go.mod", "module x\n\nrequire github.com/lib/pq v1.10.9\n", "sql", true},
		{"go.mod", "module x\n\nrequire (\n\tgithub.com/lib/pq v1.10.9 // indirect\n)\n", "sql", false},
		{"go.mod", "module x\n\nrequire (\n\tgithub.com/lib/pq v1.10.9 // indirect\r\n\tgithub.com/twmb/franz-go v1.22.0\n)\n", "kafka", true},
	} {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{c.file: c.text})
		got := false
		for _, s := range suggestPacks(dir, nil) {
			got = got || s.Pack == c.pack
		}
		if got != c.want {
			t.Errorf("%s with %q: suggested %s: %v, want %v", c.file, c.text, c.pack, got, c.want)
		}
	}
}

// The files suggestPacks may read run out only after the top of the
// repository, so a deep folder of many files leaves it what the top says.
func TestInitSuggestsPacksFromTheTopFirst(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"compose.yaml":     "services:\n  db:\n    image: postgres:16\n",
		"web/compose.yaml": "services:\n  cache:\n    image: redis:7\n",
	}
	for i := range maxSuggestFiles + 100 {
		files[fmt.Sprintf("api/x/f%04d.txt", i)] = ""
	}
	writeFiles(t, dir, files)
	got := map[string]bool{}
	for _, s := range suggestPacks(dir, nil) {
		got[s.Pack] = true
	}
	if !got["sql"] || !got["redis"] {
		t.Errorf("suggested %v, want sql (the top level) and redis (one folder down)", got)
	}
}
