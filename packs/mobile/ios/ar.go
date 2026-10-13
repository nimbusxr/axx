package mobileios

import (
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
	"strings"
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// arSources are a library axx loads into an app whose camera sees a scene: in the simulator, it
// plays ARKit and the camera from the scene, and draws the camera's picture behind RealityKit's
// AR view (ar/ar.m tells how).
//
//go:embed ar/ar.h ar/ar.m ar/ar.swift
var arSources embed.FS

// arFrameworks are what the library is built against.
var arFrameworks = []string{"ARKit", "RealityKit", "SceneKit", "UIKit", "Metal", "CoreVideo", "QuartzCore", "CoreGraphics", "Foundation"}

// arLibrary is the library, built once for the Mac's simulators with Xcode's compilers into
// axx's cache: for each version of its sources and of the simulators' SDK.
func arLibrary(ctx context.Context, s *core.Suite) (string, error) {
	return core.Cached(s, Name+"/ar", func() (string, error) {
		arch := simulatorArch(ctx)
		sdk, err := exec.CommandContext(ctx, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-version").Output()
		if err != nil {
			return "", fmt.Errorf("an app's camera sees a scene in an iOS simulator, which needs Xcode: %w", err)
		}
		h := sha256.New()
		h.Write(sdk)
		for _, f := range []string{"ar.h", "ar.m", "ar.swift"} {
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
		for _, f := range []string{"ar.h", "ar.m", "ar.swift"} {
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
		}
		for _, fw := range arFrameworks {
			swift = append(swift, "-framework", fw)
		}
		swift = append(swift, "-o", built)
		for _, args := range [][]string{
			{"-sdk", "iphonesimulator", "clang", "-target", target, "-c", "-fobjc-arc", "-O2", "-I", tmp, filepath.Join(tmp, "ar.m"), "-o", filepath.Join(tmp, "ar.o")},
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

// arCamera is the scene an app's camera sees, and what axx gives the library for it.
type arCamera struct {
	scene   *scene.Scene
	file    string // the scene's description, which the library reads
	lib     string // the library
	control string // the library's control socket
	log     string // the library's log
}

var arSeq atomic.Int64

// SetScene makes the app's camera see the scene from its next start: the scene is read and
// checked now, and the library built.
func (r runner) SetScene(sc *core.Scenario, path string) error {
	if r.app.server != "" {
		return fmt.Errorf("the %s app runs on an Appium server's device: a scene plays on an iOS simulator, a real iPhone's camera sees what is in front of it", r.app.name)
	}
	s, err := scene.Load(path)
	if err != nil {
		return err
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
	if err := s.Encode(f); err != nil {
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
		file:    f.Name(),
		lib:     lib,
		control: fmt.Sprintf("/tmp/axx-ar-%d-%d.sock", os.Getpid(), n),
		log:     strings.TrimSuffix(f.Name(), ".json") + ".log",
	}
	if old := r.app.camera; old != nil {
		old.remove()
	}
	r.app.camera = cam
	sc.OnClose(Name+".camera", func() error {
		cam.remove()
		return nil
	})
	sc.Log("the %s app's camera sees %s: objects %s", r.app.name, filepath.Base(path), s.ObjectNames())
	return nil
}

func (c *arCamera) remove() {
	_ = os.Remove(c.file)
	_ = os.Remove(c.control)
}

// env is what the app is started with for the library to play the scene. A simulator axx boots
// starts every process with its SpringBoard library (springboard.go), a value of
// DYLD_INSERT_LIBRARIES an app's own does not replace: there the SpringBoard library loads the
// library the app's AXX_AR_LIBRARY names.
func (c *arCamera) env() map[string]string {
	return map[string]string{
		"DYLD_INSERT_LIBRARIES": c.lib,
		"AXX_AR_LIBRARY":        c.lib,
		"AXX_AR_SCENE":          c.file,
		"AXX_AR_CONTROL":        c.control,
		"AXX_AR_LOG":            c.log,
	}
}

// SceneObject asks the library, in the app, where the scene's object shows on the screen, in
// points: once the camera view shows, rests in place, and has the object in view (10 seconds).
func (d *running) SceneObject(ctx context.Context, name string) (float64, float64, error) {
	cam := d.app.camera
	if cam == nil {
		return 0, 0, fmt.Errorf("the %s app's camera sees no scene: give it one with \"the %s app's camera sees <scene>\"", d.app.name, d.app.name)
	}
	if _, ok := cam.scene.Object(name); !ok {
		return 0, 0, core.Failf("The scene the %s app's camera sees has no %q: its objects are %s", d.app.name, name, cam.scene.ObjectNames())
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		x, y, retry, err := cam.where(ctx, name)
		if err == nil {
			return x, y, nil
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
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.control)
	if err != nil {
		return 0, 0, true, errors.New("the app has not started its AR yet")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(map[string]string{"where": name}); err != nil {
		return 0, 0, true, err
	}
	var res struct {
		X, Y  float64
		Error string
		Retry bool
	}
	if err := json.NewDecoder(conn).Decode(&res); err != nil {
		return 0, 0, true, err
	}
	if res.Error != "" {
		return 0, 0, res.Retry, errors.New(res.Error)
	}
	return res.X, res.Y, false, nil
}
