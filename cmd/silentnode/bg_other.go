//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

func spawnDetachedDaemon(exePath string, args []string) error {
	cmd := exec.Command(exePath, args...)
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	return cmd.Start()
}

func registerAutoStart(exePath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		agentsDir := filepath.Join(home, "Library", "LaunchAgents")
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			return err
		}
		plistPath := filepath.Join(agentsDir, "com.synthoscollective.silentnode.plist")
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.synthoscollective.silentnode</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>-daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`, exePath)
		return os.WriteFile(plistPath, []byte(plist), 0o644)
	}

	autostartDir := filepath.Join(home, ".config", "autostart")
	if err := os.MkdirAll(autostartDir, 0o755); err != nil {
		return err
	}
	desktopPath := filepath.Join(autostartDir, "synthos-silentnode.desktop")
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=SYNTHOS Background Node
Exec="%s" -daemon
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
`, exePath)
	return os.WriteFile(desktopPath, []byte(desktop), 0o644)
}

func openBrowser(target string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", target).Start()
		return
	}
	_ = exec.Command("xdg-open", target).Start()
}
