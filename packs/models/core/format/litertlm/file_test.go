package litertlm

import (
	"bytes"
	"testing"
)

func TestAFileRoundTrips(t *testing.T) {
	f := &File{
		Major: 1, Minor: 6,
		System: []KV{{"Authors", "axx"}, {"version", int32(3)}, {"public", true}},
		Sections: []Section{
			{Type: LlmMetadataProto, Data: []byte{1, 2, 3}},
			{Type: TFLiteModel, Items: []KV{{"model_type", "tf_lite_prefill_decode"}}, Data: bytes.Repeat([]byte{7}, 20000)},
		},
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	g, err := parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if g.Major != 1 || g.Minor != 6 || len(g.System) != 3 || g.System[1].Value != int32(3) || g.System[2].Value != true {
		t.Errorf("header %+v", g)
	}
	if len(g.Sections) != 2 || !bytes.Equal(g.Sections[1].Data, f.Sections[1].Data) || g.Sections[1].Items[0].Value != "tf_lite_prefill_decode" {
		t.Errorf("sections %+v", g.Sections)
	}
	if off := bytes.Index(buf.Bytes(), f.Sections[1].Data); off%16384 != 0 {
		t.Errorf("a section begins at %d, not a multiple of 16 KiB", off)
	}
	if _, err := parse([]byte("not a model at all, no........................")); err == nil {
		t.Error("a text is a LiteRT-LM file")
	}
}
