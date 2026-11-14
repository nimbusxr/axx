//go:build !darwin

package webcore

// fullChromiumRefused says why Playwright's full Chromium cannot start on this
// machine: no sandbox elsewhere is known to keep it from starting.
func fullChromiumRefused() string { return "" }
