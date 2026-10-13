package mobilecore

import (
	"context"
	"fmt"

	"github.com/nimbusxr/axx/core"
)

// SceneCamera is a platform whose apps' camera can see a 3D scene, which the platform plays in
// place of the camera and the AR a device has not got (an iOS simulator's ARKit).
type SceneCamera interface {
	// SetScene makes the app's camera see the scene file at path from the app's next start, and
	// tells what is wrong with the file.
	SetScene(sc *core.Scenario, path string) error
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
		Doc: "The app's camera sees a 3D scene, a glTF file (.glb or .gltf): axx plays the camera, and the AR the device " +
			"has not got (an iOS simulator's ARKit), from the scene. The scene's camera is where the phone is, and the app's AR " +
			"finds the scene's surfaces and objects where they are. A running app starts again, to see it.",
		Examples: []string{"Given the courier app's camera sees scenes/parcel-on-porch.glb"},
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
			cam, ok := app.Platform.(SceneCamera)
			if !ok {
				return fmt.Errorf("the %s app's camera cannot see a scene on %s: scenes play on iOS simulators", name, app.Platform.Kind())
			}
			if err := cam.SetScene(sc, path); err != nil {
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
