package mobileios

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// A scene the camera cannot see fails the step that gives it, saying why; and a real iPhone's
// camera, through an Appium server, sees what is in front of it. A simulator's app runs on a Mac
// only: elsewhere, only the Appium server's is checked.
func TestTheCameraSeesOnlyAScene(t *testing.T) {
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	h.File("scenes/lane-cones.scn", "a SceneKit scene")
	glb, err := os.ReadFile(filepath.Join("..", "internal", "scene", "testdata", "lane-cones.glb"))
	if err != nil {
		t.Fatal(err)
	}
	h.File("scenes/lane-cones.glb", string(glb))
	if runtime.GOOS == "darwin" {
		h.OK("the courier ios app with the following properties:", [][]string{{"bundle id", "example.parcels.courier"}, {"device", "iPhone 16"}})
		_ = h.Fails("the courier app's camera sees scenes/no-such-scene.glb", "no-such-scene.glb")
		_ = h.Fails("the courier app's camera sees scenes/lane-cones.scn", "lane-cones.scn is not a scene axx plays: a scene is a glTF file (.glb, .gltf)")
		_ = h.Fails(`the "left cone" in the courier app's camera view is tapped`, "the courier app is not running")
		h.NewScenario()
	}
	h.OK("the courier ios app with the following properties:", [][]string{{"bundle id", "example.parcels.courier"}, {"appium", "https://farm.example"}})
	_ = h.Fails("the courier app's camera sees scenes/lane-cones.glb", "a real iPhone's camera sees what is in front of it")
}

// A courier walks up a garden path from the van, steps onto the porch and points the phone down
// at the parcel: before the app runs, each step puts the phone where the app's camera will see
// the scene from as it starts. Where the phone is shows in the pose the library reads.
func TestThePhoneMovesThroughTheScene(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("a scene plays on an iOS simulator, on a Mac")
	}
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	glb, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "parcels", "acceptance", "features", "courier-app-measure", "scenes", "parcel-on-porch.glb"))
	if err != nil {
		t.Fatal(err)
	}
	h.File("scenes/parcel-on-porch.glb", string(glb))
	h.OK("the courier ios app with the following properties:", [][]string{{"bundle id", "example.parcels.courier"}, {"device", "iPhone 16"}})
	_ = h.Fails("the courier app's phone moves 1 m forward", `the courier app's camera sees no scene to move through: give it one with "the courier app's camera sees <scene>"`)
	h.OK("the courier app's camera sees scenes/parcel-on-porch.glb")
	pose := func() scene.Mat4 {
		t.Helper()
		files, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "mobile", "ar", "parcel-on-porch-*.pose.json"))
		if len(files) != 1 {
			t.Fatalf("pose files: %v", files)
		}
		b, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		var m scene.Mat4
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	near := func(what string, got, want [3]float32) {
		t.Helper()
		for i := range 3 {
			if math.Abs(float64(got[i]-want[i])) > 1e-3 {
				t.Errorf("%s: %v, want %v", what, got, want)
				return
			}
		}
	}
	looks := func(m scene.Mat4) [3]float32 { return [3]float32{-m[8], -m[9], -m[10]} }

	h.OK(`the courier app's phone moves to the scene's "by the van" camera`)
	near("by the van", pose().Origin(), [3]float32{2.5, 1.4, 11.5})
	near("looking along the street", looks(pose()), [3]float32{-1, 0, 0})
	h.OK(`the courier app's phone travels the scene's "up the garden path" path at 10x`)
	near("at the foot of the porch", pose().Origin(), [3]float32{0, 1.4, 1.2})
	h.OK("the courier app's phone moves 1 m forward")
	near("on the porch", pose().Origin(), [3]float32{0, 1.4, 0.2})
	h.OK("the courier app's phone turns 40 degrees down")
	down := 40 * math.Pi / 180
	near("pointed down at the parcel", looks(pose()), [3]float32{0, -float32(math.Sin(down)), -float32(math.Cos(down))})
	h.OK("the courier app's phone turns 90 degrees left")
	near("turned left, still pointed down", looks(pose()), [3]float32{-float32(math.Cos(down)), -float32(math.Sin(down)), 0})
	h.OK("the courier app's phone moves 0.5 m back")
	near("stepped back from where it looks", pose().Origin(), [3]float32{0.5, 1.4, 0.2})

	// A path the phone is still traveling: the next move waits for it to arrive.
	h.OK(`the courier app's phone starts traveling the scene's "up the garden path" path at 4x`)
	_ = h.Fails("the courier app's phone moves 1 m forward", `the courier app's phone is still traveling the scene's "up the garden path" path: wait for it with "the courier app's phone reaches the end of its route"`)
	h.OK("the courier app's phone reaches the end of its route")
	near("arrived", pose().Origin(), [3]float32{0, 1.4, 1.2})

	_ = h.Fails(`the courier app's phone moves to the scene's "porch" camera`, `the scene has no "porch" camera: its cameras are "phone", "by the van"`)
	_ = h.Fails(`the courier app's phone travels the scene's "round the back" path`, `the scene has no "round the back" path: its animations are "up the garden path"`)
	_ = h.Fails("the courier app's phone turns 30 degrees back", `a phone turns left, right, up or down, not "back"`)
	_ = h.Fails("the courier app's phone moves 0 m forward", "a phone moves a distance of more than nothing, not 0 m: for the other way, say back")
	_ = h.Fails("the courier app is swiped forward", "a swipe goes up, down, left or right, not forward")
}
