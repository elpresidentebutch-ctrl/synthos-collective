package main

import (
	"fmt"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
	"synthos-collective/internal/node"
)

func main() {
	// Spin up N nodes, all outbound-only, connected through an in-memory relay fabric.
	// This simulates "thousands of agents over relays" without opening any ports.
	const validatorsN = 4
	const replicasN = 3 // non-validator "replicas" that must not affect consensus

	bus := network.NewMemoryTransport()
	_ = bus.Start()

	// Build a shared genesis with 100B allocated to node-0's chain address for demo purposes.
	// (Real networks would use a genesis file distributed to all nodes.)
	genAlloc := make(map[chain.Address]uint64)

	agents := make([]*agent.Agent, 0, validatorsN+replicasN)
	nodes := make([]*node.Node, 0, validatorsN+replicasN)

	for i := 0; i < validatorsN+replicasN; i++ {
		keys, _ := synthoscrypto.NewKeyPair()
		hwID := fmt.Sprintf("devnet-hw-%d", i)
		a := agent.NewAgent(fmt.Sprintf("agent-%d", i), "", "", hwID, 0)
		a.AttachKeys(keys)

		agents = append(agents, a)
	}

	// Use node0’s wallet address as the genesis allocation target for this demo.
	pub0, _ := synthoscrypto.PublicKeyBytes(agents[0].Identity.PublicKey)
	addr0 := chain.AddressFromPublicKey(pub0)
	genAlloc[addr0] = 100_000_000_000

	gen := chain.Genesis{
		ChainID: "synthos-l1-devnet",
		Alloc:   genAlloc,
		Metadata: map[string]any{
			"symbol":   "SYN",
			"decimals": 0,
		},
	}

	validatorIDs := make([]string, 0, validatorsN)
	for i := 0; i < validatorsN; i++ {
		validatorIDs = append(validatorIDs, agents[i].Identity.AgentID)
	}

	for i := 0; i < validatorsN+replicasN; i++ {
		c, err := chain.NewChain(gen)
		if err != nil {
			panic(err)
		}
		eng := consensus.NewEngine(validatorsN)
		t := bus.NodeTransport(agents[i].Identity.AgentID)
		agents[i].AttachTransport(t)

		nd := node.NewNode(agents[i], c, eng, t)
		nd.SetValidators(validatorIDs)
		// Enable debug logs for the first node to diagnose envelope issues.
		if i == 0 {
			nd.Logf = func(format string, args ...any) {
				fmt.Printf("[node0] "+format+"\n", args...)
			}
		}
		nodes = append(nodes, nd)
	}

	// Establish peer keys (static registry for devnet demo).
	for i := 0; i < validatorsN+replicasN; i++ {
		for j := 0; j < validatorsN+replicasN; j++ {
			_ = nodes[i].AddPeer(agents[j].Identity.AgentID, agents[j].Identity.PublicKey)
		}
		_ = nodes[i].Start()
	}

	// Propose a block from node0.
	fmt.Printf("Devnet: proposing block from %s (need %d for finality)\n", agents[0].Identity.AgentID, nodes[0].Consensus.RequiredForFinality())
	blockHash, _ := nodes[0].ProposeBlockHash()

	// Allow message delivery to propagate through handlers.
	for attempt := 0; attempt < 10; attempt++ {
		time.Sleep(150 * time.Millisecond)
		for i := 0; i < validatorsN+replicasN; i++ {
			_ = nodes[i].TryFinalize(blockHash)
		}
		allAt1 := true
		for i := 0; i < validatorsN; i++ {
			if nodes[i].Chain.Height() < 1 {
				allAt1 = false
				break
			}
		}
		if allAt1 {
			break
		}
	}

	// Print heights and balances.
	for i := 0; i < validatorsN+replicasN; i++ {
		finalized, votesFor, required, ok := nodes[i].Consensus.FinalityStatus(blockHash)
		h := nodes[i].Chain.Height()
		root := nodes[i].Chain.State.Root()
		bal := nodes[i].Chain.State.Get(addr0).Balance
		fmt.Printf("node-%d validator=%v height=%d state_root=%s addr0_balance=%d finality_ok=%v finalized=%v votes_for=%d required=%d\n",
			i, nodes[i].IsValidator(agents[i].Identity.AgentID), h, root, bal, ok, finalized, votesFor, required)
	}
}

