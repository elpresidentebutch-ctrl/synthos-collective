package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
)

// Sovereign Mailbox Store (Cloudless Relay)
var (
	mailboxMu sync.RWMutex
	mailbox   = make(map[string][][]byte) // AgentID -> List of Envelopes
)

func main() {
	// 1. Initialize Identity & Hardware Binding
	hwID := "hardware-desktop-v1"
	vaultPath := ".synthos/vault.json"
	passphrase := os.Getenv("SYNTHOS_PASSPHRASE")
	if passphrase == "" {
		passphrase = "default_sovereign_secret"
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
		passphrase = "CLEARED"
		pub = priv.Public().(ed25519.PublicKey)
	} else {
		log.Println("✨ No vault found. Generating new hardware-bound identity...")
		pub, priv, _ = ed25519.GenerateKey(rand.Reader)
	}

	addr := chain.AddressFromPublicKey(pub)
	agentID := fmt.Sprintf("agent-%s", string(addr)[2:18])

	// 2. Setup Mesh Infrastructure (Sovereign L1)
	c, _ := chain.NewChain(chain.Genesis{
		ChainID: "synthos_mainnet",
		Alloc: map[chain.Address]uint64{
			addr: 500_000_000,
			"0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b": 79_500_000_000,
			"0x4823d9af45c0e297d818eb58cb049a0860337aeb": 17_000_000_000,
			"0x4823d9af45c0e297d818eb58cb049a0860337aec": 3_000_000_000,
		},
	})

	// Seed DEX Pools
	c.DEX.SeedPool("B12", 10_000_000, 50_000)
	c.DEX.SeedPool("NGOT", 5_000_000, 100_000)
	c.DEX.SeedPool("MOMENTUM", 2_000_000, 10_000)

	// 3. Initialize Agent with 7 Core Functions
	a := agent.NewAgent(agentID, "0x"+fmt.Sprintf("%x", pub), "0x...", hwID, 1000000)

	// 4. Setup Outbound 'Sign Language' Transport (NO LISTENERS)
	BOOTSTRAP_ANCHORS := []string{"http://synthos-anchor-1.world:8080"}
	registryURL := os.Getenv("SYNTHOS_RELAY")
	if registryURL == "" {
		registryURL = BOOTSTRAP_ANCHORS[0]
	}

	t := network.NewRelayTransport([]string{registryURL})
	t.SelfName = agentID
	t.SelfURL = "" // No self-URL because we don't listen
	a.AttachTransport(t)

	// 5. Initialize DMAS Distributed Immune System (Stake-Weighted)
	immune := chain.NewImmuneSystem(
		chain.Address("0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b"), // Founder Anchor (Immune)
		func(a chain.Address) uint64 {
			acc := c.State.Get(a)
			return acc.Balance
		},
		func(a chain.Address) {
			log.Printf("🔥 EXECUTING AUTO-SLASH ON %s", a)
			acc := c.State.Get(a)

			// Split slashed stake 50% Treasury, 50% Founder
			half := acc.Balance / 2
			otherHalf := acc.Balance - half

			founderAddr := chain.Address("0x205042f06cd3aa7d9a88deec39b9d0ba6b9fbf2b")
			treasuryAddr := chain.Address("0x4823d9af45c0e297d818eb58cb049a0860337aeb")

			founderAcc := c.State.Get(founderAddr)
			founderAcc.Balance += half
			c.State.Set(founderAddr, founderAcc)

			treasuryAcc := c.State.Get(treasuryAddr)
			treasuryAcc.Balance += otherHalf
			c.State.Set(treasuryAddr, treasuryAcc)

			acc.Balance = 0 // 100% Stake Recycled (50/50 Split)
			c.State.Set(a, acc)
		},
	)

	// 6. Initialize OpenTelemetry (Outbound Push)
	ctx := context.Background()
	mp, err := initMetrics(ctx, agentID)
	if err != nil {
		log.Printf("⚠️  Observability error: %v", err)
	} else {
		defer mp.Shutdown(ctx)
		log.Printf("📊 OpenTelemetry Enabled: Pushing to monitoring.synthos-mesh.net")
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
