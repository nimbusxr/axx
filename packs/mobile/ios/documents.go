package mobileios

import (
	"context"
	"fmt"
	"io"
	"os"
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
	return d.putDocumentsIn(group)
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

// localStorage is the folder of the simulator's filesStorageGroup.
func localStorage(ctx context.Context, set simSet, udid string) (string, error) {
	out, err := set.simctl(ctx, "get_app_container", udid, "com.apple.DocumentsApp", "groups")
	if err != nil {
		return "", fmt.Errorf("cannot find the simulator's files (On My iPhone): %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if group, path, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && group == filesStorageGroup {
			return strings.TrimSpace(path), nil
		}
	}
	return "", fmt.Errorf("the simulator has no files On My iPhone: the Files app's groups are %q", out)
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
