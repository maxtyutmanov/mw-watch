//go:build darwin

package probe

import (
	"fmt"

	"github.com/ebitengine/purego"
)

const (
	kCGEventSourceStateHIDSystemState = 1
	kCGAnyInputEventType              = 0xFFFFFFFF
	kCFStringEncodingUTF8             = 0x08000100
)

var (
	cgEventSourceSecondsSinceLastEventType func(stateID int32, eventType uint32) float64
	cgMainDisplayID                        func() uint32
	cgDisplayIsAsleep                      func(display uint32) int32
	cgSessionCopyCurrentDictionary         func() uintptr

	cfStringCreateWithCString func(alloc uintptr, cStr string, encoding uint32) uintptr
	cfDictionaryGetValue      func(dict, key uintptr) uintptr
	cfGetTypeID               func(cf uintptr) uintptr
	cfBooleanGetTypeID        func() uintptr
	cfBooleanGetValue         func(b uintptr) bool
	cfRelease                 func(cf uintptr)
)

type darwinProbe struct {
	lockedKey uintptr // CFStringRef "CGSSessionScreenIsLocked"
}

// New loads CoreGraphics and CoreFoundation and returns a polling probe.
func New() (Probe, error) {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreGraphics: %w", err)
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreFoundation: %w", err)
	}
	purego.RegisterLibFunc(&cgEventSourceSecondsSinceLastEventType, cg, "CGEventSourceSecondsSinceLastEventType")
	purego.RegisterLibFunc(&cgMainDisplayID, cg, "CGMainDisplayID")
	purego.RegisterLibFunc(&cgDisplayIsAsleep, cg, "CGDisplayIsAsleep")
	purego.RegisterLibFunc(&cgSessionCopyCurrentDictionary, cg, "CGSessionCopyCurrentDictionary")
	purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfDictionaryGetValue, cf, "CFDictionaryGetValue")
	purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
	purego.RegisterLibFunc(&cfBooleanGetTypeID, cf, "CFBooleanGetTypeID")
	purego.RegisterLibFunc(&cfBooleanGetValue, cf, "CFBooleanGetValue")
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")

	key := cfStringCreateWithCString(0, "CGSSessionScreenIsLocked", kCFStringEncodingUTF8)
	if key == 0 {
		return nil, fmt.Errorf("CFStringCreateWithCString failed")
	}
	return &darwinProbe{lockedKey: key}, nil
}

func (p *darwinProbe) Sample() (State, error) {
	return State{
		IdleSeconds: cgEventSourceSecondsSinceLastEventType(kCGEventSourceStateHIDSystemState, kCGAnyInputEventType),
		DisplayOn:   cgDisplayIsAsleep(cgMainDisplayID()) == 0,
		Locked:      p.locked(),
	}, nil
}

func (p *darwinProbe) locked() bool {
	dict := cgSessionCopyCurrentDictionary()
	if dict == 0 {
		// No GUI session for this process (e.g. at the login window).
		return true
	}
	defer cfRelease(dict)
	v := cfDictionaryGetValue(dict, p.lockedKey)
	// The key is absent when the session is unlocked.
	return v != 0 && cfGetTypeID(v) == cfBooleanGetTypeID() && cfBooleanGetValue(v)
}

func (p *darwinProbe) Close() error {
	if p.lockedKey != 0 {
		cfRelease(p.lockedKey)
		p.lockedKey = 0
	}
	return nil
}
