// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package appium

import "syscall"

// downErrnos are what the system says of a connection no server answered on.
var downErrnos = []syscall.Errno{syscall.ECONNREFUSED, syscall.ECONNRESET}
