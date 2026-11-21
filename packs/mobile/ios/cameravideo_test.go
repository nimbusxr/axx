// SPDX-License-Identifier: Apache-2.0

package mobileios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/internal/video"
)

// The simulator's camera plays a copy of a video whose every frame is a key frame: made once
// for a video, by what the video holds, and kept in axx's cache. A video the Mac cannot copy
// has none, and says why: the camera then plays it as it is.
func TestTheCamerasVideoIsACopyOfKeyFrames(t *testing.T) {
	cache, kept := t.TempDir(), keyFramedDir
	keyFramedDir = func() string { return cache }
	t.Cleanup(func() { keyFramedDir = kept })
	source := filepath.Join("..", "internal", "courierapi", "testdata", "torn-label-PX-MOB-9401.mp4")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	of, err := video.ReadMP4(data)
	if err != nil {
		t.Fatal(err)
	}
	keys := 0
	for _, s := range of.Samples {
		if s.Key {
			keys++
		}
	}
	if keys == len(of.Samples) {
		t.Fatal("every frame of the label's video is a key frame already: the test wants one that is not")
	}
	copied, err := keyFramed(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(copied, keyFramedDir()) || !strings.HasSuffix(copied, "-"+keyFramedAs+".mp4") {
		t.Errorf("the copy is %s, not a file of %s", copied, keyFramedDir())
	}
	made, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	track, err := video.ReadMP4(made)
	if err != nil {
		t.Fatal(err)
	}
	if track.Width != of.Width || track.Height != of.Height || len(track.Samples) != len(of.Samples) {
		t.Errorf("the copy is %dx%d of %d frames, and the video %dx%d of %d", track.Width, track.Height, len(track.Samples), of.Width, of.Height, len(of.Samples))
	}
	for i, s := range track.Samples {
		if !s.Key {
			t.Fatalf("frame %d of the copy is no key frame", i+1)
		}
	}
	// The same video again is the same copy, not made anew.
	before, err := os.Stat(copied)
	if err != nil {
		t.Fatal(err)
	}
	again, err := keyFramed(source)
	if err != nil || again != copied {
		t.Fatalf("the same video's copy is %s (%v), not %s", again, err, copied)
	}
	if after, err := os.Stat(copied); err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the copy was made again for the same video (%v)", err)
	}
	// A video that is not H.264, which axx's decoder does not read.
	other := filepath.Join(t.TempDir(), "label.mov")
	if err := os.WriteFile(other, []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00qt  \x00\x00\x00\x08moov"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := keyFramed(other); err == nil {
		t.Errorf("a video with no H.264 track has a copy, %s", got)
	}
	if left, _ := filepath.Glob(filepath.Join(keyFramedDir(), "*.part")); len(left) != 0 {
		t.Errorf("a copy that could not be made left %v", left)
	}
}
