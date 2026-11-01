// Package paths resolves where mw-watch keeps its local data.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "mw-watch"

// DataDir returns the per-user data directory and creates it if needed:
// %LOCALAPPDATA%\mw-watch on Windows, ~/Library/Application Support/mw-watch on macOS.
func DataDir() (string, error) {
	var base string
	var err error
	if runtime.GOOS == "windows" {
		// UserCacheDir is %LOCALAPPDATA% on Windows, which (unlike %APPDATA%) never roams.
		base, err = os.UserCacheDir()
	} else {
		base, err = os.UserConfigDir()
	}
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
