package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps every test in this package away from the machine's real
// settings. The reward keeper falls back to /etc/synthos/reward-keeper.key and
// /etc/synthos/reward-approved.json when its env vars are empty; on a server
// that runs the live registry those files exist, so tests would pick up the
// production rewards wallet key and approval list (and fail, e.g.
// TestKeeperDisabledWithoutSettings). Pointing the defaults at paths that don't
// exist makes the tests behave the same on every machine. Tests that need
// these files still set them with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cloudless-registry-test-")
	if err != nil {
		panic(err)
	}
	missing := filepath.Join(dir, "does-not-exist")
	for k, v := range map[string]string{
		"SYNTHOS_REWARD_KEEPER_PRIVATE_KEY": "",
		"SYNTHOS_REWARD_KEEPER_KEY_FILE":    filepath.Join(missing, "reward-keeper.key"),
		"SYNTHOS_REWARD_APPROVED_FILE":      filepath.Join(missing, "reward-approved.json"),
		"SYNTHOS_REWARD_MONTHLY_CAP_SYN":    "",
	} {
		os.Setenv(k, v)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
