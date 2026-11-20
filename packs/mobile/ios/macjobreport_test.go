package mobileios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// CI's Mac jobs end with scripts/mac-job-report.sh, which says what their log does not: the
// reports macOS wrote during the job (of a simulator's process that crashed or was ended, of a
// CoreSimulator service of the Mac's that crashed), oldest first, and what the runner's log says
// of cancelling the job. Here the reports and the runner's log are made up, in the formats macOS
// and GitHub's runner write.
func TestAMacJobsReportTellsItsCrashReportsAndWhyItWasCancelled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the report is of CI's Mac jobs")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("the report reads macOS's reports with jq, which is not installed here")
	}
	dir := t.TempDir()
	began := time.Date(2026, 11, 16, 15, 20, 0, 0, time.UTC)
	write := func(name, content string, at time.Time) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("began", "", began)
	// A report from before the job is left out.
	write("user/Earlier-2026-11-16-150000.ips", `{"app_name":"Earlier","timestamp":"2026-11-16 15:00:00.00 +0000","bug_type":"309"}`+"\n"+`{"procName":"Earlier"}`+"\n", began.Add(-20*time.Minute))
	// An app in the simulator that SpringBoard ended.
	write("user/WebDriverAgentRunner-Runner-2026-11-16-152610.ips",
		`{"app_name":"WebDriverAgentRunner-Runner","timestamp":"2026-11-16 15:26:10.00 +0000","bug_type":"309","name":"WebDriverAgentRunner-Runner"}`+"\n"+`{
  "procName" : "WebDriverAgentRunner-Runner",
  "parentProc" : "launchd_sim",
  "exception" : {"codes":"0x0000000000000000, 0x0000000000000000","rawCodes":[0,0],"type":"EXC_CRASH","signal":"SIGKILL"},
  "termination" : {"flags":6,"code":2343432205,"namespace":"FRONTBOARD","indicator":"scene-create watchdog transgression","byProc":"SpringBoard","byPid":4711}
}`+"\n", began.Add(6*time.Minute+10*time.Second))
	// A service of the Mac's that crashed.
	write("user/SimLaunchHost.arm64-2026-11-16-152558.ips",
		`{"app_name":"SimLaunchHost.arm64","timestamp":"2026-11-16 15:25:58.00 +0000","bug_type":"309","name":"SimLaunchHost.arm64"}`+"\n"+
			`{"procName":"SimLaunchHost.arm64","parentProc":"launchd","exception":{"type":"EXC_BAD_ACCESS","signal":"SIGSEGV"}}`+"\n", began.Add(5*time.Minute+58*time.Second))
	// A report of no one process, one whose second part is not JSON, and one that is not JSON at all.
	write("system/JetsamEvent-2026-11-16-152650.ips", `{"bug_type":"298","timestamp":"2026-11-16 15:26:50.00 +0000","name":""}`+"\n"+`{"memoryStatus":{}}`+"\n", began.Add(6*time.Minute+50*time.Second))
	write("system/Stackshot-2026-11-16-152700.ips", `{"app_name":"Stackshot","timestamp":"2026-11-16 15:27:00.00 +0000","bug_type":"288"}`+"\nnot JSON\n", began.Add(7*time.Minute))
	write("system/Torn-2026-11-16-152701.ips", "cut short\n", began.Add(7*time.Minute+time.Second))
	// A spindump, which is text.
	write("system/SpringBoard_2026-11-16-152640_Mac.spin",
		"Date/Time:        2026-11-16 15:26:40.000 +0000\nCommand:          SpringBoard\nPath:             /x/SpringBoard\nEvent:            hang\nDuration:         10.00s\nSteps:            100\n",
		began.Add(6*time.Minute+40*time.Second))
	write("diag/Runner_20261116-152000-utc.log", strings.Join([]string{
		"[2026-11-16 15:20:01Z INFO JobDispatcher] Successfully renew job request 7, job is valid till 2026-11-16 15:30:01",
		"[2026-11-16 15:26:02Z ERR  JobDispatcher] Catch exception during renew runner jobrequest 7.",
		"[2026-11-16 15:26:40Z INFO JobDispatcher] Lock renewal has run out of retry, stop renew lock for jobrequest 7.",
		"[2026-11-16 15:26:41Z INFO JobDispatcher] Send job cancellation message to worker for job 9.",
		"[2026-11-16 15:26:42Z INFO Runner] Something else.",
	}, "\n")+"\n", began.Add(7*time.Minute))
	if err := os.MkdirAll(filepath.Join(dir, "none"), 0o755); err != nil {
		t.Fatal(err)
	}

	report := func(reports, logs string, args ...string) string {
		t.Helper()
		cmd := exec.Command("bash", append([]string{filepath.Join("..", "..", "..", "scripts", "mac-job-report.sh")}, args...)...)
		cmd.Env = append(os.Environ(), "MAC_JOB_REPORTS="+reports, "MAC_JOB_RUNNER_LOGS="+logs)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("scripts/mac-job-report.sh: %v\n%s", err, out)
		}
		return string(out)
	}
	kept := filepath.Join(dir, "kept")
	got := report(filepath.Join(dir, "user")+":"+filepath.Join(dir, "system")+":"+filepath.Join(dir, "missing"), filepath.Join(dir, "diag"), filepath.Join(dir, "began"), kept)
	want := `the reports macOS wrote during the job:
  2026-11-16 15:25:58.00 +0000  SimLaunchHost.arm64  kind 309  started by launchd; EXC_BAD_ACCESS SIGSEGV
  2026-11-16 15:26:10.00 +0000  WebDriverAgentRunner-Runner  kind 309  (simulator); started by launchd_sim; EXC_CRASH SIGKILL; ended by SpringBoard: FRONTBOARD scene-create watchdog transgression
  SpringBoard_2026-11-16-152640_Mac.spin  Command: SpringBoard;Event: hang;Duration: 10.00s
  2026-11-16 15:26:50.00 +0000  JetsamEvent-2026-11-16-152650.ips  kind 298
  2026-11-16 15:27:00.00 +0000  Stackshot  kind 288
  Torn-2026-11-16-152701.ips
what the runner's log says of cancelling the job:
  [2026-11-16 15:26:02Z ERR  JobDispatcher] Catch exception during renew runner jobrequest 7.
  [2026-11-16 15:26:40Z INFO JobDispatcher] Lock renewal has run out of retry, stop renew lock for jobrequest 7.
  [2026-11-16 15:26:41Z INFO JobDispatcher] Send job cancellation message to worker for job 9.
`
	if got != want {
		t.Errorf("the report is\n%s\nwant\n%s", got, want)
	}
	// The reports of the job, and only they, are copied for the job to keep.
	copies, err := os.ReadDir(kept)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range copies {
		names = append(names, c.Name())
	}
	if len(names) != 6 || strings.Contains(strings.Join(names, " "), "Earlier") {
		t.Errorf("the reports kept are %v, want the job's six", names)
	}
	// A job without reports, on a runner whose log says nothing of a cancellation.
	if got, want := report(filepath.Join(dir, "none"), filepath.Join(dir, "none"), filepath.Join(dir, "began")),
		"the reports macOS wrote during the job:\n  none\nwhat the runner's log says of cancelling the job:\n  nothing\n"; got != want {
		t.Errorf("the report of a job without reports is\n%s\nwant\n%s", got, want)
	}
}
