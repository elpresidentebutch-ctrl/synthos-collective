package chain

import (
	"testing"
)

// TestChain_FinalizeBlock_NeedsIrregularCorrectionToReplayRealSelfSlash
// reproduces the real, live incident SetIrregularStateCorrections shipped
// for: internal/node.NewNode's SlashingTracker.SetExecuteSlash callback
// mutates chain.State directly, synchronously, outside the deterministic
// block-apply path every other validator also runs (see
// chain.ErrStateRootMismatch's doc comment and the "Live incident" note on
// validateBlockLocked's state-root-mismatch handling). Before the downtime
// instance of that bug class was fixed, synthos-rpc (agent ID
// synthos-render-validator-1) was genuinely falling behind in real time,
// and both validator-12 and validator-13 independently real-slashed
// synthos-rpc's OWN account by the tracker's DowntimePenalty (50 at the
// time), clamped to zero since the account's balance was already zero --
// a real, net-zero-supply, already-quorum-agreed change that both
// validator-12 and validator-13 finalized every subsequent block on top
// of, live on synthos-mainnet-1 at height 41950. No amount of replaying
// this chain's real transaction history can ever reproduce that change on
// its own, since it was never a transaction: a node doing a genuine
// from-genesis resync (synthos-rpc's situation the night this was found)
// permanently stalls at that exact height without this. (A first attempt
// at this fix targeted validator-12's own address instead -- a plausible
// but wrong guess, since it's this chain's sole block producer; the real
// target, synthos-rpc's own address, was confirmed by brute-forcing every
// known validator/treasury address against the block's real declared
// root. This test itself uses a generic placeholder address/amount, since
// the state-transition logic being tested doesn't depend on which real
// address or amount was involved.)
func TestChain_FinalizeBlock_NeedsIrregularCorrectionToReplayRealSelfSlash(t *testing.T) {
	const incidentHeight = 2
	const debit = 250
	const target = Address("0xtarget-validator")

	newChain := func() *Chain {
		g := Genesis{ChainID: "test-chain", Alloc: map[Address]uint64{"0xgenesis": 1}}
		c, err := NewChain(g)
		if err != nil {
			t.Fatal(err)
		}
		// Enforce the state-root check from height 1 onward -- the
		// worst case for reproducing the bug, and the case a genuine
		// from-genesis resync actually hits.
		c.SetStateRootEnforceFromHeight(0)
		return c
	}

	buildEmptyBlock := func(c *Chain, height uint64, declaredRoot string) *Block {
		tip := c.Tip()
		b := &Block{
			Header: BlockHeader{
				Height:       height,
				ParentHash:   tip.Hash,
				ProposerID:   "synthos-validator-12",
				TxMerkleRoot: EmptyTxMerkleRoot,
				StateRoot:    declaredRoot,
			},
			Tx: []Tx{},
		}
		if _, err := b.ComputeHash(); err != nil {
			t.Fatal(err)
		}
		return b
	}

	// The real, already-agreed declared root for the incident block: state
	// after clamped-to-zero debiting target by `debit`, computed the exact
	// same way consensus.SlashingTracker's real penalty execution does.
	computeRealDeclaredRoot := func(c *Chain) string {
		tmp := c.State.Clone()
		acc := tmp.Get(target)
		if acc.Balance > debit {
			acc.Balance -= debit
		} else {
			acc.Balance = 0
		}
		tmp.Set(target, acc)
		return tmp.Root()
	}

	t.Run("fails without the correction, reproducing synthos-rpc's real freeze", func(t *testing.T) {
		c := newChain()
		// Height 1: ordinary block, no irregularity yet -- must finalize
		// normally regardless of the fix.
		ordinary := buildEmptyBlock(c, 1, c.Tip().Header.StateRoot)
		if err := c.FinalizeBlock(ordinary); err != nil {
			t.Fatalf("expected ordinary block to finalize, got: %v", err)
		}

		declaredRoot := computeRealDeclaredRoot(c)
		incident := buildEmptyBlock(c, incidentHeight, declaredRoot)
		if err := c.FinalizeBlock(incident); err == nil {
			t.Fatal("expected a fresh node with no IrregularStateCorrections configured to reject the real, already-agreed incident block -- reproducing synthos-rpc's permanent freeze at height 41950")
		}
	})

	t.Run("succeeds with the correction configured, and keeps enforcing afterward", func(t *testing.T) {
		c := newChain()
		c.SetIrregularStateCorrections([]StateCorrection{
			{Height: incidentHeight, Address: target, Debit: debit},
		})

		ordinary := buildEmptyBlock(c, 1, c.Tip().Header.StateRoot)
		if err := c.FinalizeBlock(ordinary); err != nil {
			t.Fatalf("expected ordinary block to finalize, got: %v", err)
		}

		declaredRoot := computeRealDeclaredRoot(c)
		incident := buildEmptyBlock(c, incidentHeight, declaredRoot)
		if err := c.FinalizeBlock(incident); err != nil {
			t.Fatalf("expected the incident block to finalize once the real correction is configured, got: %v", err)
		}
		if c.Height() != incidentHeight {
			t.Fatalf("expected height %d after replaying the incident block, got %d", incidentHeight, c.Height())
		}
		if bal := c.State.Get(target).Balance; bal != 0 {
			t.Fatalf("expected target's balance to be clamped to 0 after the correction, got %d", bal)
		}

		// Enforcement must still be real going forward: a later block
		// with a wrong declared root is still rejected, proving this
		// isn't a blanket "stop checking" -- only the one historical
		// height it's configured for is corrected.
		bogus := buildEmptyBlock(c, incidentHeight+1, "0xnot-the-real-root")
		if err := c.FinalizeBlock(bogus); err == nil {
			t.Fatal("expected a later block with a genuinely wrong state root to still be rejected -- the correction must not disable future verification")
		}

		// ...but a correctly-computed later block still finalizes fine.
		correct := buildEmptyBlock(c, incidentHeight+1, c.State.Root())
		if err := c.FinalizeBlock(correct); err != nil {
			t.Fatalf("expected a correctly-computed later block to finalize, got: %v", err)
		}
	})

	t.Run("only applies at the configured height, not before or after", func(t *testing.T) {
		c := newChain()
		c.SetIrregularStateCorrections([]StateCorrection{
			{Height: incidentHeight, Address: target, Debit: debit},
		})
		// Height 1 (before the configured height) must be entirely
		// unaffected: an ordinary block computed against the untouched
		// state must still finalize normally.
		ordinary := buildEmptyBlock(c, 1, c.Tip().Header.StateRoot)
		if err := c.FinalizeBlock(ordinary); err != nil {
			t.Fatalf("expected the correction to leave height 1 untouched, got: %v", err)
		}
		if bal := c.State.Get(target).Balance; bal != 0 {
			t.Fatalf("expected target's balance to remain untouched (it never had a balance) before the configured height, got %d", bal)
		}
	})
}
