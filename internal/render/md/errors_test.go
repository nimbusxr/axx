package md

import (
	"strings"
	"testing"
)

func TestEveryExitCodeHasItsMeaning(t *testing.T) {
	codes := exitCodes()
	if len(codes) < 8 {
		t.Fatalf("exit codes = %v, want every one exitcode names", codes)
	}
	for _, c := range codes {
		if exitMeaning[c] == "" {
			t.Errorf("exit code %d (%s) has no meaning on the reference page", int(c), c)
		}
	}
}

func TestTheErrorCodesPageListsExitCodeSix(t *testing.T) {
	page, err := ErrorCodesPage(false)
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(page, "| 6 | Every scenario passed, but the coverage"), strings.Index(page, "| 130 |")
	if i < 0 || j < i {
		t.Errorf("the page has no exit code 6 before 130:\n%s", page[:min(len(page), 1200)])
	}
}
