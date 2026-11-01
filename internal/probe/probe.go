// Package probe reads user-idle time, display power state and session lock
// state from the operating system.
package probe

// State is a single reading of the machine's state.
type State struct {
	// IdleSeconds is the time since the last keyboard or mouse input.
	IdleSeconds float64
	// DisplayOn is true when the display is on (dimmed counts as on).
	DisplayOn bool
	// Locked is true when the user session is locked.
	Locked bool
}

// ScreenOn reports whether this reading counts as "screen on" time:
// the display is on and the session is unlocked.
func (s State) ScreenOn() bool {
	return s.DisplayOn && !s.Locked
}

// Probe samples the machine's state. Use New to get the platform implementation.
type Probe interface {
	Sample() (State, error)
	Close() error
}
