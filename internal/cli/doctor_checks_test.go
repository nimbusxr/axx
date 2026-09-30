package cli

import (
	"context"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
)

// probePack is a pack with a check of the machine, as mobile-android checks
// the Android SDK.
type probePack struct{}

func (probePack) Manifest() core.Manifest {
	return core.Manifest{Name: "doctor-probe", Namespace: "doctor-probe", Checks: []core.Check{{
		Name: "Probe tool",
		Run: func(context.Context) core.CheckResult {
			return core.CheckResult{Status: core.CheckWarn, Detail: "the probe tool is not installed", Hint: "install the probe tool"}
		},
	}}}
}

// doctor runs the checks of the packs a project uses, and of no others.
func TestDoctorRunsThePacksChecks(t *testing.T) {
	engine.Register("./doctor-probe", probePack{})
	_, _ = agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs:\n  - ./doctor-probe\n"})
	c, ok := doctorChecks(t)["Probe tool"]
	if !ok || c.Status != "warn" || c.Detail != "the probe tool is not installed" || c.Hint != "install the probe tool" {
		t.Errorf("the pack's check: %+v (%v)", c, ok)
	}

	_, _ = agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs:\n  - rest\n"})
	if c, ok := doctorChecks(t)["Probe tool"]; ok {
		t.Errorf("a check of a pack the project does not use: %+v", c)
	}
}
