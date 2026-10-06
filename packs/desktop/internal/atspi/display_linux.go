//go:build linux

package atspi

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

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
func PublishBus(addr string) (*Display, error) { return PublishBusOn("", "", addr) }

// PublishBusOn is PublishBus on a display, like ":99" ("" is $DISPLAY),
// with the X authority file it takes ("" for $XAUTHORITY's, or none).
func PublishBusOn(display, authority, addr string) (*Display, error) {
	x, err := dialX(display, authority)
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

// dialX connects to a local display, with the cookie its authority file
// has for it (Xwayland's, which GNOME Shell writes).
func dialX(display, authority string) (*xgb.Conn, error) {
	if authority == "" {
		return xgb.NewConnDisplay(display)
	}
	number, _, _ := strings.Cut(strings.TrimPrefix(display, ":"), ".")
	cookie, err := cookieFor(authority, number)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", "/tmp/.X11-unix/X"+number)
	if err != nil {
		return nil, err
	}
	return xgb.NewConnNetWithCookieHex(conn, cookie)
}

// cookieFor is the MIT-MAGIC-COOKIE-1 an X authority file has for a display
// number, in hexadecimal: an entry for it, or for any display. Each entry is
// a family, an address, a display number, a name and data, each but the
// family a length and its bytes (Xauth.h).
func cookieFor(authority, number string) (string, error) {
	f, err := os.Open(authority)
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	field := func() ([]byte, error) {
		var n uint16
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return b, err
	}
	var anyDisplay string
	for {
		var family uint16
		if err := binary.Read(r, binary.BigEndian, &family); err != nil {
			break
		}
		var parts [4][]byte
		for i := range parts {
			if parts[i], err = field(); err != nil {
				return "", fmt.Errorf("the X authority file %s: %w", authority, err)
			}
		}
		if string(parts[2]) != "MIT-MAGIC-COOKIE-1" {
			continue
		}
		switch string(parts[1]) {
		case number:
			return hex.EncodeToString(parts[3]), nil
		case "":
			anyDisplay = hex.EncodeToString(parts[3])
		}
	}
	if anyDisplay != "" {
		return anyDisplay, nil
	}
	return "", errors.New("the X authority file " + authority + " has no cookie for display :" + number)
}
