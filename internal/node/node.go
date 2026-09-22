package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

	// peersMu guards Peers, Validators, and PeerHardwareHash above. These
	// used to be plain map fields with no locking at all, but handleRaw
	// runs concurrently -- once per actively connected peer, since
	// SecureTCPTransport.acceptLoop spawns one goroutine per accepted
	// connection (see handleConn) and each one calls straight through to
	// this node's registered agentHandler. Two peers' messages arriving at
	// nearly the same time -- an entirely ordinary occurrence under real
	// multi-validator traffic, not an edge case -- meant two goroutines
	// could read/write these maps at once, which Go maps do not support:
	// at best a lost update, at worst a "fatal error: concurrent map
	// writes" crash. AddPeer and SetValidators (typically called during
	// startup/config, but not guaranteed to never race a live connection)
	// needed the same protection.
	peersMu sync.RWMutex
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
	n.peersMu.RLock()
	pubBytes, ok := n.Peers[agentID]
	n.peersMu.RUnlock()
	if ok {
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
// address and treasury address, both recorded on n.Chain.State
// (GovernanceFounder/TreasuryAddress) since that's what ApplyTx actually
// enforces when a governance_propose/governance_execute transaction is
// applied (see core.go's applyCitizenGovernanceTx and governance.go) --
// n.Governance itself is only a thin read facade over that same state now,
// not an independent copy. Deployments that don't configure a founder/
// treasury address simply never call this, and n.Governance stays nil --
// the RPC layer reports governance as unconfigured rather than silently
// accepting requests against an empty address.
func (n *Node) InitGovernance(founder, treasury chain.Address) {
	if n.Chain == nil {
		// Nothing to wire governance onto; leave n.Governance nil the same
		// way an unconfigured deployment does.
		return
	}
	n.Chain.State.GovernanceFounder = founder
	if treasury != "" {
		n.Chain.State.TreasuryAddress = treasury
	}
	n.Governance = chain.NewTreasuryGovernance(founder, treasury, n.Chain)
}

func (n *Node) IsValidator(agentID string) bool {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()
	return n.Validators[agentID]
}

func (n *Node) SetValidators(validators []string) {
	set := make(map[string]bool, len(validators))
	for _, v := range validators {
		set[v] = true
	}
	n.peersMu.Lock()
	n.Validators = set
	n.peersMu.Unlock()
	if n.Consensus != nil {
		n.Consensus.SetValidators(validators)
	}
}

func (n *Node) AddPeer(agentID string, pubKeyHex string) error {
	b, err := synthoscrypto.PublicKeyBytes(pubKeyHex)
	if err != nil {
		return err
	}
	n.peersMu.Lock()
	n.Peers[agentID] = b
	n.peersMu.Unlock()
	return nil
}

// HasPeer reports whether agentID is a known peer with a registered public
// key. Safe for concurrent use, unlike reading the Peers field directly --
// callers outside this package (e.g. internal/rpc/communicator.go) should
// use this instead of that direct field access, which predated peersMu and
// isn't synchronized against concurrent AddPeer/handleRaw calls.
func (n *Node) HasPeer(agentID string) bool {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()
	_, ok := n.Peers[agentID]
	return ok
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
	n.peersMu.RLock()
	pub, ok := n.Peers[env.FromAgentID]
	n.peersMu.RUnlock()
	if !ok {
		// Unknown peer: drop.
		if n.Logf != nil {
			n.Logf("drop: unknown peer from_agent=%s", env.FromAgentID)
		}
		return
	}

	now := time.Now().UTC()

	// Per-peer rate limiting to resist relay spam. Keyed by the envelope's
	// claimed FromAgentID -- cheap and bounded regardless of whether that
	// claim turns out to be genuine, so a flood of bogus envelopes naming
	// one peer can't force this node to pay full signature-verification
	// cost (below) for each one.
	if n.InboundLimiter != nil && !n.InboundLimiter.Allow(env.FromAgentID, now) {
		if n.Logf != nil {
			n.Logf("drop: rate-limited from_agent=%s", env.FromAgentID)
		}
		return
	}

	// Verify the envelope's signature -- and therefore that env.FromAgentID
	// really is who sent this, and that env.HardwareIDHash is a value that
	// agent's own private key actually signed -- before trusting either
	// field for anything else below, including the hardware-hash
	// consistency check that used to run first. env.FromAgentID only has
	// to NAME an already-known peer to reach this point (the n.Peers
	// lookup above), not prove control of it, so any sender relayed to
	// this node could previously send one incorrectly-signed envelope
	// claiming to be a specific known peer, with a HardwareIDHash of the
	// attacker's choosing. If that arrived before the real peer's first
	// genuine message, this node silently pinned the attacker's fake hash
	// as that peer's expected hardware hash -- and every subsequent
	// GENUINE, correctly-signed message from the real peer would then be
	// dropped as a "hardware hash changed" clone: a durable,
	// unauthenticated denial of service against any peer an attacker got
	// to first, requiring no valid signature at all.
	if err := n.Agent.VerifyEnvelope(env, pub, now); err != nil {
		if n.Logf != nil {
			n.Logf("drop: envelope verify failed from_agent=%s err=%v", env.FromAgentID, err)
		}
		return
	}
	// alreadyVerified is passed to VerifyAndUnmarshalEnvelope below in
	// place of n.Agent.VerifyEnvelope, so each message type's payload can
	// still be decoded through that same helper without re-running
	// signature verification a second time. Re-verifying isn't just
	// redundant here: Agent.VerifyEnvelope's replay cache is
	// test-and-set (ReplayCache.SeenBefore), so a second call for the
	// exact same envelope would look identical to an actual replay and
	// incorrectly drop this message.
	alreadyVerified := func(network.Envelope, []byte, time.Time) error { return nil }

	// Hardware-hash consistency check (clone/key-copy detection). Safe to
	// trust env.FromAgentID/env.HardwareIDHash now that the envelope's
	// signature has been verified above. The read-then-maybe-write below
	// is done under a single write lock (not a read lock upgraded later)
	// so two goroutines can't both observe "not yet seen" for the same
	// never-before-seen agent and both proceed to pin it.
	n.peersMu.Lock()
	prev, exists := n.PeerHardwareHash[env.FromAgentID]
	if !exists {
		n.PeerHardwareHash[env.FromAgentID] = env.HardwareIDHash
	}
	n.peersMu.Unlock()
	if exists && prev != env.HardwareIDHash {
		if n.Logf != nil {
			n.Logf("drop: hardware hash changed from_agent=%s prev=%s now=%s", env.FromAgentID, prev, env.HardwareIDHash)
		}
		return
	}

	if env.MessageType == network.MessageCoverNoise {
		if _, err := consensus.VerifyAndUnmarshalEnvelope[network.CoverNoisePayload](alreadyVerified, env, pub, now); err != nil {
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
		// Only validators may propose blocks. This is a cheap early reject
		// keyed to the envelope's already-verified sender; HandleProposal
		// below independently re-derives the same fact from the block's
		// own signature, since the HTTP consensus path (rpc.Server's
		// /consensus/propose) has no equivalent pre-verified envelope
		// sender to check here.
		if !n.IsValidator(env.FromAgentID) {
			if n.Logf != nil {
				n.Logf("drop: non-validator proposal from_agent=%s", env.FromAgentID)
			}
			return
		}
		prop, err := consensus.VerifyAndUnmarshalEnvelope[consensus.BlockProposal](alreadyVerified, env, pub, now)
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
		vote, err := n.HandleProposal(&b)
		if err != nil {
			if n.Logf != nil {
				n.Logf("warn: proposal handling failed hash=%s from_agent=%s err=%v", b.Hash, env.FromAgentID, err)
			}
			return
		}
		envOut, err := n.Agent.BuildEnvelope("block_vote", "", consensus.TopicVotes, vote)
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
		v, err := consensus.VerifyAndUnmarshalEnvelope[consensus.BlockVote](alreadyVerified, env, pub, now)
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
		finalized, _ := n.HandleVote(v)
		if finalized {
			_ = n.TryFinalize(v.BlockHash)
		}

	case network.MessageCommunicator:
		// Communicator role (docs/AGENTS_SPECIFICATION.md role 4):
		// handle_incoming_message. Everything above this switch already
		// verified the sender is a known peer, verified the envelope's
		// signature, checked their hardware-hash consistency, and
		// rate-limited them, so this only needs to decode the payload
		// (alreadyVerified, not a real re-check -- see its doc comment
		// above) before recording the message.
		payload, err := consensus.VerifyAndUnmarshalEnvelope[network.CommunicatorPayload](alreadyVerified, env, pub, now)
		if err != nil {
			if n.Logf != nil {
				n.Logf("drop: bad communicator payload from_agent=%s err=%v", env.FromAgentID, err)
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

// HandleProposal validates an incoming block proposal and, if this node is
// itself a registered validator, returns the vote it casts on it. Trust in
// b.Header.ProposerID comes entirely from Chain.ValidateBlock's own
// cryptographic check of b.ProposerSignature against that ID's registered
// validator key (see chain.Chain.verifyBlockAuthorizationLocked) -- this
// method never trusts a caller-supplied identity, so it's safe to call from
// a transport with no envelope-level sender verification of its own.
//
// Shared by two callers: handleRaw's "block_proposal" case (the TCP gossip
// path, which additionally checks an already-verified envelope sender first
// as a cheap early reject before decoding) and rpc.Server's
// /consensus/propose HTTP handler (the real transport actually wired up in
// production -- see cmd/synthosd/main.go's startBlockProducer), which has
// no equivalent pre-verified sender and relies on this check alone.
func (n *Node) HandleProposal(b *chain.Block) (consensus.BlockVote, error) {
	if b == nil {
		return consensus.BlockVote{}, errors.New("missing block")
	}
	if !n.IsValidator(b.Header.ProposerID) {
		return consensus.BlockVote{}, fmt.Errorf("proposer %q is not a registered validator", b.Header.ProposerID)
	}
	// Height sanity: proposal must extend our current tip. Checked before
	// the more expensive full ValidateBlock call below, same ordering
	// rationale as the TCP path.
	expectedHeight := n.Chain.Height() + 1
	if b.Header.Height != expectedHeight {
		return consensus.BlockVote{}, fmt.Errorf("proposal height mismatch: got %d want %d", b.Header.Height, expectedHeight)
	}
	// Basic chain validation, including the cryptographic proposer-signature
	// check (verifyBlockAuthorizationLocked) that's what actually earns
	// b.Header.ProposerID our trust. ValidateProposal (not ValidateBlock)
	// is deliberately used here: this is a bare candidate nobody has voted
	// on yet, so it cannot possibly carry quorum-of-approvals signatures --
	// requiring them here would make it impossible for any validator to
	// ever validate-then-vote on a fresh proposal. The quorum check still
	// runs for real at finalization (TryFinalize -> Chain.FinalizeBlock),
	// which is the point that actually matters.
	//
	// A failure here is real, independently-checked proof that a known
	// validator proposed a genuinely invalid block (or that ProposerID's
	// signature doesn't check out at all), not a bare accusation -- this is
	// the Enforcer wiring: every validator that receives a bad proposal
	// independently detects and records it, the same way double-signing and
	// equivocation already do via OnProposal/OnVote.
	if err := n.Chain.ValidateProposal(b); err != nil {
		if n.Slashing != nil {
			_ = n.Slashing.RecordInvalidBlock(b.Header.ProposerID, b.Header.Height, err.Error())
		}
		return consensus.BlockVote{}, fmt.Errorf("invalid proposal: %w", err)
	}
	n.Consensus.OnProposal(b)

	// Vote independently (validators only) -- ValidateBlock already
	// succeeded above, so this is always an approval; a node that
	// disagreed would have already returned the error above instead of
	// reaching here.
	if !n.IsValidator(n.Agent.Identity.AgentID) {
		return consensus.BlockVote{}, errors.New("this node is not a validator, cannot vote")
	}
	v := consensus.BlockVote{
		BlockHash: b.Hash,
		Height:    b.Header.Height,
		VoterID:   n.Agent.Identity.AgentID,
		Vote:      1,
	}
	sig, err := n.Agent.SignRaw(b.QuorumApprovalMessage())
	if err != nil {
		return consensus.BlockVote{}, fmt.Errorf("signing vote: %w", err)
	}
	v.Signature = "0x" + hex.EncodeToString(sig)
	return v, nil
}

// HandleVote records a peer's vote for a proposal this node already knows
// about (via BuildAndSignProposal or HandleProposal, both of which call
// Consensus.OnProposal) and reports whether that pushes the tally to real
// quorum. It does not itself finalize -- callers decide what to do with a
// true result (see TryFinalize).
func (n *Node) HandleVote(v consensus.BlockVote) (finalized bool, err error) {
	finalized, _, _, err = n.Consensus.OnVote(v)
	return finalized, err
}

// PeerPublicKey returns the registered public key for a known peer/
// validator agentID, safe for concurrent use (unlike reading the Peers
// field directly -- see HasPeer's doc comment for why that matters).
func (n *Node) PeerPublicKey(agentID string) ([]byte, bool) {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()
	pub, ok := n.Peers[agentID]
	return pub, ok
}

// BuildAndSignProposal builds a new candidate block extending this node's
// current tip, signs it as proposer, and records it in this node's local
// Consensus engine as the candidate for its height -- but does NOT
// broadcast it, vote on it, or finalize it. It exists for orchestrating a
// real multi-party consensus round over HTTP (see rpc.Server.
// ProposeBlockWithConsensus), where finalization must wait for
// independently verified peer votes rather than happening unconditionally
// the way ProposeBlock/ProposeBlockHash below do.
func (n *Node) BuildAndSignProposal() (*chain.Block, error) {
	if !n.IsValidator(n.Agent.Identity.AgentID) {
		return nil, errors.New("not a validator")
	}
	b, err := n.Chain.BuildBlock(n.Agent.Identity.AgentID, n.Agent.ProofRoot(), 1000)
	if err != nil {
		return nil, err
	}
	if err := n.signProposalBlock(b); err != nil {
		return nil, err
	}
	// RecordOwnProposal, not OnProposal: this is our own producer retrying
	// an HTTP-consensus round that hasn't reached quorum yet, not an
	// externally-observed proposal. See RecordOwnProposal's doc comment for
	// why that distinction matters -- using OnProposal here caused both a
	// self-vote livelock and repeated false double-sign self-slashing on
	// every retry.
	n.Consensus.RecordOwnProposal(b)
	return b, nil
}

// SelfVote casts and records this node's own approval vote for a block it
// just proposed via BuildAndSignProposal, without broadcasting it anywhere
// -- the caller (rpc.Server.ProposeBlockWithConsensus) is responsible for
// that as part of the real quorum tally it's assembling.
func (n *Node) SelfVote(b *chain.Block) (consensus.BlockVote, error) {
	v := consensus.BlockVote{
		BlockHash: b.Hash,
		Height:    b.Header.Height,
		VoterID:   n.Agent.Identity.AgentID,
		Vote:      1,
	}
	sig, err := n.Agent.SignRaw(b.QuorumApprovalMessage())
	if err != nil {
		return consensus.BlockVote{}, err
	}
	v.Signature = "0x" + hex.EncodeToString(sig)
	if _, _, _, err := n.Consensus.OnVote(v); err != nil {
		return consensus.BlockVote{}, err
	}
	return v, nil
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
