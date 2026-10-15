package scene

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Camera is one of the scene's cameras: a place a phone can be, and which way it looks there.
type Camera struct {
	Name string
	Node int
	Pose Mat4 // its world transform
}

// A Path is one of the scene's animations that moves a camera: the way a phone goes through the
// scene, carried, as the animation moves that camera. A phone that travels the path follows it.
type Path struct {
	Name string
	// Camera is the camera node the path moves: the phone's, else the only one it moves; -1 when
	// it moves none, or several but the phone's.
	Camera int
	// Duration is how long the path takes as recorded, in seconds: its last keyframe.
	Duration float64
	// why tells why the path has no camera.
	why      string
	channels []channel
}

// A channel is an animation's keyframes for one property of one node.
type channel struct {
	node   int
	prop   string // translation, rotation or scale
	interp string // LINEAR, STEP or CUBICSPLINE
	times  []float32
	// values are the keyframes' values (x, y, z, and w for a rotation); a cubic spline's are
	// three a keyframe: the in-tangent, the value and the out-tangent.
	values [][4]float32
}

// An animation is one of a scene file's animations, as its loader reads it.
type animation struct {
	name     string
	channels []channel
}

// at is the channel's value t seconds in: held before its first keyframe and after its last.
func (c channel) at(t float32) [4]float32 {
	n := len(c.times)
	cubic := c.interp == "CUBICSPLINE"
	value := func(k int) [4]float32 {
		if cubic {
			return c.values[3*k+1]
		}
		return c.values[k]
	}
	if t <= c.times[0] {
		return value(0)
	}
	if t >= c.times[n-1] {
		return value(n - 1)
	}
	k := sort.Search(n, func(i int) bool { return c.times[i] > t }) - 1
	td := float64(c.times[k+1] - c.times[k])
	s := float64(t-c.times[k]) / td
	var out [4]float32
	switch c.interp {
	case "STEP":
		return value(k)
	case "CUBICSPLINE":
		// glTF's Hermite spline: from value k, leaving along its out-tangent, to value k+1,
		// arriving along its in-tangent; the tangents scaled by the keyframes' gap.
		v0, b0, v1, a1 := c.values[3*k+1], c.values[3*k+2], c.values[3*(k+1)+1], c.values[3*(k+1)]
		s2, s3 := s*s, s*s*s
		h00, h10, h01, h11 := 2*s3-3*s2+1, s3-2*s2+s, -2*s3+3*s2, s3-s2
		var q [4]float64
		for i := range 4 {
			q[i] = h00*float64(v0[i]) + h10*td*float64(b0[i]) + h01*float64(v1[i]) + h11*td*float64(a1[i])
		}
		if c.prop == "rotation" {
			return normalized(q)
		}
		for i := range 4 {
			out[i] = float32(q[i])
		}
		return out
	}
	if c.prop == "rotation" {
		return slerp(c.values[k], c.values[k+1], s)
	}
	t3 := lerp3([3]float32(c.values[k][:3]), [3]float32(c.values[k+1][:3]), s)
	return [4]float32{t3[0], t3[1], t3[2], 0}
}

// paths makes the scene's animations its paths: the camera each moves, and how long it takes.
func (s *Scene) paths(anims []animation, phone int) {
	for _, a := range anims {
		p := Path{Name: a.name, Camera: -1, channels: a.channels}
		moved := map[int]bool{}
		for _, c := range a.channels {
			p.Duration = math.Max(p.Duration, float64(c.times[len(c.times)-1]))
			moved[c.node] = true
		}
		var cams []int
		for _, c := range s.Cameras {
			for n := c.Node; n >= 0; n = s.parents[n] {
				if moved[n] {
					cams = append(cams, c.Node)
					break
				}
			}
		}
		switch {
		case len(cams) == 0:
			p.why = "it moves no camera: a path moves the camera a phone follows"
		case len(cams) == 1:
			p.Camera = cams[0]
		default:
			names := make([]string, len(cams))
			for i, c := range cams {
				names[i] = quote(s.Nodes[c].Name)
				if c == phone {
					p.Camera = c
				}
			}
			if p.Camera < 0 {
				p.why = fmt.Sprintf("it moves %d cameras (%s): a phone follows one, the one named %q", len(cams), strings.Join(names, ", "), Phone)
			}
		}
		s.Paths = append(s.Paths, p)
	}
}

// Path is the scene's path of that name: an animation that moves a camera.
func (s *Scene) Path(name string) (*Path, error) {
	for i := range s.Paths {
		if p := &s.Paths[i]; p.Name == name {
			if p.Camera < 0 {
				return nil, fmt.Errorf("the scene's %q animation is no path: %s", name, p.why)
			}
			return p, nil
		}
	}
	if len(s.Paths) == 0 {
		return nil, fmt.Errorf("the scene has no %q path: it has no animations, and a path is an animation that moves a camera", name)
	}
	return nil, fmt.Errorf("the scene has no %q path: its animations are %s", name, s.PathNames())
}

// PathNames are the names of the scene's animations, quoted, for messages.
func (s *Scene) PathNames() string {
	names := make([]string, len(s.Paths))
	for i, p := range s.Paths {
		names[i] = quote(p.Name)
	}
	return strings.Join(names, ", ")
}

// PoseOn is where the camera the path moves is t seconds into it, and which way it looks.
func (s *Scene) PoseOn(p *Path, t float64) Mat4 {
	animated := map[int]TRS{}
	for _, c := range p.channels {
		trs, ok := animated[c.node]
		if !ok {
			trs = s.Nodes[c.node].Local.TRS()
		}
		v := c.at(float32(t))
		switch c.prop {
		case "translation":
			trs.T = [3]float32(v[:3])
		case "rotation":
			trs.R = v
		case "scale":
			trs.S = [3]float32(v[:3])
		}
		animated[c.node] = trs
	}
	m := Identity
	for n := p.Camera; n >= 0; n = s.parents[n] {
		local := s.Nodes[n].Local
		if trs, ok := animated[n]; ok {
			local = trs.Mat4()
		}
		m = local.Mul(m)
	}
	return m
}

// Camera is the scene's camera of that name.
func (s *Scene) Camera(name string) (Camera, error) {
	for _, c := range s.Cameras {
		if c.Name == name {
			return c, nil
		}
	}
	names := make([]string, len(s.Cameras))
	for i, c := range s.Cameras {
		names[i] = quote(c.Name)
	}
	return Camera{}, fmt.Errorf("the scene has no %q camera: its cameras are %s", name, strings.Join(names, ", "))
}
