//go:build ignore

// Writes parcel-on-porch.glb, the scene the courier's iPhone camera sees in
// ../courier-app-measure-ios.feature: a parcel 60 cm long on a front porch, its front corners in
// plastic corner guards, as the courier stands over it to measure it. A tap on a guard lands on
// the corner it guards (its _tap point), not on the guard's middle. A garden path leads up to the
// porch from the street, where the courier's van is parked: the scene's "by the van" camera is
// where the courier stands on getting out, looking along the street, and its "up the garden path"
// animation carries the phone from there to the foot of the porch, held level, facing the door.
// Run it here:
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
	lawn := material(doc, "lawn", [4]float64{0.22, 0.42, 0.16, 1})
	slabs := material(doc, "paving slabs", [4]float64{0.55, 0.55, 0.52, 1})
	asphalt := material(doc, "asphalt", [4]float64{0.12, 0.12, 0.13, 1})
	van := material(doc, "van", [4]float64{0.92, 0.92, 0.9, 1})
	deck := material(doc, "porch boards", [4]float64{0.42, 0.3, 0.2, 1})
	wall := material(doc, "house wall", [4]float64{0.85, 0.82, 0.75, 1})
	door := material(doc, "front door", [4]float64{0.12, 0.22, 0.35, 1})
	cardboard := material(doc, "cardboard", [4]float64{0.7, 0.52, 0.32, 1})
	tape := material(doc, "parcel tape", [4]float64{0.85, 0.7, 0.45, 1})
	guard := material(doc, "corner guard", [4]float64{0.1, 0.1, 0.1, 1})
	const front, top = -0.7, 0.3 // the parcel: 0.60 × 0.30 × 0.40 m, its front 0.7 m ahead of the courier
	root := []int{
		node(doc, "_lawn", box(doc, 14, 0.02, 10, lawn), [3]float64{0, -0.06, 5.5}),
		node(doc, "_garden path", box(doc, 1, 0.02, 10, slabs), [3]float64{0, -0.05, 5.5}),
		node(doc, "_pavement", box(doc, 14, 0.02, 1.5, slabs), [3]float64{0, -0.05, 11.25}),
		node(doc, "_street", box(doc, 14, 0.02, 6, asphalt), [3]float64{0, -0.2, 15}),
		node(doc, "_van", box(doc, 5, 2.2, 2, van), [3]float64{4.5, -0.2, 13.4}),
		node(doc, "_porch", box(doc, 3, 0.04, 3, deck), [3]float64{0, -0.04, -1}),
		node(doc, "_wall", box(doc, 10, 3, 0.1, wall), [3]float64{0, 0, -2.45}),
		node(doc, "_door", box(doc, 0.95, 2.1, 0.04, door), [3]float64{0, 0, -2.38}),
		node(doc, "_parcel", box(doc, 0.6, top, 0.4, cardboard), [3]float64{0, 0, front - 0.2}),
		node(doc, "_tape", box(doc, 0.6, 0.002, 0.05, tape), [3]float64{0, top, front - 0.2}),
		cornerGuard(doc, "front left corner", -0.3, top, front, -1, guard),
		cornerGuard(doc, "front right corner", 0.3, top, front, 1, guard),
		camera(doc, "phone", [3]float64{0, 1.25, 0.2}, 0, pitchToward([3]float64{0, 1.25, 0.2}, [3]float64{0, top, front - 0.2})),
		camera(doc, "by the van", byTheVan, 90, 0),
	}
	upTheGardenPath(doc, root[len(root)-2])
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

// byTheVan is where the courier stands on getting out of the van, the phone at eye level.
var byTheVan = [3]float64{2.5, 1.4, 11.5}

// camera is a place the courier's iPhone can be: a camera at, turned yaw degrees left from
// looking down the garden path toward the house (−Z), tilted pitch degrees up, held upright.
func camera(doc *gltf.Document, name string, at [3]float64, yaw, pitch float64) int {
	doc.Cameras = append(doc.Cameras, &gltf.Camera{Name: name, Perspective: &gltf.Perspective{Yfov: 1.1, Znear: 0.01}})
	doc.Nodes = append(doc.Nodes, &gltf.Node{Name: name, Camera: gltf.Index(len(doc.Cameras) - 1), Translation: at, Rotation: turned(yaw, pitch)})
	return len(doc.Nodes) - 1
}

// pitchToward is how far up, in degrees, a phone at from looks to see to straight ahead of it.
func pitchToward(from, to [3]float64) float64 {
	return math.Atan2(to[1]-from[1], -(to[2]-from[2])) * 180 / math.Pi
}

// turned is a rotation yaw degrees about the upright (+Y), after one pitch degrees about the
// phone's own sideways axis (+X): a quaternion.
func turned(yaw, pitch float64) [4]float64 {
	y, p := yaw*math.Pi/360, pitch*math.Pi/360
	return [4]float64{math.Cos(y) * math.Sin(p), math.Sin(y) * math.Cos(p), -math.Sin(y) * math.Sin(p), math.Cos(y) * math.Cos(p)}
}

// upTheGardenPath is the courier's walk from the van to the foot of the porch, carrying the phone
// at eye level: a turn toward the garden path, across the pavement to it, and up it to the porch,
// facing the door, at a walking pace (10.3 m in 9 s).
func upTheGardenPath(doc *gltf.Document, phone int) {
	times := []float32{0, 1, 3, 9}
	at := [][3]float32{
		{float32(byTheVan[0]), float32(byTheVan[1]), float32(byTheVan[2])},
		{float32(byTheVan[0]), float32(byTheVan[1]), float32(byTheVan[2])},
		{0, 1.4, 9.5},
		{0, 1.4, 1.2},
	}
	toPath := math.Atan2(byTheVan[0], byTheVan[2]-9.5) * 180 / math.Pi
	var facing [][4]float32
	for _, yaw := range []float64{90, toPath, 0, 0} {
		q := turned(yaw, 0)
		facing = append(facing, [4]float32{float32(q[0]), float32(q[1]), float32(q[2]), float32(q[3])})
	}
	in := modeler.WriteAccessor(doc, gltf.TargetNone, times)
	doc.Accessors[in].Min, doc.Accessors[in].Max = []float64{0}, []float64{9}
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Name: "up the garden path",
		Samplers: []*gltf.AnimationSampler{
			{Input: in, Output: modeler.WriteAccessor(doc, gltf.TargetNone, at)},
			{Input: in, Output: modeler.WriteAccessor(doc, gltf.TargetNone, facing)},
		},
		Channels: []*gltf.AnimationChannel{
			{Sampler: 0, Target: gltf.AnimationChannelTarget{Node: gltf.Index(phone), Path: gltf.TRSTranslation}},
			{Sampler: 1, Target: gltf.AnimationChannelTarget{Node: gltf.Index(phone), Path: gltf.TRSRotation}},
		},
	})
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
