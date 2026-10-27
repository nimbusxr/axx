package modelswindows

import (
	"errors"
	"slices"
	"sync"
)

// changes are what a scenario changed of the machine and its Windows apps: SAPI's voices, and the
// apps' files of the Windows AI APIs.
type changes struct {
	mu    sync.Mutex
	voice voiceChanges
	swaps []swap
}

// restore puts back the apps' files, the last first, and SAPI's voices.
func (c *changes) restore() error {
	var errs []error
	for _, s := range slices.Backward(c.swaps) {
		errs = append(errs, s.putBack())
	}
	c.swaps = nil
	return errors.Join(append(errs, c.voice.restore())...)
}
