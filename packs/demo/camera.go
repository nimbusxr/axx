package demo

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/recording"
)

// A camera films one screen of a scenario for its take: it looks at the
// screen as a video of it would, and keeps a picture each time the screen
// changed, with when. The stage shows the picture of the moment it plays.

// footageRate is how often a camera looks at its screen, a second.
const footageRate = 20

type camera struct {
	sc      *core.Scenario
	dir     string
	stamp   string
	surface string
	look    recording.Look
	halt    chan struct{}
	done    chan struct{}
	shots   []Shot
	last    [32]byte

	mu sync.Mutex
	// changed is when the screen was last seen to change.
	changed time.Time
}

// lastChange is when the camera last saw its screen change.
func (c *camera) lastChange() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changed
}

// settle waits until the screens the cameras film show what the step that
// just ran did: for a change, when one comes, and then until they are still.
// A step's own time is over by then; the wait is the film's, so that each
// step has its picture, whatever the speed of the run.
func settle(cams []*camera) {
	const (
		forChange = 350 * time.Millisecond
		still     = 180 * time.Millisecond
		most      = 2500 * time.Millisecond
	)
	start := time.Now()
	for {
		var last time.Time
		for _, c := range cams {
			if at := c.lastChange(); at.After(last) {
				last = at
			}
		}
		now := time.Now()
		switch {
		case now.Sub(start) >= most:
			return
		case last.After(start) && now.Sub(last) >= still:
			return
		case !last.After(start) && now.Sub(start) >= forChange:
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// settled keeps, for each scenario that is filmed, when the camera's wait
// after each of its steps was over, by the step's line; began, when a step
// of a show went on, once the presenter said so.
var settled = struct {
	mu    sync.Mutex
	at    map[*core.Scenario]map[int]float64
	began map[*core.Scenario]map[int]float64
}{at: map[*core.Scenario]map[int]float64{}, began: map[*core.Scenario]map[int]float64{}}

// beganNow notes that the running step goes on now: the time it waited for
// the presenter is over.
func beganNow(sc *core.Scenario, line int) {
	settled.mu.Lock()
	defer settled.mu.Unlock()
	if settled.began[sc] == nil {
		settled.began[sc] = map[int]float64{}
	}
	settled.began[sc][line] = time.Since(sc.Started()).Seconds()
}

// beganAt is when a step of a show went on, or 0.
func beganAt(sc *core.Scenario, line int) float64 {
	settled.mu.Lock()
	defer settled.mu.Unlock()
	return settled.began[sc][line]
}

// settledNow notes that the camera has the picture of what the running step
// did.
func settledNow(sc *core.Scenario, line int) {
	settled.mu.Lock()
	defer settled.mu.Unlock()
	if settled.at[sc] == nil {
		settled.at[sc] = map[int]float64{}
	}
	settled.at[sc][line] = time.Since(sc.Started()).Seconds()
}

// settledAt is when the camera had the picture of what a step did, or 0.
func settledAt(sc *core.Scenario, line int) float64 {
	settled.mu.Lock()
	defer settled.mu.Unlock()
	return settled.at[sc][line]
}

// forget drops what was kept of a scenario that is over.
func forget(sc *core.Scenario) {
	settled.mu.Lock()
	defer settled.mu.Unlock()
	delete(settled.at, sc)
	delete(settled.began, sc)
}

// film starts a camera on a scenario's screen. Its pictures go into the
// demo's folder, beside the take.
func film(sc *core.Scenario, dir, stamp, surface string, look recording.Look) *camera {
	c := &camera{sc: sc, dir: dir, stamp: stamp, surface: surface, look: look, halt: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		t := time.NewTicker(time.Second / footageRate)
		defer t.Stop()
		for {
			c.shoot()
			select {
			case <-c.halt:
				c.shoot()
				return
			case <-t.C:
			}
		}
	}()
	return c
}

// shoot keeps the screen's picture when it changed.
func (c *camera) shoot() {
	img := c.look()
	if img == nil {
		return
	}
	sum := sha256.Sum256(img.Pix)
	if sum == c.last {
		return
	}
	c.last = sum
	c.mu.Lock()
	c.changed = time.Now()
	c.mu.Unlock()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		return
	}
	name := filepath.Join(fmt.Sprintf("footage-%d-%s", c.sc.Line, c.stamp), fmt.Sprintf("%s-%05d.jpg", c.surface, len(c.sofar())))
	path := filepath.Join(c.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return
	}
	shot := Shot{Surface: c.surface, File: filepath.ToSlash(name), At: time.Since(c.sc.Started()).Seconds()}
	c.mu.Lock()
	c.shots = append(c.shots, shot)
	c.mu.Unlock()
}

// sofar are the pictures the camera has taken until now.
func (c *camera) sofar() []Shot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Shot(nil), c.shots...)
}

// stop ends the filming and returns the pictures.
func (c *camera) stop() []Shot {
	close(c.halt)
	<-c.done
	return c.sofar()
}
