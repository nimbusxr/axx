package mobileios

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

const (
	// filesStorageGroup is the app group of the files the phone keeps on itself, which the Files
	// app and every app's document picker show as On My iPhone.
	filesStorageGroup = "group.com.apple.FileProvider.LocalStorage"
	// onMyIPhone is the folder of that group they show.
	onMyIPhone = "File Provider Storage"
	// documentsList names, in the group, the documents axx put On My iPhone: a simulator of the
	// project's has files of its own there, and a scenario takes out only what one before put.
	documentsList = ".axx-documents"
)

// putDocuments takes out what a scenario before left On My iPhone, and puts the registration's
// documents there, for the app to pick. A simulator axx made (or keeps for runs) has nothing else
// there: the folder is emptied, files an app saved there included. A simulator of the project's
// keeps its own files, and loses only the documents axx put.
func (d *running) putDocuments(ctx context.Context) error {
	group, err := localStorage(ctx, d.dev.set, d.dev.udid)
	if err != nil {
		return err
	}
	if err := d.putDocumentsIn(group); err != nil {
		return err
	}
	d.forgetPickerPlace(ctx)
	if len(d.app.documents) > 0 {
		d.takeInDocuments(ctx, group)
	}
	return nil
}

// takeInDocuments has iOS take in the documents put On My iPhone before the app runs: axx's
// library, in a process axx starts in the simulator, bookmarks each, as the document picker does
// for the app that picks one. The first bookmark a simulator makes starts iOS's resolver of
// documents for apps, which sets up its records and its store of document ids (revisiond):
// left to the picker on a new simulator (GitHub's runner makes one each run), that came as an
// app picked a document, and the app got none ("The file doesn't exist", for the file the
// picker showed). A simulator that cannot take them in still has them: the run goes on.
func (d *running) takeInDocuments(ctx context.Context, group string) {
	lib, err := simulatorLibrary(ctx, d.pool.suite)
	if err == nil {
		paths := make([]string, len(d.app.documents))
		for i, f := range d.app.documents {
			paths[i] = filepath.Join(group, onMyIPhone, filepath.Base(f))
		}
		// launchctl, of the simulator's own, runs no further than the library, which ends it.
		cmd := exec.CommandContext(ctx, "xcrun", append([]string{"simctl"}, d.dev.set.args("spawn", d.dev.udid, "launchctl", "version")...)...) //nolint:gosec // simctl, with arguments axx builds
		cmd.Env = append(os.Environ(), "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="+lib, "SIMCTL_CHILD_AXX_BOOKMARK="+strings.Join(paths, "\n"))
		var out []byte
		if out, err = cmd.CombinedOutput(); err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if err != nil {
		d.pool.suite.Logger().Warn("iOS did not take in the documents put On My iPhone: the app may get none when it picks one", "simulator", d.dev.name, "error", err)
	}
}

// pickerPlaces are the keys under which iOS keeps, for an app, where its document picker was
// last (in com.apple.DocumentManager.defaults, the simulator's own preferences, not the app's).
// They outlive the app: installed afresh, it opens its picker where a scenario before left it,
// in On My iPhone, say, where the next scenario expects Browse (iOS 26.5).
var pickerPlaces = []string{
	"-com.apple.DocumentManager.Role.Picker-DOCUserDefaultsSceneState",
	"-com.apple.DocumentManager.Role.Picker-DOCUserDefaultsMostRecentSceneState",
}

// forgetPickerPlace has the app's document picker open where a new phone's does, whatever a
// scenario before did with it. A key iOS has not written yet is nothing to forget.
func (d *running) forgetPickerPlace(ctx context.Context) {
	for _, key := range pickerPlaces {
		_, _ = d.dev.set.simctl(ctx, "spawn", d.dev.udid, "defaults", "delete", "com.apple.DocumentManager.defaults", d.bundleID+key)
	}
}

// putDocumentsIn is putDocuments in the simulator's filesStorageGroup, at group.
func (d *running) putDocumentsIn(group string) error {
	dir, list := filepath.Join(group, onMyIPhone), filepath.Join(group, documentsList)
	var left []string
	if d.dev.made || d.dev.kept {
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, e := range entries {
			left = append(left, e.Name())
		}
	} else if b, err := os.ReadFile(list); err == nil {
		left = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	for _, name := range left {
		if name == "" || name != filepath.Base(name) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("cannot take %s, which a scenario before put On My iPhone, out of the simulator's files: %w", name, err)
		}
	}
	if len(d.app.documents) == 0 {
		if err := os.Remove(list); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, len(d.app.documents))
	for i, f := range d.app.documents {
		names[i] = filepath.Base(f)
		if err := copyFile(f, filepath.Join(dir, names[i])); err != nil {
			return fmt.Errorf("cannot put %s On My iPhone for the %s app: %w", f, d.app.name, err)
		}
	}
	if err := os.WriteFile(list, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	d.resets = append(d.resets, "documents On My iPhone: "+strings.Join(names, ", "))
	return nil
}

// localStorage is the folder of the simulator's filesStorageGroup. simctl names it, but for a
// simulator cloned from another, as axx makes them, it names the original's: the clone keeps the
// original's records of its folders. The clone's own is the folder of that name in its own data,
// where its Files app looks.
func localStorage(ctx context.Context, set simSet, udid string) (string, error) {
	out, err := set.simctl(ctx, "get_app_container", udid, "com.apple.DocumentsApp", "groups")
	if err != nil {
		return "", fmt.Errorf("cannot find the simulator's files (On My iPhone): %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if group, path, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && group == filesStorageGroup {
			sims, err := simulators(ctx, set)
			if err != nil {
				return "", err
			}
			if s, ok := findUDID(sims, udid); ok && s.DataPath != "" {
				return inDataOf(s.DataPath, strings.TrimSpace(path)), nil
			}
			return strings.TrimSpace(path), nil
		}
	}
	return "", fmt.Errorf("the simulator has no files On My iPhone: the Files app's groups are %q", out)
}

// inDataOf is the folder simctl named, in a simulator's data folder (data) whatever simulator's
// it named: the same path under data. Both are a Mac's paths.
func inDataOf(data, named string) string {
	_, rel, ok := strings.Cut(named, "/data/Containers/")
	if !ok {
		return named
	}
	return path.Join(data, "Containers", rel)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
