package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// Camera is a platform that plays its apps' camera, which a device has not got (an iOS
// simulator's) or has as its emulator renders it (an Android emulator's), and its AR: the camera
// sees a 3D scene, an image or a video.
type Camera interface {
	// See makes the app's camera see the file at path from the app's next start, and tells what
	// is wrong with the file.
	See(sc *core.Scenario, path string) error
	// ScenePhone is the phone in the scene the app's camera sees; nil when it sees none.
	ScenePhone() ScenePhone
}

// ScenePhone is a phone in the scene its app's camera sees, which moves through it.
type ScenePhone interface {
	// Scene is the scene the phone is in.
	Scene() *scene.Scene
	// Pose is where the phone is in the scene now, and which way it looks: its camera's world
	// transform.
	Pose() scene.Mat4
	// Move puts the phone at pose: the app's camera sees the scene from there once it returns,
	// and from the app's next start.
	Move(ctx context.Context, pose scene.Mat4) error
}

// SceneView is a running app whose camera sees a scene.
type SceneView interface {
	// SceneObject is where the scene's object of that name shows on the screen, in points, once
	// the app's camera view shows it.
	SceneObject(ctx context.Context, name string) (x, y float64, err error)
}

func cameraSteps() []core.StepDef {
	return []core.StepDef{{
		ID: "mobile-core.camera-sees", Keyword: "Given", Since: since,
		Expr: "the {word} app's camera sees {filepath}",
		Doc: "The app's camera sees a 3D scene, a glTF file (.glb or .gltf), or an image (PNG or JPEG, and HEIC on iOS) or a " +
			"video (MP4 or MOV; H.264 on Android), as the phone held upright sees it: axx plays the camera of an iOS simulator, " +
			"which has none, and of an Android emulator it starts, and, from a scene, the app's AR. A scene's camera is where " +
			"the phone is, which the phone's steps move; the app's AR finds the scene's surfaces and objects where they are. A " +
			"video plays in a loop. A running app starts again, to see it.",
		Examples: []string{"Given the courier app's camera sees scenes/parcel-on-porch.glb", "Given the courier app's camera sees labels/label-PX-IOS-9621.png"},
		Run: func(sc *core.Scenario, a core.Args) error {
			name := a.String(0)
			path, err := sc.ResolvePath(a.String(1))
			if err != nil {
				return err
			}
			app, err := mobileApp(sc, name)
			if err != nil {
				return err
			}
			cam, ok := app.Platform.(Camera)
			if !ok {
				return fmt.Errorf("the %s app's camera cannot be played on %s: what a camera sees plays on iOS simulators and Android emulators", name, app.Platform.Kind())
			}
			if err := cam.See(sc, path); err != nil {
				return err
			}
			r := scenarioRunning.Of(sc)
			r.mu.Lock()
			d, running := r.devices[name]
			r.mu.Unlock()
			if running {
				return d.Restart(sc.Context())
			}
			return nil
		},
	}, {
		ID: "mobile-core.phone-to-camera", Keyword: "When", Since: since,
		Expr: "the {word} app's phone moves to the scene's {string} camera",
		Doc: "Put the phone where one of the scene's cameras is, looking the way it looks: a place the scene names, " +
			"with a camera there. The app's camera sees the scene from there.",
		Examples: []string{`When the courier app's phone moves to the scene's "crouched beside the parcel" camera`},
		Run: func(sc *core.Scenario, a core.Args) error {
			name := a.String(0)
			p, err := scenePhone(sc, name)
			if err != nil {
				return err
			}
			c, err := p.Scene().Camera(a.String(1))
			if err != nil {
				return err
			}
			sc.Log("the %s app's phone moves to the scene's %q camera, at %s", name, c.Name, place(c.Pose))
			return p.Move(sc.Context(), c.Pose)
		},
	}, {
		ID: "mobile-core.phone-path", Keyword: "When", Since: since,
		Expr: "the {word} app's phone {travel} the scene's {string} path[[ at {speed}]]",
		Doc: "Carry the phone along one of the scene's paths: an animation that moves a camera (the phone's, or another " +
			"the phone follows), as it was recorded. With no speed, the phone keeps to the animation's own times; `at 2x` " +
			"goes twice as fast; `at 1.4 m/s` keeps one speed along the way. `travels` waits for the phone to arrive; " +
			"`starts traveling` lets the scenario go on while it moves, and a later step waits for it to reach the end. " +
			"The phone stays where the path ends.",
		Examples: []string{`When the courier app's phone travels the scene's "up the garden path" path`, `When the courier app's phone starts traveling the scene's "up the garden path" path at 2x`},
		Run: func(sc *core.Scenario, a core.Args) error {
			name := a.String(0)
			p, err := scenePhone(sc, name)
			if err != nil {
				return err
			}
			path, err := p.Scene().Path(a.String(2))
			if err != nil {
				return err
			}
			var s *speed
			if a.Present(3) {
				v, err := parseSpeed(a.String(3))
				if err != nil {
					return err
				}
				s = &v
			}
			timing, err := pathTiming(p.Scene(), path, s)
			if err != nil {
				return err
			}
			return travelPath(sc, name, p, path, timing, a.String(1) == "travels")
		},
	}, {
		ID: "mobile-core.phone-move", Keyword: "When", Since: since,
		Expr: "the {word} app's phone moves {float} m {direction}",
		Doc: "Step with the phone, as the person holding it steps, still facing the same way, at a walking pace: " +
			"`forward` and `back` along the ground the way the phone looks, `left` and `right` across it, `up` and `down`.",
		Examples: []string{"When the courier app's phone moves 1.5 m right"},
		Run: func(sc *core.Scenario, a core.Args) error {
			name, dir := a.String(0), a.String(2)
			metres, err := strconv.ParseFloat(a.Raw(1), 64)
			if err != nil || metres <= 0 {
				return fmt.Errorf("a phone moves a distance of more than nothing, not %s m: for the other way, say %s", a.Raw(1), opposite(dir))
			}
			p, err := scenePhone(sc, name)
			if err != nil {
				return err
			}
			from := p.Pose()
			to, err := from.Moved(metres, dir)
			if err != nil {
				return err
			}
			sc.Log("the %s app's phone moves %g m %s, to %s", name, metres, dir, place(to))
			return glide(sc.Context(), p, time.Duration(metres/walkingPace*float64(time.Second)), func(f float64) scene.Mat4 {
				return scene.Between(from, to, f)
			})
		},
	}, {
		ID: "mobile-core.phone-turn", Keyword: "When", Since: since,
		Expr: "the {word} app's phone turns {int} degrees {direction}",
		Doc: "Turn the phone where it stands, as the person holding it turns: `left` and `right` about the upright, keeping " +
			"its tilt; `up` and `down`, tilting it.",
		Examples: []string{"When the courier app's phone turns 90 degrees left"},
		Run: func(sc *core.Scenario, a core.Args) error {
			name, degrees, dir := a.String(0), a.Int(1), a.String(2)
			if degrees <= 0 {
				return fmt.Errorf("a phone turns by more than nothing, not %d degrees: for the other way, say %s", degrees, opposite(dir))
			}
			p, err := scenePhone(sc, name)
			if err != nil {
				return err
			}
			from := p.Pose()
			if _, err := from.Turned(float64(degrees), dir); err != nil {
				return err
			}
			sc.Log("the %s app's phone turns %d degrees %s", name, degrees, dir)
			return glide(sc.Context(), p, time.Duration(float64(degrees)/turningPace*float64(time.Second)), func(f float64) scene.Mat4 {
				m, _ := from.Turned(float64(degrees)*f, dir)
				return m
			})
		},
	}, {
		ID: "mobile-core.camera-tap", Keyword: "When", Since: since,
		Expr: "the {string} in the {word} app's camera view is tapped",
		Doc: "Tap what the app's camera shows, by its name in the scene the camera sees: the tap lands where the object shows " +
			"on the screen (its middle, or where the scene's _tap point for it says), as a finger would.",
		Examples: []string{`When the "left end of the parcel" in the courier app's camera view is tapped`},
		Run: func(sc *core.Scenario, a core.Args) error {
			object, name := a.String(0), a.String(1)
			return onDevice(sc, name, false, func(ctx context.Context, d Device) error {
				v, ok := d.(SceneView)
				if !ok {
					return fmt.Errorf("the %s app's camera sees no scene: give it one with \"the %s app's camera sees <scene>\"", name, name)
				}
				x, y, err := v.SceneObject(ctx, object)
				if err != nil {
					return err
				}
				return d.Session().Tap(ctx, x, y)
			})
		},
	}}
}

// How fast a phone moves and turns when a step moves it: a person's walking pace, in metres a
// second, and a person's turn, in degrees a second.
const (
	walkingPace = 1.4
	turningPace = 90.0
)

// frame is the shortest time between two poses of a moving phone: as often as its camera's 30
// frames a second.
const frame = time.Second / 30

// scenePhone is the phone in the scene the app's camera sees, once any path it travels has ended.
func scenePhone(sc *core.Scenario, name string) (ScenePhone, error) {
	app, err := mobileApp(sc, name)
	if err != nil {
		return nil, err
	}
	cam, ok := app.Platform.(Camera)
	if !ok {
		return nil, fmt.Errorf("the %s app's phone cannot move through a scene on %s: scenes play on iOS simulators and Android emulators", name, app.Platform.Kind())
	}
	p := cam.ScenePhone()
	if p == nil {
		return nil, fmt.Errorf("the %s app's camera sees no scene to move through: give it one with \"the %s app's camera sees <scene>\"", name, name)
	}
	if j, ok := (*journeys.Of(sc))[name]; ok && j.path != "" {
		select {
		case <-j.arrived:
		default:
			return nil, fmt.Errorf("the %s app's phone is still traveling the scene's %q path: wait for it with \"the %s app's phone reaches the end of its route\"", name, j.path, name)
		}
	}
	return p, nil
}

func opposite(direction string) string {
	return map[string]string{"forward": "back", "back": "forward", "left": "right", "right": "left", "up": "down", "down": "up"}[direction]
}

// place is where a pose is, for the log.
func place(m scene.Mat4) string {
	o := m.Origin()
	return fmt.Sprintf("%.2f, %.2f, %.2f", o[0], o[1], o[2])
}

// glide moves the phone smoothly over d: to pose(f) as f goes from 0 to 1, a pose a frame.
func glide(ctx context.Context, p ScenePhone, d time.Duration, pose func(f float64) scene.Mat4) error {
	start := time.Now()
	for {
		elapsed := time.Since(start)
		if elapsed >= d {
			return p.Move(ctx, pose(1))
		}
		if err := p.Move(ctx, pose(float64(elapsed)/float64(d))); err != nil {
			return err
		}
		if err := pause(ctx, frame-(time.Since(start)-elapsed)); err != nil {
			return err
		}
	}
}

func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// A timing is where along a path, in its own seconds, a phone is a while after it set off; done
// tells whether it has reached the end.
type timing struct {
	at       func(elapsed time.Duration) (t float64, done bool)
	duration time.Duration
	length   float64 // metres
}

// pathTiming is how a phone keeps to a path: to its own times, a pace times faster, or at one
// speed along the way.
func pathTiming(s *scene.Scene, p *scene.Path, sp *speed) (timing, error) {
	// The way the path goes, a sample a 60th of a second of it: how far along it each is.
	n := max(2, int(math.Ceil(p.Duration*60))+1)
	times, dist := make([]float64, n), make([]float64, n)
	prev := s.PoseOn(p, 0).Origin()
	for i := range n {
		times[i] = p.Duration * float64(i) / float64(n-1)
		o := s.PoseOn(p, times[i]).Origin()
		if i > 0 {
			dx, dy, dz := float64(o[0]-prev[0]), float64(o[1]-prev[1]), float64(o[2]-prev[2])
			dist[i] = dist[i-1] + math.Sqrt(dx*dx+dy*dy+dz*dz)
		}
		prev = o
	}
	length := dist[n-1]
	seconds := func(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
	switch {
	case sp == nil || sp.pace > 0:
		pace := 1.0
		if sp != nil {
			pace = sp.pace
		}
		if p.Duration == 0 {
			return timing{}, fmt.Errorf("the scene's %q path takes no time: its animation has one moment", p.Name)
		}
		return timing{
			at: func(e time.Duration) (float64, bool) {
				t := e.Seconds() * pace
				return math.Min(t, p.Duration), t >= p.Duration
			},
			duration: seconds(p.Duration / pace), length: length,
		}, nil
	case length < 0.01:
		return timing{}, fmt.Errorf("the scene's %q path does not move the phone from its place, so it has no speed: give none, or a pace like 2x", p.Name)
	}
	v := sp.metresPerSecond
	return timing{
		at: func(e time.Duration) (float64, bool) {
			d := e.Seconds() * v
			if d >= length {
				return p.Duration, true
			}
			// The first sample past d, which is a step on from the one before it: where the
			// phone stands still (as a path may start), samples are the same distance along,
			// and between two of them the phone's place is no fraction of the way (0/0).
			i := 1
			for dist[i] <= d {
				i++
			}
			f := (d - dist[i-1]) / (dist[i] - dist[i-1])
			return times[i-1] + f*(times[i]-times[i-1]), false
		},
		duration: seconds(length / v), length: length,
	}, nil
}

// travelPath carries the phone along the path, and waits for it to arrive unless the step lets
// the scenario go on.
func travelPath(sc *core.Scenario, app string, p ScenePhone, path *scene.Path, tm timing, wait bool) error {
	arrived := make(chan struct{})
	var failed error
	// The phone travels on after a step that starts it ends, until it arrives or the scenario
	// does: not with the step's context, which ends with the step.
	step := sc.Context()
	ctx, cancel := context.WithCancel(context.WithoutCancel(step))
	sc.OnClose(Name+".phone-path", func() error {
		cancel()
		return nil
	})
	go func() {
		defer close(arrived)
		start := time.Now()
		for {
			elapsed := time.Since(start)
			t, done := tm.at(elapsed)
			if err := p.Move(ctx, p.Scene().PoseOn(path, t)); err != nil {
				failed = err
				return
			}
			if done {
				return
			}
			if err := pause(ctx, frame-(time.Since(start)-elapsed)); err != nil {
				failed = err
				return
			}
		}
	}()
	(*journeys.Of(sc))[app] = journey{arrived: arrived, due: time.Now().Add(tm.duration), path: path.Name, failed: &failed}
	sc.Log("the %s app's phone travels the scene's %q path: %.1f m in %s", app, path.Name, tm.length, tm.duration.Round(100*time.Millisecond))
	if !wait {
		return nil
	}
	select {
	case <-arrived:
		if failed != nil && !errors.Is(failed, context.Canceled) {
			return failed
		}
		return nil
	case <-step.Done():
		cancel()
		return step.Err()
	}
}
