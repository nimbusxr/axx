package mobileandroid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// What an app's camera sees, on an emulator axx starts: the emulator's back camera renders its
// AVD's environment (-camera-back environment, the AVD's environment.ini), which axx sets for
// each scenario through the emulator's control service: a picture (scene.mode imagefile:), a
// video (videofile:), or a 3D scene (mesh3d:), which the camera sees from where axx puts the
// phone in it.

// cameraImages and cameraVideos are the files a camera sees that are not scenes, by extension.
var (
	cameraImages = []string{".jpeg", ".jpg", ".png"}
	cameraVideos = []string{".m4v", ".mov", ".mp4"}
)

// defaultEnvironment is what the camera of a scenario that gives it nothing to see renders: the
// emulator's own room, as an emulator whose AVD has no environment.ini shows.
var defaultEnvironment = map[string]string{"version": "1", "scene.mode": "mesh3d:Toren1BD.obj"}

// camera is what an app's camera sees; when it sees a scene, it is also the phone in the scene
// (mobilecore.ScenePhone).
type camera struct {
	sees  string            // the file the camera sees, for messages
	env   map[string]string // the environment the emulator renders for it
	scene *scene.Scene      // nil when the camera sees a picture or a video

	mu   sync.Mutex
	pose scene.Mat4 // where the phone is in the scene
	dev  *device    // the emulator that shows it, while the scenario has one
	ar   *arControl // the app's AR bridge (ar.go), while the app runs it
}

// See makes the app's camera see a scene, a picture or a video from its next start.
func (r runner) See(sc *core.Scenario, path string) error {
	a := r.app
	if a.server != "" {
		return fmt.Errorf("the %s app runs on an Appium server's device: what a camera sees plays on the emulators axx starts, and a real phone's camera sees what is in front of it", a.name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(path))
	cam := &camera{sees: filepath.Base(path)}
	switch {
	case ext == ".heic":
		return fmt.Errorf("%s is a HEIC photo, which an iOS simulator's camera sees: an Android emulator's sees a PNG or JPEG one", cam.sees)
	case slices.Contains(cameraImages, ext), slices.Contains(cameraVideos, ext):
		if err := checkCameraFile(abs, ext); err != nil {
			return err
		}
		kind, framed := "imagefile", framedImage
		if slices.Contains(cameraVideos, ext) {
			kind, framed = "videofile", framedVideo
		}
		out, err := framedFile(sc, abs, framed)
		if err != nil {
			return fmt.Errorf("the %s app's camera cannot see %s: %w", a.name, cam.sees, err)
		}
		cam.env = map[string]string{"version": "1", "scene.mode": kind + ":" + out}
	default:
		s, err := scene.Load(path)
		if err != nil {
			if !slices.Contains(scene.Formats(), ext) {
				return fmt.Errorf("%s is not what a camera sees: a scene (%s), an image (%s) or a video (%s)", cam.sees,
					strings.Join(scene.Formats(), ", "), strings.Join(cameraImages, ", "), strings.Join(cameraVideos, ", "))
			}
			return err
		}
		cam.scene, cam.pose = s, s.Phone
		cam.env = map[string]string{"version": "1", "scene.mode": "mesh3d:" + abs}
	}
	a.camera = cam
	if cam.scene != nil {
		sc.Log("the %s app's camera sees %s: objects %s", a.name, cam.sees, cam.scene.ObjectNames())
	} else {
		sc.Log("the %s app's camera sees %s", a.name, cam.sees)
	}
	return nil
}

// ScenePhone is the phone in the scene the app's camera sees; nil when it sees none.
func (r runner) ScenePhone() mobilecore.ScenePhone {
	if c := r.app.camera; c != nil && c.scene != nil {
		return c
	}
	return nil
}

func (c *camera) Scene() *scene.Scene { return c.scene }

func (c *camera) Pose() scene.Mat4 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pose
}

// Move puts the phone at pose: the camera sees the scene from there at once, and from the app's
// next start.
func (c *camera) Move(ctx context.Context, pose scene.Mat4) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pose = pose
	if c.ar != nil {
		answer, err := c.ar.ask(ctx, "pose "+poseLine(pose))
		if err != nil {
			return fmt.Errorf("cannot move the phone in the app's AR: %w", err)
		}
		if answer != "ok" {
			return fmt.Errorf("cannot move the phone in the app's AR: %s", answer)
		}
	}
	if c.dev == nil || c.dev.control == nil {
		return nil
	}
	return placePhone(ctx, c.dev.control, pose)
}

// lens is where the emulator's camera is on the phone, in metres, in the phone's own axes: a
// little right of the middle, and down (the emulator's virtual scene camera).
var lens = [3]float64{0.06, -0.03, 0}

// placePhone puts the emulator's phone where its camera is at pose (phonePlacement).
func placePhone(ctx context.Context, ctl *emulatorControl, pose scene.Mat4) error {
	turn, at := phonePlacement(pose)
	if err := ctl.setPhysical(ctx, rotation, turn, true); err != nil {
		return fmt.Errorf("cannot turn the emulator's phone: %w", err)
	}
	if err := ctl.setPhysical(ctx, position, at, true); err != nil {
		return fmt.Errorf("cannot move the emulator's phone: %w", err)
	}
	return nil
}

// phonePlacement is how the emulator's phone is turned and where it is for its camera to be at
// pose, the camera's world transform (the phone held upright, looking along its −Z, its +Y
// toward its top): the emulator takes how the phone is turned as Euler angles about X, then Y,
// then Z (R = Rx·Ry·Rz), in degrees, and where it is, in metres, its camera at lens from there.
func phonePlacement(pose scene.Mat4) (turn, at [3]float64) {
	trs := pose.TRS()
	// The rotation, by rows: r[i][j] is pose[j*4+i] over column j's scale.
	var r [3][3]float64
	for j := range 3 {
		s := float64(trs.S[j])
		if s == 0 {
			s = 1
		}
		for i := range 3 {
			r[i][j] = float64(pose[j*4+i]) / s
		}
	}
	ey := math.Asin(max(-1, min(1, r[0][2])))
	var ex, ez float64
	if math.Abs(r[0][2]) < 0.99999 {
		ex = math.Atan2(-r[1][2], r[2][2])
		ez = math.Atan2(-r[0][1], r[0][0])
	} else { // looking straight along X: Z's turn is X's
		ex = math.Atan2(r[2][1], r[1][1])
	}
	for i := range 3 {
		at[i] = float64(trs.T[i]) - (r[i][0]*lens[0] + r[i][1]*lens[1] + r[i][2]*lens[2])
	}
	deg := 180 / math.Pi
	return [3]float64{ex * deg, ey * deg, ez * deg}, at
}

// framedFile is the file at path laid out for the emulator's camera, made once a run: a video
// takes a while to encode, and the scenarios of a feature see the same files.
func framedFile(sc *core.Scenario, path string, framed func(in, out string) error) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:8])
	return core.Cached(sc.Suite(), Name+"/camera/"+key, func() (string, error) {
		dir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "mobile", "camera")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		ext := ".png"
		if slices.Contains(cameraVideos, strings.ToLower(filepath.Ext(path))) {
			ext = ".mp4"
		}
		out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-"+key+ext)
		if err := framed(path, out); err != nil {
			_ = os.Remove(out)
			return "", err
		}
		sc.Suite().OnClose(func(context.Context) error { return os.Remove(out) })
		return out, nil
	})
}

// checkCameraFile tells what is wrong with a picture or a video the camera is to see.
func checkCameraFile(path, ext string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	ok := false
	switch ext {
	case ".png":
		ok = bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n"))
	case ".jpg", ".jpeg":
		ok = bytes.HasPrefix(head, []byte{0xff, 0xd8, 0xff})
	default: // MP4 and MOV: ISO base media files, an ftyp box first
		ok = len(head) >= 8 && string(head[4:8]) == "ftyp"
	}
	if !ok {
		return fmt.Errorf("%s is not the %s file its name says", filepath.Base(path), strings.ToUpper(strings.TrimPrefix(ext, ".")))
	}
	return nil
}

// environments serializes what writes the AVDs' environment.ini files: axx before an emulator
// starts, and each emulator as axx sets its environment. An emulator reads its file only as it
// starts, and as it is set: each keeps its own environment from then on.
var environments sync.Mutex

// writeEnvironment writes the default environment to the AVD's environment.ini, which the
// emulator reads as it starts: without one, it cannot be set later.
func writeEnvironment(avd string) error {
	dir := avdDir(avd)
	if dir == "" {
		return fmt.Errorf("no folder of the %s AVD in %s", avd, avdHome())
	}
	var b strings.Builder
	for _, k := range []string{"version", "scene.mode"} {
		fmt.Fprintf(&b, "%s = %s\n", k, defaultEnvironment[k])
	}
	return os.WriteFile(filepath.Join(dir, "environment.ini"), []byte(b.String()), 0o644)
}

// avdDir is the AVD's folder, as its .ini file in the AVD home says.
func avdDir(avd string) string {
	home := avdHome()
	data, err := os.ReadFile(filepath.Join(home, avd+".ini"))
	if err == nil {
		var rel string
		for _, line := range strings.Split(string(data), "\n") {
			k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
			switch strings.TrimSpace(k) {
			case "path":
				if p := strings.TrimSpace(v); p != "" {
					if fi, err := os.Stat(p); err == nil && fi.IsDir() {
						return p
					}
				}
			case "path.rel":
				rel = strings.TrimSpace(v)
			}
		}
		if rel != "" {
			if p := filepath.Join(filepath.Dir(home), rel); dirExists(p) {
				return p
			}
		}
	}
	if p := filepath.Join(home, avd+".avd"); dirExists(p) {
		return p
	}
	return ""
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// showCamera has the device's camera see what the scenario's app's camera sees, or the default
// when it sees nothing, and puts the phone where it is in the scene.
func (d *running) showCamera(ctx context.Context) error {
	cam := d.app.camera
	if d.dev == nil || d.dev.avd == "" || !d.dev.environment {
		if cam != nil {
			return fmt.Errorf("the %s app's camera cannot see %s: what a camera sees plays on the emulators axx starts (an AVD as the app's device), and a real phone's camera sees what is in front of it", d.app.name, cam.sees)
		}
		return nil
	}
	env := defaultEnvironment
	if cam != nil {
		env = cam.env
	}
	environments.Lock()
	err := d.dev.control.setEnvironment(ctx, env)
	environments.Unlock()
	if err != nil {
		return fmt.Errorf("cannot set what the emulator's camera sees: %w", err)
	}
	if old := d.shown; old != nil && old != cam {
		old.mu.Lock()
		old.dev = nil
		old.mu.Unlock()
	}
	d.shown = cam
	if cam == nil {
		return nil
	}
	cam.mu.Lock()
	defer cam.mu.Unlock()
	cam.dev = d.dev
	if cam.scene != nil {
		return placePhone(ctx, d.dev.control, cam.pose)
	}
	return nil
}

// hideCamera is the scenario's end: its camera shows on no device.
func (d *running) hideCamera() {
	if c := d.shown; c != nil {
		c.mu.Lock()
		c.dev = nil
		c.mu.Unlock()
		d.shown = nil
	}
}
