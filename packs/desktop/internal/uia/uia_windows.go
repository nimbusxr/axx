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
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
)

// object is a COM interface pointer: its first word points to its vtable.
type object struct{ vtbl *[256]uintptr }

// call calls the interface's method at index i of its vtable.
func (o *object) call(i int, args ...uintptr) error {
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
	automationGetRootElement          = 5
	automationCreateTrueCondition     = 21
	automationCreatePropertyCondition = 23

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
)

// Property, pattern and tree scope ids.
const (
	propertyProcessID      = 30002
	propertyFullDescripton = 30159
	patternInvoke          = 10000
	scopeChildren          = 2
	scopeDescendants       = 4
)

var (
	clsidCUIAutomation = ole.NewGUID("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	iidIUIAutomation   = ole.NewGUID("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")
	iidIInvokePattern  = ole.NewGUID("{FB377FBE-8EA6-46D5-9C73-6499642D3059}")
	errPatternMissing  = errors.New("the element does not take that action")
)

// Client is a connection to UI Automation, on the calling OS thread.
type Client struct {
	automation *object
	all        *object // the condition every element meets
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
	return c, nil
}

// Close releases the client.
func (c *Client) Close() {
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
		out = append(out, &Element{c: e.c, obj: el})
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
