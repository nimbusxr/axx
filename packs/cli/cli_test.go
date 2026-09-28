package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/secrets"
)

// helperEnv makes the test binary act as the parcels admin command (see
// runHelper).
const helperEnv = "AXX_CLI_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runHelper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

const zpl = "^XA\n^FO50,50^FDPX-ADM-6101^FS\n^XZ\n"

// runHelper is a small admin command:
//
//	label <ref>     print the parcel's label
//	cancel -        cancel the parcels whose references are the input's lines
//	refuse <ref>    refuse to cancel a dispatched parcel: exit 3
//	json            print a shop's parcels as JSON
//	token           print the PARCELS_ADMIN_TOKEN variable
//	pwd             print the working directory
//	touch <name>    write a file in the working directory
//	sleep           run until stopped
func runHelper(args []string) int {
	switch args[0] {
	case "label":
		fmt.Print(zpl)
	case "cancel":
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			if ref := strings.TrimSpace(sc.Text()); ref != "" {
				fmt.Println("cancelled " + ref)
			}
		}
	case "refuse":
		fmt.Fprintf(os.Stderr, "%s is DISPATCHED: it can no longer be cancelled\n", args[1])
		return 3
	case "json":
		fmt.Println(`{"shop": "kestrel-books", "parcels": [{"reference": "PX-ADM-6105", "serviceLevel": "EXPRESS"}]}`)
	case "token":
		fmt.Println("token " + os.Getenv("PARCELS_ADMIN_TOKEN"))
		fmt.Fprintln(os.Stderr, "using "+os.Getenv("PARCELS_ADMIN_TOKEN"))
		return 1
	case "pwd":
		wd, _ := os.Getwd()
		fmt.Print(wd)
	case "touch":
		if err := os.WriteFile(args[1], []byte("report"), 0o644); err != nil {
			return 4
		}
	case "sleep":
		time.Sleep(time.Hour)
	default:
		return 2
	}
	return 0
}

func harness(t *testing.T) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.New(t, Pack())
	register(h, nil)
	return h
}

// register registers the admin command: the test binary as the helper.
func register(h *cloudtest.Harness, extra [][]string) {
	rows := append([][]string{{"command", `"` + os.Args[0] + `"`}, {"env." + helperEnv, "1"}}, extra...)
	h.OK("the admin command with the following properties:", rows)
}

func TestRunAndCheck(t *testing.T) {
	h := harness(t)
	h.File("labels/PX-ADM-6101.zpl", zpl)
	h.OK("the admin command is run with 'label PX-ADM-6101'")
	h.OK("the admin command's exit code is 0")
	h.OK("the admin command's output contains '^FDPX-ADM-6101^FS'")
	h.OK("the admin command's output has a line matching '^\\^FO\\d+,\\d+'")
	h.OK("the admin command's output is:", "^XA\n^FO50,50^FDPX-ADM-6101^FS\n^XZ")
	h.OK("the admin command's error output is empty")
	h.OK("the admin command's output is identical to the labels/PX-ADM-6101.zpl file")

	h.OK("the admin command is run with 'json'")
	h.OK("the admin command's output has the following properties:",
		[][]string{{"shop", "kestrel-books"}, {"parcels[0].reference", "PX-ADM-6105"}, {"parcels[0].serviceLevel", "EXPRESS"}, {"parcels[1]", "undefined"}})
}

func TestTheInput(t *testing.T) {
	h := harness(t)
	h.OK("the admin command is run with 'cancel -' and the input:", "PX-ADM-6103\nPX-ADM-6104")
	h.OK("the admin command's output is:", "cancelled PX-ADM-6103\ncancelled PX-ADM-6104")
	h.OK("the admin command's output has a line matching 'cancelled PX-ADM-610[34]'")
}

func TestFailures(t *testing.T) {
	h := harness(t)
	_ = h.Fails("the admin command's exit code is 0", `the admin command has not run in this scenario; run it with "the admin command is run"`)
	_ = h.Fails("the courier command is run", `no command named "courier" in this scenario`)
	h.OK("the admin command is run with 'refuse PX-ADM-6102'")
	_ = h.Fails("the admin command's exit code is 0", "The admin command's exit code is 3, not 0. Its error output:\n  PX-ADM-6102 is DISPATCHED: it can no longer be cancelled")
	h.OK("the admin command's exit code is 3")
	h.OK("the admin command's error output contains 'PX-ADM-6102 is DISPATCHED'")
	h.OK("the admin command's output is empty")
	_ = h.Fails("the admin command's error output is empty", "The admin command's error output is not empty")
	_ = h.Fails("the admin command's error output contains 'PX-ADM-6102 is RETURNED'", "The admin command's error output does not contain \"PX-ADM-6102 is RETURNED\"")
	_ = h.Fails("the admin command's output has a line matching 'cancelled'", "No line of the admin command's output matches cancelled")
	_ = h.Fails("the admin command's output is:", "The admin command's output is not what the step says", "cancelled PX-ADM-6102")
	h.File("labels/other.zpl", "^XA^XZ\n")
	_ = h.Fails("the admin command's output is identical to the labels/other.zpl file", "The admin command's output differs from the labels/other.zpl file")
	_ = h.Fails("the admin command's output has the following properties:", "the admin command's output", [][]string{{"shop", "kestrel-books"}})
}

func TestProperties(t *testing.T) {
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"dir", "."}}, `the command property "command" is required`},
		{[][]string{{"command", "parcels"}, {"shell", "true"}}, `unknown command property "shell"`},
		{[][]string{{"command", "parcels"}, {"timeout", "soon"}}, `the admin command's timeout "soon" is not a duration`},
		{[][]string{{"command", "parcels"}, {"dir", "no-such-folder"}}, "is not a folder"},
		{[][]string{{"command", "parcels"}, {"env.", "x"}}, `does not name an environment variable`},
	} {
		h := cloudtest.New(t, Pack())
		_ = h.Fails("the admin command with the following properties:", c.want, c.rows)
	}
}

// ${env:..} values are secrets: masked in attachments and failures.
func TestSecretsAreMasked(t *testing.T) {
	const token = "desk-token-desk-token"
	t.Setenv("PARCELS_ADMIN_TOKEN", token)
	h := cloudtest.New(t, Pack())
	register(h, [][]string{{"env.PARCELS_ADMIN_TOKEN", "${env:PARCELS_ADMIN_TOKEN}"}})
	h.OK("the admin command is run with 'token'")
	err := h.Fails("the admin command's exit code is 0", "The admin command's exit code is 1")
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), secrets.Masked) {
		t.Errorf("failure: %v", err)
	}
	for _, a := range h.Sink.Attachments {
		if strings.Contains(string(a.Body), token) {
			t.Errorf("%s shows the token: %s", a.Name, a.Body)
		}
	}
	if len(h.Sink.Attachments) != 2 {
		t.Errorf("attachments: %d", len(h.Sink.Attachments))
	}
}

// Each scenario's command runs in a folder of its own: removed when the
// scenario passes, kept when it fails.
func TestAFolderOfEachScenariosOwn(t *testing.T) {
	h := harness(t)
	h.OK("the admin command is run with 'touch report.csv'")
	h.OK("the admin command is run with 'pwd'")
	first := lastOutput(h)
	if _, err := os.Stat(filepath.Join(first, "report.csv")); err != nil {
		t.Fatalf("the file the command wrote: %v", err)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("a passed scenario's folder was kept: %v", err)
	}

	h.NewScenario()
	register(h, nil)
	h.OK("the admin command is run with 'pwd'")
	second := lastOutput(h)
	if second == first {
		t.Errorf("two scenarios ran in %s", first)
	}
	if _, err := os.Stat(filepath.Join(second, "report.csv")); !os.IsNotExist(err) {
		t.Errorf("a scenario sees another's file")
	}
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Errorf("a failed scenario's folder was removed: %v", err)
	}
	_ = os.RemoveAll(second)
}

func TestATimeoutFailsTheRun(t *testing.T) {
	h := cloudtest.New(t, Pack())
	register(h, [][]string{{"timeout", "300ms"}})
	_ = h.Fails("the admin command is run with 'sleep'", "the admin command ran past its timeout of 300ms and was stopped")
}

func lastOutput(h *cloudtest.Harness) string {
	st := scenarioState.Of(h.SC)
	st.mu.Lock()
	defer st.mu.Unlock()
	return string(st.runs["admin"].result.Stdout)
}
