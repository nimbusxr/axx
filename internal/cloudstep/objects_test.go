package cloudstep_test

import (
	"bytes"
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// memory is an object store in memory.
type memory struct {
	mu      sync.Mutex
	objects map[string][]byte // container/name
}

func (m *memory) Put(_ context.Context, container, name string, body []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[container+"/"+name] = body
	return nil
}

func (m *memory) Get(_ context.Context, container, name string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[container+"/"+name]
	return b, ok, nil
}

func (m *memory) List(_ context.Context, container string, _ int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for k := range m.objects {
		if name, ok := strings.CutPrefix(k, container+"/"); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

type storagePack struct{ objects cloudstep.Objects }

func (p storagePack) Manifest() core.Manifest {
	return core.Manifest{Name: "storage", Steps: p.objects.Steps()}
}

func TestObjectContent(t *testing.T) {
	st := &memory{objects: map[string][]byte{}}
	pdf, err := os.ReadFile("../filecontent/testdata/manifest.pdf")
	if err != nil {
		t.Fatal(err)
	}
	put := func(name string, body []byte) {
		_ = st.Put(context.Background(), "carrier-drops", name, body, "")
	}
	put("manifests/M-KESTREL-0412.pdf", pdf)
	put("disputes/kestrel-2026-09.csv", []byte("invoice line,parcel,reason\nIL-1,PX-KES-1001,weight differs\nIL-2,PX-KES-1002,\n"))
	put("disputes/kestrel-2026-09.xlsx", workbook(t, [][]any{{"invoice line", "parcel", "amount"}, {"IL-3", "PX-KES-1003", 12.5}}))
	put("photos/crushed-box.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))

	h := cloudtest.New(t, storagePack{cloudstep.Objects{
		Pack: "storage", Container: "s3 bucket", Object: "object", Example: "carrier-drops",
		Store: func(*core.Scenario) (cloudstep.ObjectStore, error) { return st, nil },
	}})
	h.NewScenario()

	h.OK(`the manifests/M-KESTREL-0412.pdf object in the carrier-drops s3 bucket contains "PX-KES-0412-1 Anna Weber 1200 g"`)
	h.OK(`the manifests/M-KESTREL-0412.pdf object in the carrier-drops s3 bucket contains 'Page two: signed by the driver'`)
	h.OK(`the disputes/kestrel-2026-09.xlsx object in the carrier-drops s3 bucket contains "PX-KES-1003"`)
	h.OK("the disputes/kestrel-2026-09.csv object in the carrier-drops s3 bucket has a row where:", [][]string{
		{"parcel", "PX-KES-1001"}, {"reason", "weight differs"},
	})
	h.OK("the disputes/kestrel-2026-09.csv object in the carrier-drops s3 bucket has a row where:", [][]string{
		{"invoice line", "IL-2"}, {"reason", ""},
	})
	h.OK("the disputes/kestrel-2026-09.xlsx object in the carrier-drops s3 bucket has a row where:", [][]string{
		{"parcel", "PX-KES-1003"}, {"amount", "12.5"},
	})

	err = h.Fails(`within 1s the manifests/M-KESTREL-0412.pdf object in the carrier-drops s3 bucket contains "PX-KES-0412-3 Anna Weber"`,
		`The manifests/M-KESTREL-0412.pdf object in the carrier-drops s3 bucket did not meet the expectation within 1s: `+
			`its text does not contain "PX-KES-0412-3 Anna Weber". Its text nearest that:`+"\n  PX-KES-0412-1 Anna Weber 1200 g")
	if !core.IsAssertion(err) {
		t.Errorf("a missing text is a failed check: %v", err)
	}
	_ = h.Fails("within 1s the disputes/kestrel-2026-09.csv object in the carrier-drops s3 bucket has a row where:",
		"did not meet the expectation within 1s: no row has parcel=PX-KES-1002, reason=weight differs. Its 2 rows are:\n"+
			"  parcel=PX-KES-1001, reason=weight differs\n  parcel=PX-KES-1002, reason=",
		[][]string{{"parcel", "PX-KES-1002"}, {"reason", "weight differs"}})
	_ = h.Fails("within 1s the disputes/kestrel-2026-09.csv object in the carrier-drops s3 bucket has a row where:",
		`its table has no "zone" column; its columns are: invoice line, parcel, reason`, [][]string{{"zone", "DE-1"}})
	_ = h.Fails(`within 1s the disputes/kestrel-2026-10.csv object in the carrier-drops s3 bucket contains "IL-1"`,
		"The carrier-drops s3 bucket has no object named disputes/kestrel-2026-10.csv after 1s. It has disputes/kestrel-2026-09.csv, disputes/kestrel-2026-09.xlsx")

	// A step that cannot read the object's type errs at once.
	start := time.Now()
	err = h.Fails(`the photos/crushed-box.png object in the carrier-drops s3 bucket contains "IHDR"`,
		"the photos/crushed-box.png object is a binary file (16 bytes), not a file whose text axx reads")
	if core.IsAssertion(err) || time.Since(start) > 5*time.Second {
		t.Errorf("a binary object should be an error at once: %v", err)
	}
	_ = h.Fails("the manifests/M-KESTREL-0412.pdf object in the carrier-drops s3 bucket has a row where:",
		"the manifests/M-KESTREL-0412.pdf object is a PDF, not a table axx reads (CSV, TSV, Excel .xlsx)", [][]string{{"parcel", "PX-KES-1001"}})

	// The checks wait for an object that is still being written.
	put("manifests/M-KESTREL-0413.pdf", pdf[:700])
	put("disputes/kestrel-2026-10.csv", []byte("invoice line,parcel\n"))
	go func() {
		time.Sleep(500 * time.Millisecond)
		put("manifests/M-KESTREL-0413.pdf", pdf)
		put("disputes/kestrel-2026-10.csv", []byte("invoice line,parcel\nIL-9,PX-KES-1009\n"))
	}()
	h.OK(`within 5s the manifests/M-KESTREL-0413.pdf object in the carrier-drops s3 bucket contains "Daily manifest"`)
	h.OK("within 5s the disputes/kestrel-2026-10.csv object in the carrier-drops s3 bucket has a row where:", [][]string{{"parcel", "PX-KES-1009"}})
}

func TestObjectStepsSince(t *testing.T) {
	since := func(o cloudstep.Objects) map[string]string {
		out := map[string]string{}
		for _, s := range o.Steps() {
			out[strings.TrimPrefix(s.ID, "p.")] = s.Since
		}
		return out
	}
	o := cloudstep.Objects{Pack: "p", Container: "s3 bucket", Object: "object", Example: "carrier-drops"}
	got := since(o)
	if got["upload"] != "0.1.0" || got["properties"] != "0.1.0" || got["contains"] != "0.1.1" || got["row"] != "0.1.1" {
		t.Errorf("the storage packs' steps: %v", got)
	}
	o.Since = "0.1.1"
	for id, v := range since(o) {
		if v != "0.1.1" {
			t.Errorf("%s of a pack introduced in 0.1.1 is since %s", id, v)
		}
	}
	checks := o.Checks()
	if len(checks) != 5 || checks[0].ID != "p.has" {
		t.Errorf("the checks: %d, first %s", len(checks), checks[0].ID)
	}
}

func workbook(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
