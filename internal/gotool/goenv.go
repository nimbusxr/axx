package gotool

import (
	"bytes"
	"os"
	"path/filepath"
)

// netSettings are Go's settings for where modules come from, how they are
// checked, and the credentials that reach them: an organization's proxy, its
// checksum database, its private modules.
var netSettings = []string{"GOPROXY", "GOSUMDB", "GONOSUMDB", "GONOPROXY", "GOPRIVATE", "GOINSECURE", "GOAUTH", "GOVCS"}

// goSetting is one of Go's settings as the go command reads it: the
// environment's, else the value `go env -w` wrote in the user's Go
// configuration file. "" is Go's default.
func goSetting(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return goEnvFile()[name]
}

// envFileSettings are the network settings the user's Go configuration file
// has and the environment does not set, as NAME=value: a build reads no
// configuration file (GOENV=off), so that nothing in it changes how axx
// builds, but takes these.
func envFileSettings() []string {
	file := goEnvFile()
	var env []string
	for _, name := range netSettings {
		if v, ok := file[name]; ok && os.Getenv(name) == "" {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// goEnvFile is the user's Go configuration file, which `go env -w` writes:
// GOENV, else go/env in the user's configuration directory. It is read as the
// go command reads it: NAME=value lines, others ignored.
func goEnvFile() map[string]string {
	p := os.Getenv("GOENV")
	if p == "off" {
		return nil
	}
	if p == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil
		}
		p = filepath.Join(dir, "go", "env")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		i := bytes.IndexByte(line, '=')
		if i < 0 || line[0] < 'A' || 'Z' < line[0] {
			continue
		}
		m[string(line[:i])] = string(bytes.TrimSuffix(line[i+1:], []byte("\r")))
	}
	return m
}
