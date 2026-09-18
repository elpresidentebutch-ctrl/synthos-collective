package chain

import (
	"errors"
)

// -----------------------------------------------------------------------
// Citizen: stake_tokens / claim_rewards for the Citizen agent role
// (docs/AGENTS_SPECIFICATION.md, role 7). participate_in_voting is already
// covered by TreasuryGovernance.Vote (any address with a real balance can
// already vote -- see governance.go), submit_transaction by the ordinary
// ApplyTx path, and interact_with_contracts by the existing DEX/bridge RPC
// surface, so this file only adds what didn't already exist: a real,
// reward-bearing stake.
//
// Design, deliberately conservative:
//   - Staking here is separate from governance voting weight. Voting weight
//     stays exactly what it always was (a plain account balance, via
//     TreasuryGovernance.GetStake) -- staking coins for Citizen rewards does
//     NOT also increase or decrease anyone's vote weight. That is a real
//     product decision a deployment may want to change later; this file
//     does not make that call silently.
//   - Rewards are paid out of the same treasury address Governor spends
//     from (State.TreasuryAddress, set from genesis metadata
//     "treasury_address", the same value cmd/synthosd wires into
//     Node.Governance). There is no new token issuance/inflation anywhere
//     in this file.
//   - The reward rate is a genesis-configured constant
//     (State.CitizenRewardRateBpsPerYear, from genesis metadata
//     "citizen_reward_rate_bps_per_year"). It defaults to 0, so a
//     deployment that never sets it pays no rewards at all, the same way
//     Governor stays unconfigured until a founder/treasury address is set.
//   - A claim is capped at whatever the treasury can actually afford, and
//     never partially fails: it pays out min(accrued, treasury balance).
// -----------------------------------------------------------------------

// SecondsPerYear is the fixed divisor used to turn
// CitizenRewardRateBpsPerYear into a per-second accrual rate. A plain
// 365-day year, not adjusted for leap years -- precision to the day does
// not matter for an annualized basis-point reward rate.
const SecondsPerYear = 365 * 24 * 60 * 60

// CitizenStake is one address's current staked position.
type CitizenStake struct {
	Amount      uint64 `json:"amount"`
	StakedAt    int64  `json:"staked_at"`
	LastClaimAt int64  `json:"last_claim_at"`
}

var (
	ErrCitizenRewardsNotConfigured = errors.New("citizen staking rewards are not configured for this deployment (no treasury_address or citizen_reward_rate_bps_per_year in genesis metadata)")
	ErrNoStake                     = errors.New("address has no citizen stake")
	ErrInsufficientStake           = errors.New("unstake amount exceeds current stake")
)

// StakeCitizen locks `amount` of addr's spendable balance into a Citizen
// stake. now is a unix timestamp (tx.Timestamp in the RPC caller) used to
// start (or continue) reward accrual.
func (s *State) StakeCitizen(addr Address, amount uint64, now int64) error {
	if amount == 0 {
		return errors.New("stake amount must be positive")
	}
	s.mu.Lock()
	acc, ok := s.Accounts[addr]
	if !ok || acc.Balance < amount {
		s.mu.Unlock()
		return ErrInsufficientFunds
	}
	acc.Balance -= amount
	s.Accounts[addr] = acc

	if s.CitizenStakes == nil {
		s.CitizenStakes = make(map[Address]CitizenStake)
	}
	stake := s.CitizenStakes[addr]
	if stake.StakedAt == 0 {
		stake.StakedAt = now
		stake.LastClaimAt = now
	}
	next, err := safeAdd(stake.Amount, amount)
	if err != nil {
		// Roll back the balance debit above -- this is the one error path
		// that happens after the mutation, so it must be undone explicitly.
		acc.Balance += amount
		s.Accounts[addr] = acc
		s.mu.Unlock()
		return errors.New("stake amount overflow")
	}
	stake.Amount = next
	s.CitizenStakes[addr] = stake
	s.mu.Unlock()
	return nil
}

// UnstakeCitizen returns `amount` from addr's Citizen stake back to their
// spendable balance. Pending rewards are not auto-claimed by this call --
// call ClaimCitizenRewards first if that's wanted, same as any other
// two-step claim/withdraw pattern in this codebase (e.g. bridge
// lock/release).
func (s *State) UnstakeCitizen(addr Address, amount uint64) error {
	if amount == 0 {
		return errors.New("unstake amount must be positive")
	}
	s.mu.Lock()
	if s.CitizenStakes == nil {
		s.mu.Unlock()
		return ErrNoStake
	}
	stake, ok := s.CitizenStakes[addr]
	if !ok || stake.Amount == 0 {
		s.mu.Unlock()
		return ErrNoStake
	}
	if amount > stake.Amount {
		s.mu.Unlock()
		return ErrInsufficientStake
	}
	stake.Amount -= amount
	if stake.Amount == 0 {
		delete(s.CitizenStakes, addr)
	} else {
		s.CitizenStakes[addr] = stake
	}

	acc := s.Accounts[addr]
	newBal, err := safeAdd(acc.Balance, amount)
	if err != nil {
		s.mu.Unlock()
		return errors.New("balance overflow on unstake")
	}
	acc.Balance = newBal
	s.Accounts[addr] = acc
	s.mu.Unlock()
	return nil
}

// pendingCitizenRewardsLocked computes accrued-but-unpaid rewards for addr
// as of now, without mutating anything. Caller must hold s.mu.
func (s *State) pendingCitizenRewardsLocked(addr Address, now int64) uint64 {
	if s.CitizenRewardRateBpsPerYear == 0 {
		return 0
	}
	stake, ok := s.CitizenStakes[addr]
	if !ok || stake.Amount == 0 {
		return 0
	}
	elapsed := now - stake.LastClaimAt
	if elapsed <= 0 {
		return 0
	}
	// stake.Amount * rateBps * elapsedSeconds / (10000 * secondsPerYear),
	// ordered to delay the division as long as possible for precision.
	// uint64 headroom: SYN's genesis max supply comfortably fits, and a
	// realistic rate (low hundreds of bps) and elapsed time keep this well
	// under uint64 overflow for any real deployment.
	numerator := stake.Amount * s.CitizenRewardRateBpsPerYear * uint64(elapsed)
	return numerator / (10000 * uint64(SecondsPerYear))
}

// PendingCitizenRewards is the read-only, non-mutating view of
// pendingCitizenRewardsLocked, for status endpoints.
func (s *State) PendingCitizenRewards(addr Address, now int64) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingCitizenRewardsLocked(addr, now)
}

// GetCitizenStake returns addr's current stake record (zero value if none).
func (s *State) GetCitizenStake(addr Address) CitizenStake {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.CitizenStakes == nil {
		return CitizenStake{}
	}
	return s.CitizenStakes[addr]
}

// ClaimCitizenRewards pays addr whatever Citizen staking reward has accrued
// since their last claim, capped at the treasury's actual current balance
// (a claim never fails outright for insufficient treasury funds -- it pays
// what's available and leaves the rest to accrue for a later claim once the
// treasury is topped up). Returns the amount actually paid.
func (s *State) ClaimCitizenRewards(addr Address, now int64) (uint64, error) {
	if s.TreasuryAddress == "" || s.CitizenRewardRateBpsPerYear == 0 {
		return 0, ErrCitizenRewardsNotConfigured
	}
	s.mu.Lock()
	pending := s.pendingCitizenRewardsLocked(addr, now)
	if pending == 0 {
		s.mu.Unlock()
		return 0, nil
	}
	treasury := s.Accounts[s.TreasuryAddress]
	payout := pending
	if treasury.Balance < payout {
		payout = treasury.Balance
	}
	if payout == 0 {
		s.mu.Unlock()
		return 0, nil
	}
	treasury.Balance -= payout
	s.Accounts[s.TreasuryAddress] = treasury

	acc := s.Accounts[addr]
	newBal, err := safeAdd(acc.Balance, payout)
	if err != nil {
		// Roll back the treasury debit -- this error path happens after
		// that mutation, so it must be undone explicitly.
		treasury.Balance += payout
		s.Accounts[s.TreasuryAddress] = treasury
		s.mu.Unlock()
		return 0, errors.New("balance overflow on reward claim")
	}
	acc.Balance = newBal
	s.Accounts[addr] = acc

	stake := s.CitizenStakes[addr]
	// Only advance the accrual clock by what was actually paid: if the
	// treasury couldn't cover the full accrued amount, the unpaid remainder
	// keeps accruing from where it left off rather than being silently
	// forfeited.
	if payout == pending {
		stake.LastClaimAt = now
	} else {
		covered := elapsedCoveredBySeconds(stake, payout, s.CitizenRewardRateBpsPerYear)
		stake.LastClaimAt += covered
	}
	s.CitizenStakes[addr] = stake
	s.mu.Unlock()
	return payout, nil
}

// elapsedCoveredBySeconds inverts the reward formula to find how many
// seconds of accrual a partial payout actually covered, so LastClaimAt can
// advance by exactly that much instead of either the full window (which
// would forfeit the shortfall) or not at all (which would let a
// low-treasury deployment be claimed against repeatedly for the same
// seconds).
func elapsedCoveredBySeconds(stake CitizenStake, payout uint64, rateBps uint64) int64 {
	if stake.Amount == 0 || rateBps == 0 {
		return 0
	}
	denom := stake.Amount * rateBps
	if denom == 0 {
		return 0
	}
	return int64(payout * 10000 * uint64(SecondsPerYear) / denom)
}
