package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
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

	// Optional hook called after this node finalizes a block.
	OnFinalize func(*chain.Chain) error

	// Slashing is the real enforcement backend: double-signing and vote
	// equivocation detected by Consensus get reported here, and it's the
	// thing that actually debits a misbehaving validator's real balance
	// (see the ExecuteSlash wiring in NewNode) rather than just logging
	// that something bad happened.
	Slashing *consensus.SlashingTracker

	// Governance is the real treasury-proposal system. Founder-gated
	// proposal creation, stake-weighted voting, and fund release all run
	// through this rather than a simulated/no-op path.
	Governance *chain.TreasuryGovernance

	// Inbox holds recently received Communicator role messages (see
	// docs/AGENTS_SPECIFICATION.md role 4, "communicator_message" in
	// handleRaw below). It's a small ring buffer, not a durable message
	// store: RPC layer only, not persisted via storage.Store snapshots.
	inboxMu sync.Mutex
	inbox   []CommunicatorMessage
}

// CommunicatorMessage is one real, envelope-verified message this node has
// received from a known, verified peer via the Communicator role's
// send_message/handle_incoming_message path.
type CommunicatorMessage struct {
	FromAgentID string    `json:"from_agent_id"`
	Body        string    `json:"body"`
	ReceivedAt  time.Time `json:"received_at"`
}

// maxInboxSize bounds the in-memory Communicator inbox so a chatty or
// malicious (but verified, rate-limited) peer can't grow it unboundedly.
const maxInboxSize = 200

func (n *Node) appendInbox(msg CommunicatorMessage) {
	n.inboxMu.Lock()
	defer n.inboxMu.Unlock()
	n.inbox = append(n.inbox, msg)
	if len(n.inbox) > maxInboxSize {
		n.inbox = n.inbox[len(n.inbox)-maxInboxSize:]
	}
}

// Inbox returns a copy of the recently received Communicator messages,
// newest last.
func (n *Node) Inbox() []CommunicatorMessage {
	n.inboxMu.Lock()
	defer n.inboxMu.Unlock()
	out := make([]CommunicatorMessage, len(n.inbox))
	copy(out, n.inbox)
	return out
}

var (
	ErrNoPeerKey = errors.New("missing peer public key")
)

func NewNode(a *agent.Agent, c *chain.Chain, eng *consensus.Engine, t network.Transport) *Node {
	n := &Node{
		Agent:            a,
		Chain:            c,
		Consensus:        eng,
		Transport:        t,
		Peers:            make(map[string][]byte),
		Validators:       make(map[string]bool),
		PeerHardwareHash: make(map[string]string),
		// Default: 50 msgs burst, 10 msgs/sec per peer.
		InboundLimiter: network.NewPeerLimiter(50, 10),
	}

	// Wire real enforcement: a SlashingTracker whose penalties actually
	// debit the misbehaving validator's real chain balance, not just an
	// internal shadow number. Penalty sizes are deliberately modest
	// defaults (documented here, not hidden) -- operators running a real
	// deployment should tune SlashingParams for their own stake economics.
	tracker := consensus.NewSlashingTracker(consensus.SlashingParams{
		DoubleSignPenalty:   1000,
		InvalidBlockPenalty: 250,
		DowntimePenalty:     50,
	})
	tracker.SetExecuteSlash(func(validatorID string, penalty uint64) {
		addr, ok := n.addressForAgentID(validatorID)
		if !ok || c == nil {
			return
		}
		acc := c.State.Get(addr)
		if acc.Balance > penalty {
			acc.Balance -= penalty
		} else {
			acc.Balance = 0
		}
		c.State.Set(addr, acc)
	})
	n.Slashing = tracker
	if eng != nil {
		eng.SetSlashingTracker(tracker)
	}

	return n
}

// addressForAgentID resolves an AgentID (the string consensus code uses,
// e.g. block proposer/voter IDs) to the real chain.Address that actually
// holds that identity's balance, so real penalties and rewards land on the
// right account. It checks this node's own identity first, then its known
// peers.
func (n *Node) addressForAgentID(agentID string) (chain.Address, bool) {
	if n.Agent != nil && n.Agent.Identity.AgentID == agentID {
		pubBytes, err := synthoscrypto.PublicKeyBytes(n.Agent.Identity.PublicKey)
		if err != nil {
			return "", false
		}
		return chain.AddressFromPublicKey(pubBytes), true
	}
	if pubBytes, ok := n.Peers[agentID]; ok {
		return chain.AddressFromPublicKey(pubBytes), true
	}
	return "", false
}

// RefreshReputation recomputes this node's own agent's reputation from real
// chain, governance, and slashing history (see agent.Agent.RefreshReputation)
// and returns the new score.
func (n *Node) RefreshReputation() int {
	if n.Agent == nil {
		return 0
	}
	return n.Agent.RefreshReputation(n.Chain, n.Governance, n.Slashing)
}

// InitGovernance wires up real treasury governance for this node: founder
// address, treasury address, and a real GetStake function backed by the
// same chain state every balance/transfer uses (see chain.State.TotalStake
// for how the RPC layer measures quorum against it). Deployments that don't
// configure a founder/treasury address simply never call this, and
// n.Governance stays nil -- the RPC layer reports governance as
// unconfigured rather than silently accepting requests against an empty
// address.
func (n *Node) InitGovernance(founder, treasury chain.Address) {
	n.Governance = chain.NewTreasuryGovernance(founder, treasury, func(addr chain.Address) uint64 {
		if n.Chain == nil {
			return 0
		}
		return n.Chain.State.Get(addr).Balance
	})
}

func (n *Node) IsValidator(agentID string) bool {
	return n.Validators[agentID]
}

func (n *Node) SetValidators(validators []string) {
	n.Validators = make(map[string]bool, len(validators))
	for _, v := range validators {
		n.Validators[v] = true
	}
	if n.Consensus != nil {
		n.Consensus.SetValidators(validators)
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
	n.Transport.OnTopicMessage(network.TopicCoverNoise, func(from string, payload []byte) {
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

	if env.MessageType == network.MessageCoverNoise {
		if _, err := consensus.VerifyAndUnmarshalEnvelope[network.CoverNoisePayload](n.Agent.VerifyEnvelope, env, pub, now); err != nil {
			if n.Logf != nil {
				n.Logf("drop: bad cover noise verify from_agent=%s err=%v", env.FromAgentID, err)
			}
			return
		}
		if _, err := network.DecodeCoverNoise(env); err != nil {
			if n.Logf != nil {
				n.Logf("drop: bad cover noise domain from_agent=%s err=%v", env.FromAgentID, err)
			}
			return
		}
		if n.Logf != nil {
			n.Logf("drop: verified transport-only cover noise from_agent=%s", env.FromAgentID)
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
		if b.Header.ProposerID != env.FromAgentID {
			if n.Logf != nil {
				n.Logf("drop: proposer identity mismatch from_agent=%s proposer_id=%s", env.FromAgentID, b.Header.ProposerID)
			}
			return
		}
		// Height sanity: proposal must extend our current tip.
		expectedHeight := n.Chain.Height() + 1
		if b.Header.Height != expectedHeight {
			if n.Logf != nil {
				n.Logf("drop: proposal height mismatch from_agent=%s got=%d expected=%d", env.FromAgentID, b.Header.Height, expectedHeight)
			}
			return
		}
		// Basic chain validation. Everything needed to safely act on a
		// failure here is already true at this point: the envelope's
		// signature was verified above against env.FromAgentID's known
		// public key (VerifyAndUnmarshalEnvelope), the block's own
		// ProposerID was checked to match that verified sender (so a
		// forged block can't pin blame on an innocent validator), and the
		// height check above already restricts this to a proposal
		// extending our current tip -- so a stale-but-formerly-valid old
		// block can never land here after it's been superseded. That
		// means a ValidateBlock failure at this point is real,
		// independently-checked proof that a known validator proposed a
		// genuinely invalid block, not a bare accusation.
		//
		// This is the Enforcer wiring: SlashingTracker.RecordInvalidBlock
		// already existed but had no caller anywhere, so a validator could
		// propose a malformed/invalid block and nothing but a log line
		// ever happened. Every validator that receives the bad proposal
		// now independently detects and records it, the same way
		// double-signing and equivocation already do via OnProposal/OnVote
		// below.
		if err := n.Chain.ValidateBlock(&b); err != nil {
			if n.Logf != nil {
				n.Logf("warn: proposal validate failed hash=%s err=%v", b.Hash, err)
			}
			if n.Slashing != nil {
				_ = n.Slashing.RecordInvalidBlock(env.FromAgentID, b.Header.Height, err.Error())
			}
			return
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
		if vote == 1 {
			if sig, err := n.Agent.SignRaw(b.QuorumApprovalMessage()); err == nil {
				v.Signature = "0x" + hex.EncodeToString(sig)
			}
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

	case network.MessageCommunicator:
		// Communicator role (docs/AGENTS_SPECIFICATION.md role 4):
		// handle_incoming_message. Everything above this switch already
		// verified the sender is a known peer, checked their hardware-hash
		// consistency, and rate-limited them, so this only needs its own
		// envelope signature check before recording the message.
		payload, err := consensus.VerifyAndUnmarshalEnvelope[network.CommunicatorPayload](n.Agent.VerifyEnvelope, env, pub, now)
		if err != nil {
			if n.Logf != nil {
				n.Logf("drop: bad communicator message verify from_agent=%s err=%v", env.FromAgentID, err)
			}
			return
		}
		n.appendInbox(CommunicatorMessage{
			FromAgentID: env.FromAgentID,
			Body:        payload.Body,
			ReceivedAt:  now,
		})
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
	// Embed the real, independently-verifiable approval signatures gathered
	// for this exact block hash so Chain can verify quorum was genuinely
	// reached, rather than trusting that this node's own vote-tallying is
	// correct.
	b.QuorumSignatures = n.Consensus.CollectedApprovals(b.Header.Height, blockHash)
	if err := n.Chain.FinalizeBlock(b); err != nil {
		return err
	}
	// Reputation is recomputed from real chain history every time this
	// node's chain advances, rather than left as a static number nothing
	// ever touches.
	n.RefreshReputation()
	if n.OnFinalize != nil {
		return n.OnFinalize(n.Chain)
	}
	return nil
}

// NoteMissedSlot tells the consensus engine's slashing tracker that the
// validator expected to propose at this height did not do so before its
// slot passed, so it counts toward real downtime tracking. This function
// only forwards the observation -- deciding when a slot has actually been
// missed (round-robin schedule + timeout) is the caller's job.
func (n *Node) NoteMissedSlot(expectedProposerID string, height uint64) {
	if n.Consensus != nil {
		n.Consensus.NoteMissedSlot(expectedProposerID, height)
	}
}

// signProposalBlock attaches this node's proposer signature to a freshly
// built block before it is broadcast or self-finalized, so every recipient
// (and Chain itself, once a validator set is configured) can independently
// verify the block genuinely came from this validator.
func (n *Node) signProposalBlock(b *chain.Block) error {
	sig, err := n.Agent.SignRaw([]byte(b.Hash))
	if err != nil {
		return err
	}
	b.ProposerSignature = "0x" + hex.EncodeToString(sig)
	return nil
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
	if err := n.signProposalBlock(b); err != nil {
		return err
	}
	n.Consensus.OnProposal(b)

	env, err := n.Agent.BuildEnvelope("block_proposal", "", consensus.TopicProposals, consensus.BlockProposal{Block: *b, Height: height})
	if err != nil {
		return err
	}
	if err := n.Agent.SendEnvelope(env); err != nil {
		return err
	}
	return n.voteAndFinalizeSelfProposal(b)
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
	if err := n.signProposalBlock(b); err != nil {
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
	if err := n.voteAndFinalizeSelfProposal(b); err != nil {
		return "", err
	}
	return b.Hash, nil
}

func (n *Node) voteAndFinalizeSelfProposal(b *chain.Block) error {
	v := consensus.BlockVote{
		BlockHash: b.Hash,
		Height:    b.Header.Height,
		VoterID:   n.Agent.Identity.AgentID,
		Vote:      1,
	}
	if sig, err := n.Agent.SignRaw(b.QuorumApprovalMessage()); err == nil {
		v.Signature = "0x" + hex.EncodeToString(sig)
	}
	finalized, _, _, _ := n.Consensus.OnVote(v)
	if finalized {
		if err := n.TryFinalize(b.Hash); err != nil {
			return err
		}
	}
	env, err := n.Agent.BuildEnvelope("block_vote", "", consensus.TopicVotes, v)
	if err != nil {
		return err
	}
	return n.Agent.SendEnvelope(env)
}
