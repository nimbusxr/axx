package mobilecore

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A wait looks three times before it gives up, however long its time: on a
// busy machine one look can take all of it.
func TestWaitsLookThreeTimes(t *testing.T) {
	h := cloudtest.New(t, Pack())
	looks := 0
	ok, err := waitUntil(h.SC, 0, func() (bool, error) {
		looks++
		return false, nil
	})
	if ok || err != nil || looks != minLooks {
		t.Errorf("gave up after %d looks (%v, %v), want %d", looks, ok, err, minLooks)
	}
	looks = 0
	ok, _ = waitUntil(h.SC, 0, func() (bool, error) {
		looks++
		return looks == 2, nil
	})
	if !ok || looks != 2 {
		t.Errorf("found on the second look: %v after %d", ok, looks)
	}
}
