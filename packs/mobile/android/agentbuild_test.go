package mobileandroid

import (
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/internal/memcheck"
)

// A checked build of an agent is under the checker Android has for the device's ABI:
// HWAddressSanitizer on arm64, AddressSanitizer on an x86-64 emulator. The usual build has neither.
func TestAnAgentIsCheckedByItsABIsChecker(t *testing.T) {
	t.Setenv(memcheck.Env, "")
	for _, abi := range []string{"arm64-v8a", "x86_64"} {
		if flags := checked(abi); len(flags) != 0 {
			t.Errorf("the usual build for %s gets %v", abi, flags)
		}
	}
	t.Setenv(memcheck.Env, t.TempDir())
	for abi, want := range map[string]string{"arm64-v8a": "-fsanitize=hwaddress", "x86_64": "-fsanitize=address"} {
		flags := checked(abi)
		if !slices.Contains(flags, want) {
			t.Errorf("a checked build for %s gets %v, want %s", abi, flags, want)
		}
		if other := map[string]string{"-fsanitize=hwaddress": "-fsanitize=address", "-fsanitize=address": "-fsanitize=hwaddress"}[want]; slices.Contains(flags, other) {
			t.Errorf("a checked build for %s gets both checkers: %v", abi, flags)
		}
	}
}
