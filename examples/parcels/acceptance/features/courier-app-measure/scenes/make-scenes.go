//go:build ignore

// Writes parcel-on-porch.glb, the scene the courier's iPhone camera sees in
// ../courier-app-measure-ios.feature: a parcel 60 cm long on a front porch, its front corners in
// plastic corner guards, as the courier stands over it to measure it. A tap on a guard lands on
// the corner it guards (its _tap point), not on the guard's middle. Run it here:
//
//	go run make-scenes.go
package main

import (
	"log"
	"math"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func main() {
	doc := gltf.NewDocument()
	deck := material(doc, "porch boards", [4]float64{0.42, 0.3, 0.2, 1})
	wall := material(doc, "house wall", [4]float64{0.85, 0.82, 0.75, 1})
	door := material(doc, "front door", [4]float64{0.12, 0.22, 0.35, 1})
	cardboard := material(doc, "cardboard", [4]float64{0.7, 0.52, 0.32, 1})
	tape := material(doc, "parcel tape", [4]float64{0.85, 0.7, 0.45, 1})
	guard := material(doc, "corner guard", [4]float64{0.1, 0.1, 0.1, 1})
	const front, top = -0.7, 0.3 // the parcel: 0.60 × 0.30 × 0.40 m, its front 0.7 m ahead of the courier
	root := []int{
		node(doc, "_porch", box(doc, 3, 0.04, 3, deck), [3]float64{0, -0.04, -1}),
		node(doc, "_wall", box(doc, 3, 2.6, 0.1, wall), [3]float64{0, 0, -2.45}),
		node(doc, "_door", box(doc, 0.95, 2.1, 0.04, door), [3]float64{0, 0, -2.38}),
		node(doc, "_parcel", box(doc, 0.6, top, 0.4, cardboard), [3]float64{0, 0, front - 0.2}),
		node(doc, "_tape", box(doc, 0.6, 0.002, 0.05, tape), [3]float64{0, top, front - 0.2}),
		cornerGuard(doc, "front left corner", -0.3, top, front, -1, guard),
		cornerGuard(doc, "front right corner", 0.3, top, front, 1, guard),
		phone(doc, [3]float64{0, 1.25, 0.2}, [3]float64{0, top, front - 0.2}),
	}
	doc.Scenes[0].Nodes = root
	if err := gltf.SaveBinary(doc, "parcel-on-porch.glb"); err != nil {
		log.Fatal(err)
	}
}

// cornerGuard is a 4 cm guard over the parcel's corner at x, y, z, jutting out of its side (side
// −1 left, 1 right), its top and its front: its _tap child is the corner itself.
func cornerGuard(doc *gltf.Document, name string, x, y, z, side float64, mat int) int {
	const size, inside = 0.04, 0.01 // the guard's size, and how far into the parcel it reaches
	// The guard's box rests on its node's y = 0, centered on x and z.
	at := [3]float64{x + side*(size/2-inside), y - inside, z + size/2 - inside}
	tap := len(doc.Nodes)
	doc.Nodes = append(doc.Nodes, &gltf.Node{Name: "_tap", Translation: [3]float64{x - at[0], y - at[1], z - at[2]}})
	g := node(doc, name, box(doc, size, size, size, mat), at)
	doc.Nodes[g].Children = []int{tap}
	return g
}

func material(doc *gltf.Document, name string, color [4]float64) int {
	doc.Materials = append(doc.Materials, &gltf.Material{
		Name: name,
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &color, MetallicFactor: gltf.Float(0), RoughnessFactor: gltf.Float(0.8),
		},
	})
	return len(doc.Materials) - 1
}

func node(doc *gltf.Document, name string, mesh int, at [3]float64) int {
	doc.Nodes = append(doc.Nodes, &gltf.Node{Name: name, Mesh: gltf.Index(mesh), Translation: at})
	return len(doc.Nodes) - 1
}

// phone is the courier's iPhone: a camera at from, looking at to, held upright (its +Y up).
func phone(doc *gltf.Document, from, to [3]float64) int {
	doc.Cameras = append(doc.Cameras, &gltf.Camera{Name: "phone", Perspective: &gltf.Perspective{Yfov: 1.1, Znear: 0.01}})
	pitch := math.Atan2(to[1]-from[1], -(to[2] - from[2])) // about +X: negative looks down
	q := [4]float64{math.Sin(pitch / 2), 0, 0, math.Cos(pitch / 2)}
	doc.Nodes = append(doc.Nodes, &gltf.Node{Name: "phone", Camera: gltf.Index(len(doc.Cameras) - 1), Translation: from, Rotation: q})
	return len(doc.Nodes) - 1
}

// box is a box w × h × d resting on y = 0, centered on x and z; its faces drawn flat.
func box(doc *gltf.Document, w, h, d float64, mat int) int {
	x, z, y := float32(w/2), float32(d/2), float32(h)
	faces := [][4][3]float32{
		{{-x, y, z}, {x, y, z}, {x, y, -z}, {-x, y, -z}},   // top
		{{-x, 0, -z}, {x, 0, -z}, {x, 0, z}, {-x, 0, z}},   // bottom
		{{-x, 0, z}, {x, 0, z}, {x, y, z}, {-x, y, z}},     // front
		{{x, 0, -z}, {-x, 0, -z}, {-x, y, -z}, {x, y, -z}}, // back
		{{-x, 0, -z}, {-x, 0, z}, {-x, y, z}, {-x, y, -z}}, // left
		{{x, 0, z}, {x, 0, -z}, {x, y, -z}, {x, y, z}},     // right
	}
	var ps [][3]float32
	var is []uint16
	for _, f := range faces {
		b := uint16(len(ps))
		ps = append(ps, f[:]...)
		is = append(is, b, b+1, b+2, b, b+2, b+3)
	}
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{Name: "box", Primitives: []*gltf.Primitive{{
		Indices:    gltf.Index(modeler.WriteIndices(doc, is)),
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, ps)},
		Material:   gltf.Index(mat),
	}}})
	return len(doc.Meshes) - 1
}
