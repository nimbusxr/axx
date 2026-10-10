package mobilecore

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

func testJPEG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The streams send each frame as a part of a multipart response: by its
// Content-Length (WebDriverAgent's, UiAutomator2's), or with none, from the
// JPEG's start to its end.
func TestReadMJPEG(t *testing.T) {
	a, b := testJPEG(t, 8, 16, color.RGBA{200, 0, 0, 255}), testJPEG(t, 8, 16, color.RGBA{0, 200, 0, 255})
	var stream bytes.Buffer
	fmt.Fprintf(&stream, "--BoundaryString\r\nContent-type: image/jpg\r\nContent-Length: %d\r\n\r\n", len(a))
	stream.Write(a)
	fmt.Fprintf(&stream, "\r\n--BoundaryString\r\nContent-type: image/jpg\r\n\r\n")
	stream.Write(b)
	stream.WriteString("\r\n")
	var got [][]byte
	_ = readMJPEG(&stream, func(f []byte) { got = append(got, f) })
	if len(got) != 2 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) {
		t.Fatalf("frames %d, want the 2 JPEGs as sent", len(got))
	}
}

// A phone's screen is in its place, as big as fits, and a tap shows where
// it was, in the window's coordinates.
func TestFeedDrawsTheScreenAndItsTouches(t *testing.T) {
	f := &feed{window: appium.Rect{Width: 400, Height: 800}}
	f.add(testJPEG(t, 200, 400, color.RGBA{250, 250, 250, 255}))
	now := time.Now()
	f.touch(appium.Touch{From: appium.Point{X: 100, Y: 200}, To: appium.Point{X: 100, Y: 200}, At: now})
	out := image.NewRGBA(image.Rect(0, 0, slotWidth*2, slotHeight))
	if !f.draw(out, image.Rect(slotWidth, 0, 2*slotWidth, slotHeight), now) {
		t.Fatal("no screen drawn")
	}
	// 200x400 fits a 592x1280 place as 592x1184, 48 from the top.
	if c := out.RGBAAt(slotWidth+296, 640); c.R < 200 || c.B < 200 {
		t.Errorf("the screen is not in its place: %v", c)
	}
	if c := out.RGBAAt(10, 640); c != (color.RGBA{}) {
		t.Errorf("the other place is drawn on: %v", c)
	}
	// (100, 200) of a 400x800 window is a quarter across, a quarter down.
	tap := out.RGBAAt(slotWidth+148, 48+296)
	if tap.B < 200 || tap.R > 200 {
		t.Errorf("no tap where the finger was: %v", tap)
	}
	later := image.NewRGBA(out.Bounds())
	f.draw(later, image.Rect(slotWidth, 0, 2*slotWidth, slotHeight), now.Add(time.Second))
	if c := later.RGBAAt(slotWidth+148, 48+296); c.B < 200 || c.R < 200 {
		t.Errorf("a tap still shows a second after: %v", c)
	}
}

func TestOutlineShowsTheShownControls(t *testing.T) {
	root := &Node{Class: "XCUIElementTypeApplication", Label: "Couriers", Displayed: true, Enabled: true}
	button := &Node{Role: RoleButton, Label: "Deliver", Displayed: true, Parent: root}
	hidden := &Node{Role: RoleText, Text: "behind the keyboard", Parent: root}
	box := &Node{Role: RoleCheckbox, Label: "Signed for", Displayed: true, Enabled: true, Checkable: true, Parent: root}
	root.Children = []*Node{button, hidden, box}
	got := outline(&Screen{Nodes: []*Node{root, button, hidden, box}})
	want := "XCUIElementTypeApplication name=\"Couriers\"\n  button name=\"Deliver\" enabled=\"false\"\n  checkbox name=\"Signed for\" checked=\"false\"\n"
	if got != want {
		t.Errorf("outline:\n%s\nwant:\n%s", got, want)
	}
}

func TestPhonesKeepTheirOrder(t *testing.T) {
	p := &phones{}
	for _, n := range []string{"courier", "depot", "courier"} {
		p.add(n)
	}
	if got := strings.Join(p.list(), ","); got != "courier,depot" {
		t.Errorf("phones %s", got)
	}
}
