package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"synthos-collective/internal/agent"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
)

// getHardwareID is a placeholder for real hardware binding.
// On a real node, this should use TPM, secure enclave, or another trusted
// hardware identity mechanism. For now we accept an env var override and
// otherwise derive a simple, host-local identifier.
func getHardwareID() string {
	if v := os.Getenv("SYNTHOS_HARDWARE_ID"); v != "" {
		return v
	}
	hostname, _ := os.Hostname()
	return fmt.Sprintf("host-%s", hostname)
}

func main() {
	hwID := getHardwareID()

	// Demonstrate: two agents exchanging signed envelopes over an untrusted relay fabric,
	// without opening inbound ports (local-only transport here).
	keysA, _ := synthoscrypto.NewKeyPair()
	keysB, _ := synthoscrypto.NewKeyPair()

	a := agent.NewAgent("agent-1", "", "", hwID, 0)
	b := agent.NewAgent("agent-2", "", "", hwID, 0)
	a.AttachKeys(keysA)
	b.AttachKeys(keysB)

	bus := network.NewMemoryTransport()
	ta := bus.NodeTransport(a.Identity.AgentID)
	tb := bus.NodeTransport(b.Identity.AgentID)
	_ = ta.Start()

	a.AttachTransport(ta)
	b.AttachTransport(tb)

	// Record a sample computation: "startup" and "validate_block"
	a.RecordComputation(map[string]any{
		"action":    "startup",
		"timestamp": time.Now().UTC(),
	})

	proof := a.RecordComputation(map[string]any{
		"action": "validate_block",
		"block": map[string]any{
			"height": 1,
		},
	})

	ok := a.VerifyProof(proof)

	// Register B's handler to verify envelopes from A.
	tb.OnAgentMessage(func(from string, payload []byte) {
		var env network.Envelope
		_ = json.Unmarshal(payload, &env)
		pub, _ := synthoscrypto.PublicKeyBytes(a.Identity.PublicKey)
		err := b.VerifyEnvelope(env, pub, time.Now().UTC())
		fmt.Printf("B received from=%s type=%s verified=%v err=%v\n", from, env.MessageType, err == nil, err)
	})

	// A sends a signed message to B through the untrusted "relay".
	env, _ := a.BuildEnvelope("peer_announcement", b.Identity.AgentID, "", map[string]any{
		"hello": "world",
		"state": map[string]any{
			"poc_root": a.ProofRoot(),
		},
	})
	_ = a.SendEnvelope(env)

	state := map[string]any{
		"identity": a.Identity,
		"latest_proof_valid": ok,
		"proof_root": a.ProofRoot(),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(state)
}

