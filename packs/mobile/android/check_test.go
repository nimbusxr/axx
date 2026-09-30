package mobileandroid

import (
	"context"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// doctor says what is missing of the SDK, and what installs it.
func TestSDKCheck(t *testing.T) {
	t.Setenv("ANDROID_HOME", t.TempDir())
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("PATH", t.TempDir())
	r := checkSDK(context.Background())
	if r.Status != core.CheckFail || !strings.Contains(r.Detail, "Android SDK is not found") || !strings.Contains(r.Hint, "ANDROID_HOME") {
		t.Errorf("%+v", r)
	}
}
