package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"empty", "", nil},
		{"blank", " \t\n ", nil},
		{"words", "java -jar app.jar", []string{"java", "-jar", "app.jar"}},
		{"extra whitespace", "  a\tb \n c  ", []string{"a", "b", "c"}},
		{"quoted segment", `java -jar "my app.jar" --verbose`, []string{"java", "-jar", "my app.jar", "--verbose"}},
		{"empty quotes", `echo ""`, []string{"echo", ""}},
		{"adjacent quotes", `"a b""c d"`, []string{"a b", "c d"}},
		{"quote inside word is literal", `--opt="a b"`, []string{`--opt="a`, `b"`}},
		{"unterminated quote", `echo "a b`, []string{"echo", `"a`, "b"}},
		{"single quotes are literal", `echo 'a b'`, []string{"echo", "'a", "b'"}},
		{"no expansion", `echo $HOME *.go`, []string{"echo", "$HOME", "*.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Tokenize(tt.line); !slices.Equal(got, tt.want) {
				t.Errorf("Tokenize(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestCommandArgv(t *testing.T) {
	tests := []struct {
		name    string
		cmd     config.Command
		shell   bool
		want    []string
		wantErr bool
	}{
		{"argv verbatim", config.Command{Argv: []string{"my tool", `"x"`, "a b"}}, false, []string{"my tool", `"x"`, "a b"}, false},
		{"line tokenized", config.Command{Line: `run "a b" c`}, false, []string{"run", "a b", "c"}, false},
		{"shell line", config.Command{Line: "echo $HOME | wc"}, true, shellArgv("echo $HOME | wc"), false},
		{"shell argv joined", config.Command{Argv: []string{"echo", "hi"}}, true, shellArgv("echo hi"), false},
		{"empty", config.Command{}, false, nil, true},
		{"blank line", config.Command{Line: "   "}, false, nil, true},
		{"empty program", config.Command{Argv: []string{""}}, false, nil, true},
		{"empty shell", config.Command{Line: " "}, true, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commandArgv(tt.cmd, tt.shell)
			if tt.wantErr {
				if !errors.Is(err, errEmptyCommand) {
					t.Fatalf("err = %v, want errEmptyCommand", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("argv = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppEnv(t *testing.T) {
	got := appEnv([]string{"A=1", "B=2"}, map[string]string{"C": "3", "A": "9"})
	want := []string{"A=1", "B=2", "A=9", "C=3"}
	if !slices.Equal(got, want) {
		t.Errorf("appEnv = %q, want %q", got, want)
	}
}

func TestResolveDir(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		dir     string
		want    string
		wantErr bool
	}{
		{"", base, false},
		{"svc", filepath.Join(base, "svc"), false},
		{filepath.Join(base, "svc"), filepath.Join(base, "svc"), false},
		{"missing", "", true},
		{"file", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			got, err := resolveDir(config.App{Name: "api", Dir: tt.dir}, base)
			if tt.wantErr {
				wantCode(t, err, CodeBadDir)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("resolveDir = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
