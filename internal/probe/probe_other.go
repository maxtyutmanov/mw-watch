//go:build !windows && !darwin

package probe

import (
	"fmt"
	"runtime"
)

// New is not implemented on this platform.
func New() (Probe, error) {
	return nil, fmt.Errorf("mw-watch does not support %s", runtime.GOOS)
}
