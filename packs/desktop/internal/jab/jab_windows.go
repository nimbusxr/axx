//go:build windows

// Package jab reads and acts on Java apps on Windows through the Java Access
// Bridge, as Windows screen readers do: Java Swing and AWT windows show UI
// Automation nothing but their frame. It calls the bridge's client DLL
// (WindowsAccessBridge-64.dll, in the JDK's bin) without cgo.
//
// The bridge talks to Java through window messages: a Client belongs to the
// OS thread that made it, which must pump messages (Pump) while it waits.
package jab

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	maxString   = 1024 // MAX_STRING_SIZE
	shortString = 256  // SHORT_STRING_SIZE
	maxActions  = 32   // MAX_ACTIONS_TO_DO
)

// contextInfo is AccessibleContextInfo, from AccessBridgePackages.h.
type contextInfo struct {
	Name, Description            [maxString]uint16
	Role, RoleEnUS               [shortString]uint16
	States, StatesEnUS           [shortString]uint16
	IndexInParent, Children      int32
	X, Y, Width, Height          int32
	Component, Action, Selection int32
	Text, Interfaces             int32
}

// actionsToDo is AccessibleActionsToDo.
type actionsToDo struct {
	Count   int32
	Actions [maxActions][shortString]uint16
}

// Client is a connection to the Java Access Bridge, on the calling OS thread.
type Client struct {
	run, isJavaWindow, contextFromHWND, contextInfo, childFromContext *syscall.Proc
	doActions, setText, release                                       *syscall.Proc
	parent, textInfo, textRange, contextAt, actions, sameObject       *syscall.Proc
	visibleCount, visible, withFocus, caretAt                         *syscall.Proc
	peek, translate, dispatch                                         *syscall.Proc
}

// New loads the bridge's client DLL (dll, the path of
// WindowsAccessBridge-64.dll), starts it and lets Java's bridges find it.
// The caller must have locked the goroutine to its thread.
func New(dll string) (*Client, error) {
	d, err := syscall.LoadDLL(dll)
	if err != nil {
		return nil, fmt.Errorf("cannot load the Java Access Bridge (%s): %w", dll, err)
	}
	u, err := syscall.LoadDLL("user32.dll")
	if err != nil {
		return nil, err
	}
	c := &Client{}
	for name, p := range map[string]**syscall.Proc{
		"Windows_run": &c.run, "isJavaWindow": &c.isJavaWindow, "getAccessibleContextFromHWND": &c.contextFromHWND,
		"getAccessibleContextInfo": &c.contextInfo, "getAccessibleChildFromContext": &c.childFromContext,
		"doAccessibleActions": &c.doActions, "setTextContents": &c.setText, "releaseJavaObject": &c.release,
		"getAccessibleParentFromContext": &c.parent, "getAccessibleTextInfo": &c.textInfo,
		"getAccessibleTextRange": &c.textRange, "getAccessibleContextAt": &c.contextAt, "getAccessibleActions": &c.actions,
		"isSameObject": &c.sameObject, "getVisibleChildrenCount": &c.visibleCount, "getVisibleChildren": &c.visible,
		"getAccessibleContextWithFocus": &c.withFocus, "getCaretLocation": &c.caretAt,
	} {
		if *p, err = d.FindProc(name); err != nil {
			return nil, fmt.Errorf("the Java Access Bridge has no %s: %w", name, err)
		}
	}
	for name, p := range map[string]**syscall.Proc{"PeekMessageW": &c.peek, "TranslateMessage": &c.translate, "DispatchMessageW": &c.dispatch} {
		if *p, err = u.FindProc(name); err != nil {
			return nil, err
		}
	}
	_, _, _ = c.run.Call()
	c.Pump(time.Second) // Java's bridges answer the client's hello through messages
	return c, nil
}

// Pump handles the thread's window messages for a while: the bridge's
// replies come as messages.
func (c *Client) Pump(d time.Duration) {
	var msg [48]byte // MSG
	for end := time.Now().Add(d); time.Now().Before(end); {
		for {
			r, _, _ := c.peek.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 1) // PM_REMOVE
			if r == 0 {
				break
			}
			_, _, _ = c.translate.Call(uintptr(unsafe.Pointer(&msg[0])))
			_, _, _ = c.dispatch.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// IsJavaWindow reports whether the window is a Java window the bridge knows.
func (c *Client) IsJavaWindow(hwnd uintptr) bool {
	r, _, _ := c.isJavaWindow.Call(hwnd)
	return r != 0
}

// Element is an element of a Java window's accessibility tree.
type Element struct {
	c    *Client
	vmID int32
	ac   int64
	info contextInfo
}

// Window is the root of the tree of the Java window hwnd.
func (c *Client) Window(hwnd uintptr) (*Element, error) {
	var vm int32
	var ac int64
	r, _, _ := c.contextFromHWND.Call(hwnd, uintptr(unsafe.Pointer(&vm)), uintptr(unsafe.Pointer(&ac)))
	if r == 0 {
		return nil, errors.New("the bridge has no accessible context for the window (is the bridge on in its Java?)")
	}
	e := &Element{c: c, vmID: vm, ac: ac}
	return e, e.refresh()
}

func (e *Element) refresh() error {
	r, _, _ := e.c.contextInfo.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(unsafe.Pointer(&e.info)))
	if r == 0 {
		return errors.New("the bridge gave no information about the element")
	}
	return nil
}

func str(s []uint16) string { return syscall.UTF16ToString(s) }

// Name is the element's accessible name, like a label's text.
func (e *Element) Name() string { _ = e.refresh(); return str(e.info.Name[:]) }

// Role is the element's role, in English: "push button", "text", "label".
func (e *Element) Role() string { return str(e.info.RoleEnUS[:]) }

// States are the element's states, in English.
func (e *Element) States() string { _ = e.refresh(); return str(e.info.StatesEnUS[:]) }

// Children are the element's children.
func (e *Element) Children() []*Element {
	var out []*Element
	for i := range e.info.Children {
		r, _, _ := e.c.childFromContext.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(i))
		if r == 0 {
			continue
		}
		k := &Element{c: e.c, vmID: e.vmID, ac: int64(r)}
		if k.refresh() == nil {
			out = append(out, k)
		}
	}
	return out
}

// visibleChildren is VisibleChildrenInfo.
type visibleChildren struct {
	Count    int32
	_        int32
	Children [256]int64
}

// Visible are the element's descendants that show, as the bridge finds them:
// for a table, its cells that show, each a cell of its own (its children are
// its one cell renderer).
func (e *Element) Visible() []*Element {
	n, _, _ := e.c.visibleCount.Call(uintptr(e.vmID), uintptr(e.ac))
	var out []*Element
	for start := 0; start < int(int32(n)); {
		var v visibleChildren
		if r, _, _ := e.c.visible.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(start), uintptr(unsafe.Pointer(&v))); r == 0 || v.Count <= 0 {
			break
		}
		for _, ac := range v.Children[:min(int(v.Count), len(v.Children))] {
			k := &Element{c: e.c, vmID: e.vmID, ac: ac}
			if k.refresh() == nil {
				out = append(out, k)
			}
		}
		start += int(v.Count)
	}
	return out
}

// Do has the element take its action named name, like "click".
func (e *Element) Do(name string) error {
	var todo actionsToDo
	todo.Count = 1
	n, err := syscall.UTF16FromString(name)
	if err != nil {
		return err
	}
	copy(todo.Actions[0][:], n)
	var failure int32
	r, _, _ := e.c.doActions.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(unsafe.Pointer(&todo)), uintptr(unsafe.Pointer(&failure)))
	if r == 0 {
		return fmt.Errorf("%s: the bridge could not do it", name)
	}
	return nil
}

// SetText replaces the element's text, as typing it into a field does.
func (e *Element) SetText(v string) error {
	p, err := syscall.UTF16PtrFromString(v)
	if err != nil {
		return err
	}
	r, _, _ := e.c.setText.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return errors.New("the bridge could not set the text")
	}
	return nil
}

// Bounds is the element's place on the screen, in Java's points (scaled by
// the screen's scale, on a screen that has one).
func (e *Element) Bounds() (x, y, width, height int) {
	_ = e.refresh()
	return int(e.info.X), int(e.info.Y), int(e.info.Width), int(e.info.Height)
}

// Has is whether the element is in the state, as the bridge names it in
// English: "enabled", "showing", "checked", "selected", "focused".
func (e *Element) Has(state string) bool {
	return slices.Contains(strings.Split(e.States(), ","), state)
}

// Parent is the element's parent, or nil at the top.
func (e *Element) Parent() *Element {
	r, _, _ := e.c.parent.Call(uintptr(e.vmID), uintptr(e.ac))
	if r == 0 {
		return nil
	}
	p := &Element{c: e.c, vmID: e.vmID, ac: int64(r)}
	if p.refresh() != nil {
		return nil
	}
	return p
}

// textInfo is AccessibleTextInfo.
type textInfo struct{ CharCount, CaretIndex, IndexAtPoint int32 }

// Text is the element's text, like a field's.
func (e *Element) Text() string {
	var info textInfo
	if r, _, _ := e.c.textInfo.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(unsafe.Pointer(&info)), 0, 0); r == 0 || info.CharCount <= 0 {
		return ""
	}
	buf := make([]uint16, info.CharCount+1)
	if r, _, _ := e.c.textRange.Call(uintptr(e.vmID), uintptr(e.ac), 0, uintptr(info.CharCount-1), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// Caret is where the text cursor of the Java window hwnd's focused element
// is on the screen, in Java's points, a line high and no width, and the
// element, when it has one.
func (c *Client) Caret(hwnd uintptr) (x, y, height int, field *Element, ok bool) {
	var vm int32
	var ac int64
	if r, _, _ := c.withFocus.Call(hwnd, uintptr(unsafe.Pointer(&vm)), uintptr(unsafe.Pointer(&ac))); r == 0 || ac == 0 {
		return 0, 0, 0, nil, false
	}
	f := &Element{c: c, vmID: vm, ac: ac}
	var info textInfo
	if f.refresh() != nil {
		return 0, 0, 0, nil, false
	}
	if r, _, _ := c.textInfo.Call(uintptr(vm), uintptr(ac), uintptr(unsafe.Pointer(&info)), 0, 0); r == 0 || info.CaretIndex < 0 {
		return 0, 0, 0, nil, false
	}
	var at struct{ X, Y, Width, Height int32 } // AccessibleTextRectInfo
	if r, _, _ := c.caretAt.Call(uintptr(vm), uintptr(ac), uintptr(unsafe.Pointer(&at)), uintptr(info.CaretIndex)); r == 0 || at.Height <= 0 {
		return 0, 0, 0, nil, false
	}
	return int(at.X), int(at.Y), int(at.Height), f, true
}

// At is the element under e at a point on the screen (in Java's points).
func (e *Element) At(x, y int) *Element {
	var ac int64
	if r, _, _ := e.c.contextAt.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&ac))); r == 0 || ac == 0 {
		return nil
	}
	k := &Element{c: e.c, vmID: e.vmID, ac: ac}
	if k.refresh() != nil {
		return nil
	}
	return k
}

// actionsInfo is AccessibleActions.
type actionsInfo struct {
	Count int32
	Info  [maxActions * 8][shortString]uint16
}

// Actions are the names of the element's actions, like "click".
func (e *Element) Actions() []string {
	var a actionsInfo
	if r, _, _ := e.c.actions.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(unsafe.Pointer(&a))); r == 0 {
		return nil
	}
	var out []string
	for i := range min(int(a.Count), len(a.Info)) {
		out = append(out, str(a.Info[i][:]))
	}
	return out
}

// Same is whether two elements are one Java object: the bridge gives a new
// handle for an object at each call.
func (e *Element) Same(o *Element) bool {
	if o == nil || o.vmID != e.vmID {
		return false
	}
	r, _, _ := e.c.sameObject.Call(uintptr(e.vmID), uintptr(e.ac), uintptr(o.ac))
	return r != 0
}
