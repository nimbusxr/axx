package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{Stdout: &out, Stderr: &errb, Env: func(string) string { return "" }}
	code = Run(context.Background(), app, args)
	return out.String(), errb.String(), code
}

func TestVersionJSONEnvelope(t *testing.T) {
	out, _, code := run(t, "version", "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("exit = %d, want 0", code)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if env.SchemaVersion != EnvelopeSchemaVersion || env.Command != "axx version" || !env.OK {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestUsageErrorsExit2(t *testing.T) {
	cases := [][]string{
		{"no-such-command"},
		{"version", "--no-such-flag"},
		{"version", "unexpected-arg"},
	}
	for _, args := range cases {
		_, stderr, code := run(t, args...)
		if code != int(exitcode.Usage) {
			t.Errorf("%v: exit = %d, want %d", args, code, exitcode.Usage)
		}
		if !strings.Contains(stderr, "AXX-E0001") {
			t.Errorf("%v: stderr missing error code: %q", args, stderr)
		}
	}
}

func TestUsageErrorJSON(t *testing.T) {
	out, _, code := run(t, "--json", "no-such-command")
	if code != int(exitcode.Usage) {
		t.Fatalf("exit = %d, want 2", code)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if env.OK || len(env.Errors) != 1 || env.Errors[0].Code != "AXX-E0001" || env.Errors[0].Docs == "" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestHelpMentionsIdentity(t *testing.T) {
	out, _, code := run(t, "--help")
	if code != 0 || !strings.Contains(out, Identity) {
		t.Fatalf("help must carry the identity line (exit %d):\n%s", code, out)
	}
}
