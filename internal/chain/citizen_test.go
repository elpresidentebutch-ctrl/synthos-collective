package chain

import "testing"

func fundedStateForCitizenTests(t *testing.T) (*State, Address, Address) {
	t.Helper()
	staker := Address("0xstaker00000000000000000000000000000000")
	treasury := Address("0xtreasury0000000000000000000000000000000")
	s := NewState()
	s.Set(staker, Account{Balance: 1_000_000})
	s.Set(treasury, Account{Balance: 1_000_000})
	s.TreasuryAddress = treasury
	s.CitizenRewardRateBpsPerYear = 1000 // 10%/year
	return s, staker, treasury
}

func TestStakeCitizenMovesBalanceIntoStake(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	if err := s.StakeCitizen(staker, 100_000, 1000); err != nil {
		t.Fatalf("StakeCitizen: %v", err)
	}
	acc := s.Get(staker)
	if acc.Balance != 900_000 {
		t.Fatalf("balance after stake = %d, want 900000", acc.Balance)
	}
	stake := s.GetCitizenStake(staker)
	if stake.Amount != 100_000 {
		t.Fatalf("stake amount = %d, want 100000", stake.Amount)
	}
	if stake.StakedAt != 1000 || stake.LastClaimAt != 1000 {
		t.Fatalf("unexpected stake timestamps: %+v", stake)
	}
}

func TestStakeCitizenRejectsInsufficientBalance(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	if err := s.StakeCitizen(staker, 10_000_000, 1000); err != ErrInsufficientFunds {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	// Nothing should have moved.
	if s.GetCitizenStake(staker).Amount != 0 {
		t.Fatalf("stake should be untouched after a rejected stake")
	}
}

func TestStakeCitizenAccumulatesAcrossCalls(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	if err := s.StakeCitizen(staker, 100_000, 1000); err != nil {
		t.Fatalf("first stake: %v", err)
	}
	if err := s.StakeCitizen(staker, 50_000, 2000); err != nil {
		t.Fatalf("second stake: %v", err)
	}
	stake := s.GetCitizenStake(staker)
	if stake.Amount != 150_000 {
		t.Fatalf("stake amount after two stakes = %d, want 150000", stake.Amount)
	}
	// StakedAt should not move on a top-up; it marks when staking first began.
	if stake.StakedAt != 1000 {
		t.Fatalf("StakedAt should stay at first stake time, got %d", stake.StakedAt)
	}
}

func TestUnstakeCitizenReturnsBalance(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	if err := s.StakeCitizen(staker, 100_000, 1000); err != nil {
		t.Fatalf("stake: %v", err)
	}
	if err := s.UnstakeCitizen(staker, 40_000); err != nil {
		t.Fatalf("unstake: %v", err)
	}
	if s.Get(staker).Balance != 940_000 {
		t.Fatalf("balance after partial unstake = %d, want 940000", s.Get(staker).Balance)
	}
	if s.GetCitizenStake(staker).Amount != 60_000 {
		t.Fatalf("remaining stake = %d, want 60000", s.GetCitizenStake(staker).Amount)
	}
}

func TestUnstakeCitizenRejectsMoreThanStaked(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	if err := s.StakeCitizen(staker, 100_000, 1000); err != nil {
		t.Fatalf("stake: %v", err)
	}
	if err := s.UnstakeCitizen(staker, 200_000); err != ErrInsufficientStake {
		t.Fatalf("expected ErrInsufficientStake, got %v", err)
	}
}

func TestUnstakeCitizenRejectsWithNoStake(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	if err := s.UnstakeCitizen(staker, 1); err != ErrNoStake {
		t.Fatalf("expected ErrNoStake, got %v", err)
	}
}

func TestPendingCitizenRewardsAccruesLinearlyOverTime(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t) // 10%/year
	if err := s.StakeCitizen(staker, 1_000_000, 0); err != nil {
		t.Fatalf("stake: %v", err)
	}
	// Half a year at 10%/year on 1,000,000 staked = 50,000.
	halfYear := int64(SecondsPerYear / 2)
	pending := s.PendingCitizenRewards(staker, halfYear)
	if pending != 50_000 {
		t.Fatalf("pending rewards after half a year = %d, want 50000", pending)
	}
}

func TestPendingCitizenRewardsZeroWhenRateUnconfigured(t *testing.T) {
	s, staker, _ := fundedStateForCitizenTests(t)
	s.CitizenRewardRateBpsPerYear = 0
	if err := s.StakeCitizen(staker, 1_000_000, 0); err != nil {
		t.Fatalf("stake: %v", err)
	}
	if pending := s.PendingCitizenRewards(staker, int64(SecondsPerYear)); pending != 0 {
		t.Fatalf("expected 0 pending rewards with rate unconfigured, got %d", pending)
	}
}

func TestClaimCitizenRewardsPaysFromTreasury(t *testing.T) {
	s, staker, treasury := fundedStateForCitizenTests(t)
	if err := s.StakeCitizen(staker, 1_000_000, 0); err != nil {
		t.Fatalf("stake: %v", err)
	}
	oneYear := int64(SecondsPerYear)
	paid, err := s.ClaimCitizenRewards(staker, oneYear)
	if err != nil {
		t.Fatalf("ClaimCitizenRewards: %v", err)
	}
	if paid != 100_000 { // 10% of 1,000,000
		t.Fatalf("paid = %d, want 100000", paid)
	}
	if s.Get(staker).Balance != 100_000 {
		t.Fatalf("staker balance after claim = %d, want 100000 (rest is still staked)", s.Get(staker).Balance)
	}
	if s.Get(treasury).Balance != 900_000 {
		t.Fatalf("treasury balance after payout = %d, want 900000", s.Get(treasury).Balance)
	}
	// Claiming again immediately should pay nothing (no time has passed).
	paidAgain, err := s.ClaimCitizenRewards(staker, oneYear)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if paidAgain != 0 {
		t.Fatalf("immediate second claim paid = %d, want 0", paidAgain)
	}
}

func TestClaimCitizenRewardsCapsAtTreasuryBalance(t *testing.T) {
	s, staker, treasury := fundedStateForCitizenTests(t)
	// Drain the treasury down to less than what will accrue.
	s.Set(treasury, Account{Balance: 30_000})
	if err := s.StakeCitizen(staker, 1_000_000, 0); err != nil {
		t.Fatalf("stake: %v", err)
	}
	oneYear := int64(SecondsPerYear) // would accrue 100,000, but treasury only has 30,000
	paid, err := s.ClaimCitizenRewards(staker, oneYear)
	if err != nil {
		t.Fatalf("ClaimCitizenRewards: %v", err)
	}
	if paid != 30_000 {
		t.Fatalf("paid = %d, want 30000 (capped at treasury balance)", paid)
	}
	if s.Get(treasury).Balance != 0 {
		t.Fatalf("treasury should be fully drained, got %d", s.Get(treasury).Balance)
	}
	// The unpaid remainder should still be claimable later once the
	// treasury is topped back up, rather than being forfeited outright.
	s.Set(treasury, Account{Balance: 1_000_000})
	paidLater, err := s.ClaimCitizenRewards(staker, oneYear+1)
	if err != nil {
		t.Fatalf("later claim: %v", err)
	}
	if paidLater == 0 {
		t.Fatalf("expected the shortfall to still be claimable once treasury is refunded")
	}
}

func TestClaimCitizenRewardsErrorsWhenUnconfigured(t *testing.T) {
	s := NewState()
	staker := Address("0xstaker00000000000000000000000000000000")
	s.Set(staker, Account{Balance: 1_000_000})
	if err := s.StakeCitizen(staker, 500_000, 0); err != nil {
		t.Fatalf("stake: %v", err)
	}
	_, err := s.ClaimCitizenRewards(staker, int64(SecondsPerYear))
	if err != ErrCitizenRewardsNotConfigured {
		t.Fatalf("expected ErrCitizenRewardsNotConfigured, got %v", err)
	}
}

func TestGenesisParsesTreasuryAndRewardRateMetadata(t *testing.T) {
	gen := Genesis{
		ChainID: "test",
		Alloc:   map[Address]uint64{"0xabc": 1},
		Metadata: map[string]any{
			"treasury_address":                 "0xtreasury0000000000000000000000000000000",
			"citizen_reward_rate_bps_per_year": float64(250), // JSON numbers decode as float64
		},
	}
	s, err := gen.ToState()
	if err != nil {
		t.Fatalf("ToState: %v", err)
	}
	if s.TreasuryAddress != "0xtreasury0000000000000000000000000000000" {
		t.Fatalf("TreasuryAddress = %q", s.TreasuryAddress)
	}
	if s.CitizenRewardRateBpsPerYear != 250 {
		t.Fatalf("CitizenRewardRateBpsPerYear = %d, want 250", s.CitizenRewardRateBpsPerYear)
	}
}
