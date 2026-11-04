package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
)

func pinProject(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "axx.yaml")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return file
}

func TestReadPin(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"plain", "version: 1\naxx: 0.5.0\nrun:\n  paths: [features]\n", "0.5.0"},
		{"double quotes", "axx: \"0.5.0\"\n", "0.5.0"},
		{"single quotes", "axx: '0.5.0'\n", "0.5.0"},
		{"a comment after it", "axx: 0.5.0 # the version\n", "0.5.0"},
		{"windows line endings", "version: 1\r\naxx: 0.5.0\r\nrun: {}\r\n", "0.5.0"},
		{"a pre-release", "axx: 1.0.0-rc.1\n", "1.0.0-rc.1"},
		{"read as written, for its error to say", "axx: 0.5\n", "0.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := pinProject(t, tc.content)
			pin, err := ReadPin("")
			if err != nil {
				t.Fatal(err)
			}
			if pin.Version != tc.want {
				t.Errorf("version %q, want %q", pin.Version, tc.want)
			}
			// macOS's temporary directory is a link: compare the file names only.
			if filepath.Base(pin.File) != filepath.Base(file) {
				t.Errorf("file %q, want %q", pin.File, file)
			}
		})
	}
}

// A file this axx's schema could not read still gives its pin: it may follow a
// newer version's schema, and that is what the pin is for.
func TestReadPinReadsAFileOfAnotherVersion(t *testing.T) {
	pinProject(t, "somethingNew: {a: [1, 2\naxx: 0.9.0\n")
	pin, err := ReadPin("")
	if err != nil {
		t.Fatal(err)
	}
	if pin.Version != "0.9.0" {
		t.Errorf("version %q", pin.Version)
	}
}

func TestReadPinNamesNone(t *testing.T) {
	for name, content := range map[string]string{
		"no line":        "version: 1\n",
		"nested":         "properties:\n  axx: 0.5.0\n",
		"in a comment":   "# axx: 0.5.0\n",
		"a longer name":  "axxx: 0.5.0\n",
		"a block's text": "properties:\n  note: |\n    axx: 0.5.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			pinProject(t, content)
			pin, err := ReadPin("")
			if err != nil {
				t.Fatal(err)
			}
			if pin.File != "" {
				t.Errorf("found %+v", pin)
			}
		})
	}
}

func TestReadPinWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if pin, err := ReadPin(""); err != nil || pin.File != "" {
		t.Errorf("%+v, %v", pin, err)
	}
	// The command says that --config names no file.
	if pin, err := ReadPin("nope.yaml"); err != nil || pin.File != "" {
		t.Errorf("%+v, %v", pin, err)
	}
}

func TestReadPinFollowsConfigAndParents(t *testing.T) {
	file := pinProject(t, "axx: 0.5.0\n")
	sub := filepath.Join(filepath.Dir(file), "features", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if pin, _ := ReadPin(""); pin.Version != "0.5.0" {
		t.Errorf("from a subdirectory: %+v", pin)
	}
	other := filepath.Join(t.TempDir(), "other.yaml")
	if err := os.WriteFile(other, []byte("axx: 0.7.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pin, _ := ReadPin(other); pin.Version != "0.7.0" {
		t.Errorf("--config: %+v", pin)
	}
}

func TestSetPinLeavesEverythingElse(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"plain", "# top\nversion: 1\naxx: 0.5.0\nrun: {}\n", "# top\nversion: 1\naxx: 0.6.0\nrun: {}\n"},
		{"a comment", "axx: 0.5.0  # keep this\n", "axx: 0.6.0  # keep this\n"},
		{"quotes", "axx: \"0.5.0\"\n", "axx: \"0.6.0\"\n"},
		{"windows", "version: 1\r\naxx: 0.5.0\r\n", "version: 1\r\naxx: 0.6.0\r\n"},
		{"empty", "axx:\nrun: {}\n", "axx: 0.6.0\nrun: {}\n"},
		{"empty with a comment", "axx: # soon\n", "axx: 0.6.0 # soon\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := pinProject(t, tc.before)
			if err := SetPin(file, "0.6.0"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.after {
				t.Errorf("got %q, want %q", got, tc.after)
			}
		})
	}
}

func TestSetPinNeedsALine(t *testing.T) {
	file := pinProject(t, "version: 1\n")
	if err := SetPin(file, "0.6.0"); err == nil {
		t.Error("no error")
	}
}

func TestValidPin(t *testing.T) {
	for _, v := range []string{"0.5.0", "1.2.3", "1.0.0-rc.1"} {
		if !ValidPin(v) {
			t.Errorf("%s refused", v)
		}
	}
	for _, v := range []string{"", "0.5", "v0.5.0", "latest", "nightly", ">=0.5.0", "0.5.x", "../../evil", "https://example.com/axx", "0.5.0/../../x", "0.5.0 "} {
		if ValidPin(v) {
			t.Errorf("%q accepted", v)
		}
	}
}

func TestSchemaTakesThePin(t *testing.T) {
	pinProject(t, "version: 1\naxx: 0.5.0\n")
	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Axx != "0.5.0" {
		t.Errorf("axx %q", cfg.Axx)
	}
	pinProject(t, "version: 1\naxx: latest\n")
	if _, err := Load(LoadOptions{}); err == nil {
		t.Error("axx: latest accepted")
	}
}

// axx reads only the base file's pin, so an overlay's could do nothing.
func TestOverlaysCannotMoveThePin(t *testing.T) {
	refused := func(t *testing.T, opts LoadOptions) {
		t.Helper()
		_, err := Load(opts)
		var ae *axxerr.Error
		if err == nil || !strings.Contains(err.Error(), "belongs in axx.yaml") {
			t.Fatalf("got %v", err)
		}
		if !asAxxErr(err, &ae) || ae.Code != CodeInvalid {
			t.Errorf("code of %v", err)
		}
	}
	t.Run("a profile in the file", func(t *testing.T) {
		pinProject(t, "axx: 0.5.0\nprofiles:\n  old:\n    axx: 0.6.0\n")
		refused(t, LoadOptions{Profile: "old"})
	})
	t.Run("a profile's file", func(t *testing.T) {
		file := pinProject(t, "axx: 0.5.0\n")
		if err := os.WriteFile(filepath.Join(filepath.Dir(file), "axx.old.yaml"), []byte("axx: 0.6.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		refused(t, LoadOptions{Profile: "old"})
	})
	t.Run("axx.local.yaml", func(t *testing.T) {
		file := pinProject(t, "version: 1\n")
		if err := os.WriteFile(filepath.Join(filepath.Dir(file), "axx.local.yaml"), []byte("axx: 0.6.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		refused(t, LoadOptions{})
	})
	t.Run("the same value is no change", func(t *testing.T) {
		file := pinProject(t, "axx: 0.5.0\n")
		if err := os.WriteFile(filepath.Join(filepath.Dir(file), "axx.local.yaml"), []byte("axx: 0.5.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(LoadOptions{}); err != nil {
			t.Error(err)
		}
	})
}

func asAxxErr(err error, target **axxerr.Error) bool {
	e, ok := err.(*axxerr.Error) //nolint:errorlint // Load returns it as it is
	if ok {
		*target = e
	}
	return ok
}
