package mobilecore

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// BackgroundTasks is a device whose system can run one of an app's background tasks now.
type BackgroundTasks interface {
	// RunBackgroundTask has the system run the app's background task with this name now, as it
	// does when the task's time comes: the app's own code runs, in the app, and it returns once the
	// task is done. The name is the app's own: on Android, its WorkManager work's unique name or one
	// of its tags; on iOS, the task's identifier.
	RunBackgroundTask(ctx context.Context, name string) error
}

// UnknownBackgroundTask is the error of a name that names none of the app's background tasks:
// it lists those the app has.
func UnknownBackgroundTask(app, name string, tasks []string) error {
	if len(tasks) == 0 {
		return fmt.Errorf("the %s app has no background task %q: it has none", app, name)
	}
	tasks = slices.Clone(tasks)
	slices.Sort(tasks)
	for i, t := range tasks {
		tasks[i] = fmt.Sprintf("%q", t)
	}
	return fmt.Errorf("the %s app has no background task %q: its background tasks are %s", app, name, strings.Join(tasks, ", "))
}

func backgroundSteps() []core.StepDef {
	return []core.StepDef{{
		ID: "mobile-core.background-task", Keyword: "When", Since: since,
		Expr: "the {string} background task is run in the {word} app",
		Doc: "Have the phone run one of the app's background tasks now, as it does when the task's time comes: the app's own " +
			"code runs, in the app, and does what it does then (a sync, a reminder). axx only moves the moment: the app must have " +
			"scheduled the task (axx waits up to 15 seconds for it to, as an app does a moment after it launches), and on Android " +
			"the conditions it set (a network, say) still apply. The task is named as the app " +
			"names it: on Android, its WorkManager work's unique name or one of its tags (each is tagged with its worker's class); " +
			"on iOS, its identifier (BGTaskSchedulerPermittedIdentifiers). The step waits for the task to be done: on iOS, until " +
			"the app says so, in the 30 seconds iOS gives a task; on Android, until the work ends.",
		Examples: []string{
			`When the "round-reminder" background task is run in the courier app`,
			`When the "example.parcels.courier.round-reminder" background task is run in the courier app`,
		},
		Run: func(sc *core.Scenario, a core.Args) error {
			name, app := secrets.Expand(sc, a.String(0)), a.String(1)
			return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
				b, ok := d.(BackgroundTasks)
				if !ok {
					return fmt.Errorf("the %s app's device cannot run its background tasks", app)
				}
				return b.RunBackgroundTask(ctx, name)
			})
		},
	}}
}
