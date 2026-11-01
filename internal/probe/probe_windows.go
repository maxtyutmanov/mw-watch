//go:build windows

package probe

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	wtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")

	procGetLastInputInfo                   = user32.NewProc("GetLastInputInfo")
	procRegisterClassExW                   = user32.NewProc("RegisterClassExW")
	procUnregisterClassW                   = user32.NewProc("UnregisterClassW")
	procCreateWindowExW                    = user32.NewProc("CreateWindowExW")
	procDestroyWindow                      = user32.NewProc("DestroyWindow")
	procDefWindowProcW                     = user32.NewProc("DefWindowProcW")
	procGetMessageW                        = user32.NewProc("GetMessageW")
	procTranslateMessage                   = user32.NewProc("TranslateMessage")
	procDispatchMessageW                   = user32.NewProc("DispatchMessageW")
	procPostMessageW                       = user32.NewProc("PostMessageW")
	procPostQuitMessage                    = user32.NewProc("PostQuitMessage")
	procRegisterPowerSettingNotification   = user32.NewProc("RegisterPowerSettingNotification")
	procUnregisterPowerSettingNotification = user32.NewProc("UnregisterPowerSettingNotification")

	procGetTickCount     = kernel32.NewProc("GetTickCount")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procWTSRegisterSessionNotification   = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegisterSessionNotification = wtsapi32.NewProc("WTSUnRegisterSessionNotification")
	procWTSQuerySessionInformationW      = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory                    = wtsapi32.NewProc("WTSFreeMemory")
)

const (
	wmDestroy             = 0x0002
	wmClose               = 0x0010
	wmPowerBroadcast      = 0x0218
	wmWTSSessionChange    = 0x02B1
	pbtPowerSettingChange = 0x8013

	wtsSessionLock   = 0x7
	wtsSessionUnlock = 0x8

	notifyForThisSession     = 0
	deviceNotifyWindowHandle = 0

	wtsSessionInfoEx    = 25
	wtsSessionStateLock = 0
	wtsCurrentServer    = 0
	displayStateOff     = 0
)

// GUID_CONSOLE_DISPLAY_STATE: data is 0 = off, 1 = on, 2 = dimmed.
var guidConsoleDisplayState = windows.GUID{
	Data1: 0x6fe69556, Data2: 0x704a, Data3: 0x47a0,
	Data4: [8]byte{0x8f, 0x24, 0xc2, 0x8d, 0x93, 0x6f, 0xda, 0x47},
}

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type msg struct {
	hwnd     windows.HWND
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX, ptY int32
	lPrivate uint32
}

// powerBroadcastSetting mirrors POWERBROADCAST_SETTING; Data is variable length.
type powerBroadcastSetting struct {
	powerSetting windows.GUID
	dataLength   uint32
	data         [1]byte
}

type winProbe struct {
	displayState atomic.Uint32
	locked       atomic.Bool
	hwnd         windows.HWND
	done         chan struct{}
}

// current is the probe that the window procedure updates. Only one probe exists per process.
var current atomic.Pointer[winProbe]

var wndProcCallback = windows.NewCallback(wndProc)

// New starts a hidden window that receives display power and session lock
// notifications, and returns a probe backed by it.
func New() (Probe, error) {
	p := &winProbe{done: make(chan struct{})}
	p.displayState.Store(1)
	p.locked.Store(queryLocked())
	if !current.CompareAndSwap(nil, p) {
		return nil, errors.New("probe already running")
	}
	ready := make(chan error, 1)
	go p.loop(ready)
	if err := <-ready; err != nil {
		current.Store(nil)
		return nil, err
	}
	return p, nil
}

func (p *winProbe) Sample() (State, error) {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return State{}, fmt.Errorf("GetLastInputInfo: %w", err)
	}
	tick, _, _ := procGetTickCount.Call()
	// Both values are 32-bit millisecond tick counts; unsigned subtraction handles wraparound.
	idleMs := uint32(tick) - info.dwTime
	return State{
		IdleSeconds: float64(idleMs) / 1000,
		DisplayOn:   p.displayState.Load() != displayStateOff,
		Locked:      p.locked.Load(),
	}, nil
}

func (p *winProbe) Close() error {
	procPostMessageW.Call(uintptr(p.hwnd), wmClose, 0, 0)
	<-p.done
	current.Store(nil)
	return nil
}

func (p *winProbe) loop(ready chan<- error) {
	defer close(p.done)
	// Window messages are delivered to the thread that created the window.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hinst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString("mw-watch-probe")
	wc := wndClassEx{
		lpfnWndProc:   wndProcCallback,
		hInstance:     windows.Handle(hinst),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		ready <- fmt.Errorf("RegisterClassExW: %w", err)
		return
	}
	defer procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hinst)

	// A hidden top-level window rather than a message-only one, because
	// message-only windows don't receive broadcast power messages.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowExW: %w", err)
		return
	}
	p.hwnd = windows.HWND(hwnd)

	powerNotify, _, err := procRegisterPowerSettingNotification.Call(hwnd,
		uintptr(unsafe.Pointer(&guidConsoleDisplayState)), deviceNotifyWindowHandle)
	if powerNotify == 0 {
		procDestroyWindow.Call(hwnd)
		ready <- fmt.Errorf("RegisterPowerSettingNotification: %w", err)
		return
	}
	defer procUnregisterPowerSettingNotification.Call(powerNotify)

	if r, _, err := procWTSRegisterSessionNotification.Call(hwnd, notifyForThisSession); r == 0 {
		procDestroyWindow.Call(hwnd)
		ready <- fmt.Errorf("WTSRegisterSessionNotification: %w", err)
		return
	}
	defer procWTSUnRegisterSessionNotification.Call(hwnd)

	ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd, message, wParam uintptr, lParam unsafe.Pointer) uintptr {
	p := current.Load()
	switch message {
	case wmPowerBroadcast:
		if wParam == pbtPowerSettingChange && p != nil && lParam != nil {
			s := (*powerBroadcastSetting)(lParam)
			if s.powerSetting == guidConsoleDisplayState && s.dataLength >= 4 {
				p.displayState.Store(*(*uint32)(unsafe.Pointer(&s.data[0])))
			}
		}
		return 1
	case wmWTSSessionChange:
		if p != nil {
			switch wParam {
			case wtsSessionLock:
				p.locked.Store(true)
			case wtsSessionUnlock:
				p.locked.Store(false)
			}
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, uintptr(lParam))
	return r
}

// queryLocked reads the current session's lock state. Later changes arrive as
// WM_WTSSESSION_CHANGE messages.
func queryLocked() bool {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil {
		return false
	}
	var buf *byte
	var size uint32
	r, _, _ := procWTSQuerySessionInformationW.Call(wtsCurrentServer, uintptr(sessionID), wtsSessionInfoEx,
		uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&size)))
	if r == 0 || buf == nil {
		return false
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	// WTSINFOEXW{Level DWORD; Data union} where the union is 8-byte aligned and
	// starts with WTSINFOEX_LEVEL1_W{SessionId, SessionState, SessionFlags}.
	if size < 20 {
		return false
	}
	flags := *(*int32)(unsafe.Add(unsafe.Pointer(buf), 16))
	return flags == wtsSessionStateLock
}
