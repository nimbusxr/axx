// Package scene is a 3D scene as axx plays it to an app's camera, whatever file it came from:
// what is in front of the phone, where the phone is, and the objects a scenario names.
//
// A format's loader reads a file into a Scene of nodes, meshes and materials (glTF and GLB
// today); the scene's conventions, the same for every format, then find the phone and the
// objects in it. A device plays the scene from its description (Encode), which carries
// everything the device needs: it reads no format of its own.
package scene

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

// Scene is a scene ready to play: its nodes, the phone's pose and the objects a scenario names.
type Scene struct {
	// Nodes are the scene's nodes; Roots are those at its top, the others their children.
	Nodes []Node
	Roots []int
	// Materials and Images are what the meshes are made of.
	Materials []Material
	Images    []Image
	// Phone is where the phone is, and which way it looks: its world transform, the phone held
	// upright, looking along its −Z, its +Y toward the top of the phone (a glTF camera's axes).
	Phone Mat4
	// Objects are the scene's objects a scenario can name.
	Objects []Object
}

// Node is a node of the scene: a transform, and what it holds.
type Node struct {
	Name     string
	Local    Mat4 // its transform within its parent's
	Children []int
	Meshes   []Mesh
	Camera   bool // a camera: where a phone can be
}

// Mesh is a mesh of triangles.
type Mesh struct {
	Positions []float32 // x, y, z of each vertex
	Normals   []float32 // x, y, z of each vertex
	UVs       []float32 // u, v of each vertex, when its material has an image
	Indices   []uint32  // three vertices a triangle
	Material  int       // -1 for none
}

// Material is what a mesh is made of: its base color, an image on it, and how it shines.
type Material struct {
	Color       [4]float32 // linear RGBA, multiplying the image
	Image       int        // -1 for none
	Metallic    float32
	Roughness   float32
	DoubleSided bool
}

// Image is an image a material shows: PNG or JPEG.
type Image struct {
	MIME string
	Data []byte
}

// Object is one of the scene's objects a scenario names: a top-level node of the scene with a
// name, but for its cameras, a node holding the phone, and the scene's furniture (names starting
// with _).
type Object struct {
	Name string
	Node int
	// Tap is where a tap on the object lands, in the world: its _tap child's place, else the
	// middle of the object.
	Tap [3]float32
}

// Mat4 is a 4×4 transform, column by column (glTF's and SceneKit's order).
type Mat4 [16]float32

// Identity is the transform that moves nothing.
var Identity = Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}

// Mul is m·n: n, then m.
func (m Mat4) Mul(n Mat4) Mat4 {
	var out Mat4
	for c := range 4 {
		for r := range 4 {
			var s float32
			for k := range 4 {
				s += m[k*4+r] * n[c*4+k]
			}
			out[c*4+r] = s
		}
	}
	return out
}

// Apply is where m takes the point p.
func (m Mat4) Apply(p [3]float32) [3]float32 {
	return [3]float32{
		m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12],
		m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13],
		m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14],
	}
}

// Origin is where m takes the origin.
func (m Mat4) Origin() [3]float32 { return [3]float32{m[12], m[13], m[14]} }

// A Loader reads a scene file of its format into nodes, meshes and materials: Nodes, Roots,
// Materials and Images. Load finds the phone and the objects.
type Loader func(path string) (*Scene, error)

// loaders are the formats axx plays, by file extension.
var loaders = map[string]Loader{
	".glb":  loadGLTF,
	".gltf": loadGLTF,
}

// Formats are the file extensions of the formats axx plays.
func Formats() []string {
	out := make([]string, 0, len(loaders))
	for ext := range loaders {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// Load reads a scene file and finds the phone and the objects in it.
func Load(path string) (*Scene, error) {
	ext := strings.ToLower(filepath.Ext(path))
	load, ok := loaders[ext]
	if !ok {
		return nil, fmt.Errorf("%s is not a scene axx plays: a scene is a glTF file (%s)", filepath.Base(path), strings.Join(Formats(), ", "))
	}
	s, err := load(path)
	if err != nil {
		return nil, fmt.Errorf("the scene %s: %w", filepath.Base(path), err)
	}
	if err := s.conventions(); err != nil {
		return nil, fmt.Errorf("the scene %s: %w", filepath.Base(path), err)
	}
	return s, nil
}

// Phone is the name of the camera node where the phone is, when the scene has several cameras.
const Phone = "phone"

// tapName is the name of an object's child that says where a tap on the object lands.
const tapName = "_tap"

// conventions finds the phone and the objects: the same for every format.
func (s *Scene) conventions() error {
	world := s.worldTransforms()
	var cameras []int
	phone := -1
	for i, n := range s.Nodes {
		if !n.Camera {
			continue
		}
		cameras = append(cameras, i)
		if n.Name == Phone {
			phone = i
		}
	}
	switch {
	case phone >= 0:
	case len(cameras) == 1:
		phone = cameras[0]
	case len(cameras) == 0:
		return fmt.Errorf("it has no camera: add one where the phone is, looking along its -Z, its +Y toward the top of the phone")
	default:
		names := make([]string, len(cameras))
		for i, c := range cameras {
			names[i] = quote(s.Nodes[c].Name)
		}
		return fmt.Errorf("it has %d cameras (%s): name the one where the phone is %q", len(cameras), strings.Join(names, ", "), Phone)
	}
	s.Phone = world[phone]
	seen := map[string]bool{}
	for _, r := range s.Roots {
		n := s.Nodes[r]
		if n.Name == "" || strings.HasPrefix(n.Name, "_") || n.Camera || s.holds(r, phone) {
			continue
		}
		if seen[n.Name] {
			return fmt.Errorf("it has two objects named %q: a scenario names each object, so each needs a name of its own", n.Name)
		}
		seen[n.Name] = true
		s.Objects = append(s.Objects, Object{Name: n.Name, Node: r, Tap: s.tapPoint(r, world)})
	}
	return nil
}

// holds is whether node a holds node b, among its children or deeper.
func (s *Scene) holds(a, b int) bool {
	for _, c := range s.Nodes[a].Children {
		if c == b || s.holds(c, b) {
			return true
		}
	}
	return false
}

// worldTransforms are the nodes' transforms in the world.
func (s *Scene) worldTransforms() []Mat4 {
	world := make([]Mat4, len(s.Nodes))
	var walk func(i int, parent Mat4)
	walk = func(i int, parent Mat4) {
		world[i] = parent.Mul(s.Nodes[i].Local)
		for _, c := range s.Nodes[i].Children {
			walk(c, world[i])
		}
	}
	for _, r := range s.Roots {
		walk(r, Identity)
	}
	return world
}

// tapPoint is where a tap on the object at node i lands: its _tap child's place; else the middle
// of its meshes, in the world; else its origin.
func (s *Scene) tapPoint(i int, world []Mat4) [3]float32 {
	for _, c := range s.Nodes[i].Children {
		if s.Nodes[c].Name == tapName {
			return world[c].Origin()
		}
	}
	lo := [3]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
	hi := [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
	found := false
	var walk func(n int)
	walk = func(n int) {
		for _, m := range s.Nodes[n].Meshes {
			for v := 0; v+2 < len(m.Positions); v += 3 {
				p := world[n].Apply([3]float32{m.Positions[v], m.Positions[v+1], m.Positions[v+2]})
				for k := range 3 {
					lo[k], hi[k] = min(lo[k], p[k]), max(hi[k], p[k])
				}
				found = true
			}
		}
		for _, c := range s.Nodes[n].Children {
			walk(c)
		}
	}
	walk(i)
	if !found {
		return world[i].Origin()
	}
	return [3]float32{(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2}
}

// ObjectNames are the names of the scene's objects, quoted, for messages.
func (s *Scene) ObjectNames() string {
	if len(s.Objects) == 0 {
		return "none: an object is a top-level node with a name"
	}
	names := make([]string, len(s.Objects))
	for i, o := range s.Objects {
		names[i] = quote(o.Name)
	}
	return strings.Join(names, ", ")
}

// Object is the scene's object of that name.
func (s *Scene) Object(name string) (Object, bool) {
	for _, o := range s.Objects {
		if o.Name == name {
			return o, true
		}
	}
	return Object{}, false
}

func quote(s string) string { return fmt.Sprintf("%q", s) }
