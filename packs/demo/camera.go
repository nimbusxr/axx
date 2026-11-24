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

// film starts a camera on a scenario's screen. Its pictures go into the
// demo's folder, beside the take.
func film(sc *core.Scenario, dir, surface string, look recording.Look) *camera {
	c := &camera{sc: sc, dir: dir, surface: surface, look: look, halt: make(chan struct{}), done: make(chan struct{})}
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
	name := filepath.Join(fmt.Sprintf("footage-%d", c.sc.Line), fmt.Sprintf("%s-%05d.jpg", c.surface, len(c.shots)))
	path := filepath.Join(c.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return
	}
	c.shots = append(c.shots, Shot{Surface: c.surface, File: filepath.ToSlash(name), At: time.Since(c.sc.Started()).Seconds()})
}

// stop ends the filming and returns the pictures.
func (c *camera) stop() []Shot {
	close(c.halt)
	<-c.done
	return c.shots
}
