//go:build darwin

// Package ax reads and acts on macOS's accessibility tree (AX), as screen
// readers do, without cgo: it calls the ApplicationServices and
// CoreFoundation frameworks through purego.
package ax

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ref is a CoreFoundation object: an AXUIElementRef, a CFStringRef and the
// like.
type ref = uintptr

const (
	cfUTF8           = 0x08000100 // kCFStringEncodingUTF8
	cfNumberFloat64  = 6          // kCFNumberFloat64Type
	axValueCGPoint   = 1          // kAXValueCGPointType
	axValueCGSize    = 2          // kAXValueCGSizeType
	axSuccess        = 0
	axNoValue        = -25212
	axUnsupported    = -25205
	axAPIDisabled    = -25211
	axInvalidElement = -25202
	axCannotComplete = -25204
)

var (
	loadOnce sync.Once
	loadErr  error

	axIsProcessTrusted             func() bool
	axUIElementCreateApplication   func(pid int32) ref
	axUIElementGetPid              func(el ref, pid *int32) int32
	axUIElementCopyElementAtPos    func(app ref, x, y float32, el *ref) int32
	axUIElementCopyAttributeValue  func(el, attr ref, value *ref) int32
	axUIElementCopyAttributeNames  func(el ref, names *ref) int32
	axUIElementCopyActionNames     func(el ref, names *ref) int32
	axUIElementPerformAction       func(el, action ref) int32
	axUIElementSetAttributeValue   func(el, attr, value ref) int32
	axUIElementSetMessagingTimeout func(el ref, seconds float32) int32
	axUIElementGetTypeID           func() uintptr
	axValueGetTypeID               func() uintptr
	axValueGetType                 func(v ref) int32
	axValueGetValue                func(v ref, typ int32, out unsafe.Pointer) bool
	axValueCreate                  func(typ int32, value unsafe.Pointer) ref

	cfStringCreateWithCString         func(alloc uintptr, s string, encoding uint32) ref
	cfStringGetLength                 func(s ref) int
	cfStringGetMaximumSizeForEncoding func(length int, encoding uint32) int
	cfStringGetCString                func(s ref, buf *byte, size int, encoding uint32) bool
	cfGetTypeID                       func(cf ref) uintptr
	cfStringGetTypeID                 func() uintptr
	cfArrayGetTypeID                  func() uintptr
	cfBooleanGetTypeID                func() uintptr
	cfNumberGetTypeID                 func() uintptr
	cfArrayGetCount                   func(a ref) int
	cfArrayGetValueAtIndex            func(a ref, i int) ref
	cfBooleanGetValue                 func(b ref) bool
	cfNumberGetValue                  func(n ref, typ int, out unsafe.Pointer) bool
	cfRetain                          func(cf ref) ref
	cfRelease                         func(cf ref)
	cfEqual                           func(a, b ref) bool

	cfTrue, cfFalse ref
)

// load opens the frameworks and binds their functions, once.
func load() error {
	loadOnce.Do(func() {
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("cannot load ApplicationServices: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("cannot load CoreFoundation: %w", err)
			return
		}
		for name, fn := range map[string]any{
			"AXIsProcessTrusted":               &axIsProcessTrusted,
			"AXUIElementCreateApplication":     &axUIElementCreateApplication,
			"AXUIElementGetPid":                &axUIElementGetPid,
			"AXUIElementCopyElementAtPosition": &axUIElementCopyElementAtPos,
			"AXUIElementCopyAttributeValue":    &axUIElementCopyAttributeValue,
			"AXUIElementCopyAttributeNames":    &axUIElementCopyAttributeNames,
			"AXUIElementCopyActionNames":       &axUIElementCopyActionNames,
			"AXUIElementPerformAction":         &axUIElementPerformAction,
			"AXUIElementSetAttributeValue":     &axUIElementSetAttributeValue,
			"AXUIElementSetMessagingTimeout":   &axUIElementSetMessagingTimeout,
			"AXUIElementGetTypeID":             &axUIElementGetTypeID,
			"AXValueGetTypeID":                 &axValueGetTypeID,
			"AXValueGetType":                   &axValueGetType,
			"AXValueGetValue":                  &axValueGetValue,
			"AXValueCreate":                    &axValueCreate,
		} {
			purego.RegisterLibFunc(fn, as, name)
		}
		for name, fn := range map[string]any{
			"CFStringCreateWithCString":         &cfStringCreateWithCString,
			"CFStringGetLength":                 &cfStringGetLength,
			"CFStringGetMaximumSizeForEncoding": &cfStringGetMaximumSizeForEncoding,
			"CFStringGetCString":                &cfStringGetCString,
			"CFGetTypeID":                       &cfGetTypeID,
			"CFStringGetTypeID":                 &cfStringGetTypeID,
			"CFArrayGetTypeID":                  &cfArrayGetTypeID,
			"CFBooleanGetTypeID":                &cfBooleanGetTypeID,
			"CFNumberGetTypeID":                 &cfNumberGetTypeID,
			"CFArrayGetCount":                   &cfArrayGetCount,
			"CFArrayGetValueAtIndex":            &cfArrayGetValueAtIndex,
			"CFBooleanGetValue":                 &cfBooleanGetValue,
			"CFNumberGetValue":                  &cfNumberGetValue,
			"CFRetain":                          &cfRetain,
			"CFRelease":                         &cfRelease,
			"CFEqual":                           &cfEqual,
		} {
			purego.RegisterLibFunc(fn, cf, name)
		}
		for name, v := range map[string]*ref{"kCFBooleanTrue": &cfTrue, "kCFBooleanFalse": &cfFalse} {
			sym, err := purego.Dlsym(cf, name)
			if err != nil {
				loadErr = fmt.Errorf("cannot find %s: %w", name, err)
				return
			}
			// sym is the global's address, which holds the CFBoolean.
			*v = **(**ref)(unsafe.Pointer(&sym))
		}
	})
	return loadErr
}

// Trusted reports whether macOS lets this process use the accessibility tree:
// the app that started it (a terminal, an IDE, a CI agent) is allowed in
// System Settings > Privacy & Security > Accessibility.
func Trusted() (bool, error) {
	if err := load(); err != nil {
		return false, err
	}
	return axIsProcessTrusted(), nil
}

// Element is an element of an app's accessibility tree: the app, a window, a
// button, a web page's text.
type Element struct{ ref ref }

func wrap(r ref) *Element {
	e := &Element{ref: r}
	runtime.SetFinalizer(e, func(e *Element) { cfRelease(e.ref) })
	return e
}

// Application is the root of the accessibility tree of the process pid.
func Application(pid int) (*Element, error) {
	if err := load(); err != nil {
		return nil, err
	}
	app := axUIElementCreateApplication(int32(pid))
	if app == 0 {
		return nil, fmt.Errorf("no accessibility element for process %d", pid)
	}
	// An app that hangs answers no one: its calls give up after this long.
	axUIElementSetMessagingTimeout(app, 5)
	return wrap(app), nil
}

// ElementAt is the app's element at a point on the screen: what a click there
// would reach.
func (e *Element) ElementAt(at Point) (*Element, error) {
	var el ref
	if code := axUIElementCopyElementAtPos(e.ref, float32(at.X), float32(at.Y), &el); code != axSuccess {
		return nil, &Error{Call: "AXUIElementCopyElementAtPosition", Code: code}
	}
	return wrap(el), nil
}

// PID is the process the element belongs to.
func (e *Element) PID() (int, error) {
	var pid int32
	if code := axUIElementGetPid(e.ref, &pid); code != axSuccess {
		return 0, &Error{Call: "AXUIElementGetPid", Code: code}
	}
	return int(pid), nil
}

// Error is an AXError, the result of a failed call.
type Error struct {
	Call string
	Code int32
}

func (e *Error) Error() string {
	what := map[int32]string{
		axAPIDisabled:    "the accessibility API is not allowed for this process",
		axInvalidElement: "the element is gone",
		axCannotComplete: "the app did not answer",
		axUnsupported:    "the element has no such attribute",
	}[e.Code]
	if what == "" {
		what = fmt.Sprintf("AXError %d", e.Code)
	}
	return e.Call + ": " + what
}

// ErrNoValue is an attribute the element has, without a value now.
var ErrNoValue = errors.New("no value")

func cfString(s string) ref { return cfStringCreateWithCString(0, s, cfUTF8) }

// Attribute is the value of one of the element's attributes, as Go values:
// string, bool, float64, *Element, []any, Point or Size. An attribute the
// element does not have is an *Error; one without a value is ErrNoValue.
func (e *Element) Attribute(name string) (any, error) {
	attr := cfString(name)
	defer cfRelease(attr)
	var v ref
	code := axUIElementCopyAttributeValue(e.ref, attr, &v)
	runtime.KeepAlive(e)
	switch {
	case code == axNoValue:
		return nil, ErrNoValue
	case code != axSuccess:
		return nil, &Error{Call: "the " + name + " attribute", Code: code}
	}
	defer cfRelease(v)
	return decode(v), nil
}

// String is a text attribute, or "" when the element has none.
func (e *Element) String(name string) string {
	v, err := e.Attribute(name)
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Bool is a boolean attribute, or false when the element has none.
func (e *Element) Bool(name string) bool {
	v, err := e.Attribute(name)
	if err != nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// Elements is an attribute that lists elements, like AXChildren or
// AXWindows.
func (e *Element) Elements(name string) ([]*Element, error) {
	v, err := e.Attribute(name)
	if errors.Is(err, ErrNoValue) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	list, _ := v.([]any)
	out := make([]*Element, 0, len(list))
	for _, item := range list {
		if el, ok := item.(*Element); ok {
			out = append(out, el)
		}
	}
	return out, nil
}

// Children are the element's children in the tree.
func (e *Element) Children() ([]*Element, error) { return e.Elements("AXChildren") }

// AttributeNames are the attributes the element has.
func (e *Element) AttributeNames() ([]string, error) {
	var v ref
	if code := axUIElementCopyAttributeNames(e.ref, &v); code != axSuccess {
		return nil, &Error{Call: "the attribute names", Code: code}
	}
	runtime.KeepAlive(e)
	defer cfRelease(v)
	return stringList(v), nil
}

// ActionNames are the actions the element takes, like AXPress.
func (e *Element) ActionNames() ([]string, error) {
	var v ref
	if code := axUIElementCopyActionNames(e.ref, &v); code != axSuccess {
		return nil, &Error{Call: "the action names", Code: code}
	}
	runtime.KeepAlive(e)
	defer cfRelease(v)
	return stringList(v), nil
}

// Perform has the element take an action, like AXPress.
func (e *Element) Perform(action string) error {
	a := cfString(action)
	defer cfRelease(a)
	code := axUIElementPerformAction(e.ref, a)
	runtime.KeepAlive(e)
	if code != axSuccess {
		return &Error{Call: action, Code: code}
	}
	return nil
}

// SetString sets a text attribute, like a field's AXValue.
func (e *Element) SetString(name, value string) error {
	attr, v := cfString(name), cfString(value)
	defer cfRelease(attr)
	defer cfRelease(v)
	code := axUIElementSetAttributeValue(e.ref, attr, v)
	runtime.KeepAlive(e)
	if code != axSuccess {
		return &Error{Call: "setting " + name, Code: code}
	}
	return nil
}

// SetBool sets a boolean attribute, like AXFocused.
func (e *Element) SetBool(name string, value bool) error {
	attr := cfString(name)
	defer cfRelease(attr)
	v := cfFalse
	if value {
		v = cfTrue
	}
	code := axUIElementSetAttributeValue(e.ref, attr, v)
	runtime.KeepAlive(e)
	if code != axSuccess {
		return &Error{Call: "setting " + name, Code: code}
	}
	return nil
}

// SetPosition moves the element, like a window, to a point on the screen.
func (e *Element) SetPosition(p Point) error {
	return e.setValue("AXPosition", axValueCGPoint, unsafe.Pointer(&p))
}

// SetSize resizes the element, like a window.
func (e *Element) SetSize(s Size) error {
	return e.setValue("AXSize", axValueCGSize, unsafe.Pointer(&s))
}

func (e *Element) setValue(name string, typ int32, value unsafe.Pointer) error {
	attr, v := cfString(name), axValueCreate(typ, value)
	defer cfRelease(attr)
	defer cfRelease(v)
	code := axUIElementSetAttributeValue(e.ref, attr, v)
	runtime.KeepAlive(e)
	if code != axSuccess {
		return &Error{Call: "setting " + name, Code: code}
	}
	return nil
}

// Equal is whether two elements are one: the same control of the app.
func (e *Element) Equal(o *Element) bool {
	return o != nil && cfEqual(e.ref, o.ref)
}

// Point is a position on the screen, in points from its top left.
type Point struct{ X, Y float64 }

// Size is a width and a height, in points.
type Size struct{ Width, Height float64 }

// decode turns a CoreFoundation value into a Go value. It does not release v.
func decode(v ref) any {
	switch t := cfGetTypeID(v); t {
	case cfStringGetTypeID():
		return goString(v)
	case cfBooleanGetTypeID():
		return cfBooleanGetValue(v)
	case cfNumberGetTypeID():
		var f float64
		cfNumberGetValue(v, cfNumberFloat64, unsafe.Pointer(&f))
		return f
	case cfArrayGetTypeID():
		n := cfArrayGetCount(v)
		out := make([]any, 0, n)
		for i := range n {
			out = append(out, decode(cfArrayGetValueAtIndex(v, i)))
		}
		return out
	case axUIElementGetTypeID():
		return wrap(cfRetain(v))
	case axValueGetTypeID():
		switch axValueGetType(v) {
		case axValueCGPoint:
			var p Point
			axValueGetValue(v, axValueCGPoint, unsafe.Pointer(&p))
			return p
		case axValueCGSize:
			var s Size
			axValueGetValue(v, axValueCGSize, unsafe.Pointer(&s))
			return s
		}
	}
	return nil
}

func goString(s ref) string {
	n := cfStringGetMaximumSizeForEncoding(cfStringGetLength(s), cfUTF8) + 1
	buf := make([]byte, n)
	if !cfStringGetCString(s, &buf[0], n, cfUTF8) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

func stringList(array ref) []string {
	n := cfArrayGetCount(array)
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, goString(cfArrayGetValueAtIndex(array, i)))
	}
	return out
}
