package scene

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// loadGLTF reads a glTF 2.0 file (.gltf, with its buffers and images beside it or in it, or a
// .glb): its default scene's nodes, their triangle meshes, the meshes' base color (a factor and
// an image) and the cameras. Points and lines are left out: a camera's ray finds no surface on
// them.
func loadGLTF(path string) (*Scene, error) {
	doc, err := gltf.Open(path)
	if err != nil {
		return nil, err
	}
	g := &gltfReader{doc: doc, dir: filepath.Dir(path), images: map[int]int{}}
	s := &Scene{}
	g.scene = s
	for _, m := range doc.Materials {
		mat, err := g.material(m)
		if err != nil {
			return nil, err
		}
		s.Materials = append(s.Materials, mat)
	}
	s.Nodes = make([]Node, len(doc.Nodes))
	for i, n := range doc.Nodes {
		node := Node{Name: n.Name, Local: local(n), Children: n.Children, Camera: n.Camera != nil}
		if n.Mesh != nil {
			if *n.Mesh < 0 || *n.Mesh >= len(doc.Meshes) {
				return nil, fmt.Errorf("its node %s names mesh %d, which it does not have", nodeName(n, i), *n.Mesh)
			}
			meshes, err := g.meshes(doc.Meshes[*n.Mesh])
			if err != nil {
				return nil, fmt.Errorf("its node %s: %w", nodeName(n, i), err)
			}
			node.Meshes = meshes
		}
		s.Nodes[i] = node
	}
	s.Roots, err = roots(doc)
	if err != nil {
		return nil, err
	}
	return s, nil
}

type gltfReader struct {
	doc    *gltf.Document
	dir    string
	scene  *Scene
	images map[int]int // the document's image → the scene's
}

func nodeName(n *gltf.Node, i int) string {
	if n.Name != "" {
		return fmt.Sprintf("%q", n.Name)
	}
	return fmt.Sprintf("%d", i)
}

// roots are the nodes at the top of the document's default scene (else its first; else every
// node no other holds).
func roots(doc *gltf.Document) ([]int, error) {
	if len(doc.Scenes) > 0 {
		i := 0
		if doc.Scene != nil {
			i = *doc.Scene
		}
		if i < 0 || i >= len(doc.Scenes) {
			return nil, fmt.Errorf("its default scene is %d, which it does not have", i)
		}
		return doc.Scenes[i].Nodes, nil
	}
	held := map[int]bool{}
	for _, n := range doc.Nodes {
		for _, c := range n.Children {
			held[c] = true
		}
	}
	var out []int
	for i := range doc.Nodes {
		if !held[i] {
			out = append(out, i)
		}
	}
	return out, nil
}

// local is a node's transform in its parent's: its matrix, else its translation, rotation and
// scale.
func local(n *gltf.Node) Mat4 {
	var m Mat4
	if raw := n.MatrixOrDefault(); raw != gltf.DefaultMatrix {
		for i, v := range raw {
			m[i] = float32(v)
		}
		return m
	}
	t, q, s := n.TranslationOrDefault(), n.RotationOrDefault(), n.ScaleOrDefault()
	x, y, z, w := q[0], q[1], q[2], q[3]
	r := [9]float64{ // column by column
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w),
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w),
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y),
	}
	for c := range 3 {
		for k := range 3 {
			m[c*4+k] = float32(r[c*3+k] * s[c])
		}
	}
	m[12], m[13], m[14], m[15] = float32(t[0]), float32(t[1]), float32(t[2]), 1
	return m
}

// meshes are a glTF mesh's primitives that are triangles, each a mesh.
func (g *gltfReader) meshes(m *gltf.Mesh) ([]Mesh, error) {
	var out []Mesh
	for _, p := range m.Primitives {
		if p.Mode != gltf.PrimitiveTriangles && p.Mode != gltf.PrimitiveTriangleStrip && p.Mode != gltf.PrimitiveTriangleFan {
			continue
		}
		mesh, err := g.primitive(p)
		if err != nil {
			return nil, err
		}
		out = append(out, mesh)
	}
	return out, nil
}

func (g *gltfReader) accessor(i int) (*gltf.Accessor, error) {
	if i < 0 || i >= len(g.doc.Accessors) {
		return nil, fmt.Errorf("accessor %d is not in the file", i)
	}
	return g.doc.Accessors[i], nil
}

func (g *gltfReader) primitive(p *gltf.Primitive) (Mesh, error) {
	mesh := Mesh{Material: -1}
	pos, ok := p.Attributes[gltf.POSITION]
	if !ok {
		return mesh, errors.New("a mesh of it has no POSITION")
	}
	acc, err := g.accessor(pos)
	if err != nil {
		return mesh, err
	}
	positions, err := modeler.ReadPosition(g.doc, acc, nil)
	if err != nil {
		return mesh, fmt.Errorf("its positions: %w", err)
	}
	var indices []uint32
	if p.Indices != nil {
		acc, err := g.accessor(*p.Indices)
		if err != nil {
			return mesh, err
		}
		if indices, err = modeler.ReadIndices(g.doc, acc, nil); err != nil {
			return mesh, fmt.Errorf("its indices: %w", err)
		}
	} else {
		indices = make([]uint32, len(positions))
		for i := range indices {
			indices[i] = uint32(i) //nolint:gosec // a mesh's vertex count fits
		}
	}
	for _, i := range indices {
		if int(i) >= len(positions) {
			return mesh, fmt.Errorf("an index of it, %d, is past its %d vertices", i, len(positions))
		}
	}
	indices = triangles(p.Mode, indices)
	var normals [][3]float32
	if n, ok := p.Attributes[gltf.NORMAL]; ok {
		acc, err := g.accessor(n)
		if err != nil {
			return mesh, err
		}
		if normals, err = modeler.ReadNormal(g.doc, acc, nil); err != nil {
			return mesh, fmt.Errorf("its normals: %w", err)
		}
	}
	var uvs [][2]float32
	if p.Material != nil {
		if *p.Material < 0 || *p.Material >= len(g.doc.Materials) {
			return mesh, fmt.Errorf("a mesh of it names material %d, which the file does not have", *p.Material)
		}
		mesh.Material = *p.Material
		if set, ok := g.texCoord(g.doc.Materials[*p.Material]); ok {
			if t, ok := p.Attributes[fmt.Sprintf("TEXCOORD_%d", set)]; ok {
				acc, err := g.accessor(t)
				if err != nil {
					return mesh, err
				}
				if uvs, err = modeler.ReadTextureCoord(g.doc, acc, nil); err != nil {
					return mesh, fmt.Errorf("its texture coordinates: %w", err)
				}
			}
		}
	}
	// A mesh with no normals is drawn flat, as glTF says: each triangle its own vertices.
	if len(normals) != len(positions) {
		positions, uvs, indices, normals = flat(positions, uvs, indices)
	}
	mesh.Positions = flatten3(positions)
	mesh.Normals = flatten3(normals)
	if len(uvs) == len(positions) {
		mesh.UVs = make([]float32, 0, 2*len(uvs))
		for _, uv := range uvs {
			mesh.UVs = append(mesh.UVs, uv[0], uv[1])
		}
	}
	mesh.Indices = indices
	return mesh, nil
}

// triangles are a primitive's triangles, three indices each, from its strip or fan.
func triangles(mode gltf.PrimitiveMode, in []uint32) []uint32 {
	switch mode {
	case gltf.PrimitiveTriangleStrip:
		var out []uint32
		for i := 0; i+2 < len(in); i++ {
			if i%2 == 0 {
				out = append(out, in[i], in[i+1], in[i+2])
			} else {
				out = append(out, in[i+1], in[i], in[i+2])
			}
		}
		return out
	case gltf.PrimitiveTriangleFan:
		var out []uint32
		for i := 1; i+1 < len(in); i++ {
			out = append(out, in[0], in[i], in[i+1])
		}
		return out
	}
	return in[:len(in)/3*3]
}

// flat gives each triangle vertices of its own, with the triangle's normal.
func flat(positions [][3]float32, uvs [][2]float32, indices []uint32) ([][3]float32, [][2]float32, []uint32, [][3]float32) {
	ps := make([][3]float32, 0, len(indices))
	ns := make([][3]float32, 0, len(indices))
	var us [][2]float32
	if len(uvs) == len(positions) {
		us = make([][2]float32, 0, len(indices))
	}
	is := make([]uint32, 0, len(indices))
	for t := 0; t+2 < len(indices); t += 3 {
		a, b, c := positions[indices[t]], positions[indices[t+1]], positions[indices[t+2]]
		n := normal(a, b, c)
		for k := range 3 {
			is = append(is, uint32(len(ps))) //nolint:gosec // a mesh's vertex count fits
			ps = append(ps, positions[indices[t+k]])
			ns = append(ns, n)
			if us != nil {
				us = append(us, uvs[indices[t+k]])
			}
		}
	}
	return ps, us, is, ns
}

func normal(a, b, c [3]float32) [3]float32 {
	u := [3]float32{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float32{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	n := [3]float32{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
	l := float32(math.Sqrt(float64(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])))
	if l == 0 {
		return [3]float32{0, 1, 0}
	}
	return [3]float32{n[0] / l, n[1] / l, n[2] / l}
}

func flatten3(v [][3]float32) []float32 {
	out := make([]float32, 0, 3*len(v))
	for _, p := range v {
		out = append(out, p[0], p[1], p[2])
	}
	return out
}

// texCoord is the texture coordinate set a material's base color image uses, if it has one.
func (g *gltfReader) texCoord(m *gltf.Material) (int, bool) {
	if m.PBRMetallicRoughness == nil || m.PBRMetallicRoughness.BaseColorTexture == nil {
		return 0, false
	}
	return m.PBRMetallicRoughness.BaseColorTexture.TexCoord, true
}

// material is a glTF material's base color (factor and image), metal and roughness.
func (g *gltfReader) material(m *gltf.Material) (Material, error) {
	out := Material{Color: [4]float32{1, 1, 1, 1}, Image: -1, Roughness: 1, Metallic: 1, DoubleSided: m.DoubleSided}
	pbr := m.PBRMetallicRoughness
	if pbr == nil {
		return out, nil
	}
	for i, v := range pbr.BaseColorFactorOrDefault() {
		out.Color[i] = float32(v)
	}
	out.Metallic, out.Roughness = float32(pbr.MetallicFactorOrDefault()), float32(pbr.RoughnessFactorOrDefault())
	if pbr.BaseColorTexture != nil {
		img, err := g.image(pbr.BaseColorTexture.Index)
		if err != nil {
			return out, fmt.Errorf("its material %q: %w", m.Name, err)
		}
		out.Image = img
	}
	return out, nil
}

// image is the scene's image a texture shows, read once.
func (g *gltfReader) image(texture int) (int, error) {
	if texture < 0 || texture >= len(g.doc.Textures) || g.doc.Textures[texture].Source == nil {
		return -1, fmt.Errorf("its texture %d has no image", texture)
	}
	src := *g.doc.Textures[texture].Source
	if i, ok := g.images[src]; ok {
		return i, nil
	}
	if src < 0 || src >= len(g.doc.Images) {
		return -1, fmt.Errorf("its texture %d names image %d, which the file does not have", texture, src)
	}
	im := g.doc.Images[src]
	var data []byte
	var err error
	mime := im.MimeType
	switch {
	case im.BufferView != nil:
		if *im.BufferView < 0 || *im.BufferView >= len(g.doc.BufferViews) {
			return -1, fmt.Errorf("its image %d is in buffer view %d, which the file does not have", src, *im.BufferView)
		}
		data, err = modeler.ReadBufferView(g.doc, g.doc.BufferViews[*im.BufferView])
	case im.IsEmbeddedResource():
		data, err = im.MarshalData()
		if mime == "" {
			mime, _, _ = strings.Cut(strings.TrimPrefix(im.URI, "data:"), ";")
		}
	case im.URI != "":
		data, err = os.ReadFile(filepath.Join(g.dir, filepath.FromSlash(im.URI)))
		if mime == "" {
			mime = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg"}[strings.ToLower(filepath.Ext(im.URI))]
		}
	default:
		return -1, fmt.Errorf("its image %d has no data", src)
	}
	if err != nil {
		return -1, fmt.Errorf("its image %d: %w", src, err)
	}
	if mime != "image/png" && mime != "image/jpeg" {
		return -1, fmt.Errorf("its image %d is %s: a material's image is a PNG or a JPEG", src, orUnknown(mime))
	}
	g.scene.Images = append(g.scene.Images, Image{MIME: mime, Data: data})
	g.images[src] = len(g.scene.Images) - 1
	return g.images[src], nil
}

func orUnknown(s string) string {
	if s == "" {
		return "of no known type"
	}
	return s
}
