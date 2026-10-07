package files

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
		want := "0.1.1"
		switch s.ID {
		case "files.screenshot":
			want = "0.2.1"
		case "files.copy", "files.content", "files.emptied", "files.absent", "files.empty":
			want = "0.2.3"
		}
		if s.Since != want {
			t.Errorf("%s is since %s", s.ID, s.Since)
		}
	}
	if got := strings.Join(ids, " "); got != "files.folder files.copy files.content files.emptied files.has files.identical files.properties files.contains files.row files.absent files.empty files.screenshot" {
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
	_ = h.Fails("the labels folder with the following properties:", `unknown folder property "url" (supported: path, owner)`, [][]string{{"url", "file://labels"}})
	_ = h.Fails("the labels folder with the following properties:", `the folder property "path" is required`, [][]string{{"path", " "}})
}

// A scenario puts files in a folder (a copy of the project's, or a doc
// string's content) and empties it: a folder of the project, a service's or
// an app's, never one elsewhere on the machine.
func TestPuts(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	if err := os.MkdirAll(filepath.Join(h.Dir, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "reports", "earlier.csv"), []byte("line,status\n1,IMPORTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARCELS_SHOP", "kestrel-books")
	h.OK("the exports folder with the following properties:", [][]string{{"path", "exports"}})
	h.OK("the manifests/M-1/report.csv file in the exports folder is a copy of the reports/earlier.csv file")
	h.OK("the manifests/M-1/summary.json file in the exports folder has the content:", `{"shop": "${env:PARCELS_SHOP}", "lines": 1}`)
	h.OK("the .marker file in the exports folder has the content:", "read")
	dir := filepath.Join(h.Dir, "exports")
	if b, _ := os.ReadFile(filepath.Join(dir, "manifests", "M-1", "report.csv")); string(b) != "line,status\n1,IMPORTED\n" {
		t.Errorf("a copy has the project file's content, in the folders made for it: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "manifests", "M-1", "summary.json")); string(b) != `{"shop": "kestrel-books", "lines": 1}` {
		t.Errorf("a file has the doc string's content, ${env:..} expanded: %q", b)
	}
	h.OK("the manifests/M-1/report.csv file in the exports folder contains \"IMPORTED\"")
	h.OK("the exports folder has no file named manifests/M-2/report.csv")
	_ = h.Fails("within 1s the exports folder has no file named manifests/M-1/report.csv", "The exports folder still has a file named manifests/M-1/report.csv after 1s")
	_ = h.Fails("within 1s the exports folder has no file named .marker", "still has a file named .marker")
	_ = h.Fails("within 1s the exports folder is empty", "The exports folder is not empty after 1s: it has 2 files")

	// A check waits for what a service takes away.
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.Remove(filepath.Join(dir, "manifests", "M-1", "report.csv"))
	}()
	h.OK("within 5s the exports folder has no file named manifests/M-1/report.csv")
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.Remove(filepath.Join(dir, "manifests", "M-1", "summary.json"))
	}()
	h.OK("within 5s the exports folder is empty") // .marker is hidden: not counted

	h.OK("the exports folder is emptied")
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("an emptied folder is there, with nothing in it, hidden files too: %v %v", entries, err)
	}
	h.OK("the archive folder with the following properties:", [][]string{{"path", "never-made"}})
	h.OK("the archive folder is emptied") // not there: empty
	h.OK("the archive folder is empty")
	h.OK("the archive folder has no file named x.csv")
	_ = h.Fails("the archive folder has no file named ../x.csv", "is not a file in the archive folder")
	_ = h.Fails("the parcels folder is empty", `no folder named "parcels"`)

	_ = h.Fails("the ../outside/x.csv file in the exports folder is a copy of the reports/earlier.csv file", "is not a file in the exports folder")
	_ = h.Fails("the x.csv file in the exports folder is a copy of the reports/missing.csv file", "missing.csv not found")
	h.OK("the home folder with the following properties:", [][]string{{"path", t.TempDir()}})
	_ = h.Fails("the home folder is emptied", "is not in the project")
	h.OK("the parent folder with the following properties:", [][]string{{"path", ".."}})
	_ = h.Fails("the parent folder is emptied", "is not in the project")
	h.OK("the project folder with the following properties:", [][]string{{"path", "."}})
	_ = h.Fails("the project folder is emptied", "is not in the project")
}

// A folder's owner is a service of axx.yaml, whose paths are relative to the
// folder axx runs it in, or an app, whose files a pack that runs apps gives.
func TestFolderOwners(t *testing.T) {
	h := cloudtest.NewWithServices(t, map[string]string{"parcels": "../infra", "billing": "."}, Pack())
	h.NewScenario()
	h.OK("the exports folder with the following properties:", [][]string{{"owner", "service:parcels"}, {"path", "./exports"}})
	exports, err := Folders(h.SC).Get("exports")
	if err != nil || exports.Path != filepath.Join(filepath.Dir(h.Dir), "infra", "exports") {
		t.Errorf("a service's path is relative to the folder axx runs it in: %+v %v", exports, err)
	}
	h.OK("the spaced folder with the following properties:", [][]string{{"owner", "service:billing"}, {"path", `"./daily runs"`}})
	if spaced, _ := Folders(h.SC).Get("spaced"); spaced.Path != filepath.Join(h.Dir, "daily runs") {
		t.Errorf("a quoted path loses its quotes: %s", spaced.Path)
	}
	_ = h.Fails("the claims folder with the following properties:", `the folder's owner "service:claims" is no service of axx.yaml (its services: billing, parcels)`,
		[][]string{{"owner", "service:claims"}, {"path", "./exports"}})
	_ = h.Fails("the claims folder with the following properties:", `the folder's owner "parcels" is not "service:<name>" (a service of axx.yaml) or "app:<name>" (an app the scenario registers)`,
		[][]string{{"owner", "parcels"}, {"path", "./exports"}})
	_ = h.Fails("the desk folder with the following properties:", `the folder's owner "app:depot" is an app, and no pack of the run runs apps`,
		[][]string{{"owner", "app:depot"}, {"path", "./Parcels"}})

	// An app's files are what the pack that runs it gives.
	data := filepath.Join(h.Dir, "depot-data")
	h.SC.Suite().SetFileOwner("app", func(sc *core.Scenario, name, path string) (core.Files, error) {
		if name != "depot" || path != "./Parcels/Depot desk" {
			t.Errorf("the owner gets the app's name and the path: %q %q", name, path)
		}
		return core.LocalFiles(filepath.Join(data, "Parcels", "Depot desk")), nil
	})
	h.OK("the desk folder with the following properties:", [][]string{{"owner", "app:depot"}, {"path", `"./Parcels/Depot desk"`}})
	h.File("depot-data/Parcels/Depot desk/arrivals.json", `[{"reference":"PX-DSK-4102"}]`)
	h.OK(`the arrivals.json file in the desk folder contains "PX-DSK-4102"`)
	if logs := strings.Join(h.Sink.Logs, "\n"); !strings.Contains(logs, "the desk folder is "+filepath.Join(data, "Parcels", "Depot desk")) {
		t.Errorf("the scenario should log where an app's folder is: %s", logs)
	}
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
	// A path with a space is quoted.
	h.OK("the runs folder with the following properties:", [][]string{{"path", "runs"}})
	h.File("runs/day one/summary.json", `{"imported":1}`)
	h.OK(`the runs folder has a file named "day one/summary.json"`)
	h.OK(`the "day one/summary.json" file in the runs folder has the following properties:`, [][]string{{"imported", "1"}})

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

// A * in a file's path stands for any characters but a slash: the path names
// the one file it matches, like a save named by the time.
func TestPattern(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	h.File("inbox/snap-20261005-181256-123.json", `{"annotations":[{"type":"rect","color":"#007AFF"}]}`)
	h.File("inbox/snap-20261005-181256-123.png", "png")
	h.File("inbox/report[1].csv", "a")
	h.OK("the inbox folder with the following properties:", [][]string{{"path", "inbox"}})
	h.OK(`the inbox folder has a file named "snap-*.png"`)
	h.OK(`the "snap-*.json" file in the inbox folder has the following properties:`, [][]string{{"annotations[0].type", "rect"}, {"annotations[0].color", "#007AFF"}})
	h.OK(`the "report[1].csv" file in the inbox folder contains "a"`)
	_ = h.Fails(`within 1s the "snap-*.txt" file in the inbox folder contains "x"`, "The inbox folder has no file named snap-*.txt")
	h.File("inbox/snap-20261005-181301-456.png", "png")
	_ = h.Fails(`within 1s the inbox folder has a file named "snap-*.png"`, "2 files in the inbox folder are named snap-*.png")
	// A * does not reach into a subfolder: *.csv names report[1].csv only.
	h.File("inbox/runs/today.csv", "b")
	h.OK(`the "*.csv" file in the inbox folder contains "a"`)
	h.OK(`the "runs/*.csv" file in the inbox folder contains "b"`)
}

// An image an app saved is compared with its screenshot, one for each
// platform: the first time, the screenshot is taken.
func TestScreenshot(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.NewScenario()
	blue, red := color.RGBA{0, 122, 255, 255}, color.RGBA{255, 59, 48, 255}
	h.File("inbox/snap-20261005-181256-123.png", pngOf(t, blue))
	h.File("inbox/notes.txt", "Align the checkout button")
	h.OK("the inbox folder with the following properties:", [][]string{{"path", "inbox"}})
	step := `the "snap-*.png" file in the inbox folder looks like the "annotated" screenshot`
	_ = h.Fails(step, `There was no "annotated.`+platform+`" screenshot to compare with, so it was taken`)
	if _, err := os.Stat(filepath.Join(h.Dir, "screenshots", "annotated."+platform+".png")); err != nil {
		t.Fatal(err)
	}
	h.OK(step)
	h.File("inbox/snap-20261005-181256-123.png", pngOf(t, red))
	h.Sink.Reset()
	_ = h.Fails("within 1s "+step, "1600 pixels of 1600 differ from its \"annotated\" screenshot")
	var names []string
	for _, a := range h.Sink.Attachments {
		names = append(names, a.Name)
	}
	if got := strings.Join(names, ", "); got != "expected screenshot, the image, difference" {
		t.Errorf("attachments: %s", got)
	}
	_ = h.Fails(`within 1s the "notes.txt" file in the inbox folder looks like the "notes" screenshot`, "it is not an image axx reads")
	_ = h.Fails(`the "notes.txt" file in the inbox folder looks like the "../notes" screenshot`, "a screenshot's name is a file name")
}

func pngOf(t *testing.T, c color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := range 40 {
		for x := range 40 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
