package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/config"
	"synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
)

// Sovereign Mailbox Store (Cloudless Relay)
var (
	mailboxMu sync.RWMutex
	mailbox   = make(map[string][][]byte) // AgentID -> List of Envelopes
)

// hardwareID returns a best-effort, non-secret machine identifier: an
// explicit override, else the hostname. This is the same placeholder
// pattern cmd/agent/main.go already uses for the same purpose. It is NOT a
// real hardware root of trust (TPM, secure enclave) -- it exists so a vault
// isn't silently portable to a different machine by accident, not as
// tamper-proof binding. Previously this was a hardcoded literal
// ("hardware-desktop-v1", identical on every install), which combined with
// vault.go's old unsalted-SHA256 KDF and a hardcoded default passphrase
// fallback below made the vault's encryption key a fixed, publicly
// computable value -- see internal/crypto/vault.go's doc comments.
func hardwareID() string {
	if v := os.Getenv("SYNTHOS_HARDWARE_ID"); v != "" {
		return v
	}
	hostname, _ := os.Hostname()
	return fmt.Sprintf("host-%s", hostname)
}

func main() {
	// 1. Initialize Identity & Hardware Binding
	hwID := hardwareID()
	vaultPath := ".synthos/vault.json"
	passphrase := os.Getenv("SYNTHOS_PASSPHRASE")
	if passphrase == "" {
		// No default fallback: a fixed, hardcoded passphrase here would once
		// again make the vault's encryption key a fixed, publicly
		// computable value regardless of how good vault.go's KDF is (this
		// is exactly how the audit recovered a real key from this repo's
		// own checked-in .synthos/vault.json). The operator must supply one.
		log.Fatal("❌ SYNTHOS_PASSPHRASE is required (no default is used) -- set it to a real passphrase and rerun. This encrypts a real private key; losing it after vault creation loses the identity, and using a weak/reused one defeats the point of encrypting it at all.")
	}

	var pub ed25519.PublicKey
	var priv ed25519.PrivateKey

	if _, err := os.Stat(vaultPath); err == nil {
		log.Println("🔓 Vault found. Decrypting agent identity...")
		var loadErr error
		priv, loadErr = crypto.LoadEncryptedKey(vaultPath, passphrase, hwID)
		if loadErr != nil {
			log.Fatalf("❌ Vault Decryption Failed: %v", loadErr)
		}
		pub = priv.Public().(ed25519.PublicKey)
	} else {
		log.Println("✨ No vault found. Generating new hardware-bound identity...")
		pub, priv, _ = ed25519.GenerateKey(rand.Reader)
		// Previously this new identity was never persisted at all -- every
		// restart with no vault present silently generated (and lost) a
		// brand new identity. Save it now, encrypted with the operator's
		// own passphrase, so the "no vault found" path actually produces a
		// durable identity rather than only an ephemeral in-memory one.
		if err := os.MkdirAll(filepath.Dir(vaultPath), 0o700); err != nil {
			log.Fatalf("❌ Creating vault directory: %v", err)
		}
		if err := crypto.SaveEncryptedKey(vaultPath, priv, passphrase, hwID); err != nil {
			log.Fatalf("❌ Saving new vault: %v", err)
		}
		log.Println("🔐 New identity encrypted and saved to", vaultPath, "-- back up this file and your passphrase together; losing either one loses this identity permanently.")
	}
	passphrase = "CLEARED"

	addr := chain.AddressFromPublicKey(pub)
	agentID := fmt.Sprintf("agent-%s", string(addr)[2:18])

	// 2. Setup Mesh Infrastructure (Sovereign L1)
	//
	// Genesis comes from config/genesis.json — the single, documented source
	// of truth for allocation (see docs/SYN_COINS_GENESIS_WALLETS.md). This
	// used to construct its own separate, undocumented genesis block here
	// with three hardcoded addresses that received 99.5% of supply outside
	// of anything in config/genesis.json or the docs. That parallel genesis
	// has been removed; this node now loads the same genesis every other
	// entrypoint uses. If the file can't be loaded, we fail loudly instead
	// of silently minting supply to addresses nobody can see.
	gen, err := config.LoadGenesis("config/genesis.json")
	if err != nil {
		log.Fatalf("❌ Failed to load config/genesis.json: %v", err)
	}
	c, err := chain.NewChain(gen)
	if err != nil {
		log.Fatalf("❌ Failed to initialize chain from genesis: %v", err)
	}

	// Seed DEX Pools
	c.DEX.SeedPool("B12", 10_000_000, 50_000)
	c.DEX.SeedPool("NGOT", 5_000_000, 100_000)
	c.DEX.SeedPool("MOMENTUM", 2_000_000, 10_000)

	// 3. Initialize Agent with 7 Core Functions
	a := agent.NewAgent(agentID, "0x"+fmt.Sprintf("%x", pub), string(addr), hwID, 1000000)

	// 4. Setup Outbound 'Sign Language' Transport (NO LISTENERS)
	//
	// The old fallback here was "http://synthos-anchor-1.world:8080" -- a
	// placeholder domain that was never registered and has never resolved,
	// so any operator who ran this binary without setting SYNTHOS_RELAY
	// silently talked to nothing. The real default now matches the same
	// production backend every other entrypoint (cmd/silentnode, the
	// installer scripts) falls back to: SYNTHOS_REGISTRY_URL/synthos-www's
	// public host, https://synthos-www.onrender.com. SYNTHOS_RELAY still
	// overrides it for local/dev use against a different backend.
	const defaultRelayURL = "https://synthos-www.onrender.com"
	registryURL := os.Getenv("SYNTHOS_RELAY")
	if registryURL == "" {
		registryURL = defaultRelayURL
	}

	t := network.NewRelayTransport([]string{registryURL})
	t.SelfName = agentID
	t.SelfURL = "" // No self-URL because we don't listen
	a.AttachTransport(t)

	// 5. Initialize DMAS Distributed Immune System (Stake-Weighted)
	//
	// This used to hardcode a "Founder Anchor" address as permanently immune
	// from slashing, and to route 50% of every slashed stake to that same
	// address and 50% to another undocumented address — both removed. Every
	// address is now slashable on equal terms, and a slashed stake is burned
	// (removed from circulation) rather than funneled to any address. If you
	// want slashed stake to go somewhere specific instead of being burned,
	// that destination needs to be a real, documented address — not a
	// hardcoded one only the code knows about.
	immune := chain.NewImmuneSystem(
		func(a chain.Address) uint64 {
			acc := c.State.Get(a)
			return acc.Balance
		},
		func(a chain.Address) {
			log.Printf("🔥 EXECUTING AUTO-SLASH ON %s (stake burned)", a)
			acc := c.State.Get(a)
			acc.Balance = 0
			c.State.Set(a, acc)
		},
	)

	// 6. Initialize OpenTelemetry (Outbound Push)
	ctx := context.Background()
	mp, err := initMetrics(ctx, agentID)
	if err != nil {
		if errors.Is(err, errMetricsNotConfigured) {
			log.Printf("📊 OpenTelemetry disabled: %v", err)
		} else {
			log.Printf("⚠️  Observability error: %v", err)
		}
	} else {
		defer mp.Shutdown(ctx)
		log.Printf("📊 OpenTelemetry Enabled: pushing to %s", os.Getenv("SYNTHOS_OTEL_ENDPOINT"))
	}

	if err := t.Start(); err != nil {
		log.Printf("⚠️  Failed to start outbound transport: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	log.Printf("✅ Node %s (Addr: %s) is running in ABSOLUTE SILENCE mode.", agentID, addr)
	log.Printf("🛡️  Immune System ACTIVE: Distributed Consensus Mode Engaged.")
	log.Printf("📡 Status: Outbound Sign Language Active. No ports open.")

	// 7. Start the Autonomous Idle Worker (Fractal Heartbeat + Immune Pulse)
	go startIdleWorker(a, c, priv, uint64(1), immune)

	<-stop

	log.Println("🛑 Shutting down...")
	log.Println("👋 Shutdown complete.")
}

func startIdleWorker(a *agent.Agent, c *chain.Chain, priv ed25519.PrivateKey, chainID uint64, immune *chain.ImmuneSystem) {
	ticker := time.NewTicker(15 * time.Second)
	for range ticker.C {
		if len(c.Mempool) == 0 {
			pubKeyStr := a.Identity.PublicKey
			if len(pubKeyStr) >= 2 && pubKeyStr[:2] == "0x" {
				pubKeyStr = pubKeyStr[2:]
			}
			pubBytes, _ := hex.DecodeString(pubKeyStr)
			addr := chain.AddressFromPublicKey(pubBytes)

			tx := chain.Tx{
				ChainID:   chainID,
				From:      addr,
				To:        chain.Address("0xdeadbeef"),
				Amount:    100,
				Fee:       chain.MIN_FEE,
				Nonce:     c.State.GetNextNonce(addr),
				PublicKey: a.Identity.PublicKey,
				Timestamp: time.Now().Unix(),
			}

			if err := tx.Sign(priv); err == nil {
				_ = c.SubmitTx(tx)
				block, err := c.BuildBlock(a.Identity.AgentID, a.ProofRoot(), 10)
				if err == nil {
					_ = c.FinalizeBlock(block)
					log.Printf("📦 Heartbeat finalized at block #%d", block.Header.Height)
				}
			}
		}
	}
}
