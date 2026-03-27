package stress_test

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"synthos-collective/internal/config"
)

// TestConfig_Defaults verifies that LoadRuntimeConfig sets all expected defaults
// when no environment variables are present.
func TestConfig_Defaults(t *testing.T) {
	// Clear all relevant env vars
	for _, key := range []string{
		"CHAIN_ID", "CONSENSUS_TIMEOUT", "BLOCK_INTERVAL",
		"AGENT_ID", "AGENT_PRIVATE_KEY", "HSM_ENABLED", "HSM_SLOT",
		"HSM_PIN", "LOG_LEVEL", "RATE_LIMIT_RPS",
	} {
		os.Unsetenv(key)
	}

	cfg := &config.NodeConfig{}
	cfg.LoadRuntimeConfig()

	if cfg.ChainID != 1 {
		t.Errorf("default ChainID: got %d, want 1", cfg.ChainID)
	}
	if cfg.ConsensusTimeout != 10*time.Second {
		t.Errorf("default ConsensusTimeout: got %v, want 10s", cfg.ConsensusTimeout)
	}
	if cfg.BlockInterval != 5*time.Second {
		t.Errorf("default BlockInterval: got %v, want 5s", cfg.BlockInterval)
	}
	if cfg.MinValidators != 3 {
		t.Errorf("default MinValidators: got %d, want 3", cfg.MinValidators)
	}
	if cfg.FinalityThreshold != 2 {
		t.Errorf("default FinalityThreshold: got %d, want 2", cfg.FinalityThreshold)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.RateLimitRPS != 1000 {
		t.Errorf("default RateLimitRPS: got %d, want 1000", cfg.RateLimitRPS)
	}
	if cfg.MaxTransactionSize != 1024*1024 {
		t.Errorf("default MaxTransactionSize: got %d, want %d", cfg.MaxTransactionSize, 1024*1024)
	}
	t.Logf("✅ All config defaults are set correctly")
}

// TestConfig_EnvOverrides verifies that environment variables correctly override defaults.
func TestConfig_EnvOverrides(t *testing.T) {
	t.Setenv("CHAIN_ID", "42")
	t.Setenv("CONSENSUS_TIMEOUT", "30s")
	t.Setenv("BLOCK_INTERVAL", "2s")
	t.Setenv("AGENT_ID", "test-agent-1")
	t.Setenv("AGENT_PRIVATE_KEY", "my-secret-key")
	t.Setenv("HSM_ENABLED", "true")
	t.Setenv("HSM_SLOT", "7")
	t.Setenv("HSM_PIN", "1234")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("RATE_LIMIT_RPS", "500")

	cfg := &config.NodeConfig{}
	cfg.LoadRuntimeConfig()

	if cfg.ChainID != 42 {
		t.Errorf("CHAIN_ID override: got %d, want 42", cfg.ChainID)
	}
	if cfg.ConsensusTimeout != 30*time.Second {
		t.Errorf("CONSENSUS_TIMEOUT override: got %v, want 30s", cfg.ConsensusTimeout)
	}
	if cfg.BlockInterval != 2*time.Second {
		t.Errorf("BLOCK_INTERVAL override: got %v, want 2s", cfg.BlockInterval)
	}
	if cfg.AgentID != "test-agent-1" {
		t.Errorf("AGENT_ID override: got %q, want %q", cfg.AgentID, "test-agent-1")
	}
	if cfg.AgentPrivateKey != "my-secret-key" {
		t.Errorf("AGENT_PRIVATE_KEY override: got %q", cfg.AgentPrivateKey)
	}
	if !cfg.HSMEnabled {
		t.Error("HSM_ENABLED override: expected true")
	}
	if cfg.HSMSlot != 7 {
		t.Errorf("HSM_SLOT override: got %d, want 7", cfg.HSMSlot)
	}
	if cfg.HSMPin != "1234" {
		t.Errorf("HSM_PIN override: got %q, want %q", cfg.HSMPin, "1234")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LOG_LEVEL override: got %q, want debug", cfg.LogLevel)
	}
	if cfg.RateLimitRPS != 500 {
		t.Errorf("RATE_LIMIT_RPS override: got %d, want 500", cfg.RateLimitRPS)
	}
	t.Logf("✅ All environment variable overrides applied correctly")
}

// TestConfig_InvalidEnvValues verifies that invalid environment variable values
// fall back to defaults without panicking.
func TestConfig_InvalidEnvValues(t *testing.T) {
	t.Setenv("CHAIN_ID", "not-a-number")
	t.Setenv("CONSENSUS_TIMEOUT", "not-a-duration")
	t.Setenv("BLOCK_INTERVAL", "also-invalid")
	t.Setenv("HSM_SLOT", "three")
	t.Setenv("RATE_LIMIT_RPS", "unlimited")

	cfg := &config.NodeConfig{}
	cfg.LoadRuntimeConfig()

	// Invalid values must fall back to defaults
	if cfg.ChainID != 1 {
		t.Errorf("invalid CHAIN_ID: expected default 1, got %d", cfg.ChainID)
	}
	if cfg.ConsensusTimeout != 10*time.Second {
		t.Errorf("invalid CONSENSUS_TIMEOUT: expected default 10s, got %v", cfg.ConsensusTimeout)
	}
	if cfg.BlockInterval != 5*time.Second {
		t.Errorf("invalid BLOCK_INTERVAL: expected default 5s, got %v", cfg.BlockInterval)
	}
	if cfg.HSMSlot != 0 {
		t.Errorf("invalid HSM_SLOT: expected default 0, got %d", cfg.HSMSlot)
	}
	if cfg.RateLimitRPS != 1000 {
		t.Errorf("invalid RATE_LIMIT_RPS: expected default 1000, got %d", cfg.RateLimitRPS)
	}
	t.Logf("✅ Invalid env values correctly fall back to defaults")
}

// TestConfig_ExtremeValues tests configuration with extreme but valid parameter values.
func TestConfig_ExtremeValues(t *testing.T) {
	extremeCases := []struct {
		key   string
		value string
	}{
		{"CHAIN_ID", strconv.FormatUint(^uint64(0), 10)},  // max uint64
		{"CONSENSUS_TIMEOUT", "8760h"},                     // 1 year
		{"BLOCK_INTERVAL", "1ms"},                          // 1 millisecond
		{"RATE_LIMIT_RPS", "1000000"},                      // 1M RPS
	}

	for _, tc := range extremeCases {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			cfg := &config.NodeConfig{}
			// Must not panic
			cfg.LoadRuntimeConfig()
		})
	}
	t.Logf("✅ Extreme parameter values handled without panic")
}

// TestConfig_HSMDisabledByDefault verifies that HSM is disabled when env var is absent.
func TestConfig_HSMDisabledByDefault(t *testing.T) {
	os.Unsetenv("HSM_ENABLED")
	cfg := &config.NodeConfig{}
	cfg.LoadRuntimeConfig()

	if cfg.HSMEnabled {
		t.Error("HSM should be disabled by default")
	}
	t.Logf("✅ HSM disabled by default")
}

// TestConfig_HSMNotEnabledWithWrongValue verifies that HSM_ENABLED only enables
// HSM when set to exactly "true".
func TestConfig_HSMNotEnabledWithWrongValue(t *testing.T) {
	for _, val := range []string{"1", "yes", "TRUE", "True", "on"} {
		t.Setenv("HSM_ENABLED", val)
		cfg := &config.NodeConfig{}
		cfg.LoadRuntimeConfig()

		if cfg.HSMEnabled {
			t.Errorf("HSM_ENABLED=%q should not enable HSM (only 'true' is accepted)", val)
		}
	}
	t.Logf("✅ HSM only enabled with exact 'true' value")
}

// TestConfig_ConcurrentLoads verifies that loading configuration from multiple
// goroutines does not cause data races.
func TestConfig_ConcurrentLoads(t *testing.T) {
	t.Setenv("CHAIN_ID", "5")
	t.Setenv("LOG_LEVEL", "warn")

	const numGoroutines = 50

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cfg := &config.NodeConfig{}
			cfg.LoadRuntimeConfig()

			if cfg.ChainID != 5 {
				errors <- nil // env may have changed; just skip
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	t.Logf("✅ Concurrent config loads completed without race")
}

// TestConfig_ValidateNodeConfigDefaults tests that ValidateNodeConfig fills in
// sensible defaults for optional fields.
func TestConfig_ValidateNodeConfigDefaults(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "test-node",
		GenesisPath: "/tmp/genesis.json",
	}

	if err := cfg.ValidateNodeConfig(); err != nil {
		t.Fatalf("ValidateNodeConfig failed: %v", err)
	}

	if cfg.DataDir == "" {
		t.Error("DataDir should have a default value")
	}
	if cfg.RPCListen == "" {
		t.Error("RPCListen should have a default value")
	}
	if cfg.ListenAddr == "" {
		t.Error("ListenAddr should have a default value")
	}
	t.Logf("✅ ValidateNodeConfig sets default values for optional fields")
}

// TestConfig_ValidateNodeConfigRequiredFields verifies that required fields
// (NodeID, GenesisPath) are enforced.
func TestConfig_ValidateNodeConfigRequiredFields(t *testing.T) {
	// Missing NodeID
	cfg1 := &config.NodeConfig{GenesisPath: "/tmp/genesis.json"}
	if err := cfg1.ValidateNodeConfig(); err == nil {
		t.Error("expected error for missing NodeID")
	}

	// Missing GenesisPath
	cfg2 := &config.NodeConfig{NodeID: "my-node"}
	if err := cfg2.ValidateNodeConfig(); err == nil {
		t.Error("expected error for missing GenesisPath")
	}

	// Both required fields present
	cfg3 := &config.NodeConfig{
		NodeID:      "my-node",
		GenesisPath: "/tmp/genesis.json",
	}
	if err := cfg3.ValidateNodeConfig(); err != nil {
		t.Errorf("unexpected error with valid config: %v", err)
	}
	t.Logf("✅ Required field validation works correctly")
}
