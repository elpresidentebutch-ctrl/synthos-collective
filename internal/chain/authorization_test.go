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
