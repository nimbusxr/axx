package istanbul

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What lcov says of a file reads back to the same lcov: the reports of
// web-coverage's cases, which Node's tools wrote.
func TestLcovReadsBackAsWritten(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "internal", "v8cov", "testdata", "*", "lcov.info"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no lcov testdata: %v", err)
	}
	for _, p := range paths {
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files, m, err := ReadLcov(bytes.NewReader(want))
		if err != nil {
			t.Fatal(err)
		}
		if !m.Branches || !m.Functions {
			t.Errorf("%s: measured %+v", p, m)
		}
		if got := Lcov(files); !bytes.Equal(got, want) {
			t.Errorf("%s: read back differs:\n%s", p, got)
		}
	}
}

func TestReadLcovOfOtherTools(t *testing.T) {
	// lcov 2's FN with its end line, a branch whose block never ran, a file
	// listed twice, no functions or branches measured for the second file.
	in := "TN:\nSF:src/orders.py\nFN:3,9,register\nFNDA:4,register\nDA:3,4\nDA:4,0\nBRDA:4,0,0,-\nBRDA:4,0,1,4\nend_of_record\n" +
		"SF:src/orders.py\nDA:4,2\nend_of_record\nSF:src/shipments.py\nDA:1,1\nend_of_record\n"
	files, m, err := ReadLcov(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !m.Branches || !m.Functions {
		t.Fatalf("%d files, %+v", len(files), m)
	}
	s := files[0].Summary()
	if s.Lines.Total != 2 || s.Lines.Covered != 2 || s.Branches.Total != 2 || s.Branches.Covered != 1 || s.Functions.Covered != 1 || files[0].FnMap[0].Name != "register" {
		t.Errorf("orders.py: %+v %+v", s, files[0].FnMap)
	}
	if _, _, err := ReadLcov(strings.NewReader("DA:1,1\n")); err == nil {
		t.Error("a DA before any SF reads")
	}
}

func TestReadCobertura(t *testing.T) {
	in := `<?xml version="1.0" ?>
<coverage line-rate="0.75" branch-rate="0.5" version="7.4">
  <sources><source>/app/src</source></sources>
  <packages><package name="parcels">
    <classes>
      <class name="orders.py" filename="parcels/orders.py" line-rate="0.75" branch-rate="0.5">
        <methods><method name="register" signature=""><lines><line number="3" hits="2"/></lines></method></methods>
        <lines>
          <line number="3" hits="2"/>
          <line number="4" hits="2" branch="true" condition-coverage="50% (1/2)"/>
          <line number="5" hits="0"/>
          <line number="7" hits="1"/>
        </lines>
      </class>
    </classes>
  </package></packages>
</coverage>`
	files, m, err := ReadCobertura(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "/app/src/parcels/orders.py" || !m.Branches || !m.Functions {
		t.Fatalf("%+v %+v", files, m)
	}
	if s := files[0].Summary(); s.Lines.Total != 4 || s.Lines.Covered != 3 || s.Branches.Total != 2 || s.Branches.Covered != 1 || s.Functions.Covered != 1 {
		t.Errorf("summary: %+v", s)
	}
	if _, _, err := ReadCobertura(strings.NewReader("TN:\n")); err == nil {
		t.Error("lcov read as Cobertura")
	}
}
