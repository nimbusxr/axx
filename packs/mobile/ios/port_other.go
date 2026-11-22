// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package mobileios

import "github.com/nimbusxr/axx/packs/mobile/internal/appium"

// serverPort is a port for a server on a simulator. Simulators run on a Mac only (port_unix.go).
func serverPort() (int, error) { return appium.FreePort() }
