package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
	"github.com/nimbusxr/axx/internal/version"
)

const (
	envNoReminder = "AXX_NO_UPGRADE_REMINDER"
	// A person is reminded of a newer release at most this often, however many
	// come out: axx releases often, and a pin is meant to stay put.
	remindEvery = 7 * 24 * time.Hour
	// The list of releases is read at most this often, in the background.
	checkEvery   = 24 * time.Hour
	checkTimeout = 10 * time.Second
	// How long a command that is done waits for that reading, so that a short run
	// can still finish it. It happens at most once a day, for a person at a terminal.
	checkGrace = time.Second
)

// reminderState is what the reminder remembers between runs.
type reminderState struct {
	Checked time.Time `json:"checked"`
	Shown   time.Time `json:"shown"`
	Newest  string    `json:"newest,omitempty"`
}

func reminderPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "axx", "upgrade-reminder.json"), nil
}

func readReminder() (reminderState, string) {
	var st reminderState
	path, err := reminderPath()
	if err != nil {
		return st, ""
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st, path
}

func writeReminder(path string, st reminderState) {
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// stderrIsTerminal is a variable so that tests can be a person at a terminal.
var stderrIsTerminal = func() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// wantsReminder is whether this command line is a person running their tests, the
// only place a reminder belongs: never an agent, CI, a script or --json.
func (a *App) wantsReminder(args []string, info version.Info) bool {
	if peekArgs(args).command != "run" || slices.Contains(args, "--json") || info.Channel == "dev" {
		return false
	}
	for _, off := range []string{envNoReminder, "CI", envUseVersion, selfupdate.EnvDownloadURL} {
		if a.Env(off) != "" {
			return false
		}
	}
	return stderrIsTerminal() && !a.detectAgent()
}

// startUpgradeReminder reads the list of releases in the background, when it is
// time to, and gives what to call with the command's exit code: after a run that
// passed, it says once in a while that a newer release is out. Whatever goes wrong
// with this stays silent: a reminder is never a reason for a run to wait or fail.
func (a *App) startUpgradeReminder(ctx context.Context, args []string) func(code int) {
	info := version.Get()
	if !a.wantsReminder(args, info) {
		return func(int) {}
	}
	checked := make(chan struct{})
	if st, path := readReminder(); path != "" && time.Since(st.Checked) >= checkEvery {
		go func() {
			defer close(checked)
			ctx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			env, err := newUpgradeEnv()
			if err != nil {
				return
			}
			client := *env.client
			client.Token = "" // nothing of the person's goes with a question nobody asked
			newest := ""
			if releases, err := client.Releases(ctx); err == nil {
				if r := selfupdate.Newest(releases, runtime.GOOS, runtime.GOARCH); r != nil {
					newest = r.Version()
				}
			}
			st, path := readReminder()
			st.Checked = time.Now()
			if newest != "" {
				st.Newest = newest
			}
			writeReminder(path, st)
		}()
	} else {
		close(checked)
	}
	return func(code int) {
		select {
		case <-checked:
		case <-time.After(checkGrace):
		}
		if code != int(exitcode.OK) {
			return
		}
		st, path := readReminder()
		note := a.reminderLine(st, info.Version, args)
		if note == "" || time.Since(st.Shown) < remindEvery {
			return
		}
		fmt.Fprintln(a.Stderr, note)
		st.Shown = time.Now()
		writeReminder(path, st)
	}
}

// reminderLine says that a newer release is out, if one is known to be.
func (a *App) reminderLine(st reminderState, current string, args []string) string {
	if st.Newest == "" || selfupdate.Compare(st.Newest, current) <= 0 {
		return ""
	}
	if pin, err := config.ReadPin(peekArgs(args).config); err == nil && pin.File != "" {
		return fmt.Sprintf("axx %s is out, and this project is on %s: `axx upgrade` moves it and says what changed", st.Newest, current)
	}
	return fmt.Sprintf("axx %s is out, and you have %s: `axx upgrade` installs it and says what changed", st.Newest, current)
}

// newerRelease is what `axx doctor` says of a newer release, from what the reminder
// last read: doctor does not go to the network for it.
func (a *App) newerRelease(current string) string {
	st, _ := readReminder()
	if st.Newest == "" || selfupdate.Compare(st.Newest, current) <= 0 {
		return ""
	}
	return fmt.Sprintf("axx %s is out, and this is %s: `axx upgrade` says what changed", st.Newest, current)
}
