//go:build windows

// Package autostart registers mw-watch to start at login.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "mw-watch"
)

// Install adds a per-user Run entry for exe with args and, if startNow is
// set, launches it in the background.
func Install(exe string, args []string, startNow bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	parts := []string{syscall.EscapeArg(exe)}
	for _, a := range args {
		parts = append(parts, syscall.EscapeArg(a))
	}
	if err := k.SetStringValue(valueName, strings.Join(parts, " ")); err != nil {
		return err
	}
	if !startNow {
		return nil
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return cmd.Process.Release()
}

// Uninstall removes the Run entry and stops other running copies of this executable.
func Uninstall() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Exit code 128 means no matching process, which is fine.
	kill := exec.Command("taskkill", "/F", "/IM", filepath.Base(exe), "/FI", fmt.Sprintf("PID ne %d", os.Getpid()))
	kill.Run()
	return nil
}
