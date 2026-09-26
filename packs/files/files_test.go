package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

func TestManifest(t *testing.T) {
	m := Pack().Manifest()
	var ids []string
	for _, s := range m.Steps {
		ids = append(ids, s.ID)
		if s.Since != "0.1.1" {
			t.Errorf("%s is since %s", s.ID, s.Since)
		}
	}
	if got := strings.Join(ids, " "); got != "files.folder files.has files.identical files.properties files.contains files.row" {
		t.Errorf("steps: %s", got)
	}
}

func TestFolder(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	t.Setenv("PARCELS_ARCHIVE", filepath.Join(h.Dir, "archive"))
	h.OK("the exports folder with the following properties:", [][]string{{"path", "../infra/exports"}})
	h.OK("the archive folder with the following properties:", [][]string{{"path", "${env:PARCELS_ARCHIVE}"}})

	exports, err := Folders(h.SC).Get("exports")
	if err != nil || exports.Path != filepath.Join(filepath.Dir(h.Dir), "infra", "exports") {
		t.Errorf("a relative path is relative to the project directory: %+v %v", exports, err)
	}
	if archive, _ := Folders(h.SC).Get("archive"); archive.Path != filepath.Join(h.Dir, "archive") {
		t.Errorf("the path should expand ${env:..}: %s", archive.Path)
	}
	if logs := strings.Join(h.Sink.Logs, "\n"); !strings.Contains(logs, "the exports folder is "+exports.Path+", which is not there yet") {
		t.Errorf("the scenario should log where the folder is: %s", logs)
	}

	_ = h.Fails("the exports folder with the following properties:", `Folder "exports" already set`, [][]string{{"path", "exports"}})
	_ = h.Fails("the labels folder with the following properties:", `unknown folder property "url" (supported: path)`, [][]string{{"url", "file://labels"}})
	_ = h.Fails("the labels folder with the following properties:", `the folder property "path" is required`, [][]string{{"path", " "}})
}

func TestFiles(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	h.OK("the exports folder with the following properties:", [][]string{{"path", "exports"}})
	h.File("exports/manifests/M-KESTREL-0412/report.v2.csv", "line,reference,status,reason\nML-KES-0412-1,PX-KES-1001,IMPORTED,\nML-KES-0412-2,PX-KES-1002,REJECTED,unknown service level\n")
	h.File("exports/manifests/M-KESTREL-0412/summary.json", `{"manifest":"M-KESTREL-0412","lines":2,"imported":1,"rejected":1}`)
	h.File("exports/.DS_Store", "x")
	h.File("expected/report.csv", "line,reference,status,reason\nML-KES-0412-1,PX-KES-1001,IMPORTED,\nML-KES-0412-2,PX-KES-1002,REJECTED,unknown service level\n")
	pdf, err := os.ReadFile("../../internal/filecontent/testdata/manifest.pdf")
	if err != nil {
		t.Fatal(err)
	}
	h.File("exports/manifests/M-KESTREL-0412/handover.pdf", string(pdf))

	h.OK("the exports folder has a file named manifests/M-KESTREL-0412/report.v2.csv")
	h.OK("the manifests/M-KESTREL-0412/report.v2.csv file in the exports folder is identical to the expected/report.csv file")
	h.OK("the manifests/M-KESTREL-0412/summary.json file in the exports folder has the following properties:", [][]string{
		{"manifest", "M-KESTREL-0412"}, {"imported", "1"}, {"$.rejected", "1"},
	})
	h.OK(`the manifests/M-KESTREL-0412/handover.pdf file in the exports folder contains "Total: 2 parcels, 2000 g"`)
	h.OK("the manifests/M-KESTREL-0412/report.v2.csv file in the exports folder has a row where:", [][]string{
		{"line", "ML-KES-0412-2"}, {"status", "REJECTED"}, {"reason", "unknown service level"},
	})

	// A file written later is waited for.
	go func() {
		time.Sleep(500 * time.Millisecond)
		p := filepath.Join(h.Dir, "exports", "manifests", "M-KESTREL-0413", "report.csv")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("line,status\nML-KES-0413-1,IMPORTED\n"), 0o644)
	}()
	h.OK("within 5s the manifests/M-KESTREL-0413/report.csv file in the exports folder has a row where:", [][]string{{"status", "IMPORTED"}})

	err = h.Fails("within 1s the exports folder has a file named manifests/M-KESTREL-0414/report.csv",
		"The exports folder has no file named manifests/M-KESTREL-0414/report.csv after 1s. It has manifests/M-KESTREL-0412/handover.pdf, "+
			"manifests/M-KESTREL-0412/report.v2.csv, manifests/M-KESTREL-0412/summary.json, manifests/M-KESTREL-0413/report.csv")
	if !core.IsAssertion(err) || strings.Contains(err.Error(), ".DS_Store") {
		t.Errorf("the failure should list the folder's files, not hidden ones: %v", err)
	}
	_ = h.Fails(`within 1s the manifests/M-KESTREL-0412/handover.pdf file in the exports folder contains "Total: 3 parcels"`,
		"The manifests/M-KESTREL-0412/handover.pdf file in the exports folder did not meet the expectation within 1s: "+
			`its text does not contain "Total: 3 parcels". Its text nearest that:`+"\n  Total: 2 parcels, 2000 g — handed over at depot Berlin-Mitte")
	_ = h.Fails("the ../expected/report.csv file in the exports folder is identical to the expected/report.csv file",
		"../expected/report.csv is not a file in the exports folder: name a file by its path in the folder")
	_ = h.Fails("the report.csv file in the archive folder contains 'ML-1'",
		`no folder named "archive" in this scenario; register it first with "the archive folder with the following properties:"`)
}

func TestFolderNotThereYet(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	h.OK("the exports folder with the following properties:", [][]string{{"path", "exports"}})
	_ = h.Fails("within 1s the exports folder has a file named report.csv", "The exports folder has no file named report.csv after 1s. It has no files")
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.MkdirAll(filepath.Join(h.Dir, "exports"), 0o755)
		_ = os.WriteFile(filepath.Join(h.Dir, "exports", "report.csv"), []byte("line,status\n"), 0o644)
	}()
	h.OK("within 5s the exports folder has a file named report.csv")
}
