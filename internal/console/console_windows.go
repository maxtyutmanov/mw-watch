//go:build windows

// Package console makes command-line output visible from the GUI-subsystem Windows build.
package console

import (
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

const attachParentProcess = ^uintptr(0) // (DWORD)-1

// Attach connects stdout and stderr to the parent's console. The release
// build is a GUI-subsystem binary so that autostart shows no window, which
// also means it starts without a console. Output that is already redirected
// to a file or pipe is left alone.
func Attach() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout = f
	os.Stderr = f
}
