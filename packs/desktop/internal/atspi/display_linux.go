//go:build linux

package atspi

import (
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Display holds the accessibility bus's address on an X11 display's root
// window (property AT_SPI_BUS), as at-spi-bus-launcher puts it on a desktop.
//
// Qt 5 needs it there. Debian's and Ubuntu's Qt 5 (their patch
// a11y_root.diff) otherwise registers with the bus before it has connected
// when accessibility is on as it starts, and never registers again. A bare
// Xvfb loses the launcher's property: it resets when its last client leaves.
// The property lasts while a Display is open.
type Display struct{ x *xgb.Conn }

// PublishBus puts the bus address addr on the root window of the display
// that $DISPLAY names.
func PublishBus(addr string) (*Display, error) { return PublishBusOn("", addr) }

// PublishBusOn is PublishBus on a display, like ":99" ("" is $DISPLAY).
func PublishBusOn(display, addr string) (*Display, error) {
	x, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("no X display %s: %w", display, err)
	}
	const name = "AT_SPI_BUS"
	atom, err := xproto.InternAtom(x, false, uint16(len(name)), name).Reply()
	if err != nil {
		x.Close()
		return nil, err
	}
	root := xproto.Setup(x).DefaultScreen(x).Root
	if err := xproto.ChangePropertyChecked(x, xproto.PropModeReplace, root, atom.Atom,
		xproto.AtomString, 8, uint32(len(addr)), []byte(addr)).Check(); err != nil {
		x.Close()
		return nil, fmt.Errorf("publishing the accessibility bus on the display: %w", err)
	}
	return &Display{x: x}, nil
}

// Close closes the connection to the display.
func (d *Display) Close() { d.x.Close() }
