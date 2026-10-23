package scene

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func dist(a, b [3]float32) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// The lane-cones scene, as DTCD's measure tool sees it: two cones 3.6 m apart, center to center,
// with the phone 8 m short of them or 14.
func TestLaneConesAreThreePointSixMetresApart(t *testing.T) {
	for file, at := range map[string][3]float32{"lane-cones.glb": {0, 1.4, 0}, "lane-cones-far.glb": {0, 1.4, 6}} {
		s, err := Load(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		if got := s.ObjectNames(); got != `"left cone", "right cone"` {
			t.Errorf("%s: objects %s", file, got)
		}
		l, _ := s.Object("left cone")
		r, _ := s.Object("right cone")
		if d := dist(l.Tap, r.Tap); math.Abs(d-3.6) > 1e-6 {
			t.Errorf("%s: the cones are %.6f m apart", file, d)
		}
		// A cone's middle: half its height, on its axis.
		if l.Tap != [3]float32{-1.8, 0.35, l.Tap[2]} {
			t.Errorf("%s: a tap on the left cone lands at %v", file, l.Tap)
		}
		if p := s.Phone.Origin(); p != at {
			t.Errorf("%s: the phone is at %v", file, p)
		}
		// The phone looks at the cones: its -Z points down the road, and down a little.
		fwd := [3]float32{-s.Phone[8], -s.Phone[9], -s.Phone[10]}
		if fwd[2] > -0.9 || fwd[1] >= 0 || math.Abs(float64(fwd[0])) > 1e-6 {
			t.Errorf("%s: the phone looks along %v", file, fwd)
		}
	}
}

// The parcels example's parcel on a porch: a tap on a corner guard lands on the corner it guards,
// 60 cm from the other, where the guards' middles are 62 cm apart.
func TestTheParcelsCornersAreItsLengthApart(t *testing.T) {
	s, err := Load(filepath.Join("..", "..", "..", "..", "examples", "parcels", "acceptance", "scenes", "parcel-on-porch.glb"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.ObjectNames(); got != `"front left corner", "front right corner"` {
		t.Errorf("objects %s", got)
	}
	l, _ := s.Object("front left corner")
	r, _ := s.Object("front right corner")
	if dist(l.Tap, [3]float32{-0.3, 0.3, -0.7}) > 1e-6 || dist(r.Tap, [3]float32{0.3, 0.3, -0.7}) > 1e-6 {
		t.Errorf("taps land at %v and %v", l.Tap, r.Tap)
	}
	if d := dist(l.Tap, r.Tap); math.Abs(d-0.6) > 1e-6 {
		t.Errorf("the corners are %.6f m apart", d)
	}
	// The courier walks up from the van: the path starts where the "by the van" camera is, and
	// ends at the foot of the porch, facing the door.
	van, err := s.Camera("by the van")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Path("up the garden path")
	if err != nil {
		t.Fatal(err)
	}
	if p.Duration != 9 || s.Nodes[p.Camera].Name != "phone" {
		t.Errorf("the path takes %gs, moving %q", p.Duration, s.Nodes[p.Camera].Name)
	}
	start, end := s.PoseOn(p, 0), s.PoseOn(p, p.Duration)
	near(t, "the path starts by the van", start.Origin(), van.Pose.Origin())
	near(t, "looking along the street", looks(start), looks(van.Pose))
	near(t, "it ends at the foot of the porch", end.Origin(), [3]float32{0, 1.4, 1.2})
	near(t, "facing the door", looks(end), [3]float32{0, 0, -1})
}

// builder writes small glTF documents for the conventions' cases.
type builder struct{ doc *gltf.Document }

func newBuilder() *builder { return &builder{doc: gltf.NewDocument()} }

func (b *builder) mesh(ps [][3]float32, indices []uint16, mode gltf.PrimitiveMode, attrs gltf.PrimitiveAttributes, mat *int) int {
	if attrs == nil {
		attrs = gltf.PrimitiveAttributes{}
	}
	attrs[gltf.POSITION] = modeler.WritePosition(b.doc, ps)
	p := &gltf.Primitive{Attributes: attrs, Mode: mode, Material: mat}
	if indices != nil {
		p.Indices = gltf.Index(modeler.WriteIndices(b.doc, indices))
	}
	b.doc.Meshes = append(b.doc.Meshes, &gltf.Mesh{Primitives: []*gltf.Primitive{p}})
	return len(b.doc.Meshes) - 1
}

// square is a mesh of a 2 m square, from -1 to 1 on x and z.
func (b *builder) square() int {
	return b.mesh([][3]float32{{-1, 0, -1}, {1, 0, -1}, {1, 0, 1}, {-1, 0, 1}}, []uint16{0, 2, 1, 0, 3, 2}, gltf.PrimitiveTriangles, nil, nil)
}

func (b *builder) node(n *gltf.Node, root bool) int {
	b.doc.Nodes = append(b.doc.Nodes, n)
	i := len(b.doc.Nodes) - 1
	if root {
		b.doc.Scenes[0].Nodes = append(b.doc.Scenes[0].Nodes, i)
	}
	return i
}

func (b *builder) camera(name string, at [3]float64, root bool) int {
	b.doc.Cameras = append(b.doc.Cameras, &gltf.Camera{Perspective: &gltf.Perspective{Yfov: 1, Znear: 0.01}})
	return b.node(&gltf.Node{Name: name, Camera: gltf.Index(len(b.doc.Cameras) - 1), Translation: at}, root)
}

func (b *builder) save(t *testing.T, ext string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scene"+ext)
	var err error
	if ext == ".glb" {
		err = gltf.SaveBinary(b.doc, p)
	} else {
		err = gltf.Save(b.doc, p)
	}
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestATapLandsOnTheTapChildElseTheMiddle(t *testing.T) {
	b := newBuilder()
	b.camera("", [3]float64{0, 1.5, 3}, true)
	// A parcel's corner guard: a tap on it lands on the corner it guards, not its middle.
	guard := b.node(&gltf.Node{Name: "front left corner", Mesh: gltf.Index(b.square()), Translation: [3]float64{1, 2, 3}, Scale: [3]float64{0.5, 1, 0.5}}, true)
	tap := b.node(&gltf.Node{Name: "_tap", Translation: [3]float64{-1, 0, 1}}, false)
	b.doc.Nodes[guard].Children = []int{tap}
	b.node(&gltf.Node{Name: "pallet", Mesh: gltf.Index(b.square()), Translation: [3]float64{10, 0, 0}}, true)
	s, err := Load(b.save(t, ".gltf"))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.Object("front left corner")
	if g.Tap != [3]float32{0.5, 2, 3.5} { // (-1, 0, 1) scaled by (0.5, 1, 0.5), moved by (1, 2, 3)
		t.Errorf("a tap on the guard lands at %v", g.Tap)
	}
	p, _ := s.Object("pallet")
	if p.Tap != [3]float32{10, 0, 0} {
		t.Errorf("a tap on the pallet lands at %v", p.Tap)
	}
}

func TestThePhone(t *testing.T) {
	cases := []struct {
		name  string
		build func(b *builder)
		phone [3]float32
		err   string
	}{
		{"the only camera", func(b *builder) { b.camera("Camera", [3]float64{0, 1, 2}, true) }, [3]float32{0, 1, 2}, ""},
		{"the one named phone", func(b *builder) {
			b.camera("drone", [3]float64{0, 30, 0}, true)
			b.camera("phone", [3]float64{1, 1.4, 0}, true)
		}, [3]float32{1, 1.4, 0}, ""},
		{"a camera held in a node", func(b *builder) {
			c := b.camera("phone", [3]float64{0, 1.6, 0}, false)
			b.node(&gltf.Node{Name: "crew member", Translation: [3]float64{2, 0, -1}, Children: []int{c}}, true)
		}, [3]float32{2, 1.6, -1}, ""},
		{"no camera", func(b *builder) {}, [3]float32{}, "it has no camera: add one where the phone is"},
		{"two unnamed cameras", func(b *builder) {
			b.camera("left", [3]float64{}, true)
			b.camera("right", [3]float64{}, true)
		}, [3]float32{}, `it has 2 cameras ("left", "right"): name the one where the phone is "phone"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBuilder()
			b.node(&gltf.Node{Name: "_ground", Mesh: gltf.Index(b.square())}, true)
			c.build(b)
			s, err := Load(b.save(t, ".glb"))
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p := s.Phone.Origin(); p != c.phone {
				t.Errorf("the phone is at %v, want %v", p, c.phone)
			}
			// The phone, and a node that holds it, are not objects; nor is the ground.
			if len(s.Objects) != 0 {
				t.Errorf("objects %s", s.ObjectNames())
			}
		})
	}
}

func TestObjectsNeedNamesOfTheirOwn(t *testing.T) {
	b := newBuilder()
	b.camera("", [3]float64{}, true)
	b.node(&gltf.Node{Name: "cone", Mesh: gltf.Index(b.square())}, true)
	b.node(&gltf.Node{Name: "cone", Mesh: gltf.Index(b.square())}, true)
	_, err := Load(b.save(t, ".glb"))
	if err == nil || !strings.Contains(err.Error(), `two objects named "cone"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestOnlyGLTFPlays(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lane-cones.scn")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || err.Error() != "lane-cones.scn is not a scene axx plays: a scene is a glTF file (.glb, .gltf)" {
		t.Fatalf("err = %v", err)
	}
}

func decode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A strip's triangles, a mesh with no normals drawn flat, a material's color and image: what
// the device reads.
func TestTheDescriptionCarriesMeshesAndMaterials(t *testing.T) {
	b := newBuilder()
	b.camera("", [3]float64{0, 2, 2}, true)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{200, 150, 90, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	im, err := modeler.WriteImage(b.doc, "cardboard", "image/png", &buf)
	if err != nil {
		t.Fatal(err)
	}
	b.doc.Textures = append(b.doc.Textures, &gltf.Texture{Source: gltf.Index(im)})
	b.doc.Materials = append(b.doc.Materials, &gltf.Material{Name: "cardboard", DoubleSided: true, PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorFactor: &[4]float64{1, 0.9, 0.8, 1}, MetallicFactor: gltf.Float(0), RoughnessFactor: gltf.Float(0.7),
		BaseColorTexture: &gltf.TextureInfo{Index: 0},
	}})
	uv := modeler.WriteTextureCoord(b.doc, [][2]float32{{0, 0}, {1, 0}, {0, 1}, {1, 1}})
	strip := b.mesh([][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 0, -1}, {1, 0, -1}}, nil, gltf.PrimitiveTriangleStrip,
		gltf.PrimitiveAttributes{gltf.TEXCOORD_0: uv}, gltf.Index(0))
	b.node(&gltf.Node{Name: "parcel", Mesh: gltf.Index(strip)}, true)
	s, err := Load(b.save(t, ".glb"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := s.Encode(&out); err != nil {
		t.Fatal(err)
	}
	var d struct {
		Phone Mat4
		Roots []int
		Nodes []struct {
			Name   string
			Meshes []struct{ Positions, Normals, UVs, Indices string }
		}
		Materials []struct {
			Color               [4]float32
			Image               int
			Metallic, Roughness float32
			DoubleSided         bool
		}
		Images  []string
		Objects []struct {
			Name string
			Node int
			Tap  [3]float32
		}
	}
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	var parcel int
	for i, n := range d.Nodes {
		if n.Name == "parcel" {
			parcel = i
		}
	}
	m := d.Nodes[parcel].Meshes[0]
	// Two triangles of a strip of four vertices, drawn flat: each its own three vertices, facing up.
	if n := len(decode(t, m.Indices)) / 4; n != 6 {
		t.Errorf("%d indices", n)
	}
	if n := len(decode(t, m.Positions)) / 12; n != 6 {
		t.Errorf("%d vertices", n)
	}
	normals := decode(t, m.Normals)
	for v := 0; v < len(normals); v += 12 {
		y := math.Float32frombits(uint32(normals[v+4]) | uint32(normals[v+5])<<8 | uint32(normals[v+6])<<16 | uint32(normals[v+7])<<24)
		if math.Abs(float64(y)-1) > 1e-6 {
			t.Errorf("vertex %d's normal is not up: y %v", v/12, y)
		}
	}
	if n := len(decode(t, m.UVs)) / 8; n != 6 {
		t.Errorf("%d texture coordinates", n)
	}
	mat := d.Materials[0]
	if mat.Color != [4]float32{1, 0.9, 0.8, 1} || mat.Image != 0 || mat.Metallic != 0 || mat.Roughness != 0.7 || !mat.DoubleSided {
		t.Errorf("material %+v", mat)
	}
	if got, err := png.Decode(bytes.NewReader(decode(t, d.Images[0]))); err != nil || got.Bounds().Dx() != 2 {
		t.Errorf("image: %v", err)
	}
	if len(d.Objects) != 1 || d.Objects[0].Name != "parcel" || d.Objects[0].Node != parcel || d.Objects[0].Tap != [3]float32{0.5, 0, -0.5} {
		t.Errorf("objects %+v", d.Objects)
	}
	if d.Phone.Origin() != [3]float32{0, 2, 2} {
		t.Errorf("phone %v", d.Phone)
	}
}
