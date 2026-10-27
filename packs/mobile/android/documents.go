package mobileandroid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// downloadsDir is the device's Downloads, which the system's file picker shows.
	downloadsDir = "/sdcard/Download"
	// documentsList names, on the device, the documents axx put in Downloads: a device axx did not
	// start has files of its own there, and a scenario takes out only what one before put.
	documentsList = "/data/local/tmp/axx-documents"
	// stagedDocument is where a document waits on the device to be written over another of its
	// name in Downloads.
	stagedDocument = "/data/local/tmp/axx-document"
)

// putDocuments takes out what a scenario before left in Downloads, and puts the registration's
// documents there, for the app to pick, where the media store indexes them as it does a download.
// A document of a scenario before's that this one has too is left as it is, or written over.
// An emulator axx started (read-only: nothing on it outlives the run) has nothing else there:
// Downloads is emptied, files an app saved there included, and the system's file picker starts
// afresh. A device axx did not start keeps its own files, and loses only the documents axx put.
func (d *running) putDocuments(ctx context.Context) error {
	serial := d.dev.serial
	var left []string
	if d.dev.avd != "" {
		out, err := d.sdk.shell(ctx, serial, "ls -1A "+q(downloadsDir)+" 2>/dev/null || true")
		if err != nil {
			return fmt.Errorf("cannot read the device's Downloads: %w", err)
		}
		left = lines(out)
	} else if out, err := d.sdk.shell(ctx, serial, "cat "+q(documentsList)+" 2>/dev/null || true"); err == nil {
		left = lines(out)
	}
	if d.dev.avd != "" {
		if err := d.resetFilePicker(ctx); err != nil {
			return err
		}
	}
	// A document of a name this scenario puts there again is not taken out: the media store
	// forgets a file taken off the device a while after, by its path, and would forget the new
	// one of that path too (the first documents pushed, of the scenario before's).
	pushing := map[string]bool{}
	for _, f := range d.app.documents {
		pushing[filepath.Base(f)] = true
	}
	left = slices.DeleteFunc(left, func(name string) bool { return pushing[name] })
	if len(left) > 0 {
		// Out of the media store, then off the device: the store forgets a file it is told is
		// gone, not one that just disappeared. It has them under their real path
		// (/storage/emulated/0/...), not /sdcard's.
		var where, rm []string
		for _, name := range left {
			where = append(where, "_data LIKE "+sqlString("%/Download/"+name)+" OR _data LIKE "+sqlString("%/Download/"+name+"/%"))
			rm = append(rm, q(downloadsDir+"/"+name))
		}
		if _, err := d.sdk.shell(ctx, serial, "content delete --uri content://media/external/file --where "+q(strings.Join(where, " OR "))); err != nil {
			return fmt.Errorf("cannot take the documents of the scenario before out of Downloads: %w", err)
		}
		if _, err := d.sdk.shell(ctx, serial, "rm -rf "+strings.Join(rm, " ")); err != nil {
			return fmt.Errorf("cannot take the documents of the scenario before out of Downloads: %w", err)
		}
		// The store's forgetting them done before files of their names come back: one still
		// under way would forget those too (the first put, of a scenario's documents the one
		// before had).
		left := "content query --uri content://media/external/file --projection _id --where " + q(strings.Join(where, " OR "))
		for deadline := time.Now().Add(30 * time.Second); ; {
			out, err := d.sdk.shell(ctx, serial, left)
			if err == nil && !strings.Contains(out, "Row:") {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("the device's Downloads did not let go of the documents of the scenario before in 30s")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	if len(d.app.documents) == 0 {
		_, err := d.sdk.shell(ctx, serial, "rm -f "+q(documentsList))
		return err
	}
	if _, err := d.sdk.shell(ctx, serial, "mkdir -p "+q(downloadsDir)); err != nil {
		return err
	}
	names := make([]string, len(d.app.documents))
	quoted := make([]string, len(names))
	sums := map[string]string{}
	for i, f := range d.app.documents {
		names[i] = filepath.Base(f)
		quoted[i] = sqlString(names[i])
		sum, err := fileSum(f)
		if err != nil {
			return fmt.Errorf("cannot read %s for the %s app's documents: %w", f, d.app.name, err)
		}
		sums[names[i]] = sum
	}
	there, err := d.downloaded(ctx, names)
	if err != nil {
		return fmt.Errorf("cannot read the device's Downloads: %w", err)
	}
	for i, f := range d.app.documents {
		dst := downloadsDir + "/" + names[i]
		switch there[names[i]] {
		case sums[names[i]]:
			// The same document, there from a scenario before: it stays as it is.
		case "":
			if _, err := d.sdk.run(ctx, serial, "push", f, dst); err != nil {
				return fmt.Errorf("cannot put %s in the device's Downloads for the %s app: %w", f, d.app.name, err)
			}
		default:
			// Another document of its name: written over where it is, as a push would take that
			// one off the device first (adbd does), and the media store would forget the new one.
			if _, err := d.sdk.run(ctx, serial, "push", f, stagedDocument); err != nil {
				return fmt.Errorf("cannot put %s in the device's Downloads for the %s app: %w", f, d.app.name, err)
			}
			if _, err := d.sdk.shell(ctx, serial, "cat "+q(stagedDocument)+" > "+q(dst)+" && rm -f "+q(stagedDocument)); err != nil {
				return fmt.Errorf("cannot put %s in the device's Downloads for the %s app: %w", f, d.app.name, err)
			}
		}
		if _, err := d.sdk.shell(ctx, serial, "am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d "+q("file://"+dst)); err != nil {
			return err
		}
	}
	// The scan is the media store's own work, after the broadcast: the app is not started before
	// the picker can show every document, each of a row of its own (a document's path is never
	// taken away, so a row of its name is its own).
	rows := "content query --uri content://media/external/file --projection _display_name --where " +
		q("_display_name IN ("+strings.Join(quoted, ", ")+") AND _data LIKE '%/Download/%'")
	for deadline := time.Now().Add(30 * time.Second); ; {
		out, err := d.sdk.shell(ctx, serial, rows)
		if err == nil && listed(out, names) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the device's Downloads did not take the %s app's documents in 30s (%s)", d.app.name, strings.Join(names, ", "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	quotedNames := make([]string, len(names))
	for i, n := range names {
		quotedNames[i] = q(n)
	}
	if _, err := d.sdk.shell(ctx, serial, "printf '%s\\n' "+strings.Join(quotedNames, " ")+" > "+q(documentsList)); err != nil {
		return err
	}
	d.resets = append(d.resets, "documents in Downloads: "+strings.Join(names, ", "))
	return nil
}

// downloaded gives the SHA-256 sums of the files of these names in the device's Downloads, of
// those it has.
func (d *running) downloaded(ctx context.Context, names []string) (map[string]string, error) {
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = q("./" + n)
	}
	out, err := d.sdk.shell(ctx, d.dev.serial, "cd "+q(downloadsDir)+" 2>/dev/null && sha256sum "+strings.Join(paths, " ")+" 2>/dev/null; true")
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	for _, line := range lines(out) {
		if sum, path, ok := strings.Cut(line, "  ./"); ok {
			sums[path] = sum
		}
	}
	return sums, nil
}

// fileSum gives a file's SHA-256 sum.
func fileSum(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// listed tells whether the media store's rows (a content query's, of _display_name) have every
// one of these names.
func listed(rows string, names []string) bool {
	seen := map[string]bool{}
	for _, line := range lines(rows) {
		if _, name, ok := strings.Cut(line, " _display_name="); ok {
			seen[name] = true
		}
	}
	for _, n := range names {
		if !seen[n] {
			return false
		}
	}
	return true
}

// resetFilePicker makes the system's file picker start as on a new phone: it keeps, for each app,
// where it last picked a file, and opens there next time, so a scenario would open it where one
// before left it. Its package is Google's or the open-source one, as the system image has it.
func (d *running) resetFilePicker(ctx context.Context) error {
	out, err := d.sdk.shell(ctx, d.dev.serial, "pm list packages documentsui")
	if err != nil {
		return err
	}
	for _, line := range lines(out) {
		pkg, ok := strings.CutPrefix(line, "package:")
		if !ok {
			continue
		}
		if _, err := d.sdk.shell(ctx, d.dev.serial, "pm clear "+q(pkg)); err != nil {
			return fmt.Errorf("cannot reset the system's file picker (%s): %w", pkg, err)
		}
	}
	return nil
}

// lines are the non-empty lines of a command's output.
func lines(out string) []string {
	var l []string
	for _, s := range strings.Split(out, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			l = append(l, s)
		}
	}
	return l
}

// sqlString is s as an SQL string literal.
func sqlString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
