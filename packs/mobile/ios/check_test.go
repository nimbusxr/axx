package mobileios

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// doctor says what Xcode and runtime a Mac has, and elsewhere that iOS
// simulators need a Mac.
func TestXcodeCheck(t *testing.T) {
	r := checkXcode(context.Background())
	if runtime.GOOS != "darwin" {
		if r.Status != core.CheckWarn || !strings.Contains(r.Detail, "macOS only") {
			t.Errorf("%+v", r)
		}
		return
	}
	if r.Status == core.CheckOK && !strings.HasPrefix(r.Detail, "Xcode ") {
		t.Errorf("%+v", r)
	}
	if r.Status == core.CheckFail && r.Hint == "" {
		t.Errorf("a failure without a hint: %+v", r)
	}
}
