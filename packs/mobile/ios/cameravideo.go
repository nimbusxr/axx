// SPDX-License-Identifier: Apache-2.0

package mobileios

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// keyFramedAs names how keyFramed copies a video: another name when the copy is made another
// way, so that a copy an earlier axx kept is not taken for it.
const keyFramedAs = "key1"

// keyFramedDir is where the copies are kept: in axx's cache, a file for each video, by what the
// video holds. A variable, for tests.
var keyFramedDir = func() string { return filepath.Join(npm.CacheDir("mobile"), "camera-videos") }

// keyFramed is the video at path as the simulator's camera plays it: a copy whose every frame is
// a key frame, of the same size, shown the same way up and at the same times. The camera shows
// the frame that is due now, and of a video as a phone or an encoder writes it, that frame is
// decoded from the last key frame on, up to seconds of frames; on a Mac short of processor
// time the camera then shows what a video ends on long after the video's length. Of the copy,
// the frame due is decoded alone.
//
// The copy is made once for a video, by what the video holds, with the encoder axx downloads
// for its own videos (OpenH264), and kept in axx's cache. It takes about a quarter of the
// video's length to make, and several times its size. A video the Mac cannot copy (one that is
// not H.264, as an iPhone's HEVC) is no error: the camera plays it as it is.
func keyFramed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	dir := keyFramedDir()
	out := filepath.Join(dir, hex.EncodeToString(sum[:16])+"-"+keyFramedAs+".mp4")
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Written whole under another name first: another run may be making the same copy.
	f, err := os.CreateTemp(dir, "*.part")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := video.KeyFrames(data, f, video.StreamQuality); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// As any file of the cache: a temporary file is its maker's alone.
	if err := os.Chmod(f.Name(), 0o644); err != nil { //nolint:gosec // a copy of a video the project has, in the user's own cache
		return "", err
	}
	if err := os.Rename(f.Name(), out); err != nil {
		return "", err
	}
	return out, nil
}
