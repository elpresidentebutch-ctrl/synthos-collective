package node

import (
	"encoding/json"
	"errors"
	"time"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
)

// Node binds together: Agent identity + Chain + Consensus + Transport.
// It is designed to run outbound-only behind NAT, relying on relays for transport.
type Node struct {
	Agent     *agent.Agent
	Chain     *chain.Chain
	Consensus *consensus.Engine

	Transport network.Transport

	// Peer registry: agentID -> public key bytes
	Peers map[string][]byte

	// Validator set: only these agent IDs can propose/vote; only their votes count.
	Validators map[string]bool

	// Tracks the first-seen hardware hash per agent. If it ever changes, treat as clone/key-copy.
	PeerHardwareHash map[string]string

	// Inbound rate limiter (per peer).
	InboundLimiter *network.PeerLimiter

	// Optional debug logger.
	Logf func(format string, args ...any)
}

var (
	ErrNoPeerKey = errors.New("missing peer public key")
)

func NewNode(a *agent.Agent, c *chain.Chain, eng *consensus.Engine, t network.Transport) *Node {
	n := &Node{
		Agent:     a,
		Chain:     c,
		Consensus: eng,
		Transport: t,
		Peers:     make(map[string][]byte),
		Validators:        make(map[string]bool),
		PeerHardwareHash:  make(map[string]string),
		// Default: 50 msgs burst, 10 msgs/sec per peer.
		InboundLimiter: network.NewPeerLimiter(50, 10),
	}
	return n
}

func (n *Node) IsValidator(agentID string) bool {
	return n.Validators[agentID]
}

func (n *Node) SetValidators(validators []string) {
	n.Validators = make(map[string]bool, len(validators))
	for _, v := range validators {
		n.Validators[v] = true
	}
}

func (n *Node) AddPeer(agentID string, pubKeyHex string) error {
	b, err := synthoscrypto.PublicKeyBytes(pubKeyHex)
	if err != nil {
		return err
	}
	n.Peers[agentID] = b
	return nil
}

// Start registers message handlers on the transport.
func (n *Node) Start() error {
	if err := n.Transport.Start(); err != nil {
		return err
	}

	// Receive direct messages (optional).
	n.Transport.OnAgentMessage(func(from string, payload []byte) {
		n.handleRaw(from, payload)
	})
	// Receive consensus topics.
	n.Transport.OnTopicMessage(consensus.TopicProposals, func(from string, payload []byte) {
		n.handleRaw(from, payload)
	})
	n.Transport.OnTopicMessage(consensus.TopicVotes, func(from string, payload []byte) {
		n.handleRaw(from, payload)
	})
	return nil
}

func (n *Node) handleRaw(from string, payload []byte) {
	var env network.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		if n.Logf != nil {
			n.Logf("drop: bad json from=%s err=%v", from, err)
		}
		return
	}
	pub, ok := n.Peers[env.FromAgentID]
	if !ok {
		// Unknown peer: drop.
		if n.Logf != nil {
			n.Logf("drop: unknown peer from_agent=%s", env.FromAgentID)
		}
		return
	}

	// Hardware-hash consistency check (clone/key-copy detection).
	if prev, exists := n.PeerHardwareHash[env.FromAgentID]; exists {
		if prev != env.HardwareIDHash {
			if n.Logf != nil {
				n.Logf("drop: hardware hash changed from_agent=%s prev=%s now=%s", env.FromAgentID, prev, env.HardwareIDHash)
			}
			return
		}
	} else {
		n.PeerHardwareHash[env.FromAgentID] = env.HardwareIDHash
	}

	now := time.Now().UTC()

	// Per-peer rate limiting to resist relay spam.
	if n.InboundLimiter != nil && !n.InboundLimiter.Allow(env.FromAgentID, now) {
		if n.Logf != nil {
			n.Logf("drop: rate-limited from_agent=%s", env.FromAgentID)
		}
		return
	}

	switch env.MessageType {
	case "block_proposal":
		// Only validators may propose blocks.
		if !n.IsValidator(env.FromAgentID) {
			if n.Logf != nil {
				n.Logf("drop: non-validator proposal from_agent=%s", env.FromAgentID)
			}
			return
		}
		prop, err := consensus.VerifyAndUnmarshalEnvelope[consensus.BlockProposal](n.Agent.VerifyEnvelope, env, pub, now)
		if err != nil {
			if n.Logf != nil {
				n.Logf("drop: bad proposal verify from_agent=%s err=%v", env.FromAgentID, err)
			}
			return
		}
		b := prop.Block
		// Height sanity: proposal must extend our current tip.
		expectedHeight := n.Chain.Height() + 1
		if b.Header.Height != expectedHeight {
			if n.Logf != nil {
				n.Logf("drop: proposal height mismatch from_agent=%s got=%d expected=%d", env.FromAgentID, b.Header.Height, expectedHeight)
			}
			return
		}
		// Basic chain validation.
		if err := n.Chain.ValidateBlock(&b); err != nil {
			if n.Logf != nil {
				n.Logf("warn: proposal validate failed hash=%s err=%v", b.Hash, err)
			}
		}
		n.Consensus.OnProposal(&b)

		// Vote independently (validators only).
		if !n.IsValidator(n.Agent.Identity.AgentID) {
			return
		}
		vote := 1
		if err := n.Chain.ValidateBlock(&b); err != nil {
			vote = -1
		}
		v := consensus.BlockVote{
			BlockHash: b.Hash,
			Height:    b.Header.Height,
			VoterID:   n.Agent.Identity.AgentID,
			Vote:      vote,
		}
		envOut, err := n.Agent.BuildEnvelope("block_vote", "", consensus.TopicVotes, v)
		if err == nil {
			_ = n.Agent.SendEnvelope(envOut)
		} else if n.Logf != nil {
			n.Logf("warn: failed to build vote env err=%v", err)
		}

	case "block_vote":
		// Only validators' votes count.
		if !n.IsValidator(env.FromAgentID) {
			if n.Logf != nil {
				n.Logf("drop: non-validator vote from_agent=%s", env.FromAgentID)
			}
			return
		}
		v, err := consensus.VerifyAndUnmarshalEnvelope[consensus.BlockVote](n.Agent.VerifyEnvelope, env, pub, now)
		if err != nil {
			if n.Logf != nil {
				n.Logf("drop: bad vote verify from_agent=%s err=%v", env.FromAgentID, err)
			}
			return
		}
		// Prevent voter spoofing.
		if v.VoterID != env.FromAgentID {
			if n.Logf != nil {
				n.Logf("drop: vote voter_id mismatch from_agent=%s voter_id=%s", env.FromAgentID, v.VoterID)
			}
			return
		}
		finalized, _, _, _ := n.Consensus.OnVote(v)
		if finalized {
			_ = n.TryFinalize(v.BlockHash)
		}
	}
}

// TryFinalize finalizes a block locally if we have the proposal and it extends our tip.
func (n *Node) TryFinalize(blockHash string) error {
	b, ok := n.Consensus.Proposal(blockHash)
	if !ok || b == nil {
		return nil
	}
	finalized, _, _, ok := n.Consensus.FinalityStatus(blockHash)
	if !ok || !finalized {
		return nil
	}
	// Only finalize once.
	if tip := n.Chain.Tip(); tip != nil && tip.Hash == blockHash {
		return nil
	}
	return n.Chain.FinalizeBlock(b)
}

// ProposeBlock builds and broadcasts a block proposal.
func (n *Node) ProposeBlock() error {
	if !n.IsValidator(n.Agent.Identity.AgentID) {
		return errors.New("not a validator")
	}
	// Height-based round-robin proposer selection.
	height := n.Chain.Height() + 1
	b, err := n.Chain.BuildBlock(n.Agent.Identity.AgentID, n.Agent.ProofRoot(), 1000)
	if err != nil {
		return err
	}
	n.Consensus.OnProposal(b)

	env, err := n.Agent.BuildEnvelope("block_proposal", "", consensus.TopicProposals, consensus.BlockProposal{Block: *b, Height: height})
	if err != nil {
		return err
	}
	return n.Agent.SendEnvelope(env)
}

func (n *Node) ProposeBlockHash() (string, error) {
	if !n.IsValidator(n.Agent.Identity.AgentID) {
		return "", errors.New("not a validator")
	}
	height := n.Chain.Height() + 1
	b, err := n.Chain.BuildBlock(n.Agent.Identity.AgentID, n.Agent.ProofRoot(), 1000)
	if err != nil {
		return "", err
	}
	n.Consensus.OnProposal(b)
	env, err := n.Agent.BuildEnvelope("block_proposal", "", consensus.TopicProposals, consensus.BlockProposal{Block: *b, Height: height})
	if err != nil {
		return "", err
	}
	if err := n.Agent.SendEnvelope(env); err != nil {
		return "", err
	}
	return b.Hash, nil
}

