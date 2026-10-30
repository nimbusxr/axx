package mobileios

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// arSources are a library axx loads into an app whose camera sees a scene, an image or a video:
// in the simulator, it plays the camera (ar/camera.m tells how) and, from a scene, ARKit, and
// draws the camera's picture behind RealityKit's AR view (ar/ar.m).
//
//go:embed ar/ar.h ar/ar.m ar/camera.m ar/ar.swift
var arSources embed.FS

// arFiles are the library's sources.
var arFiles = []string{"ar.h", "ar.m", "camera.m", "ar.swift"}

// arFrameworks are what the library is built against.
var arFrameworks = []string{
	"ARKit", "RealityKit", "SceneKit", "UIKit", "Metal", "CoreVideo", "QuartzCore", "CoreGraphics", "Foundation",
	"AVFoundation", "CoreMedia", "CoreML", "Vision", "Accelerate", "ImageIO",
}

// arLibrary is the library, built once for the Mac's simulators with Xcode's compilers into
// axx's cache: for each version of its sources and of the simulators' SDK.
func arLibrary(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/ar", func() (string, error) {
		arch := SimulatorArch(ctx)
		sdk, err := exec.CommandContext(ctx, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-version").Output()
		if err != nil {
			return "", fmt.Errorf("an app's camera sees a scene in an iOS simulator, which needs Xcode: %w", err)
		}
		h := sha256.New()
		h.Write(sdk)
		for _, f := range arFiles {
			b, err := arSources.ReadFile("ar/" + f)
			if err != nil {
				return "", err
			}
			h.Write(b)
		}
		cache := npm.CacheDir("mobile")
		dir := filepath.Join(cache, "ar-"+hex.EncodeToString(h.Sum(nil)[:6])+"-"+arch)
		lib := filepath.Join(dir, "axx-ar.dylib")
		if _, err := os.Stat(lib); err == nil {
			return lib, nil
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(cache, "prepare-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		for _, f := range arFiles {
			b, _ := arSources.ReadFile("ar/" + f)
			if err := os.WriteFile(filepath.Join(tmp, f), b, 0o644); err != nil {
				return "", err
			}
		}
		target := arch + "-apple-ios15.0-simulator"
		built := filepath.Join(tmp, "axx-ar.dylib")
		swift := []string{
			"-sdk", "iphonesimulator", "swiftc", "-target", target, "-O", "-parse-as-library", "-module-name", "axxar",
			"-emit-library", "-import-objc-header", filepath.Join(tmp, "ar.h"), filepath.Join(tmp, "ar.swift"), filepath.Join(tmp, "ar.o"),
			filepath.Join(tmp, "camera.o"),
		}
		for _, fw := range arFrameworks {
			swift = append(swift, "-framework", fw)
		}
		swift = append(swift, "-o", built)
		for _, args := range [][]string{
			{"-sdk", "iphonesimulator", "clang", "-target", target, "-c", "-fobjc-arc", "-O2", "-I", tmp, filepath.Join(tmp, "ar.m"), "-o", filepath.Join(tmp, "ar.o")},
			{"-sdk", "iphonesimulator", "clang", "-target", target, "-c", "-fobjc-arc", "-O2", "-I", tmp, filepath.Join(tmp, "camera.m"), "-o", filepath.Join(tmp, "camera.o")},
			swift,
			{"codesign", "--sign", "-", "--force", built},
		} {
			cmd := exec.CommandContext(ctx, "xcrun", args...) //nolint:gosec // Xcode's tools, with arguments axx builds
			cmd.Dir = tmp
			if out, err := cmd.CombinedOutput(); err != nil {
				tool := args[0]
				if tool == "-sdk" {
					tool = args[2]
				}
				return "", fmt.Errorf("cannot build axx's AR library for the iOS simulators (xcrun %s): %w: %s", tool, err, strings.TrimSpace(string(out)))
			}
		}
		// Another axx may have built it meanwhile: keep the first.
		if err := os.Rename(tmp, dir); err != nil {
			if _, statErr := os.Stat(lib); statErr == nil {
				return lib, nil
			}
			return "", err
		}
		return lib, nil
	})
}

// arCamera is what an app's camera sees, and what axx gives the library for it. When it sees a
// scene, it is also the phone in the scene (mobilecore.ScenePhone).
type arCamera struct {
	scene   *scene.Scene // nil when the camera sees an image or a video
	sees    string       // the file the camera sees, for messages
	file    string       // what it sees, as the library reads it: the scene, or the image's or video's path
	lib     string       // the library
	control string       // the library's control socket
	log     string       // the library's log
	posed   string       // where axx moved the phone last, which the library reads as the app starts

	mu   sync.Mutex
	pose scene.Mat4 // where the phone is now
}

var arSeq atomic.Int64

// cameraImages and cameraVideos are the files a camera sees that are not scenes, by extension.
var (
	cameraImages = []string{".heic", ".jpeg", ".jpg", ".png"}
	cameraVideos = []string{".m4v", ".mov", ".mp4"}
)

// See makes the app's camera see a scene, an image or a video from its next start: the file is
// read and checked now, and the library built.
func (r runner) See(sc *core.Scenario, path string) error {
	if r.app.server != "" {
		return fmt.Errorf("the %s app runs on an Appium server's device: what a camera sees plays on an iOS simulator, a real iPhone's camera sees what is in front of it", r.app.name)
	}
	var s *scene.Scene
	var feed map[string]string
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case slices.Contains(cameraImages, ext), slices.Contains(cameraVideos, ext):
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if err := checkCameraFile(abs, ext); err != nil {
			return err
		}
		kind := "image"
		if slices.Contains(cameraVideos, ext) {
			kind = "video"
		}
		feed = map[string]string{kind: abs}
	default:
		var err error
		if s, err = scene.Load(path); err != nil {
			if !slices.Contains(scene.Formats(), ext) {
				return fmt.Errorf("%s is not what a camera sees: a scene (%s), an image (%s) or a video (%s)", filepath.Base(path),
					strings.Join(scene.Formats(), ", "), strings.Join(cameraImages, ", "), strings.Join(cameraVideos, ", "))
			}
			return err
		}
	}
	lib, err := arLibrary(sc.Context(), sc.Suite())
	if err != nil {
		return err
	}
	dir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "mobile", "ar")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-*.json")
	if err != nil {
		return err
	}
	if s != nil {
		err = s.Encode(f)
	} else {
		err = json.NewEncoder(f).Encode(feed)
	}
	if err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// A Unix socket's path is short: in /tmp, which the simulator's apps share with the Mac.
	n := arSeq.Add(1)
	cam := &arCamera{
		scene:   s,
		sees:    filepath.Base(path),
		file:    f.Name(),
		lib:     lib,
		control: fmt.Sprintf("/tmp/axx-ar-%d-%d.sock", os.Getpid(), n),
		log:     strings.TrimSuffix(f.Name(), ".json") + ".log",
		posed:   strings.TrimSuffix(f.Name(), ".json") + ".pose.json",
	}
	if s != nil {
		cam.pose = s.Phone
	}
	if old := r.app.camera; old != nil {
		old.remove()
	}
	r.app.camera = cam
	sc.OnClose(Name+".camera", func() error {
		cam.remove()
		return nil
	})
	if s != nil {
		sc.Log("the %s app's camera sees %s: objects %s", r.app.name, filepath.Base(path), s.ObjectNames())
	} else {
		sc.Log("the %s app's camera sees %s", r.app.name, filepath.Base(path))
	}
	return nil
}

// checkCameraFile tells what is wrong with an image or a video a camera is to see: the simulator
// reads it as the app starts, where a broken file shows as nothing.
func checkCameraFile(path, ext string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 16)
	n, _ := f.Read(head)
	head = head[:n]
	ok := false
	switch ext {
	case ".png":
		ok = bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n"))
	case ".jpg", ".jpeg":
		ok = bytes.HasPrefix(head, []byte{0xff, 0xd8, 0xff})
	default: // HEIC, MP4 and MOV: ISO base media files, an ftyp box first
		ok = len(head) >= 8 && string(head[4:8]) == "ftyp"
	}
	if !ok {
		return fmt.Errorf("%s is not the %s file its name says", filepath.Base(path), strings.ToUpper(strings.TrimPrefix(ext, ".")))
	}
	return nil
}

func (c *arCamera) remove() {
	_ = os.Remove(c.file)
	_ = os.Remove(c.control)
	_ = os.Remove(c.posed)
}

// ScenePhone is the phone in the scene the app's camera sees, which a step moves; nil when the
// camera sees no scene.
func (r runner) ScenePhone() mobilecore.ScenePhone {
	if r.app.camera == nil || r.app.camera.scene == nil {
		return nil
	}
	return r.app.camera
}

// Scene is the scene the phone is in.
func (c *arCamera) Scene() *scene.Scene { return c.scene }

// Pose is where the phone is in the scene now.
func (c *arCamera) Pose() scene.Mat4 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pose
}

// Move puts the phone at pose. It is written down for the app's next start, and a running app's
// library draws the scene from there and shows it before Move returns.
func (c *arCamera) Move(ctx context.Context, pose scene.Mat4) error {
	c.mu.Lock()
	c.pose = pose
	c.mu.Unlock()
	b, err := json.Marshal(pose)
	if err != nil {
		return err
	}
	tmp := c.posed + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.posed); err != nil {
		return err
	}
	var res struct {
		Error string
	}
	if err := c.ask(ctx, map[string]any{"pose": pose}, &res); err != nil {
		if errors.Is(err, errNotStarted) {
			return nil // the app is not running: it starts where the phone is
		}
		return fmt.Errorf("cannot move the phone in the scene: %w (axx's AR library logs to %s)", err, c.log)
	}
	if res.Error != "" {
		return fmt.Errorf("cannot move the phone in the scene: %s (axx's AR library logs to %s)", res.Error, c.log)
	}
	return nil
}

// env is what the app is started with for the library to play the scene. The
// launch composes its libraries with simulator support and other packs' libraries.
func (c *arCamera) env() map[string]string {
	return map[string]string{
		"DYLD_INSERT_LIBRARIES": c.lib,
		"AXX_AR_LIBRARY":        c.lib,
		"AXX_AR_SCENE":          c.file,
		"AXX_AR_CONTROL":        c.control,
		"AXX_AR_LOG":            c.log,
		"AXX_AR_POSE":           c.posed,
	}
}

// SceneObject asks the library, in the app, where the scene's object shows on the screen, in
// points: once the camera view shows, rests in place, and has the object in view (10 seconds).
func (d *running) SceneObject(ctx context.Context, name string) (float64, float64, error) {
	cam := d.app.camera
	if cam == nil {
		return 0, 0, fmt.Errorf("the %s app's camera sees no scene: give it one with \"the %s app's camera sees <scene>\"", d.app.name, d.app.name)
	}
	if cam.scene == nil {
		return 0, 0, core.Failf("The %s app's camera sees %s, which has no objects to tap: a scene names them", d.app.name, cam.sees)
	}
	if _, ok := cam.scene.Object(name); !ok {
		return 0, 0, core.Failf("The scene the %s app's camera sees has no %q: its objects are %s", d.app.name, name, cam.scene.ObjectNames())
	}
	deadline := time.Now().Add(10 * time.Second)
	var answered error // the library's last answer: an app busy past the deadline leaves it standing
	for {
		x, y, retry, err := cam.where(ctx, name)
		if err == nil {
			return x, y, nil
		}
		if a := arAnswer(""); errors.As(err, &a) {
			answered = err
		} else if answered != nil {
			err = answered
		}
		if !retry || time.Now().After(deadline) {
			return 0, 0, fmt.Errorf("the %s app's camera view: %w (axx's AR library logs to %s)", d.app.name, err, cam.log)
		}
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// where asks the library where an object shows; retry tells whether asking again can help (the
// app, or its camera view, is not up yet, or the object is out of view).
func (c *arCamera) where(ctx context.Context, name string) (x, y float64, retry bool, err error) {
	var res struct {
		X, Y  float64
		Error string
		Retry bool
	}
	if err := c.ask(ctx, map[string]string{"where": name}, &res); err != nil {
		return 0, 0, true, err
	}
	if res.Error != "" {
		return 0, 0, res.Retry, arAnswer(res.Error)
	}
	return res.X, res.Y, false, nil
}

// arAnswer is what the library answered when it could not tell where an object shows, rather
// than an answer that never came.
type arAnswer string

func (a arAnswer) Error() string { return string(a) }

// errNotStarted is the library not answering: the app has not started, or not loaded it yet.
var errNotStarted = errors.New("the app has not started its AR yet")

// ask sends the library one request on its control socket, and reads its answer into res.
func (c *arCamera) ask(ctx context.Context, req, res any) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.control)
	if err != nil {
		return errNotStarted
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	return json.NewDecoder(conn).Decode(res)
}
