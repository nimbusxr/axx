package desktopcore

import (
	"image"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// Driver runs the apps of one OS: the desktop-macos, desktop-windows or
// desktop-linux pack, each through its accessibility API.
type Driver interface {
	// Platform is the OS, as registrations name it: macos, windows or linux.
	Platform() string
	// Claim takes a desktop for the scenario, which it has until it ends:
	// the machine's, in turns with every other desktop scenario on it
	// (macOS, Windows), or one of axx's own (Linux). It waits for one,
	// with the step's clock stopped (core.Scenario.Hold).
	Claim(sc *core.Scenario) (Desktop, error)
}

// Desktop is the desktop a scenario has: a screen, a pointer and a keyboard,
// and the homes of the apps it runs.
type Desktop interface {
	// Home is the app's home on this desktop, a folder of the project's
	// .axx/desktop, as this machine reads it.
	Home(app *App) string
	// DataDir is where the OS keeps an app's data in its home, relative to
	// it, with forward slashes: Library/Application Support on macOS.
	DataDir() string
	// Reset resets what the OS keeps of the app beyond its home, before the
	// app starts in a scenario: its home is empty already. macOS empties the
	// app's preferences, Windows its registry keys and its package's data.
	Reset(sc *core.Scenario, app *App) error
	// Start starts the app with its home, waits for its window, and fits
	// the window into the screen's usable part.
	Start(sc *core.Scenario, app *App) (Process, error)
	// Watch reads the system's app (owner: system) as it runs, from the
	// scenario's registration of it on: one that runs already (Finder,
	// File Explorer), or that another app starts on the scenario's desktop.
	// Its Stop closes the windows it showed since, as a person closes them,
	// and leaves the app running; it has not exited while it is watched.
	Watch(sc *core.Scenario, app *App) (Process, error)
	// Screen is what the scenario's apps show now, for its video: the whole
	// screen of axx's own desktops (Linux), and on the person's (macOS,
	// Windows) the scenario's windows only, black elsewhere.
	Screen() (Screen, error)
	// Release gives the desktop back, its apps stopped.
	Release()
}

// Screen is a look at a desktop's screen: what it shows, its display's
// scale, and where the pointer is on it, in its pixels, when the desktop
// knows.
type Screen struct {
	Image      image.Image
	Scale      float64
	Pointer    image.Point
	HasPointer bool
}

// Process is an app as it runs on its desktop.
type Process interface {
	// Find returns the controls of kind k named name in the app's windows,
	// the front one first, with their dialogs, sheets and menus, and in its
	// menu bar: those that show when shown is true (a size on the screen),
	// else every one the app has (a field scrolled away). A control in
	// another that matches counts once, as the outer one.
	Find(k appcore.Kind, name string, shown bool) ([]Control, error)
	// Names are the names of the shown controls of kind k, for a failure:
	// the names a person may have meant.
	Names(k appcore.Kind) []string
	// Shows reports whether the app shows text: in a control's text, label
	// or value, whitespace collapsed. Texts are what it shows, for a failure.
	Shows(text string) (bool, error)
	Texts() []string
	// Front brings the app to the front, with the keyboard, and fails before
	// a key or a click could reach another app.
	Front() error
	// Away moves the pointer off the app's window (to AwaySpot), where it
	// hovers over none of its controls: a control under the pointer draws
	// itself hovered.
	Away() error
	// Leave moves the pointer beside the app's window when the pointer is
	// over it and the screen has room there (LeaveSpot), and leaves it as it
	// is otherwise: a pointer moving over a window shows its controls (an
	// image viewer's arrows).
	Leave() error
	// ScrollTo scrolls until the control of kind k named name is in view,
	// as a person looks for it: area by area, the outermost first, and
	// through every list and table for a control the app has only once it
	// shows. It returns nil when no scroll brings one.
	ScrollTo(k appcore.Kind, name string) (Control, error)
	// ScrollIntoView scrolls until the control is in view.
	ScrollIntoView(c Control) error
	// Click brings the control into view and clicks its middle with the
	// pointer; ClickAt clicks at a point from an anchor of it (its top
	// left, its middle, a corner), and Drag drags from one point to another
	// on it. A control with no place on the screen takes its accessibility
	// action instead, which the log says.
	Click(c Control) error
	ClickAt(c Control, from Anchor, x, y float64) error
	Drag(c Control, from Anchor, x1, y1, x2, y2 float64) error
	// Key presses a key with its modifiers, as the web pack names them
	// (Enter, Control+Shift+S, ControlOrMeta+A); Type types text, each
	// character as the keyboard would. Both go to the app's focused window.
	Key(spec string) error
	Type(text string) error
	// Window is a screenshot of the app's front window, and the scale of its
	// display: 2 on a display at twice the scale. The text cursor of the
	// focused field is hidden (HideCaret), as the web pack's screenshots hide
	// it: it blinks, so no two screenshots would agree.
	Window() (image.Image, float64, error)
	// Tree is the app's windows and menu bar as a tree, for xpath= and
	// id= selectors and for a failure's outline.
	Tree() (*Node, error)
	// Stop stops the app and every process it started, and waits for them.
	Stop() error
	// Exited reports whether the app has stopped on its own.
	Exited() bool
	// Describe is the app as it runs, for a failed scenario's context.
	Describe() map[string]any
}

// Control is a control an app shows, as its accessibility tree has it.
type Control interface {
	// Name is the name people see: its text or label, else its description
	// or tooltip.
	Name() string
	// Enabled reports whether the control can be used; reported is false
	// when the app does not say (Flutter on macOS).
	Enabled() (enabled, reported bool)
	// Value is what a field holds; reported is false when the app does not
	// say (Java on Linux).
	Value() (value string, reported bool)
}

// Node is an element of an app's tree, as the OS's accessibility API gives
// it: its role (AXButton, Button, push button) is its element name in
// xpath= selectors, its name, identifier and value its attributes.
type Node struct {
	Role     string
	Name     string
	ID       string
	Value    string
	Attrs    map[string]string
	Children []*Node
	// Control is the element, for a step to act on.
	Control Control
}

// actionWait is how long an action waits for its control.
var actionWait = 10 * time.Second
