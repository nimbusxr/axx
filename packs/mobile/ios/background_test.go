package mobileios

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestTheAppsProcessIsFoundInLaunchdsList(t *testing.T) {
	list := `PID	Status	Label
-	0	com.apple.backboardd
80876	0	UIKitApplication:example.parcels.courier[e876][rb-legacy]
81002	0	UIKitApplication:example.parcels.courier.widget[1a2b][rb-legacy]
75996	0	com.apple.SpringBoard`
	if got := appProcessIn(list, "example.parcels.courier"); got != 80876 {
		t.Errorf("courier: %d", got)
	}
	if got := appProcessIn(list, "example.parcels.depot"); got != 0 {
		t.Errorf("an app not running: %d", got)
	}
}

func TestTheAppsBackgroundTasksAreTheOnesItsInfoPlistPermits(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil reads Info.plist on a Mac")
	}
	app := filepath.Join(t.TempDir(), "Courier.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>example.parcels.courier</string>
<key>BGTaskSchedulerPermittedIdentifiers</key><array><string>example.parcels.courier.round-reminder</string></array>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, err := backgroundTasks(context.Background(), app)
	if err != nil || !slices.Equal(tasks, []string{"example.parcels.courier.round-reminder"}) {
		t.Errorf("tasks = %v, %v", tasks, err)
	}
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict></dict></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if tasks, err := backgroundTasks(context.Background(), app); err != nil || len(tasks) != 0 {
		t.Errorf("an app without background tasks: %v, %v", tasks, err)
	}
}
