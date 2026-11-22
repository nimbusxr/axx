//go:build integration

package mobileios

// TEMP, to be removed: the sign-in, again and again, to catch the tap on "Sign in" that the app
// takes no notice of on CI's Macs, with the simulator's log of it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/courierapi"
)

func zzBooted(t *testing.T) string {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "booted", "-j").Output()
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Devices map[string][]struct{ UDID, Name string } `json:"devices"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		t.Fatal(err)
	}
	for _, ds := range listed.Devices {
		for _, d := range ds {
			return d.UDID
		}
	}
	return ""
}

func zzLog(udid, last string) []byte {
	out, _ := exec.Command("xcrun", "simctl", "spawn", udid, "log", "show", "--last", last, "--debug", "--info", "--style", "compact", "--predicate",
		`process == "Courier" OR (process == "testmanagerd" AND eventMessage BEGINSWITH "Synthesizing event")`+
			` OR (process == "SpringBoard" AND (subsystem BEGINSWITH "com.apple.KeyboardArbiter" OR category == "KeyboardArbiter" OR subsystem == "com.apple.BackBoard"))`+
			` OR (process == "backboardd" AND (category CONTAINS[c] "touch" OR category CONTAINS[c] "event"))`).CombinedOutput()
	return out
}

func TestZZSignInLoop(t *testing.T) {
	device := simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	budget := 4*time.Minute + 30*time.Second
	if v := os.Getenv("AXX_TEMP_LOOP"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			budget = d
		}
	}
	kept := os.Getenv("AXX_TEST_FAILURES")
	if kept == "" {
		kept = t.TempDir()
	}
	_ = os.MkdirAll(kept, 0o755)
	part := os.Getenv("AXX_TEMP_PART")
	debug := os.Getenv("AXX_TEMP_DEBUG_LOG") == "1"
	var udid string
	tries, failed, second, passKept := 0, 0, 0, false
	for start := time.Now(); time.Since(start) < budget; {
		tries++
		i := tries
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			h := harnessWith(t, nil)
			warm(t, h, device)
			if udid == "" {
				udid = zzBooted(t)
				if debug {
					for _, subsystem := range []string{"com.apple.UIKit", "com.apple.SwiftUI"} {
						out, err := exec.Command("xcrun", "simctl", "spawn", udid, "log", "config", "--mode", "level:debug,persist:debug", "--subsystem", subsystem).CombinedOutput()
						t.Logf("debug log of %s on %s: %v %s", subsystem, udid, err, out)
					}
				}
			}
			h.OK("the courier ios app with the following properties:", [][]string{{"app", courierBuild(t)}, {"device", device}})
			signIn(h)
			tapped := time.Now()
			err := h.Step(`the courier app's dialog is accepted`)
			if err == nil {
				if !passKept {
					passKept = true
					_ = os.WriteFile(filepath.Join(kept, fmt.Sprintf("signin-%s-%d-pass.log", part, i)), zzLog(udid, "25s"), 0o644)
				}
				_ = h.End("passed")
				return
			}
			failed++
			since := time.Since(tapped).Round(time.Second)
			_ = os.WriteFile(filepath.Join(kept, fmt.Sprintf("signin-%s-%d-fail.log", part, i)), zzLog(udid, "40s"), 0o644)
			_ = mobilecore.OnDevice(h.SC, "courier", func(_ context.Context, d mobilecore.Device) error {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if src, err := d.Session().Source(ctx); err == nil {
					_ = os.WriteFile(filepath.Join(kept, fmt.Sprintf("signin-%s-%d-fail.xml", part, i)), []byte(src), 0o644)
				}
				if png, err := d.Session().Screenshot(ctx); err == nil {
					_ = os.WriteFile(filepath.Join(kept, fmt.Sprintf("signin-%s-%d-fail.png", part, i)), png, 0o644)
				}
				return nil
			})
			// A second tap, as a person makes whose tap did nothing: is that one taken?
			again := h.Step(`the "Sign in" button is tapped in the courier app`)
			if again == nil {
				again = h.Step(`the courier app's dialog is accepted`)
			}
			if again == nil {
				second++
			}
			_ = os.WriteFile(filepath.Join(kept, fmt.Sprintf("signin-%s-%d-second.log", part, i)), zzLog(udid, "30s"), 0o644)
			t.Logf("ZZ the tap on Sign in was not taken (try %d, %s after it: %v); a second tap: %v", i, since, err, again)
			_ = h.End("failed")
		})
	}
	t.Logf("ZZ RESULT part=%s debug=%v tries=%d not-taken=%d second-tap-taken=%d", part, debug, tries, failed, second)
	if failed > 0 {
		t.Errorf("ZZ %d of %d taps on Sign in were not taken", failed, tries)
	}
}
