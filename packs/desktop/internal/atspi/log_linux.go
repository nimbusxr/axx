//go:build linux

package atspi

import (
	"io"
	"log"

	"github.com/jezek/xgb"
)

// The X client logs what it tries and what fails to its own logger: a
// display without an authority file, a server that has gone. axx reports
// what matters itself.
func init() { xgb.Logger = log.New(io.Discard, "", 0) }
