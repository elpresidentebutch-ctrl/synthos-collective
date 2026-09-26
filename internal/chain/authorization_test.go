package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func mustGenerateKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func signHex(priv ed25519.PrivateKey, msg []byte) string {
	return "0x" + hex.EncodeToString(ed25519.Sign(priv, msg))
}

// TestChain_FinalizeBlock_RejectsUnsignedBlockOnceValidatorSetConfigured
// reproduces the original vulnerability directly: once a validator set is
// configured, an attacker with no validator key at all must not be able to
// get an arbitrary block finalized just by calling FinalizeBlock/gossiping
// it in (see internal/rpc/server.go's old applyPeerBlock).
func TestChain_FinalizeBlock_RejectsUnsignedBlockOnceValidatorSetConfigured(t *testing.T) {
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub}, 1)

	tip := c.Tip()
	b := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "attacker",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	if err := c.FinalizeBlock(b); err == nil {
		t.Fatal("expected FinalizeBlock to reject a block from an unregistered, unsigned proposer once a validator set is configured")
	}
}

// TestChain_FinalizeBlock_RejectsForgedProposerID covers a block that
// claims to be from a real, registered validator (Header.ProposerID) but
// was actually signed by a different key entirely -- exactly the shape of
// the original bug, where nothing checked that the claimed proposer and the
// real author were the same identity.
func TestChain_FinalizeBlock_RejectsForgedProposerID(t *testing.T) {
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	realPub, _ := mustGenerateKey(t)
	_, attackerPriv := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": realPub}, 1)

	tip := c.Tip()
	b := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	b.ProposerSignature = signHex(attackerPriv, []byte(b.Hash))
	b.QuorumSignatures = map[string]string{"validator-1": signHex(attackerPriv, b.QuorumApprovalMessage())}

	if err := c.FinalizeBlock(b); err == nil {
		t.Fatal("expected FinalizeBlock to reject a block forged under a validator's name but signed with a different key")
	}
}

// TestChain_FinalizeBlock_AcceptsProperlySignedQuorumApprovedBlock is the
// positive case: a block signed by its real proposer, with valid approval
// signatures from enough distinct registered validators to meet quorum,
// must still finalize normally.
func TestChain_FinalizeBlock_AcceptsProperlySignedQuorumApprovedBlock(t *testing.T) {
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	pub1, priv1 := mustGenerateKey(t)
	pub2, priv2 := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub1, "validator-2": pub2}, 2)

	tip := c.Tip()
	b := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	b.ProposerSignature = signHex(priv1, []byte(b.Hash))
	approval := b.QuorumApprovalMessage()
	b.QuorumSignatures = map[string]string{
		"validator-1": signHex(priv1, approval),
		"validator-2": signHex(priv2, approval),
	}

	if err := c.FinalizeBlock(b); err != nil {
		t.Fatalf("expected a properly signed, quorum-approved block to finalize, got: %v", err)
	}
	if c.Height() != 1 {
		t.Fatalf("expected chain height 1, got %d", c.Height())
	}
}

// TestChain_FinalizeBlock_RejectsBelowQuorumThreshold checks that a single
// validator's honest signature is not, by itself, enough when the
// configured quorum requires more than one.
func TestChain_FinalizeBlock_RejectsBelowQuorumThreshold(t *testing.T) {
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	pub1, priv1 := mustGenerateKey(t)
	pub2, _ := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub1, "validator-2": pub2}, 2)

	tip := c.Tip()
	b := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	b.ProposerSignature = signHex(priv1, []byte(b.Hash))
	b.QuorumSignatures = map[string]string{
		"validator-1": signHex(priv1, b.QuorumApprovalMessage()),
	}

	if err := c.FinalizeBlock(b); err == nil {
		t.Fatal("expected FinalizeBlock to reject a block with fewer approvals than the configured quorum")
	}
}

// TestChain_TryReorg_SwitchesToHeavierFullyValidBranch is the positive
// fork-choice case: a genuinely more-approved, fully valid competing block
// for an already-finalized height replaces it.
func TestChain_TryReorg_SwitchesToHeavierFullyValidBranch(t *testing.T) {
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	pub1, priv1 := mustGenerateKey(t)
	pub2, priv2 := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub1, "validator-2": pub2}, 1)

	tip := c.Tip()
	weak := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := weak.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	weak.ProposerSignature = signHex(priv1, []byte(weak.Hash))
	weak.QuorumSignatures = map[string]string{"validator-1": signHex(priv1, weak.QuorumApprovalMessage())}
	if err := c.FinalizeBlock(weak); err != nil {
		t.Fatalf("failed to finalize initial block: %v", err)
	}

	heavy := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-2",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := heavy.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	heavy.ProposerSignature = signHex(priv2, []byte(heavy.Hash))
	happroval := heavy.QuorumApprovalMessage()
	heavy.QuorumSignatures = map[string]string{
		"validator-1": signHex(priv1, happroval),
		"validator-2": signHex(priv2, happroval),
	}

	reorged, err := c.TryReorg(1, []*Block{heavy})
	if err != nil {
		t.Fatalf("TryReorg returned error: %v", err)
	}
	if !reorged {
		t.Fatal("expected TryReorg to switch to the strictly heavier, fully valid competing block")
	}
	if got := c.Tip().Hash; got != heavy.Hash {
		t.Fatalf("expected chain tip to be the heavier block %q, got %q", heavy.Hash, got)
	}
}

// TestChain_TryReorg_RejectsWeakerOrInvalidBranch is the negative
// fork-choice case: a forged, unauthenticated competing block can never
// displace an already-finalized, fully-approved one.
func TestChain_TryReorg_RejectsWeakerOrInvalidBranch(t *testing.T) {
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	c, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	pub1, priv1 := mustGenerateKey(t)
	pub2, priv2 := mustGenerateKey(t)
	c.SetValidatorSet(map[string]ed25519.PublicKey{"validator-1": pub1, "validator-2": pub2}, 1)

	tip := c.Tip()
	strong := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-1",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := strong.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	strong.ProposerSignature = signHex(priv1, []byte(strong.Hash))
	sapproval := strong.QuorumApprovalMessage()
	strong.QuorumSignatures = map[string]string{
		"validator-1": signHex(priv1, sapproval),
		"validator-2": signHex(priv2, sapproval),
	}
	if err := c.FinalizeBlock(strong); err != nil {
		t.Fatalf("failed to finalize initial block: %v", err)
	}

	_, attackerPriv := mustGenerateKey(t)
	forged := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "validator-2",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := forged.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	forged.ProposerSignature = signHex(attackerPriv, []byte(forged.Hash))
	forged.QuorumSignatures = map[string]string{"validator-2": signHex(attackerPriv, forged.QuorumApprovalMessage())}

	reorged, _ := c.TryReorg(1, []*Block{forged})
	if reorged {
		t.Fatal("expected TryReorg to refuse a forged, unauthenticated competing block")
	}
	if c.Tip().Hash != strong.Hash {
		t.Fatalf("chain tip must remain the originally finalized block; got %q", c.Tip().Hash)
	}
}

// TestChain_FinalizeBlock_FollowerMustTrustProducersKeyToCatchUp reproduces
// the exact production incident that froze synthos-validator-13 and
// synthos-rpc permanently at a fixed height while synthos-validator-12 (the
// sole block producer) kept advancing: a follower configured with only its
// OWN key in its validator set (cfg.Validators falling back to []string{self}
// with no cfg.TrustedValidators/cfg.PeerKeys set, exactly as
// config/render-validator-13.json and config/render-node.json shipped)
// rejects every properly-signed, properly-quorum-approved block from the
// real producer with "proposer ... is not a registered validator" -- forever,
// since the block is genuinely valid and nothing about retrying changes that.
// Registering the producer's key too (what cfg.TrustedValidators/PeerKeys is
// for -- see cmd/synthosd/main.go's buildValidatorKeySet) fixes it.
func TestChain_FinalizeBlock_FollowerMustTrustProducersKeyToCatchUp(t *testing.T) {
	producerPub, producerPriv := mustGenerateKey(t)
	followerPub, _ := mustGenerateKey(t)

	// Build the real producer chain (mirrors synthos-validator-12: a
	// validator set of just itself, quorum 1) and let it actually finalize a
	// block the normal way, so the block under test carries genuine
	// signatures produced by the real signing/quorum-approval code path, not
	// hand-waved test fixtures.
	g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
	producerChain, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	producerChain.SetValidatorSet(map[string]ed25519.PublicKey{"synthos-validator-12": producerPub}, 1)

	tip := producerChain.Tip()
	b := &Block{
		Header: BlockHeader{
			Height:       1,
			ParentHash:   tip.Hash,
			ProposerID:   "synthos-validator-12",
			TxMerkleRoot: EmptyTxMerkleRoot,
			StateRoot:    tip.Header.StateRoot,
		},
		Tx: []Tx{},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	b.ProposerSignature = signHex(producerPriv, []byte(b.Hash))
	b.QuorumSignatures = map[string]string{
		"synthos-validator-12": signHex(producerPriv, b.QuorumApprovalMessage()),
	}
	if err := producerChain.FinalizeBlock(b); err != nil {
		t.Fatalf("producer failed to finalize its own properly signed block: %v", err)
	}

	// Broken (pre-fix) follower: validator set contains only its own key,
	// same as render-validator-13.json/render-node.json before
	// trusted_validators/peer_keys were added. Catch-up must reject the
	// producer's genuinely valid block.
	brokenFollower, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	brokenFollower.SetValidatorSet(map[string]ed25519.PublicKey{"synthos-validator-13": followerPub}, 1)
	if err := brokenFollower.FinalizeBlock(b); err == nil {
		t.Fatal("expected an un-fixed follower (trusting only its own key) to reject the producer's block, reproducing the permanent-freeze bug")
	}

	// Fixed follower: validator set contains both its own key AND the
	// producer's key, matching what render-validator-13.json/render-node.json
	// now configure via trusted_validators + peer_keys. Catch-up must
	// succeed.
	fixedFollower, err := NewChain(g)
	if err != nil {
		t.Fatal(err)
	}
	fixedFollower.SetValidatorSet(map[string]ed25519.PublicKey{
		"synthos-validator-13": followerPub,
		"synthos-validator-12": producerPub,
	}, 1)
	if err := fixedFollower.FinalizeBlock(b); err != nil {
		t.Fatalf("expected a fixed follower (also trusting the producer's key) to accept the producer's properly signed block, got: %v", err)
	}
	if fixedFollower.Height() != 1 {
		t.Fatalf("expected fixed follower chain height 1, got %d", fixedFollower.Height())
	}
}

// TestChain_FinalizeBlock_FreshNodeNeedsAuthEnforceFromHeightToReplayPreSigningHistory
// reproduces a second, independent production incident found while
// investigating the validator-13/rpc freeze above: synthos-validator-14, a
// long-suspended node with no local chain data, was resumed and immediately
// froze at height 11 trying to catch up from genesis -- even after it was
// correctly configured to trust synthos-validator-12's key. Live chain data
// confirmed this chain has real, permanently unsigned production history:
// every block from height 1 through 15264 has no ProposerSignature/
// QuorumSignatures at all (block-signing was only turned on starting at
// height 15265, the first signed block, confirmed live). A node that
// already has that old history loaded from its own persisted snapshot never
// re-validates it (see chain.go's snapshot-load path), so this never
// affected validator-13/rpc's boot. But a node replaying the ENTIRE chain
// from genesis via HTTP catch-up (a brand new node, or one recovering from
// lost data) re-validates every single block through FinalizeBlock, and
// once a validator set is configured (as it must be, to authenticate the
// signed blocks from height 15265 on), validateBlockLocked has no way to
// know those early unsigned blocks are legitimate pre-signing history
// rather than a forgery -- it rejects them exactly like the unsigned-block
// attack in TestChain_FinalizeBlock_RejectsUnsignedBlockOnceValidatorSetConfigured
// above, permanently blocking replay past height 1. AuthEnforceFromHeight
// exists for exactly this: grandfather in real pre-signing history below the
// height signing actually started at, while still requiring valid
// signatures on and after it.
//
// A THIRD incident (synthos-rpc's own from-genesis resync, the same night)
// showed AuthEnforceFromHeight needed to grandfather much more than "the
// first signed block." The initial fix moved it from 15265 to 15266,
// on the theory that height 15265 -- the very first block produced once
// signing was turned on -- was a single transitional block that only ever
// picked up its proposer's own signature before the real multi-party
// quorum flow was fully live. That theory was wrong: once synthos-rpc's
// resync got past 15265 it immediately hit the identical "only 1 of 2
// required validator approvals verified" rejection at 15266, and live
// chain data confirmed the real quorum flow (2+ genuine validator
// signatures per block, not just the proposer's own) did not start
// producing consistently until height 33908 -- every block from 15265
// through 33907 (about 18,600 blocks) carries only the proposer's own
// signature. Every node that already had that range in memory from
// before never re-checked it, so nobody noticed until a fresh resync
// independently re-verified it from genesis. Production's
// AuthEnforceFromHeight was moved from 15266 to 33908 to grandfather
// that entire range in -- it's genuine legitimate history, just never
// fully quorum-signed, exactly like everything below it.
func TestChain_FinalizeBlock_FreshNodeNeedsAuthEnforceFromHeightToReplayPreSigningHistory(t *testing.T) {
	producerPub, producerPriv := mustGenerateKey(t)
	const signingStartHeight = 3 // stands in for production's real height 15265

	buildUnsignedHistoryBlock := func(c *Chain, height uint64) *Block {
		tip := c.Tip()
		b := &Block{
			Header: BlockHeader{
				Height:       height,
				ParentHash:   tip.Hash,
				ProposerID:   "synthos-validator-11", // the old, pre-signing-era proposer, exactly as seen live
				TxMerkleRoot: EmptyTxMerkleRoot,
				StateRoot:    tip.Header.StateRoot,
			},
			Tx: []Tx{},
		}
		if _, err := b.ComputeHash(); err != nil {
			t.Fatal(err)
		}
		return b // deliberately no ProposerSignature/QuorumSignatures: matches real pre-signing blocks
	}

	newChainWithValidatorSet := func() *Chain {
		g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
		c, err := NewChain(g)
		if err != nil {
			t.Fatal(err)
		}
		c.SetValidatorSet(map[string]ed25519.PublicKey{"synthos-validator-12": producerPub}, 1)
		return c
	}

	// Without AuthEnforceFromHeight (the state every config shipped in before
	// this fix): a fresh node can't even replay block 1 of real history.
	t.Run("fails without grandfathering", func(t *testing.T) {
		c := newChainWithValidatorSet()
		unsigned1 := buildUnsignedHistoryBlock(c, 1)
		if err := c.FinalizeBlock(unsigned1); err == nil {
			t.Fatal("expected a fresh node with no AuthEnforceFromHeight set to reject real, unsigned pre-signing history, reproducing the validator-14 freeze")
		}
	})

	// With AuthEnforceFromHeight set to the real signing-start height: the
	// same unsigned history replays cleanly, and enforcement still kicks in
	// exactly at that height, rejecting an unsigned block there.
	t.Run("succeeds with grandfathering, still enforces from the configured height", func(t *testing.T) {
		c := newChainWithValidatorSet()
		c.SetAuthEnforceFromHeight(signingStartHeight)

		for h := uint64(1); h < signingStartHeight; h++ {
			unsigned := buildUnsignedHistoryBlock(c, h)
			if err := c.FinalizeBlock(unsigned); err != nil {
				t.Fatalf("expected grandfathered unsigned block at height %d to finalize, got: %v", h, err)
			}
		}
		if c.Height() != signingStartHeight-1 {
			t.Fatalf("expected height %d after replaying grandfathered history, got %d", signingStartHeight-1, c.Height())
		}

		// An unsigned block AT the enforcement height must now be rejected...
		stillUnsigned := buildUnsignedHistoryBlock(c, signingStartHeight)
		if err := c.FinalizeBlock(stillUnsigned); err == nil {
			t.Fatal("expected an unsigned block at/after AuthEnforceFromHeight to be rejected -- grandfathering must not extend past the configured height")
		}

		// ...but a properly signed one at that same height finalizes normally.
		tip := c.Tip()
		signed := &Block{
			Header: BlockHeader{
				Height:       signingStartHeight,
				ParentHash:   tip.Hash,
				ProposerID:   "synthos-validator-12",
				TxMerkleRoot: EmptyTxMerkleRoot,
				StateRoot:    tip.Header.StateRoot,
			},
			Tx: []Tx{},
		}
		if _, err := signed.ComputeHash(); err != nil {
			t.Fatal(err)
		}
		signed.ProposerSignature = signHex(producerPriv, []byte(signed.Hash))
		signed.QuorumSignatures = map[string]string{
			"synthos-validator-12": signHex(producerPriv, signed.QuorumApprovalMessage()),
		}
		if err := c.FinalizeBlock(signed); err != nil {
			t.Fatalf("expected a properly signed block at the enforcement height to finalize, got: %v", err)
		}
		if c.Height() != signingStartHeight {
			t.Fatalf("expected height %d, got %d", signingStartHeight, c.Height())
		}
	})
}
