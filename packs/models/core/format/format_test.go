package format_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A pack's format, a folder: a model's graph and its weights.
func init() {
	format.Register(format.Format{
		Type: "folder", Name: "Folder", Extensions: []string{".folder"},
		Contract: func(string) (*contract.Contract, error) { return nil, errors.New("no reader") },
		Write: func(s *stub.Stub) (format.Model, error) {
			return format.Model{Files: map[string][]byte{"graph.json": []byte("{}"), "weights/answers.bin": {1, 2, 3}}}, nil
		},
	})
}

func TestAFormatIsFoundByItsTypeOrItsModelsExtension(t *testing.T) {
	f, ok := format.ByType("folder")
	if !ok || f.Name != "Folder" {
		t.Fatalf("%+v %v", f, ok)
	}
	if f, ok := format.ByFile(filepath.Join("models", "Photo.FOLDER")); !ok || f.Type != "folder" {
		t.Errorf("by its extension: %+v %v", f, ok)
	}
	if _, ok := format.ByFile("photo.unknown"); ok {
		t.Error("a model of no format has one")
	}
	m, err := f.Write(&stub.Stub{})
	if err != nil || m.File != nil || len(m.Files) != 2 {
		t.Errorf("a folder's model: %+v %v", m, err)
	}
}

func TestAFormatIsRegisteredOnce(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), `format "folder" is registered twice`) {
			t.Errorf("recovered %v", r)
		}
	}()
	format.Register(format.Format{Type: "folder"})
}

func TestAFormatWithoutAStubsMatcherSaysSo(t *testing.T) {
	err := format.Unmatched("Folder", &stub.Stub{Matcher: "contains"})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != format.CodeWrite || !strings.Contains(err.Error(), "not by the contains matcher") {
		t.Errorf("%v", err)
	}
}

func TestAOneFileModelIsItsFile(t *testing.T) {
	write := format.File(func(*stub.Stub) ([]byte, error) { return []byte("model"), nil })
	m, err := write(&stub.Stub{})
	if err != nil || string(m.File) != "model" || m.Files != nil {
		t.Errorf("%+v %v", m, err)
	}
}

// Of formats whose models share an extension, one that tells its models by
// their content is chosen when it does, before one of the extension alone,
// whatever their order.
func TestAFormatThatSniffsIsChosenFirst(t *testing.T) {
	dir := t.TempDir()
	ours, theirs := filepath.Join(dir, "ours.sniffed"), filepath.Join(dir, "theirs.sniffed")
	for path, content := range map[string]string{ours: "ours", theirs: "theirs"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	format.Register(format.Format{Type: "test-plain", Extensions: []string{".sniffed"}})
	format.Register(format.Format{Type: "test-sniffing", Extensions: []string{".sniffed"}, Sniff: func(path string) bool {
		b, _ := os.ReadFile(path)
		return string(b) == "ours"
	}})
	for range 20 {
		if f, _ := format.ByFile(ours); f.Type != "test-sniffing" {
			t.Fatalf("a model the sniffing format tells is the %s format's", f.Type)
		}
		if f, _ := format.ByFile(theirs); f.Type != "test-plain" {
			t.Fatalf("a model the sniffing format does not tell is the %s format's", f.Type)
		}
	}
}
