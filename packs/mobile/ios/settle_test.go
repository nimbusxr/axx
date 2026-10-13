package mobileios

import (
	"math"
	"testing"
)

// A simulator's processor time is that of the processes its launchd_sim started, read from ps.
func TestASimulatorsProcessorTimeIsItsProcesses(t *testing.T) {
	ps := `    1     0   9:41.02 /sbin/launchd
70879     1   0:01.50 launchd_sim /Users/someone/Library/Caches/axx/mobile/simulators/0209CDD4-620F-4AF3-B062-80807BA95CD1/data/var/run/launchd_bootstrap.plist
70880 70879   0:31.95 /path/to/RuntimeRoot/usr/libexec/diagnosticd
70881 70879   0:28.50 /path/to/RuntimeRoot/System/Library/CoreServices/SpringBoard.app/SpringBoard
70882 70881   1:00.00 /path/to/a/process/SpringBoard/started
80000     1   0:02.00 launchd_sim /Users/someone/Library/Caches/axx/mobile/simulators/5262C1E9-0E1C-4715-BCF2-DEA68DF427EE/data/var/run/launchd_bootstrap.plist
80001 80000   5:00.00 /path/to/RuntimeRoot/usr/libexec/diagnosticd`
	got, ok := simulatorCPUIn(ps, "0209CDD4-620F-4AF3-B062-80807BA95CD1")
	if !ok || math.Abs(got-60.45) > 0.001 {
		t.Errorf("the simulator's processes took %v (%v), want 60.45 seconds: its launchd_sim's own, not another simulator's", got, ok)
	}
	if _, ok := simulatorCPUIn(ps, "9F3B2D3E-0000-0000-0000-000000000000"); ok {
		t.Error("a simulator that does not run has processes")
	}
}

func TestProcessorTimesAsPsWritesThem(t *testing.T) {
	for in, want := range map[string]float64{
		"0:31.95":    31.95,
		"12:01.23":   721.23,
		"1:02:03":    3723,
		"1-02:03:04": 93784,
		"":           0,
	} {
		if got := cpuSeconds(in); math.Abs(got-want) > 0.001 {
			t.Errorf("cpuSeconds(%q) = %v, want %v", in, got, want)
		}
	}
}
