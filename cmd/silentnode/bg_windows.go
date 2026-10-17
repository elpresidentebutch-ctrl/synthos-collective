//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	createNoWindow   = 0x08000000
	detachedProcess  = 0x00000008
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
	hwndBroadcast    = 0xffff
	wmSysCommand     = 0x0112
	scMonitorPower   = 0xF170
	monitorPowerOff  = 2
	screenIdleLimit  = 10 * 60 * 1000 // 10 minutes in milliseconds
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	user32                   = syscall.NewLazyDLL("user32.dll")
	procSetThreadExecState   = kernel32.NewProc("SetThreadExecutionState")
	procGetTickCount         = kernel32.NewProc("GetTickCount")
	procGetLastInputInfo     = user32.NewProc("GetLastInputInfo")
	procPostMessageW         = user32.NewProc("PostMessageW")
	powercfgConfiguredOnce   bool
	screenAlreadyPoweredDown bool
)

// keepSystemAwakeAllowScreenSleep tells Windows to keep the system/CPU/network
// awake for background validation (ES_CONTINUOUS | ES_SYSTEM_REQUIRED) WITHOUT
// setting ES_DISPLAY_REQUIRED. It configures powercfg only ONCE at startup (so
// it never resets Windows' display idle countdown) and actively powers off the
// monitor after 10 minutes of no mouse/keyboard input.
func keepSystemAwakeAllowScreenSleep() {
	_, _, _ = procSetThreadExecState.Call(uintptr(esContinuous | esSystemRequired))

	if !powercfgConfiguredOnce {
		powercfgConfiguredOnce = true
		powerArgs := [][]string{
			{"/change", "standby-timeout-ac", "0"},
			{"/change", "standby-timeout-dc", "0"},
			{"/change", "hibernate-timeout-ac", "0"},
			{"/change", "hibernate-timeout-dc", "0"},
			{"/change", "monitor-timeout-ac", "10"},
			{"/change", "monitor-timeout-dc", "5"},
			{"/setactive", "SCHEME_CURRENT"},
		}
		for _, args := range powerArgs {
			cmd := exec.Command("powercfg", args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{
				CreationFlags: createNoWindow,
				HideWindow:    true,
			}
			_ = cmd.Run()
		}
	}

	checkIdleAndSleepScreen()
}

func checkIdleAndSleepScreen() {
	var lii lastInputInfo
	lii.cbSize = uint32(unsafe.Sizeof(lii))
	ret, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if ret == 0 {
		return
	}
	ticks, _, _ := procGetTickCount.Call()
	idleMs := uint32(ticks) - lii.dwTime
	if idleMs >= screenIdleLimit {
		if !screenAlreadyPoweredDown {
			screenAlreadyPoweredDown = true
			_, _, _ = procPostMessageW.Call(
				uintptr(hwndBroadcast),
				uintptr(wmSysCommand),
				uintptr(scMonitorPower),
				uintptr(monitorPowerOff),
			)
		}
	} else {
		screenAlreadyPoweredDown = false
	}
}

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

