//go:build !darwin

// axx-launcher runs on macOS only (ADR 0013).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "axx-launcher runs on macOS only: elsewhere axx needs no launcher")
	os.Exit(2)
}
