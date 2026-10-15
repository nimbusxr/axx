package scene

import (
	"fmt"
	"math"
)

// TRS is a transform as a translation, a rotation (a unit quaternion: x, y, z, w) and a scale,
// glTF's way of writing a node's transform, and the way its animations change it.
type TRS struct {
	T [3]float32
	R [4]float32
	S [3]float32
}

// Mat4 is the transform: scale, then rotate, then translate.
func (t TRS) Mat4() Mat4 {
	x, y, z, w := t.R[0], t.R[1], t.R[2], t.R[3]
	r := [9]float32{ // column by column
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w),
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w),
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y),
	}
	var m Mat4
	for c := range 3 {
		for k := range 3 {
			m[c*4+k] = r[c*3+k] * t.S[c]
		}
	}
	m[12], m[13], m[14], m[15] = t.T[0], t.T[1], t.T[2], 1
	return m
}

// TRS is the transform as a translation, a rotation and a scale: a transform with no shear, as
// a scene's nodes and a phone's poses are.
func (m Mat4) TRS() TRS {
	out := TRS{T: [3]float32{m[12], m[13], m[14]}}
	var r [9]float64 // the rotation, column by column
	for c := range 3 {
		col := [3]float64{float64(m[c*4]), float64(m[c*4+1]), float64(m[c*4+2])}
		s := math.Sqrt(col[0]*col[0] + col[1]*col[1] + col[2]*col[2])
		out.S[c] = float32(s)
		if s == 0 {
			s = 1
		}
		for k := range 3 {
			r[c*3+k] = col[k] / s
		}
	}
	// r(row, col) is r[col*3+row].
	r00, r11, r22 := r[0], r[4], r[8]
	r10, r01, r20, r02, r21, r12 := r[1], r[3], r[2], r[6], r[5], r[7]
	var x, y, z, w float64
	switch tr := r00 + r11 + r22; {
	case tr > 0:
		s := math.Sqrt(tr+1) * 2
		w, x, y, z = s/4, (r21-r12)/s, (r02-r20)/s, (r10-r01)/s
	case r00 > r11 && r00 > r22:
		s := math.Sqrt(1+r00-r11-r22) * 2
		w, x, y, z = (r21-r12)/s, s/4, (r01+r10)/s, (r02+r20)/s
	case r11 > r22:
		s := math.Sqrt(1+r11-r00-r22) * 2
		w, x, y, z = (r02-r20)/s, (r01+r10)/s, s/4, (r12+r21)/s
	default:
		s := math.Sqrt(1+r22-r00-r11) * 2
		w, x, y, z = (r10-r01)/s, (r02+r20)/s, (r12+r21)/s, s/4
	}
	out.R = normalized([4]float64{x, y, z, w})
	return out
}

func normalized(q [4]float64) [4]float32 {
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	if n == 0 {
		return [4]float32{0, 0, 0, 1}
	}
	return [4]float32{float32(q[0] / n), float32(q[1] / n), float32(q[2] / n), float32(q[3] / n)}
}

// slerp is the rotation f of the way from a to b, turning the shortest way.
func slerp(a, b [4]float32, f float64) [4]float32 {
	dot := float64(a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3])
	if dot < 0 { // the same rotation as -b, the shorter way round
		b, dot = [4]float32{-b[0], -b[1], -b[2], -b[3]}, -dot
	}
	ka, kb := 1-f, f
	if dot < 0.9995 {
		theta := math.Acos(math.Min(dot, 1))
		ka, kb = math.Sin((1-f)*theta)/math.Sin(theta), math.Sin(f*theta)/math.Sin(theta)
	}
	var q [4]float64
	for i := range 4 {
		q[i] = ka*float64(a[i]) + kb*float64(b[i])
	}
	return normalized(q)
}

func lerp3(a, b [3]float32, f float64) [3]float32 {
	var out [3]float32
	for i := range 3 {
		out[i] = a[i] + float32(f)*(b[i]-a[i])
	}
	return out
}

// Between is the pose f of the way from a to b (0 is a, 1 is b): moved along the straight line
// between them, and turned the shortest way.
func Between(a, b Mat4, f float64) Mat4 {
	ta, tb := a.TRS(), b.TRS()
	return TRS{T: lerp3(ta.T, tb.T, f), R: slerp(ta.R, tb.R, f), S: lerp3(ta.S, tb.S, f)}.Mat4()
}

// Up is the scene's up: glTF's +Y.
var Up = [3]float64{0, 1, 0}

// ground is the way along the ground that a phone at pose m faces: where it looks, level; or,
// when it looks straight down or up, where the top of the phone points, level.
func (m Mat4) ground() [3]float64 {
	for _, v := range [][3]float64{{-float64(m[8]), 0, -float64(m[10])}, {float64(m[4]), 0, float64(m[6])}} {
		if n := math.Hypot(v[0], v[2]); n > 1e-6 {
			return [3]float64{v[0] / n, 0, v[2] / n}
		}
	}
	return [3]float64{0, 0, -1}
}

// Moved is the pose of a phone carried metres toward direction, as a person steps with it, still
// facing the same way: forward and back along the ground the way the phone looks, left and right
// across it, up and down.
func (m Mat4) Moved(metres float64, direction string) (Mat4, error) {
	f := m.ground()
	var d [3]float64
	switch direction {
	case "forward":
		d = f
	case "back":
		d = [3]float64{-f[0], 0, -f[2]}
	case "right":
		d = [3]float64{-f[2], 0, f[0]}
	case "left":
		d = [3]float64{f[2], 0, -f[0]}
	case "up":
		d = Up
	case "down":
		d = [3]float64{0, -1, 0}
	default:
		return m, fmt.Errorf("a phone moves forward, back, left, right, up or down, not %q", direction)
	}
	out := m
	for i := range 3 {
		out[12+i] += float32(d[i] * metres)
	}
	return out, nil
}

// Turned is the pose of a phone turned degrees toward direction, where it stands, as the person
// holding it turns: left and right about the upright through it, keeping its tilt; up and down
// about its own sideways axis, tilting it.
func (m Mat4) Turned(degrees float64, direction string) (Mat4, error) {
	r := degrees * math.Pi / 180
	switch direction {
	case "left", "right":
		if direction == "right" {
			r = -r
		}
		// About the world's up, through the phone: the turn is applied in the world, around its place.
		c, s := float32(math.Cos(r)), float32(math.Sin(r))
		turn := Mat4{c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, 0, 0, 0, 1}
		at := m.Origin()
		out := turn.Mul(m)
		out[12], out[13], out[14] = at[0], at[1], at[2]
		return out, nil
	case "up", "down":
		if direction == "down" {
			r = -r
		}
		// About the phone's own x: the turn is applied in the phone's frame.
		c, s := float32(math.Cos(r)), float32(math.Sin(r))
		return m.Mul(Mat4{1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1}), nil
	}
	return m, fmt.Errorf("a phone turns left, right, up or down, not %q", direction)
}
