package node_test

import (
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

func TestSingleValidatorProposalFinalizes(t *testing.T) {
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	a := agent.NewAgent("validator-1", "", "", "test-hardware", 0)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatal(err)
	}

	ch, err := chain.NewChain(chain.Genesis{
		ChainID:   "test-chain",
		TxChainID: 20260702,
		Alloc: map[chain.Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 100,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	bus := network.NewMemoryTransport()
	transport := bus.NodeTransport(a.Identity.AgentID)
	a.AttachTransport(transport)

	n := node.NewNode(a, ch, consensus.NewEngine(1), transport)
	n.SetValidators([]string{a.Identity.AgentID})
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}

	hash, err := n.ProposeBlockHash()
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected proposed block hash")
	}
	if got := ch.Height(); got != 1 {
		t.Fatalf("height after proposal: got %d, want 1", got)
	}
}
