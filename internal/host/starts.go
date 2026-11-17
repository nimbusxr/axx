package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/packbuild"
	"github.com/nimbusxr/axx/internal/packset"
)

// Starts is what starting the host in the project in dir runs of the
// project's own, besides its services: the packs its axx is built with
// (axx-packs.yaml, the versions of those that are Go modules, and every
// file a pack of the project's own compiles, by its hash), and the commands
// axx.yaml runs before and after `axx host` (hooks), with the project files
// they run. As text, so that a person reads what changed.
//
// A person who starts the host runs these as they are. A host that
// restarts by itself (Follower) compares them with what it started with,
// and asks first when they differ: what an agent wrote there runs on this
// machine only with the person's word.
func Starts(dir string, hooks []config.CommandHook) (string, error) {
	project := realPath(dir)
	var text strings.Builder
	list, err := os.ReadFile(filepath.Join(dir, packset.FileName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fmt.Fprintf(&text, "# %s: none\n", packset.FileName)
	case err != nil:
		return "", err
	default:
		fmt.Fprintf(&text, "# %s: the packs the host's axx is built with\n", packset.FileName)
		writeText(&text, list)
		f, _, err := packset.Load(dir)
		if err != nil {
			return "", err
		}
		entries, err := f.Entries()
		if err != nil {
			return "", err
		}
		lock, err := packset.LoadLock(dir)
		if err != nil {
			return "", err
		}
		if len(lock.Modules) > 0 {
			fmt.Fprintf(&text, "\n# %s: the versions of the packs that are Go modules\n", packset.LockName)
			for _, m := range lock.Modules {
				fmt.Fprintf(&text, "%s %s\n", m.Path, m.Version)
			}
		}
		inputs, err := packbuild.Inputs(dir, entries)
		if err != nil {
			return "", err
		}
		if len(inputs) > 0 {
			text.WriteString("\n# the project's own packs, built into it: each file they compile, by its SHA-256\n")
			for _, p := range inputs {
				b, err := os.ReadFile(p)
				if err != nil {
					return "", err
				}
				sum := sha256.Sum256(b)
				fmt.Fprintf(&text, "%s %s\n", relTo(project, p), hex.EncodeToString(sum[:8]))
			}
		}
	}
	named := map[string]bool{}
	for _, h := range hooks {
		_, when := h.Names()
		line := h.Command.String()
		if h.Shell {
			line += " (through the shell)"
		}
		fmt.Fprintf(&text, "\n# axx.yaml runs %s `axx host`\n%s\n", when, line)
		argv := h.Command.Argv
		if len(argv) == 0 {
			argv = words(h.Command.Line)
		}
		for _, f := range executedFiles(argv, dir, project) {
			if named[f] {
				continue
			}
			named[f] = true
			b, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			if len(b) > maxFileText || !utf8.Valid(b) {
				sum := sha256.Sum256(b)
				fmt.Fprintf(&text, "\n# %s (sha256 %s)\n", relTo(project, f), hex.EncodeToString(sum[:]))
				continue
			}
			fmt.Fprintf(&text, "\n# %s\n", relTo(project, f))
			writeText(&text, b)
		}
	}
	return text.String(), nil
}

// writeText writes b, ending its last line.
func writeText(text *strings.Builder, b []byte) {
	text.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		text.WriteString("\n")
	}
}
