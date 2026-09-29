package chain

import "testing"

// These tests cover ReapplyGenesisCitizenRewardConfig: a chain restored from
// a storage snapshot whose persisted State lost the genesis Citizen reward
// config (TreasuryAddress/CitizenRewardRateBpsPerYear zeroed, as on the live
// validators) must end up agreeing with a chain built straight from genesis.

const (
	restoreTestTreasury = Address("0xtreasury0000000000000000000000000000000")
	restoreTestStaker   = Address("0xstaker00000000000000000000000000000000")
	restoreTestRate     = uint64(650)
)

func genesisWithCitizenRewards() Genesis {
	return Genesis{
		ChainID: "citizen-restore-test",
		Alloc: map[Address]uint64{
			restoreTestTreasury: 1_000_000,
			restoreTestStaker:   1_000_000,
		},
		Metadata: map[string]any{
			"treasury_address":                 string(restoreTestTreasury),
			"citizen_reward_rate_bps_per_year": float64(restoreTestRate),
		},
	}
}

// restoredChainWithZeroedRewardConfig mimics cmd/synthosd's snapshot-restore
// path for a node whose persisted state has the reward config wiped: it
// builds the Chain from a saved State (not from genesis) and a saved
// hot-window base state, both with the two fields zeroed.
func restoredChainWithZeroedRewardConfig(t *testing.T, fresh *Chain) *Chain {
	t.Helper()
	saved := fresh.State.Clone()
	saved.TreasuryAddress = ""
	saved.CitizenRewardRateBpsPerYear = 0
	savedBase := saved.Clone()

	c := &Chain{
		ChainID:   fresh.ChainID,
		TxChainID: fresh.TxChainID,
		State:     saved,
		DEX:       NewDEX(),
		Oracle:    NewOracle(),
		Blocks:    fresh.Blocks,
		Mempool:   make(map[string]Tx),
	}
	c.SeedGenesisState(fresh.State)
	c.RestoreHotWindow(0, savedBase, nil, 0, nil)
	return c
}

func TestReapplyGenesisCitizenRewardConfigRestoresZeroedSnapshot(t *testing.T) {
	gen := genesisWithCitizenRewards()
	fresh, err := NewChain(gen)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	if fresh.State.CitizenRewardRateBpsPerYear != restoreTestRate || fresh.State.TreasuryAddress != restoreTestTreasury {
		t.Fatalf("precondition: genesis chain should carry the reward config, got rate=%d treasury=%q",
			fresh.State.CitizenRewardRateBpsPerYear, fresh.State.TreasuryAddress)
	}

	restored := restoredChainWithZeroedRewardConfig(t, fresh)
	restored.ReapplyGenesisCitizenRewardConfig(fresh.State)

	for name, s := range map[string]*State{"live state": restored.State, "hot-window base state": restored.hotWindowBaseState} {
		if s.CitizenRewardRateBpsPerYear != restoreTestRate {
			t.Errorf("%s: CitizenRewardRateBpsPerYear = %d, want %d", name, s.CitizenRewardRateBpsPerYear, restoreTestRate)
		}
		if s.TreasuryAddress != restoreTestTreasury {
			t.Errorf("%s: TreasuryAddress = %q, want %q", name, s.TreasuryAddress, restoreTestTreasury)
		}
	}
}

// TestZeroedRewardConfigDivergesFromGenesisReplayUntilReapplied shows why the
// fix matters for consensus, not just display: the same claim transaction
// fails on a zeroed restored node but pays out on a from-genesis node, and
// payouts move balances that are part of Root(). After re-applying genesis
// config, both nodes produce the same payout and the same state root.
func TestZeroedRewardConfigDivergesFromGenesisReplayUntilReapplied(t *testing.T) {
	gen := genesisWithCitizenRewards()
	fresh, err := NewChain(gen)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	restored := restoredChainWithZeroedRewardConfig(t, fresh)

	const stakeAt, claimAt = int64(1_000), int64(1_000 + 365*24*60*60)
	stakeAndClaim := func(s *State) (uint64, error) {
		if err := s.StakeCitizen(restoreTestStaker, 100_000, stakeAt); err != nil {
			t.Fatalf("StakeCitizen: %v", err)
		}
		return s.ClaimCitizenRewards(restoreTestStaker, claimAt)
	}

	// Without the fix: the restored node rejects the claim the genesis node pays.
	if _, err := stakeAndClaim(restored.State.Clone()); err != ErrCitizenRewardsNotConfigured {
		t.Fatalf("zeroed restored state: expected ErrCitizenRewardsNotConfigured, got %v", err)
	}
	freshState := fresh.State.Clone()
	freshPaid, err := stakeAndClaim(freshState)
	if err != nil {
		t.Fatalf("genesis state claim: %v", err)
	}
	if freshPaid == 0 {
		t.Fatalf("genesis state should pay a nonzero reward after a year staked")
	}

	// With the fix: identical payout and identical resulting state root.
	restored.ReapplyGenesisCitizenRewardConfig(fresh.State)
	restoredState := restored.State.Clone()
	restoredPaid, err := stakeAndClaim(restoredState)
	if err != nil {
		t.Fatalf("restored state claim after reapply: %v", err)
	}
	if restoredPaid != freshPaid {
		t.Fatalf("payout mismatch after reapply: restored=%d genesis=%d", restoredPaid, freshPaid)
	}
	if restoredState.Root() != freshState.Root() {
		t.Fatalf("state root mismatch after reapply: restored=%s genesis=%s", restoredState.Root(), freshState.Root())
	}
}

func TestReapplyGenesisCitizenRewardConfigNilGenesisIsNoOp(t *testing.T) {
	fresh, err := NewChain(genesisWithCitizenRewards())
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	restored := restoredChainWithZeroedRewardConfig(t, fresh)
	restored.ReapplyGenesisCitizenRewardConfig(nil)
	if restored.State.CitizenRewardRateBpsPerYear != 0 || restored.State.TreasuryAddress != "" {
		t.Fatalf("nil genesis state must leave the restored state untouched")
	}
}
