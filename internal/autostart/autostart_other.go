//go:build !windows && !darwin

// Package autostart registers mw-watch to start at login.
package autostart

import (
	"fmt"
	"runtime"
)

func Install(exe string, args []string, startNow bool) error {
	return fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
}

func Uninstall() error {
	return fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
}
