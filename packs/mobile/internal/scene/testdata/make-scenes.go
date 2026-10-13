//go:build ignore

// Writes the lane-cones scenes the scene package's tests read: two traffic cones 3.6 m apart
// (11.81 ft, center to center) across a lane of a road, as a crew member's phone sees them to
// measure a closure's width, held 1.4 m up, 8 m short of them and tilted 15° down;
// lane-cones-far.glb has the phone 6 m farther back. Run it here:
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
	for _, s := range []struct {
		file  string
		phone float64 // how far behind the cones' line the phone stands
	}{{"lane-cones.glb", 8}, {"lane-cones-far.glb", 14}} {
		doc := gltf.NewDocument()
		asphalt := material(doc, "asphalt", [4]float64{0.08, 0.08, 0.09, 1})
		paint := material(doc, "lane paint", [4]float64{0.9, 0.9, 0.85, 1})
		orange := material(doc, "cone orange", [4]float64{0.95, 0.3, 0.02, 1})
		black := material(doc, "cone base", [4]float64{0.03, 0.03, 0.03, 1})
		white := material(doc, "reflective band", [4]float64{0.95, 0.95, 0.95, 1})
		root := []int{
			node(doc, "_road", box(doc, 12, 0.02, 60, asphalt), [3]float64{0, -0.01, -20}),
			node(doc, "_lane line", box(doc, 0.12, 0.004, 60, paint), [3]float64{-1.8, 0.002, -20}),
			node(doc, "_lane line", box(doc, 0.12, 0.004, 60, paint), [3]float64{1.8, 0.002, -20}),
			trafficCone(doc, "left cone", [3]float64{-1.8, 0, -8}, orange, black, white),
			trafficCone(doc, "right cone", [3]float64{1.8, 0, -8}, orange, black, white),
			phone(doc, [3]float64{0, 1.4, -8 + s.phone}, -15),
		}
		doc.Scenes[0].Nodes = root
		if err := gltf.SaveBinary(doc, s.file); err != nil {
			log.Fatal(err)
		}
	}
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

// trafficCone is a 70 cm traffic cone on a black base, with a white reflective band.
func trafficCone(doc *gltf.Document, name string, at [3]float64, body, base, band int) int {
	b := node(doc, "_base", box(doc, 0.36, 0.03, 0.36, base), [3]float64{0, 0, 0})
	w := node(doc, "_band", cone(doc, 0.105, 0.24, band), [3]float64{0, 0.33, 0})
	c := node(doc, name, cone(doc, 0.17, 0.7, body), at)
	doc.Nodes[c].Children = []int{b, w}
	return c
}

// phone is the phone's camera at from, looking down the road (−Z), tilted down by tilt degrees,
// held upright (its +Y up).
func phone(doc *gltf.Document, from [3]float64, tilt float64) int {
	doc.Cameras = append(doc.Cameras, &gltf.Camera{Name: "phone", Perspective: &gltf.Perspective{Yfov: 1.1, Znear: 0.01}})
	pitch := tilt * math.Pi / 180 // about +X: negative looks down
	q := [4]float64{math.Sin(pitch / 2), 0, 0, math.Cos(pitch / 2)}
	doc.Nodes = append(doc.Nodes, &gltf.Node{Name: "phone", Camera: gltf.Index(len(doc.Cameras) - 1), Translation: from, Rotation: q})
	return len(doc.Nodes) - 1
}

func mesh(doc *gltf.Document, name string, positions [][3]float32, indices []uint16, mat int) int {
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{Name: name, Primitives: []*gltf.Primitive{{
		Indices:    gltf.Index(modeler.WriteIndices(doc, indices)),
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)},
		Material:   gltf.Index(mat),
	}}})
	return len(doc.Meshes) - 1
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
	return mesh(doc, "box", ps, is, mat)
}

// cone is a cone of radius r and height h standing on y = 0.
func cone(doc *gltf.Document, r, h float64, mat int) int {
	const n = 32
	ps := [][3]float32{{0, float32(h), 0}, {0, 0, 0}}
	var is []uint16
	for i := range n {
		a := 2 * math.Pi * float64(i) / n
		ps = append(ps, [3]float32{float32(r * math.Cos(a)), 0, float32(-r * math.Sin(a))})
	}
	for i := range n {
		a, b := uint16(2+i), uint16(2+(i+1)%n)
		is = append(is, 0, a, b, 1, b, a)
	}
	return mesh(doc, "cone", ps, is, mat)
}
