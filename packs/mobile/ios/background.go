package mobileios

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// The notification the simulator's processes take axx's requests by (library/background.m).
const backgroundTaskNotification = "com.nimbusxr.axx.background-task"

// backgroundTaskAnswer is how long the app has to say it ran the task: SpringBoard wakes it first,
// if it is suspended in the background.
const backgroundTaskAnswer = 15 * time.Second

// backgroundTaskRun is how long a task may run once it started: the 30 seconds iOS gives an app
// refresh task, and a few for the app to say how it went.
const backgroundTaskRun = 35 * time.Second

// RunBackgroundTask has iOS run one of the app's background tasks now, as it does when the task's
// time comes, and as Xcode's debugger command does on an iPhone: the task the app scheduled is
// handed to the handler it registered, in the app, which SpringBoard wakes for it if it is
// suspended. A simulator refuses to schedule any task; axx's library, which the simulator's
// processes load, keeps the app's requests as an iPhone does, and runs the task when asked
// (library/background.m). It returns once the app said the task is done (setTaskCompleted).
func (d *running) RunBackgroundTask(ctx context.Context, name string) error {
	if d.dev == nil {
		return fmt.Errorf("axx runs the %s app's background tasks on the simulators it drives, not on a device an Appium server runs", d.app.name)
	}
	tasks, err := backgroundTasks(ctx, d.app.path)
	if err != nil {
		return err
	}
	if !slices.Contains(tasks, name) {
		err := mobilecore.UnknownBackgroundTask(d.app.name, name, tasks)
		return fmt.Errorf("%w (the identifiers its Info.plist permits, BGTaskSchedulerPermittedIdentifiers)", err)
	}
	if status := springboardStatus(d.dev.udid); status == "" {
		return fmt.Errorf("the simulator %s runs without axx's library, which runs apps' background tasks: start it again with axx (shut it down first)", d.dev.name)
	}
	pid, err := appProcess(ctx, d.dev, d.bundleID)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("the %s app is not running: axx runs a background task in the app as it runs (launch it first)", d.app.name)
	}
	status, nonce, err := askLibrary(ctx, d.dev, d.bundleID, pid, name)
	if err != nil {
		return fmt.Errorf("cannot run the %s app's %q background task: %w", d.app.name, name, err)
	}
	switch status {
	case "ran":
		return waitTaskDone(ctx, d.dev, nonce, d.app.name, name)
	case "not scheduled":
		return fmt.Errorf("the %s app has not scheduled its %q background task: iOS runs a task once the app asks for it "+
			"(BGTaskScheduler submit), and once for each time it asks", d.app.name, name)
	case "not registered":
		return fmt.Errorf("the %s app has no handler for its %q background task: iOS runs a task with the handler the app "+
			"registers for it (BGTaskScheduler register), before it finishes launching", d.app.name, name)
	case "running":
		return fmt.Errorf("the %s app's %q background task is running already", d.app.name, name)
	default:
		if method, ok := strings.CutPrefix(status, "unsupported "); ok {
			return fmt.Errorf("cannot run the %s app's %q background task on the simulator %s: its iOS has no %s, which axx runs a task with",
				d.app.name, name, d.dev.name, method)
		}
		return fmt.Errorf("cannot run the %s app's %q background task: axx's library answered %q", d.app.name, name, status)
	}
}

// backgroundTasks are the identifiers of the background tasks an app's Info.plist permits.
func backgroundTasks(ctx context.Context, app string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "plutil", "-extract", "BGTaskSchedulerPermittedIdentifiers", "json", "-o", "-", filepath.Join(app, "Info.plist")) //nolint:gosec // plutil, on the app the scenario registered
	out, err := cmd.Output()
	if err != nil {
		return nil, nil //nolint:nilerr // no such key: the app has no background tasks
	}
	var ids []string
	if err := json.Unmarshal(out, &ids); err != nil {
		return nil, fmt.Errorf("cannot read the background tasks in %s: %w", filepath.Join(app, "Info.plist"), err)
	}
	return ids, nil
}

// appJob is the line launchd lists for a running app: its process, its status, and its label.
var appJob = regexp.MustCompile(`^(\d+)\s+\S+\s+UIKitApplication:([^\[\s]+)`)

// appProcess is the process of the app on the simulator: 0 when it is not running.
func appProcess(ctx context.Context, d *device, bundleID string) (int, error) {
	out, err := d.set.simctl(ctx, "spawn", d.udid, "launchctl", "list")
	if err != nil {
		return 0, fmt.Errorf("cannot list the simulator's processes: %w", err)
	}
	return appProcessIn(out, bundleID), nil
}

func appProcessIn(list, bundleID string) int {
	for _, line := range strings.Split(list, "\n") {
		m := appJob.FindStringSubmatch(strings.TrimSpace(line))
		if m != nil && m[2] == bundleID {
			pid, _ := strconv.Atoi(m[1])
			return pid
		}
	}
	return 0
}

// askLibrary asks axx's library in the simulator to run a task in the app's process, and returns
// its answer and the request's id: it writes the request where the library reads it, notifies
// the simulator's SpringBoard, and waits for the app's answer.
func askLibrary(ctx context.Context, d *device, bundleID string, pid int, task string) (status, nonce string, err error) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	nonce = hex.EncodeToString(b)
	dir := springboardStatuses()
	result := filepath.Join(dir, d.udid+".task-result")
	_ = os.Remove(result)
	_ = os.Remove(filepath.Join(dir, d.udid+".task-done"))
	req := strings.Join([]string{nonce, bundleID, strconv.Itoa(pid), task}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, d.udid+".task"), []byte(req), 0o644); err != nil {
		return "", "", err
	}
	if _, err := d.set.simctl(ctx, "spawn", d.udid, "notifyutil", "-p", backgroundTaskNotification); err != nil {
		return "", "", fmt.Errorf("cannot notify the simulator: %w", err)
	}
	status, ok, err := waitLibrary(ctx, result, nonce, backgroundTaskAnswer)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", fmt.Errorf("the app did not answer in %s: axx's library does not run in it (an app launched before the simulator had the library: launch it again)", backgroundTaskAnswer)
	}
	return status, nonce, nil
}

// waitTaskDone waits for the app to say the task it started is done, as the library writes it.
func waitTaskDone(ctx context.Context, d *device, nonce, app, task string) error {
	how, ok, err := waitLibrary(ctx, filepath.Join(springboardStatuses(), d.udid+".task-done"), nonce, backgroundTaskRun)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("the %s app did not say whether its %q background task is done, in %s", app, task, backgroundTaskRun)
	case how != "done":
		return fmt.Errorf("the %s app's %q background task was not done in the 30 seconds iOS gives one: "+
			"iOS takes it as done when the app says so (setTaskCompleted)", app, task)
	}
	return nil
}

// waitLibrary waits, up to wait, for the library to write a line for the request nonce in a file,
// and returns the rest of the line.
func waitLibrary(ctx context.Context, file, nonce string, wait time.Duration) (string, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		if data, err := os.ReadFile(file); err == nil {
			if got, rest, _ := strings.Cut(strings.TrimSpace(string(data)), " "); got == nonce {
				return rest, true, nil
			}
		}
		if time.Now().After(deadline) {
			return "", false, nil
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
