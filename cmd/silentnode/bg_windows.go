//go:build windows

package main

func keepSystemAwakeAllowScreenSleep() {
	// Standard foreground execution without low-level DLL injection or powercfg modification.
}

func spawnDetachedDaemon(exePath string, args []string) error {
	return nil
}

func registerAutoStart(exePath string) error {
	return nil
}

func openBrowser(target string) {
}
