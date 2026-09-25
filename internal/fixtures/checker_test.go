package fixtures

import (
	"strings"
	"testing"
)

// Checker behavior: green on generated state, precise findings for every
// deviation class.

func checkedWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := avroWorkspace(t)
	w.generate()
	return w
}

func TestCheckCleanGeneratedStatePasses(t *testing.T) {
	w := checkedWorkspace(t)
	if f := w.check(); len(f) != 0 {
		t.Fatalf("failures: %v", f)
	}
	// 3 fixtures + 1 lint rules drift checks + 1 manifest consistency.
	if n := w.checks(); n != 5 {
		t.Errorf("checks: %d", n)
	}
}

func TestCheckDriftNamesFileFirstDifferenceAndFix(t *testing.T) {
	w := checkedWorkspace(t)
	w.replace(orderPayments+"/order-authorized.json", `"POS"`, `"WEB"`)
	f := w.check()
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "order-authorized.json", "FIXTURE DRIFT", "WEB", "POS", "axx fixtures generate")
}

func TestCheckMissingManagedFile(t *testing.T) {
	w := checkedWorkspace(t)
	w.remove(orderPayments + "/order-captured.json")
	f := w.check()
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "order-captured.json", "missing")
}

func TestCheckFactoryEditWithoutRegenerationIsDrift(t *testing.T) {
	w := checkedWorkspace(t)
	w.replace(orderPayments+"/order-payments.prototype.yaml", "amount_cents: 12999", "amount_cents: 15000")
	f := w.check()
	if len(f) != 3 { // every fixture inherits the prototype value
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "12999", "15000")
}

func TestCheckOrphanedManifestEntry(t *testing.T) {
	w := checkedWorkspace(t)
	w.write(orderPayments+"/order-captured-renamed.fixture.yaml", w.read(orderPayments+"/order-captured.fixture.yaml"))
	w.remove(orderPayments + "/order-captured.fixture.yaml")
	found := false
	for _, f := range w.check() {
		found = found || strings.Contains(f, "orphan") && strings.Contains(f, "order-captured.json")
	}
	if !found {
		t.Errorf("no orphan finding: %v", w.check())
	}
}

func TestCheckExpansionFailureIsOneFailure(t *testing.T) {
	w := checkedWorkspace(t)
	w.replace(orderPayments+"/order-payments.prototype.yaml", `status: "AUTHORIZED"`, `status: "PENDING"`)
	f := w.check()
	if len(f) != 1 || !strings.HasPrefix(f[0], "expansion") {
		t.Fatalf("failures: %v", f)
	}
}

func TestFirstDifference(t *testing.T) {
	tests := []struct{ committed, expected, want string }{
		{"a\nb\n", "a\nc\n", "line 2:\n  --- committed: b\n  +++ expected:  c\n"},
		{"a\n", "a\nb\n", "line 2:\n  --- committed: \n  +++ expected:  b\n"},
		{"a", "a\n", "line 2:\n  --- committed: <end of file>\n  +++ expected:  \n"},
		{"a\r\n", "a\n", "line 1:\n  --- committed: a\n  +++ expected:  a\n"},
	}
	for _, tc := range tests {
		if got := firstDifference([]byte(tc.committed), []byte(tc.expected)); !strings.Contains(got, tc.want) {
			t.Errorf("firstDifference(%q, %q) = %q", tc.committed, tc.expected, got)
		}
	}
}
