//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	createNoWindow  = 0x08000000
	detachedProcess = 0x00000008
)

func spawnDetachedDaemon(exePath string, args []string) error {
	cmd := exec.Command(exePath, args...)
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow | detachedProcess,
		HideWindow:    true,
	}
	return cmd.Start()
}

func registerAutoStart(exePath string) error {
	val := fmt.Sprintf(`"%s" -daemon`, exePath)
	cmd := exec.Command("reg", "add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "SynthosBackgroundNode",
		"/t", "REG_SZ",
		"/d", val,
		"/f",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
	return cmd.Run()
}

func openBrowser(target string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
	_ = cmd.Start()
}
