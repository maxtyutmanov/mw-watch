//go:build darwin

// Package autostart registers mw-watch to start at login.
package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const label = "com.mw-watch"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes a LaunchAgent for exe with args and loads it. launchd starts
// it right away (RunAtLoad), so startNow only matters when a copy is not
// already running; an existing agent is restarted with the new settings.
func Install(exe string, args []string, startNow bool) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	var argsXML bytes.Buffer
	for _, a := range append([]string{exe}, args...) {
		argsXML.WriteString("\t\t<string>")
		xml.EscapeText(&argsXML, []byte(a))
		argsXML.WriteString("</string>\n")
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`, label, argsXML.String())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	// Unload a previous version first; this fails harmlessly if none is loaded.
	exec.Command("launchctl", "bootout", domain()+"/"+label).Run()
	if out, err := exec.Command("launchctl", "bootstrap", domain(), path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return nil
}

// Uninstall stops the LaunchAgent and removes its plist.
func Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	exec.Command("launchctl", "bootout", domain()+"/"+label).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
