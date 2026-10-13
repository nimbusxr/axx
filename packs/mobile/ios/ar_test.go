package mobileios

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// A scene the camera cannot see fails the step that gives it, saying why; and a real iPhone's
// camera, through an Appium server, sees what is in front of it.
func TestTheCameraSeesOnlyAScene(t *testing.T) {
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	h.File("scenes/lane-cones.scn", "a SceneKit scene")
	glb, err := os.ReadFile(filepath.Join("..", "internal", "scene", "testdata", "lane-cones.glb"))
	if err != nil {
		t.Fatal(err)
	}
	h.File("scenes/lane-cones.glb", string(glb))
	h.OK("the courier ios app with the following properties:", [][]string{{"bundle id", "example.parcels.courier"}, {"device", "iPhone 16"}})
	_ = h.Fails("the courier app's camera sees scenes/no-such-scene.glb", "no-such-scene.glb")
	_ = h.Fails("the courier app's camera sees scenes/lane-cones.scn", "lane-cones.scn is not a scene axx plays: a scene is a glTF file (.glb, .gltf)")
	_ = h.Fails(`the "left cone" in the courier app's camera view is tapped`, "the courier app is not running")

	h.NewScenario()
	h.OK("the courier ios app with the following properties:", [][]string{{"bundle id", "example.parcels.courier"}, {"appium", "https://farm.example"}})
	_ = h.Fails("the courier app's camera sees scenes/lane-cones.glb", "a real iPhone's camera sees what is in front of it")
}
