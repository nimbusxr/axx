//go:build windows

// Package uia reads and acts on Windows' accessibility tree (UI Automation),
// as screen readers do, without cgo: it calls UI Automation's COM interfaces
// through their vtables.
//
// COM objects belong to the thread that made them: use a Client on one
// goroutine, locked to its OS thread.
package uia

import (
	"errors"
	"fmt"
	"math"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
)

// object is a COM interface pointer: its first word points to its vtable.
type object struct{ vtbl *[256]uintptr }

// call calls the interface's method at index i of its vtable.
func (o *object) call(i int, args ...uintptr) error {
	if o == nil {
		return errGone
	}
	r, _, _ := syscall.SyscallN(o.vtbl[i], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	if int32(r) < 0 {
		return fmt.Errorf("HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func (o *object) release() {
	if o != nil {
		_ = o.call(2)
	}
}

// Vtable indexes, from UIAutomationClient.h.
const (
	automationCompareElements         = 3
	automationGetRootElement          = 5
	automationElementFromPoint        = 7
	automationGetControlViewWalker    = 14
	automationCreateTrueCondition     = 21
	automationCreatePropertyCondition = 23

	walkerGetParentElement = 3

	elementSetFocus                = 3
	elementFindFirst               = 5
	elementFindAll                 = 6
	elementGetCurrentPropertyValue = 10
	elementGetCurrentPatternAs     = 14
	elementCurrentProcessID        = 20
	elementCurrentControlType      = 21
	elementCurrentName             = 23
	elementCurrentIsEnabled        = 28
	elementCurrentAutomationID     = 29
	elementCurrentClassName        = 30
	elementCurrentHelpText         = 31
	elementCurrentBoundingRect     = 43

	arrayLength     = 3
	arrayGetElement = 4

	invokeInvoke = 3

	valueSetValue     = 3
	valueCurrentValue = 4

	toggleToggle             = 3
	toggleCurrentToggleState = 4

	selectionItemSelect            = 3
	selectionItemCurrentIsSelected = 6

	scrollItemScrollIntoView = 3

	scrollScroll = 3

	expandCollapseExpand       = 3
	expandCollapseCollapse     = 4
	expandCollapseCurrentState = 5
)

// Property, pattern and tree scope ids.
const (
	propertyProcessID          = 30002
	propertyLocalizedType      = 30004
	propertyIsOffscreen        = 30022
	propertyNativeWindowHandle = 30020
	propertyFullDescripton     = 30159
	patternInvoke              = 10000
	patternValue               = 10002
	patternExpandCollapse      = 10005
	patternSelectionItem       = 10010
	patternToggle              = 10015
	patternScrollItem          = 10017
	patternLegacyIAccessible   = 10018
	patternScroll              = 10004
	scopeChildren              = 2
	scopeDescendants           = 4
)

var (
	clsidCUIAutomation = ole.NewGUID("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	iidIUIAutomation   = ole.NewGUID("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")
	iidIInvokePattern  = ole.NewGUID("{FB377FBE-8EA6-46D5-9C73-6499642D3059}")
	iidIValuePattern   = ole.NewGUID("{A94CD8B1-0844-4CD6-9D2D-640537AB39E9}")
	iidIToggle         = ole.NewGUID("{94CF8058-9B8D-4AB9-8BFD-4CD0A33C8C70}")
	iidISelectionItem  = ole.NewGUID("{A8EFA66A-0FDA-421A-9194-38021F3578EA}")
	iidIScrollItem     = ole.NewGUID("{B488300F-D015-4F19-9C29-BB595E3645EF}")
	iidIScroll         = ole.NewGUID("{88F4D42A-E881-459D-A77C-73BBBB7E02DC}")
	iidIExpandCollapse = ole.NewGUID("{619BE086-1F4E-4EE4-BAFA-210128738730}")
	errPatternMissing  = errors.New("the element does not take that action")
	errGone            = errors.New("the element is gone")
)

// Client is a connection to UI Automation, on the calling OS thread.
type Client struct {
	automation *object
	all        *object // the condition every element meets
	walker     *object // the control view's tree walker
}

// New initializes COM on the calling OS thread and connects to UI
// Automation. The caller must have locked the goroutine to its thread.
func New() (*Client, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oe *ole.OleError
		// S_FALSE: COM was initialized on this thread already.
		if !errors.As(err, &oe) || oe.Code() != 1 {
			return nil, fmt.Errorf("cannot initialize COM: %w", err)
		}
	}
	unk, err := ole.CreateInstance(clsidCUIAutomation, iidIUIAutomation)
	if err != nil {
		return nil, fmt.Errorf("cannot create UI Automation: %w", err)
	}
	c := &Client{automation: (*object)(unsafe.Pointer(unk))}
	if err := c.automation.call(automationCreateTrueCondition, uintptr(unsafe.Pointer(&c.all))); err != nil {
		return nil, fmt.Errorf("cannot create a condition: %w", err)
	}
	if err := c.automation.call(automationGetControlViewWalker, uintptr(unsafe.Pointer(&c.walker))); err != nil {
		return nil, fmt.Errorf("cannot walk the tree: %w", err)
	}
	return c, nil
}

// Close releases the client.
func (c *Client) Close() {
	c.walker.release()
	c.all.release()
	c.automation.release()
	ole.CoUninitialize()
}

// Element is an element of the accessibility tree: a window, a button, a web
// page's text.
type Element struct {
	c   *Client
	obj *object
}

// Release releases the element.
func (e *Element) Release() { e.obj.release() }

// Root is the desktop, whose children are the top-level windows.
func (c *Client) Root() (*Element, error) {
	var root *object
	if err := c.automation.call(automationGetRootElement, uintptr(unsafe.Pointer(&root))); err != nil {
		return nil, fmt.Errorf("the desktop: %w", err)
	}
	return &Element{c: c, obj: root}, nil
}

// WindowsOf are the top-level windows of the process pid.
func (c *Client) WindowsOf(pid int) ([]*Element, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	defer root.Release()
	v := ole.NewVariant(ole.VT_I4, int64(pid))
	var cond *object
	// A VARIANT is wider than a register: it is passed by its address.
	if err := c.automation.call(automationCreatePropertyCondition, propertyProcessID, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&cond))); err != nil {
		return nil, fmt.Errorf("a process condition: %w", err)
	}
	defer cond.release()
	return root.find(scopeChildren, cond)
}

// Windows are the top-level windows, of every process. A packaged app's
// window belongs to Windows' frame host, not to the process that started it.
func (c *Client) Windows() ([]*Element, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	defer root.Release()
	return root.find(scopeChildren, c.all)
}

// Descendants are every element under e, in document order.
func (e *Element) Descendants() ([]*Element, error) { return e.find(scopeDescendants, e.c.all) }

func (e *Element) find(scope int, cond *object) ([]*Element, error) {
	var arr *object
	if err := e.obj.call(elementFindAll, uintptr(scope), uintptr(unsafe.Pointer(cond)), uintptr(unsafe.Pointer(&arr))); err != nil {
		return nil, fmt.Errorf("finding elements: %w", err)
	}
	if arr == nil {
		return nil, nil
	}
	defer arr.release()
	var n int32
	if err := arr.call(arrayLength, uintptr(unsafe.Pointer(&n))); err != nil {
		return nil, err
	}
	out := make([]*Element, 0, n)
	for i := range n {
		var el *object
		if err := arr.call(arrayGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))); err != nil {
			return nil, err
		}
		if el != nil { // gone as it was read: WinUI recycles a list's items as it scrolls
			out = append(out, &Element{c: e.c, obj: el})
		}
	}
	return out, nil
}

func (e *Element) bstr(index int) string {
	var s *uint16
	if err := e.obj.call(index, uintptr(unsafe.Pointer(&s))); err != nil || s == nil {
		return ""
	}
	defer func() { _ = ole.SysFreeString((*int16)(unsafe.Pointer(s))) }()
	return ole.BstrToString(s)
}

// Name is the element's name: what a screen reader says first.
func (e *Element) Name() string { return e.bstr(elementCurrentName) }

// AutomationID is the element's identifier, a DOM id in web content.
func (e *Element) AutomationID() string { return e.bstr(elementCurrentAutomationID) }

// ClassName is the element's class: its window class, or its DOM classes.
func (e *Element) ClassName() string { return e.bstr(elementCurrentClassName) }

// HelpText is the element's help, like a tooltip.
func (e *Element) HelpText() string { return e.bstr(elementCurrentHelpText) }

// FullDescription is the element's description (a web element's title or
// aria-description).
func (e *Element) FullDescription() string {
	var v ole.VARIANT
	if err := e.obj.call(elementGetCurrentPropertyValue, propertyFullDescripton, uintptr(unsafe.Pointer(&v))); err != nil {
		return ""
	}
	defer func() { _ = v.Clear() }()
	if v.VT != ole.VT_BSTR {
		return ""
	}
	return v.ToString()
}

// ControlType is the element's control type id, like 50000 for a button.
func (e *Element) ControlType() int {
	var t int32
	if err := e.obj.call(elementCurrentControlType, uintptr(unsafe.Pointer(&t))); err != nil {
		return 0
	}
	return int(t)
}

// ProcessID is the process the element belongs to.
func (e *Element) ProcessID() int {
	var pid int32
	_ = e.obj.call(elementCurrentProcessID, uintptr(unsafe.Pointer(&pid)))
	return int(pid)
}

// Enabled reports whether the element takes input.
func (e *Element) Enabled() bool {
	var b int32
	_ = e.obj.call(elementCurrentIsEnabled, uintptr(unsafe.Pointer(&b)))
	return b != 0
}

// Rect is the element's place on the screen, in pixels.
type Rect struct{ Left, Top, Right, Bottom int32 }

// Bounds is the element's bounding rectangle on the screen.
func (e *Element) Bounds() Rect {
	var r Rect
	_ = e.obj.call(elementCurrentBoundingRect, uintptr(unsafe.Pointer(&r)))
	return r
}

// Invoke has the element do what it does, as clicking a button does.
func (e *Element) Invoke() error {
	var p *object
	if err := e.obj.call(elementGetCurrentPatternAs, patternInvoke, uintptr(unsafe.Pointer(iidIInvokePattern)), uintptr(unsafe.Pointer(&p))); err != nil {
		return fmt.Errorf("the invoke pattern: %w", err)
	}
	if p == nil {
		return errPatternMissing
	}
	defer p.release()
	return p.call(invokeInvoke)
}

func (e *Element) valuePattern() (*object, error) {
	var p *object
	if err := e.obj.call(elementGetCurrentPatternAs, patternValue, uintptr(unsafe.Pointer(iidIValuePattern)), uintptr(unsafe.Pointer(&p))); err != nil {
		return nil, fmt.Errorf("the value pattern: %w", err)
	}
	if p == nil {
		return nil, errPatternMissing
	}
	return p, nil
}

// SetValue sets the element's value, as typing it into a field does.
func (e *Element) SetValue(v string) error {
	p, err := e.valuePattern()
	if err != nil {
		return err
	}
	defer p.release()
	s := ole.SysAllocString(v)
	defer func() { _ = ole.SysFreeString(s) }()
	return p.call(valueSetValue, uintptr(unsafe.Pointer(s)))
}

// Value is the element's value, like a field's text.
func (e *Element) Value() string {
	p, err := e.valuePattern()
	if err != nil {
		return ""
	}
	defer p.release()
	var s *uint16
	if err := p.call(valueCurrentValue, uintptr(unsafe.Pointer(&s))); err != nil || s == nil {
		return ""
	}
	defer func() { _ = ole.SysFreeString((*int16)(unsafe.Pointer(s))) }()
	return ole.BstrToString(s)
}

// Focus gives the element the keyboard focus.
func (e *Element) Focus() error { return e.obj.call(elementSetFocus) }

// ControlTypeName is a control type's name, for people reading a dump.
func ControlTypeName(id int) string {
	if n, ok := controlTypes[id]; ok {
		return n
	}
	return fmt.Sprint(id)
}

var controlTypes = map[int]string{
	50000: "Button", 50001: "Calendar", 50002: "CheckBox", 50003: "ComboBox", 50004: "Edit",
	50005: "Hyperlink", 50006: "Image", 50007: "ListItem", 50008: "List", 50009: "Menu",
	50010: "MenuBar", 50011: "MenuItem", 50012: "ProgressBar", 50013: "RadioButton",
	50014: "ScrollBar", 50015: "Slider", 50016: "Spinner", 50017: "StatusBar", 50018: "Tab",
	50019: "TabItem", 50020: "Text", 50021: "ToolBar", 50022: "ToolTip", 50023: "Tree",
	50024: "TreeItem", 50025: "Custom", 50026: "Group", 50027: "Thumb", 50028: "DataGrid",
	50029: "DataItem", 50030: "Document", 50031: "SplitButton", 50032: "Window", 50033: "Pane",
	50034: "Header", 50035: "HeaderItem", 50036: "Table", 50037: "TitleBar", 50038: "Separator",
}

// Children are the elements right under e.
func (e *Element) Children() ([]*Element, error) { return e.find(scopeChildren, e.c.all) }

// Parent is the element above e in the control view, or nil at the top.
func (e *Element) Parent() *Element {
	var p *object
	if err := e.c.walker.call(walkerGetParentElement, uintptr(unsafe.Pointer(e.obj)), uintptr(unsafe.Pointer(&p))); err != nil || p == nil {
		return nil
	}
	return &Element{c: e.c, obj: p}
}

// Same is whether two elements are one.
func (e *Element) Same(o *Element) bool {
	if o == nil {
		return false
	}
	var same int32
	if err := e.c.automation.call(automationCompareElements, uintptr(unsafe.Pointer(e.obj)), uintptr(unsafe.Pointer(o.obj)), uintptr(unsafe.Pointer(&same))); err != nil {
		return false
	}
	return same != 0
}

// ElementAt is the element at a point on the screen: what a click there
// would reach.
func (c *Client) ElementAt(x, y int) *Element {
	var el *object
	point := uintptr(uint32(int32(x))) | uintptr(uint32(int32(y)))<<32
	if err := c.automation.call(automationElementFromPoint, point, uintptr(unsafe.Pointer(&el))); err != nil || el == nil {
		return nil
	}
	return &Element{c: c, obj: el}
}

func (e *Element) property(id int) (ole.VARIANT, bool) {
	var v ole.VARIANT
	if err := e.obj.call(elementGetCurrentPropertyValue, uintptr(id), uintptr(unsafe.Pointer(&v))); err != nil {
		return v, false
	}
	return v, true
}

// isTrue is whether a variant is a true VARIANT_BOOL: its 16 bits only, as
// the rest of the variant's value can hold anything.
func isTrue(v ole.VARIANT) bool { return v.VT == ole.VT_BOOL && int16(v.Val) != 0 }

// Offscreen is whether the element is out of sight: scrolled away, or hidden.
func (e *Element) Offscreen() bool {
	v, ok := e.property(propertyIsOffscreen)
	if !ok {
		return false
	}
	defer func() { _ = v.Clear() }()
	return isTrue(v)
}

// LocalizedType is the element's control type as people read it, like
// "toggle switch".
func (e *Element) LocalizedType() string {
	v, ok := e.property(propertyLocalizedType)
	if !ok {
		return ""
	}
	defer func() { _ = v.Clear() }()
	if v.VT != ole.VT_BSTR {
		return ""
	}
	return v.ToString()
}

func (e *Element) pattern(id int, iid *ole.GUID) (*object, error) {
	var p *object
	if err := e.obj.call(elementGetCurrentPatternAs, uintptr(id), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p))); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errPatternMissing
	}
	return p, nil
}

// Toggled is the element's toggle state: whether a checkbox or a switch is
// on, and whether it can be toggled at all.
func (e *Element) Toggled() (on, toggles bool) {
	p, err := e.pattern(patternToggle, iidIToggle)
	if err != nil {
		return false, false
	}
	defer p.release()
	var state int32
	if err := p.call(toggleCurrentToggleState, uintptr(unsafe.Pointer(&state))); err != nil {
		return false, true
	}
	return state == 1, true
}

// Selected is whether the element is selected, like a radio button that is
// on, and whether it can be selected at all.
func (e *Element) Selected() (on, selects bool) {
	p, err := e.pattern(patternSelectionItem, iidISelectionItem)
	if err != nil {
		return false, false
	}
	defer p.release()
	var b int32
	if err := p.call(selectionItemCurrentIsSelected, uintptr(unsafe.Pointer(&b))); err != nil {
		return false, true
	}
	return b != 0, true
}

// ScrollIntoView asks the app to scroll the element into view.
func (e *Element) ScrollIntoView() error {
	p, err := e.pattern(patternScrollItem, iidIScrollItem)
	if err != nil {
		return err
	}
	defer p.release()
	return p.call(scrollItemScrollIntoView)
}

// legacyDoDefaultAction is IUIAutomationLegacyIAccessiblePattern's
// DoDefaultAction.
const legacyDoDefaultAction = 4

var iidILegacyIAccessible = ole.NewGUID("{828055AD-355B-4435-86D5-3B51C14A9B1B}")

// DefaultAction has the element do its default action, as Windows'
// accessibility before UI Automation (MSAA) gives it: Flutter's tap.
func (e *Element) DefaultAction() error {
	p, err := e.pattern(patternLegacyIAccessible, iidILegacyIAccessible)
	if err != nil {
		return err
	}
	defer p.release()
	return p.call(legacyDoDefaultAction)
}

// Expand opens the element, like a menu.
func (e *Element) Expand() error {
	p, err := e.pattern(patternExpandCollapse, iidIExpandCollapse)
	if err != nil {
		return err
	}
	defer p.release()
	return p.call(expandCollapseExpand)
}

// Handle is the element's window handle (HWND), for a window.
func (e *Element) Handle() uintptr {
	v, ok := e.property(propertyNativeWindowHandle)
	if !ok {
		return 0
	}
	defer func() { _ = v.Clear() }()
	if v.VT != ole.VT_I4 {
		return 0
	}
	return uintptr(uint32(int32(v.Val))) // its 32 bits only, as with isTrue
}

// Scroll asks the element (a scroll box) to scroll down, or up with up: a
// page, as Page Down does, or a step, as an arrow of its scroll bar does. A
// page is what the box shows, so each of its items shows its middle at one
// page or another.
func (e *Element) Scroll(up, page bool) error {
	p, err := e.pattern(patternScroll, iidIScroll)
	if err != nil {
		return err
	}
	defer p.release()
	const noAmount, largeDecrement, smallDecrement, largeIncrement, smallIncrement = 2, 0, 1, 3, 4
	v := map[[2]bool]int{
		{false, true}: largeIncrement, {true, true}: largeDecrement,
		{false, false}: smallIncrement, {true, false}: smallDecrement,
	}[[2]bool{up, page}]
	return p.call(scrollScroll, noAmount, uintptr(v))
}

// ScrollPercent is how far down the element (a scroll box) has scrolled, 0
// to 100, as its Scroll pattern says; ok is false when it does not scroll up
// and down.
func (e *Element) ScrollPercent() (percent float64, ok bool) {
	const propertyVerticalScrollPercent = 30053
	v, has := e.property(propertyVerticalScrollPercent)
	if !has {
		return 0, false
	}
	defer func() { _ = v.Clear() }()
	if v.VT != ole.VT_R8 {
		return 0, false
	}
	p := math.Float64frombits(uint64(v.Val))
	return p, p >= 0 // UIA_ScrollPatternNoScroll is -1
}

// Scrolls is whether the element scrolls what it holds when asked (its
// Scroll pattern).
func (e *Element) Scrolls() bool {
	p, err := e.pattern(patternScroll, iidIScroll)
	if err != nil {
		return false
	}
	p.release()
	return true
}
