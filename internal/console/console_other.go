//go:build !windows

// Package console makes command-line output visible from the GUI-subsystem Windows build.
package console

// Attach is only needed on Windows.
func Attach() {}
