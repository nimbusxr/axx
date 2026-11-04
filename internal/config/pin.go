package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Pin is the version of axx a project's axx.yaml names (`axx: 0.5.0`). File is
// empty when it names none.
type Pin struct {
	Version string
	File    string
}

// pinLine is the top-level `axx:` line. Any axx reads it, older or newer than the
// version it names, so it is read as a line: the rest of the file follows that
// version's schema, which this axx may not know.
var pinLine = regexp.MustCompile(`(?m)^axx:[ \t]*(?:"([^"\r\n]*)"|'([^'\r\n]*)'|([^\s#'"][^\s]*))?[ \t]*(?:#[^\r\n]*)?\r?$`)

// pinVersion is what a pin may say: one release, never a range, a channel or an
// address.
var pinVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// ValidPin reports whether v is a version a project can name.
func ValidPin(v string) bool { return pinVersion.MatchString(v) }

// ReadPin reads the pin of the axx.yaml that Load would use for path (as given
// to --config). A file it cannot find names none: the command reports that.
func ReadPin(path string) (Pin, error) {
	wd, err := os.Getwd()
	if err != nil {
		return Pin{}, err
	}
	file, err := discover(LoadOptions{Path: path, WorkDir: wd})
	if err != nil || file == "" {
		return Pin{}, nil //nolint:nilerr // the command reports a --config it cannot find
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Pin{}, nil //nolint:nilerr // so does the command, for a file it cannot read
	}
	m := pinLine.FindSubmatch(data)
	if m == nil {
		return Pin{}, nil
	}
	return Pin{Version: string(m[1]) + string(m[2]) + string(m[3]), File: file}, nil
}

// SetPin moves the pin in file to v, leaving every other byte as it is.
func SetPin(file, v string) error {
	st, err := os.Stat(file)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	loc := pinLine.FindSubmatchIndex(data)
	if loc == nil {
		return fmt.Errorf("%s has no axx: line", file)
	}
	// Where the value goes: over the one it has, or after `axx:` when it has none.
	start, end, text := loc[0]+len("axx:"), loc[0]+len("axx:"), " "+v
	for g := 1; g <= 3; g++ {
		if s, e := loc[2*g], loc[2*g+1]; e > s {
			start, end, text = s, e, v
		}
	}
	return os.WriteFile(file, slices.Concat(data[:start], []byte(text), data[end:]), st.Mode().Perm())
}

// checkPinNotOverlaid refuses an overlay that changes the base file's pin: axx
// reads only the base file, so the overlay's would do nothing.
func checkPinNotOverlaid(base, merged string) error {
	if base == merged {
		return nil
	}
	return axxerr.New(CodeInvalid, exitcode.Usage, "`axx:` belongs in axx.yaml: a profile, axx.local.yaml or --set cannot change the version a project runs on").
		WithHint("to try another version for a while, set AXX_USE_VERSION=<version>")
}
