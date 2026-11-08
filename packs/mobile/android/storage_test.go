package mobileandroid

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// An emulator is booted once its shared storage is mounted too: Android says it has booted a
// second or so before, while it still checks the volume, and the first documents pushed then can
// go missing from the file picker.
func TestBootedOnceTheSharedStorageIsMounted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	adb := filepath.Join(dir, "adb")
	asked := filepath.Join(dir, "asked")
	script := `#!/bin/sh
case "$*" in
*"getprop sys.boot_completed"*) echo 1 ;;
*"pm path android"*) echo package:/system/framework/framework-res.apk ;;
*"dumpsys connectivity"*) echo "Active default network: 100" ;;
*"sm list-volumes all"*)
  echo x >> ` + asked + `
  echo "private mounted null"
  if [ "$(wc -l < ` + asked + `)" -lt 2 ]; then echo "emulated;0 checking null"; else echo "emulated;0 mounted null"; fi ;;
esac
`
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&sdk{adb: adb}).waitBooted(t.Context(), "emulator-5554", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(asked)
	if n := strings.Count(string(b), "x"); n != 2 {
		t.Errorf("asked for the volumes %d times, not until the emulated one was mounted", n)
	}

	// An Android that cannot say is taken as mounted.
	if err := os.WriteFile(adb, []byte("#!/bin/sh\ncase \"$*\" in\n*\"sm list-volumes\"*) echo 'sm: not found' >&2; exit 127 ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !(&sdk{adb: adb}).storageMounted(t.Context(), "emulator-5554") {
		t.Error("an Android without sm is taken as not mounted")
	}
}

// A failed scenario that picks documents says what the device has in Downloads, on its storage
// and in the media store, and what the media store logged: which of them lost a document the
// file picker does not list.
func TestAFailedScenarioSaysWhatDownloadsHas(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	adb := filepath.Join(dir, "adb")
	script := `#!/bin/sh
case "$*" in
*"pm list packages -U"*) echo 'package:example.parcels.courier uid:10190' ;;
*"--uid=10190"*) echo '11-07 19:20:40.100 4211 4211 I Courier: attaching a document' ;;
*"main,system,crash"*) printf '%s\n' \
  '11-07 19:20:39.000  900  950 I ModernMediaScanner: Scanned /storage/emulated/0/Download/parcel-at-door.jpg' \
  '11-07 19:20:39.100  900  950 W MediaProvider: deleting /storage/emulated/0/Download/safe-place-PX-MOB-9481.pdf' \
  '11-07 19:20:39.200  600  700 D WifiService: scan done' ;;
*"ls -la"*) echo '-rw-rw---- 1 u0_a183 media_rw 1824 2026-11-07 19:20 parcel-at-door.jpg' ;;
*"media/external/file"*) echo 'Row: 0 _id=31, _display_name=parcel-at-door.jpg, _data=/storage/emulated/0/Download/parcel-at-door.jpg, _size=1824, date_modified=1794080440' ;;
*"media/external/downloads"*) echo 'No result found.' ;;
esac
`
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &running{
		app: &app{name: "courier", documents: []string{"safe-place-PX-MOB-9481.pdf", "parcel-at-door.jpg"}},
		pkg: "example.parcels.courier", sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554", avd: "parcels-pixel"},
		resetAt: time.Unix(1794080400, 0),
	}
	log := d.AppLog(t.Context())
	for _, want := range []string{
		"the system's log of it, of where the phone is, and of the media store",
		"ModernMediaScanner: Scanned /storage/emulated/0/Download/parcel-at-door.jpg",
		"MediaProvider: deleting /storage/emulated/0/Download/safe-place-PX-MOB-9481.pdf",
		"the files in the device's Downloads (/sdcard/Download):\n-rw-rw---- 1 u0_a183 media_rw 1824 2026-11-07 19:20 parcel-at-door.jpg",
		"the media store's rows of Downloads (content://media/external/file):\nRow: 0 _id=31, _display_name=parcel-at-door.jpg",
		"the media store's rows of Downloads (content://media/external/downloads):\nNo result found.",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the app's log:\n%s\nwants %q", log, want)
		}
	}
	if strings.Contains(log, "WifiService") {
		t.Errorf("the app's log has the rest of the device's:\n%s", log)
	}

	// A scenario without documents says nothing of Downloads.
	d.app.documents = nil
	if log := d.AppLog(t.Context()); strings.Contains(log, "Downloads") || strings.Contains(log, "MediaProvider") {
		t.Errorf("the app's log of a scenario without documents:\n%s", log)
	}
}
