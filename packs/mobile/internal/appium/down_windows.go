// SPDX-License-Identifier: Apache-2.0

package appium

import "syscall"

// downErrnos are what Windows says of a connection no server answered on. Winsock has numbers of
// its own for them, which Go's syscall.ECONNREFUSED and ECONNRESET (made up for Windows, so that
// the names exist there) never equal.
var downErrnos = []syscall.Errno{
	10061, // WSAECONNREFUSED: the target machine actively refused it
	10054, // WSAECONNRESET: forcibly closed by the remote host
	10053, // WSAECONNABORTED: aborted by the software in the host machine
}
