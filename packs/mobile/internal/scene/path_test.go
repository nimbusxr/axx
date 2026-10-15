package scene

import (
	"math"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func near(t *testing.T, what string, got, want [3]float32) {
	t.Helper()
	if dist(got, want) > 1e-4 {
		t.Errorf("%s: %v, want %v", what, got, want)
	}
}

// looks is the way a pose looks: its −Z.
func looks(m Mat4) [3]float32 { return [3]float32{-m[8], -m[9], -m[10]} }

// tilted is a phone at at, looking along −Z tilted down by degrees.
func tilted(at [3]float32, degrees float64) Mat4 {
	r := -degrees * math.Pi / 180 / 2
	return TRS{T: at, R: [4]float32{float32(math.Sin(r)), 0, 0, float32(math.Cos(r))}, S: [3]float32{1, 1, 1}}.Mat4()
}

func TestATransformIsItsTranslationRotationAndScale(t *testing.T) {
	in := TRS{T: [3]float32{1, 2, 3}, R: normalized([4]float64{0.2, -0.4, 0.1, 0.9}), S: [3]float32{2, 0.5, 1}}
	out := in.Mat4().TRS()
	near(t, "translation", out.T, in.T)
	near(t, "scale", out.S, in.S)
	q := out.R
	if q[3]*in.R[3] < 0 { // −q is the same rotation
		q = [4]float32{-q[0], -q[1], -q[2], -q[3]}
	}
	near(t, "rotation", [3]float32(q[:3]), [3]float32(in.R[:3]))
}

// A courier stands over a parcel, the phone tilted 40° down: steps and turns as a person makes them.
func TestAPhoneStepsAndTurnsAsItsCarrierDoes(t *testing.T) {
	phone := tilted([3]float32{0, 1.25, 0.2}, 40)
	down := float32(math.Sin(40 * math.Pi / 180))
	steps := []struct {
		metres float64
		dir    string
		at     [3]float32
	}{
		{1, "forward", [3]float32{0, 1.25, -0.8}},
		{1, "back", [3]float32{0, 1.25, 1.2}},
		{1.5, "right", [3]float32{1.5, 1.25, 0.2}},
		{1.5, "left", [3]float32{-1.5, 1.25, 0.2}},
		{0.5, "up", [3]float32{0, 1.75, 0.2}},
		{0.5, "down", [3]float32{0, 0.75, 0.2}},
	}
	for _, s := range steps {
		m, err := phone.Moved(s.metres, s.dir)
		if err != nil {
			t.Fatal(err)
		}
		near(t, s.dir, m.Origin(), s.at)
		near(t, s.dir+": still looks", looks(m), looks(phone))
	}
	turns := []struct {
		degrees float64
		dir     string
		looks   [3]float32
	}{
		{90, "left", [3]float32{-float32(math.Cos(40 * math.Pi / 180)), -down, 0}},
		{90, "right", [3]float32{float32(math.Cos(40 * math.Pi / 180)), -down, 0}},
		{40, "up", [3]float32{0, 0, -1}},
		{50, "down", [3]float32{0, -1, 0}},
	}
	for _, s := range turns {
		m, err := phone.Turned(s.degrees, s.dir)
		if err != nil {
			t.Fatal(err)
		}
		near(t, "turned "+s.dir, looks(m), s.looks)
		near(t, "turned "+s.dir+": stands", m.Origin(), phone.Origin())
	}
	// Looking straight down, forward is where the top of the phone points.
	m, _ := tilted([3]float32{0, 1, 0}, 90).Moved(1, "forward")
	near(t, "forward, looking down", m.Origin(), [3]float32{0, 1, -1})
	if _, err := phone.Moved(1, "sideways"); err == nil || !strings.Contains(err.Error(), "forward, back, left, right, up or down") {
		t.Errorf("err = %v", err)
	}
	if _, err := phone.Turned(10, "back"); err == nil || !strings.Contains(err.Error(), "left, right, up or down") {
		t.Errorf("err = %v", err)
	}
}

func TestBetweenTwoPosesIsAlongTheWay(t *testing.T) {
	a, b := tilted([3]float32{0, 1, 0}, 0), tilted([3]float32{2, 1, -4}, 60)
	m := Between(a, b, 0.5)
	near(t, "halfway", m.Origin(), [3]float32{1, 1, -2})
	r := 30 * math.Pi / 180
	near(t, "half the tilt", looks(m), [3]float32{0, -float32(math.Sin(r)), -float32(math.Cos(r))})
	near(t, "the start", Between(a, b, 0).Origin(), a.Origin())
	near(t, "the end", looks(Between(a, b, 1)), looks(b))
}

// animate adds an animation of a node's property to the builder's document.
func (b *builder) animate(name string, node int, prop gltf.TRSProperty, interp gltf.Interpolation, times []float32, values any) {
	in := modeler.WriteAccessor(b.doc, gltf.TargetNone, times)
	out := modeler.WriteAccessor(b.doc, gltf.TargetNone, values)
	var a *gltf.Animation
	for _, x := range b.doc.Animations {
		if x.Name == name {
			a = x
		}
	}
	if a == nil {
		a = &gltf.Animation{Name: name}
		b.doc.Animations = append(b.doc.Animations, a)
	}
	a.Samplers = append(a.Samplers, &gltf.AnimationSampler{Input: in, Output: out, Interpolation: interp})
	a.Channels = append(a.Channels, &gltf.AnimationChannel{Sampler: len(a.Samplers) - 1, Target: gltf.AnimationChannelTarget{Node: gltf.Index(node), Path: prop}})
}

// A courier walks up a garden path to a porch: an animation of the phone, or of a node holding it.
func TestAPathIsAnAnimationThatMovesACamera(t *testing.T) {
	b := newBuilder()
	b.node(&gltf.Node{Name: "_ground", Mesh: gltf.Index(b.square())}, true)
	phone := b.camera("phone", [3]float64{0, 1.4, 6}, true)
	porch := b.camera("porch", [3]float64{0, 1.4, 1}, true)
	courier := b.node(&gltf.Node{Name: "_courier"}, true)
	drone := b.camera("drone", [3]float64{0, 30, 0}, false)
	b.doc.Nodes[courier].Children = []int{drone}
	half := float32(math.Sqrt(0.5))
	// Up the path, turning left at the porch.
	b.animate("up the garden path", phone, gltf.TRSTranslation, gltf.InterpolationLinear, []float32{0, 4}, [][3]float32{{0, 1.4, 6}, {0, 1.4, 1}})
	b.animate("up the garden path", phone, gltf.TRSRotation, gltf.InterpolationLinear, []float32{4, 5}, [][4]float32{{0, 0, 0, 1}, {0, half, 0, half}})
	// A hop, as steps: there, then here.
	b.animate("hop", phone, gltf.TRSTranslation, gltf.InterpolationStep, []float32{0, 1}, [][3]float32{{0, 1.4, 6}, {0, 1.4, 4}})
	// A smooth walk: a cubic spline with no tangents eases in and out.
	b.animate("ease", phone, gltf.TRSTranslation, gltf.InterpolationCubicSpline, []float32{0, 2}, [][3]float32{{}, {0, 1.4, 6}, {}, {}, {0, 1.4, 2}, {}})
	// A node holding a camera that is not the phone's: the drone it carries goes along.
	b.animate("survey", courier, gltf.TRSTranslation, gltf.InterpolationLinear, []float32{0, 10}, [][3]float32{{0, 0, 0}, {10, 0, 0}})
	b.animate("both", phone, gltf.TRSTranslation, gltf.InterpolationLinear, []float32{0, 1}, [][3]float32{{0, 1.4, 6}, {0, 1.4, 5}})
	b.animate("both", porch, gltf.TRSTranslation, gltf.InterpolationLinear, []float32{0, 1}, [][3]float32{{0, 1.4, 1}, {0, 1.4, 0}})
	b.animate("swing", porch, gltf.TRSTranslation, gltf.InterpolationLinear, []float32{0, 1}, [][3]float32{{0, 1.4, 1}, {0, 1.4, 0}})
	b.animate("swing", courier, gltf.TRSTranslation, gltf.InterpolationLinear, []float32{0, 1}, [][3]float32{{0, 0, 0}, {1, 0, 0}})
	gate := b.node(&gltf.Node{Name: "gate", Mesh: gltf.Index(b.square())}, true)
	b.animate("gate opens", gate, gltf.TRSRotation, gltf.InterpolationLinear, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, half, 0, half}})
	s, err := Load(b.save(t, ".glb"))
	if err != nil {
		t.Fatal(err)
	}

	p, err := s.Path("up the garden path")
	if err != nil {
		t.Fatal(err)
	}
	if p.Duration != 5 || p.Camera != phone {
		t.Errorf("the path takes %gs, moving node %d", p.Duration, p.Camera)
	}
	near(t, "halfway up", s.PoseOn(p, 2).Origin(), [3]float32{0, 1.4, 3.5})
	near(t, "before the turn", looks(s.PoseOn(p, 4)), [3]float32{0, 0, -1})
	near(t, "half turned", looks(s.PoseOn(p, 4.5)), [3]float32{-half, 0, -half})
	near(t, "at the porch", looks(s.PoseOn(p, 5)), [3]float32{-1, 0, 0})
	near(t, "after the end", s.PoseOn(p, 9).Origin(), [3]float32{0, 1.4, 1})

	hop, _ := s.Path("hop")
	near(t, "a step holds", s.PoseOn(hop, 0.99).Origin(), [3]float32{0, 1.4, 6})
	near(t, "then moves", s.PoseOn(hop, 1).Origin(), [3]float32{0, 1.4, 4})
	ease, _ := s.Path("ease")
	near(t, "eased halfway", s.PoseOn(ease, 1).Origin(), [3]float32{0, 1.4, 4})
	near(t, "eased, slow to start", s.PoseOn(ease, 0.2).Origin(), [3]float32{0, 1.4, 6 - 4*(3*0.01-2*0.001)})
	survey, _ := s.Path("survey")
	if survey.Camera != drone {
		t.Errorf("the survey moves node %d, not the drone", survey.Camera)
	}
	near(t, "the drone, carried", s.PoseOn(survey, 5).Origin(), [3]float32{5, 30, 0})
	both, _ := s.Path("both")
	if both.Camera != phone {
		t.Errorf("a path that moves the phone and another camera moves node %d", both.Camera)
	}

	if c, err := s.Camera("porch"); err != nil || c.Pose.Origin() != [3]float32{0, 1.4, 1} {
		t.Errorf("the porch camera: %v, %v", c.Pose.Origin(), err)
	}
	for name, want := range map[string]string{
		"swing":      `the scene's "swing" animation is no path: it moves 2 cameras ("porch", "drone"): a phone follows one, the one named "phone"`,
		"gate opens": `the scene's "gate opens" animation is no path: it moves no camera`,
		"walk":       `the scene has no "walk" path: its animations are "up the garden path", "hop", "ease", "survey", "both", "swing", "gate opens"`,
	} {
		if _, err := s.Path(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := s.Camera("van"); err == nil || !strings.Contains(err.Error(), `the scene has no "van" camera: its cameras are "phone", "porch", "drone"`) {
		t.Errorf("err = %v", err)
	}
}
