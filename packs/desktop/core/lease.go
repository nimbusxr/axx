package desktopcore

import (
	"path/filepath"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/filelock"
	"github.com/nimbusxr/axx/internal/npm"
)

// machine is this run's turn at the machine's desktop.
var machine = make(chan struct{}, 1)

// ClaimMachine waits for the machine's desktop, which has one screen, one
// pointer and one keyboard focus: this run's other desktop scenarios take
// it in turns, and every other run on the machine too. The wait stops the
// step's clock. It returns what gives the desktop back.
func ClaimMachine(sc *core.Scenario) (release func(), err error) {
	held := sc.Hold()
	defer held()
	select {
	case machine <- struct{}{}:
	default:
		sc.Log("waiting for the desktop: another of this run's desktop scenarios has it")
		select {
		case machine <- struct{}{}:
		case <-sc.Context().Done():
			return nil, sc.Context().Err()
		}
	}
	unlock, err := filelock.Lock(sc.Context(), filepath.Join(npm.CacheDir("desktop"), "desktop.lock"), func() {
		sc.Log("waiting for the desktop: another run's desktop scenario on this machine has it")
	})
	if err != nil {
		<-machine
		return nil, err
	}
	return func() {
		unlock()
		<-machine
	}, nil
}

// HomeOf is an app's home on the machine's desktop: a folder of the
// project's .axx/desktop, named after the app.
func HomeOf(sc *core.Scenario, app *App) string {
	return filepath.Join(sc.Suite().ProjectDir(), ".axx", "desktop", app.Driver.Platform(), app.Name)
}
