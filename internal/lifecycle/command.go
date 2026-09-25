package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

// tokenRE splits a command line the way the original runner did: a
// double-quoted segment is one token (quotes stripped), anything else splits
// on whitespace. There is no escaping and no shell expansion.
var tokenRE = regexp.MustCompile(`"([^"]*)"|(\S+)`)

// Tokenize splits a command line into argv without a shell. Double-quoted
// segments become a single token with the quotes removed; everything else is
// split on whitespace. Tokenize("java -jar \"my app.jar\"") returns
// ["java", "-jar", "my app.jar"].
func Tokenize(line string) []string {
	var argv []string
	for _, m := range tokenRE.FindAllStringSubmatchIndex(line, -1) {
		if m[2] >= 0 {
			argv = append(argv, line[m[2]:m[3]])
		} else {
			argv = append(argv, line[m[4]:m[5]])
		}
	}
	return argv
}

// errEmptyCommand reports a command with no words.
var errEmptyCommand = errors.New("empty command")

// commandArgv turns a configured command into argv. An argv list is used
// verbatim; a string is tokenized. With shell set, the command line (an argv
// list is joined with spaces) runs through /bin/sh -c (cmd /C on Windows).
func commandArgv(c config.Command, shell bool) ([]string, error) {
	if shell {
		line := c.Line
		if line == "" {
			line = strings.Join(c.Argv, " ")
		}
		if strings.TrimSpace(line) == "" {
			return nil, errEmptyCommand
		}
		return shellArgv(line), nil
	}
	argv := c.Argv
	if len(argv) == 0 {
		argv = Tokenize(c.Line)
	}
	if len(argv) == 0 || argv[0] == "" {
		return nil, errEmptyCommand
	}
	return append([]string(nil), argv...), nil
}

// appEnv returns base followed by the app's own variables, sorted by name so
// the environment is deterministic. Later entries win.
func appEnv(base []string, extra map[string]string) []string {
	env := append([]string(nil), base...)
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}
	return env
}

// resolveDir returns the app's absolute working directory: App.Dir relative
// to the config directory (the config directory itself when Dir is empty).
func resolveDir(app config.App, configDir string) (string, error) {
	dir := app.Dir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(configDir, dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", envErr(CodeBadDir, "app %s: cannot resolve working directory %q: %v", app.Name, app.Dir, err).
			WithHint("check apps.%s.dir", app.Name)
	}
	info, err := os.Stat(abs)
	switch {
	case err != nil:
		return "", envErr(CodeBadDir, "app %s: working directory %s does not exist", app.Name, abs).
			WithHint("check apps.%s.dir (it is relative to the directory of axx.yaml)", app.Name)
	case !info.IsDir():
		return "", envErr(CodeBadDir, "app %s: working directory %s is not a directory", app.Name, abs).
			WithHint("check apps.%s.dir (it is relative to the directory of axx.yaml)", app.Name)
	}
	return abs, nil
}

// displayArgv renders argv for messages, quoting words with spaces.
func displayArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"") {
			parts[i] = fmt.Sprintf("%q", a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
