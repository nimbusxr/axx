package scene

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
)

// Encode writes the scene's description, which a device plays: JSON, its numbers in base64,
// little-endian (float32 and uint32), as a device reads them straight into its buffers.
//
//	{"phone": [16], "roots": [...], "nodes": [{"name", "matrix": [16], "children": [...],
//	  "meshes": [{"positions", "normals", "uvs", "indices", "material"}]}],
//	 "materials": [{"color": [4], "image", "metallic", "roughness", "doubleSided"}],
//	 "images": [base64], "objects": [{"name", "node", "tap": [3]}]}
func (s *Scene) Encode(w io.Writer) error {
	type mesh struct {
		Positions string `json:"positions"`
		Normals   string `json:"normals"`
		UVs       string `json:"uvs,omitempty"`
		Indices   string `json:"indices"`
		Material  int    `json:"material"`
	}
	type node struct {
		Name     string `json:"name,omitempty"`
		Matrix   Mat4   `json:"matrix"`
		Children []int  `json:"children,omitempty"`
		Meshes   []mesh `json:"meshes,omitempty"`
	}
	type material struct {
		Color       [4]float32 `json:"color"`
		Image       int        `json:"image"`
		Metallic    float32    `json:"metallic"`
		Roughness   float32    `json:"roughness"`
		DoubleSided bool       `json:"doubleSided,omitempty"`
	}
	type object struct {
		Name string     `json:"name"`
		Node int        `json:"node"`
		Tap  [3]float32 `json:"tap"`
	}
	out := struct {
		Phone     Mat4       `json:"phone"`
		Roots     []int      `json:"roots"`
		Nodes     []node     `json:"nodes"`
		Materials []material `json:"materials"`
		Images    []string   `json:"images"`
		Objects   []object   `json:"objects"`
	}{Phone: s.Phone, Roots: s.Roots, Nodes: []node{}, Materials: []material{}, Images: []string{}, Objects: []object{}}
	for _, n := range s.Nodes {
		e := node{Name: n.Name, Matrix: n.Local, Children: n.Children}
		for _, m := range n.Meshes {
			e.Meshes = append(e.Meshes, mesh{
				Positions: floats(m.Positions), Normals: floats(m.Normals), UVs: floats(m.UVs),
				Indices: uints(m.Indices), Material: m.Material,
			})
		}
		out.Nodes = append(out.Nodes, e)
	}
	for _, m := range s.Materials {
		out.Materials = append(out.Materials, material(m))
	}
	for _, im := range s.Images {
		out.Images = append(out.Images, base64.StdEncoding.EncodeToString(im.Data))
	}
	for _, o := range s.Objects {
		out.Objects = append(out.Objects, object(o))
	}
	return json.NewEncoder(w).Encode(out)
}

func floats(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func uints(v []uint32) string {
	b := make([]byte, 4*len(v))
	for i, u := range v {
		binary.LittleEndian.PutUint32(b[4*i:], u)
	}
	return base64.StdEncoding.EncodeToString(b)
}
