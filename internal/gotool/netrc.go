package gotool

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// netrcFor is the login and password the user's .netrc has for u's host, as
// the go command sends them to a proxy or a checksum database over HTTPS:
// when GOAUTH, unset or listing netrc, says so.
func netrcFor(u *url.URL) (login, password string, ok bool) {
	if !netrcOn() {
		return "", "", false
	}
	p := netrcPath()
	if p == "" {
		return "", "", false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", "", false
	}
	for _, m := range parseNetrc(string(data)) {
		if m.machine == u.Host || m.machine == u.Hostname() {
			return m.login, m.password, true
		}
	}
	return "", "", false
}

// netrcOn reports whether GOAUTH lets the go command read .netrc: unset, it
// does; otherwise when one of its ;-separated commands is netrc.
func netrcOn() bool {
	auth := goSetting("GOAUTH")
	if auth == "" {
		return true
	}
	for command := range strings.SplitSeq(auth, ";") {
		if strings.TrimSpace(command) == "netrc" {
			return true
		}
	}
	return false
}

// netrcPath is the user's .netrc, as the go command finds it: NETRC, else
// .netrc in the home directory (_netrc first on Windows).
func netrcPath() string {
	if p := os.Getenv("NETRC"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "windows" {
		if p := filepath.Join(home, "_netrc"); fileExists(p) {
			return p
		}
	}
	return filepath.Join(home, ".netrc")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

type netrcMachine struct{ machine, login, password string }

// parseNetrc reads a .netrc's machines as the go command does: machine,
// login and password tokens, macdef's macros skipped, nothing after default.
func parseNetrc(data string) []netrcMachine {
	var out []netrcMachine
	var m netrcMachine
	inMacro := false
	for line := range strings.SplitSeq(data, "\n") {
		if inMacro {
			if strings.TrimSpace(line) == "" {
				inMacro = false
			}
			continue
		}
		f := strings.Fields(line)
		i := 0
		for ; i < len(f)-1; i += 2 {
			switch f[i] {
			case "machine":
				m = netrcMachine{machine: f[i+1]}
			case "login":
				m.login = f[i+1]
			case "password":
				m.password = f[i+1]
			case "macdef":
				inMacro = true
			}
			if m.machine != "" && m.login != "" && m.password != "" {
				out = append(out, m)
				m = netrcMachine{}
			}
		}
		if i < len(f) && f[i] == "default" {
			break
		}
	}
	return out
}
