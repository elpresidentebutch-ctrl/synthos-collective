package chain

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

// FeeModelV2ActivationHeight is the block height at which the native fee model v2 activates.
const FeeModelV2ActivationHeight int64 = 102550 // Set for rollout: ~333 blocks from 102217 (~1 hour)

// FeeEpochBlocks is the epoch length for validator fee payouts (about 30 days at ~12 s/block).
const FeeEpochBlocks uint64 = 216_000

var ErrFeeTooLow = errors.New("transaction fee is below minimum required")

// RequiredFee computes the minimum required fee for a transaction from FeeModelV2ActivationHeight.
// A transaction is a SYN send when AssetID == "" and metadata type is empty or "transfer".
// SYN send: max(10, min(1000, ceil(Amount / 1000)))
// other: 10
func RequiredFee(tx Tx) uint64 {
	txType := metadataValue(tx.Metadata, "type")
	if tx.AssetID == "" && (txType == "" || txType == "transfer") {
		ceil := tx.Amount / 1000
		if tx.Amount%1000 != 0 {
			ceil++
		}
		fee := ceil
		if fee < 10 {
			fee = 10
		}
		if fee > 1000 {
			fee = 1000
		}
		return fee
	}
	return 10
}

// -----------------------------------------------------------------------
// Entry points for running these chain rules under an external consensus
// engine (CometBFT, see cometapp/). The engine decides block order,
// proposers, votes and finality; everything a transaction or a block does
// to state still goes through the same code the legacy Chain uses, so the
// two can never disagree about what a transaction means.
// -----------------------------------------------------------------------

// TxContext is what a transaction's effect depends on besides the state:
// the height of the block it's in, the chain ID (part of what validators
// sign), the chain-wide staking rules, and the block's time.
type TxContext struct {
	Height  uint64
	ChainID string
	Staking ValidatorStakingParams
	// BlockTime is the consensus time of the block (unix seconds), which
	// every validator agreed on. When set it replaces tx.Timestamp for
	// the transaction's effect: Timestamp is not covered by the signature,
	// so the sender -- or anyone relaying the transaction -- can put any
	// value there, and time-based rules (citizen reward accrual) must not
	// depend on it. Zero keeps the legacy behavior.
	BlockTime int64
}

// ApplyTransaction applies tx to st exactly as a block at ctx.Height does
// (Chain.applyTxLocked is this same function). It may leave st partially
// modified when it returns an error, so callers that need to keep going
// after a failed transaction should apply it to a Clone and keep the clone
// only on success.
func ApplyTransaction(st *State, tx Tx, ctx TxContext) error {
	if int64(ctx.Height) >= FeeModelV2ActivationHeight && tx.Fee < RequiredFee(tx) {
		return ErrFeeTooLow
	}
	if ctx.BlockTime != 0 {
		tx.Timestamp = ctx.BlockTime // tx is a copy; the caller's is untouched
	}
	if txType := metadataValue(tx.Metadata, "type"); validatorTxTypes[txType] && ctx.Staking.EnabledAt(ctx.Height) {
		if err := tx.Verify(); err != nil {
			return err
		}
		return st.applyValidatorTxCtx(tx, txType, validatorTxContext{height: ctx.Height, params: ctx.Staking, chainID: ctx.ChainID})
	}
	return st.ApplyTx(tx)
}

// FeeSplit holds the breakdown of applied block fees.
type FeeSplit struct {
	TotalFees  uint64
	BurnAmount uint64
	ToTreasury uint64
	ToProposer uint64
}

// ApplyBlockFees splits the fees of a block's successfully applied transactions.
// Below FeeModelV2ActivationHeight, fees are split between the proposer's address
// and burn (via creditProposerFees); an empty proposer burns everything.
// At and after FeeModelV2ActivationHeight, 50% is burned and 50% is credited
// to st.TreasuryAddress and added to st.FeeEpochTreasuryIncome; no fee goes directly
// to any validator.
func ApplyBlockFees(st *State, appliedTxs []Tx, proposer Address, height int64) (FeeSplit, error) {
	if height < FeeModelV2ActivationHeight {
		var totalFees uint64
		for _, tx := range appliedTxs {
			var err error
			totalFees, err = safeAdd(totalFees, tx.Fee)
			if err != nil {
				return FeeSplit{}, fmt.Errorf("block fee overflow: %w", err)
			}
		}
		if totalFees == 0 || proposer == "" {
			return FeeSplit{TotalFees: totalFees, BurnAmount: totalFees}, creditProposerFees(st, appliedTxs, proposer)
		}
		burnAmount := (totalFees * BURN_PERCENT) / 100
		rewardAmount := totalFees - burnAmount
		if err := creditProposerFees(st, appliedTxs, proposer); err != nil {
			return FeeSplit{}, err
		}
		return FeeSplit{
			TotalFees:  totalFees,
			BurnAmount: burnAmount,
			ToProposer: rewardAmount,
		}, nil
	}

	var totalFees uint64
	for _, tx := range appliedTxs {
		var err error
		totalFees, err = safeAdd(totalFees, tx.Fee)
		if err != nil {
			return FeeSplit{}, fmt.Errorf("block fee overflow: %w", err)
		}
	}
	if totalFees == 0 {
		return FeeSplit{}, nil
	}

	burnAmount := (totalFees * BURN_PERCENT) / 100
	toTreasury := totalFees - burnAmount

	if st.TreasuryAddress == "" {
		return FeeSplit{}, errors.New("cannot apply block fees: TreasuryAddress is required from FeeModelV2ActivationHeight onward")
	}

	treasury := st.Get(st.TreasuryAddress)
	nextBalance, err := safeAdd(treasury.Balance, toTreasury)
	if err != nil {
		return FeeSplit{}, fmt.Errorf("treasury fee deposit overflow: %w", err)
	}
	treasury.Balance = nextBalance
	st.Set(st.TreasuryAddress, treasury)

	newIncome, err := safeAdd(st.FeeEpochTreasuryIncome, toTreasury)
	if err != nil {
		return FeeSplit{}, fmt.Errorf("fee epoch treasury income overflow: %w", err)
	}
	st.FeeEpochTreasuryIncome = newIncome

	return FeeSplit{
		TotalFees:  totalFees,
		BurnAmount: burnAmount,
		ToTreasury: toTreasury,
	}, nil
}

// RecordFeeEpochVote records a validator vote during a block for the current fee epoch.
func (s *State) RecordFeeEpochVote(op Address, power uint64, committed bool) {
	if power == 0 {
		return
	}
	s.FeeEpochTotalPower += power
	if committed && op != "" {
		if s.FeeEpochSignedPower == nil {
			s.FeeEpochSignedPower = make(map[Address]uint64)
		}
		s.FeeEpochSignedPower[op] += power
	}
}

// ValidatorPayout records an operator payout from the fee epoch treasury disbursement.
type ValidatorPayout struct {
	Operator Address
	Amount   uint64
	Epoch    uint64
}

// DistributeFeeEpochPayout computes and distributes monthly validator payouts
// from the treasury pool at epoch boundaries (height - H + 1) % 216000 == 0.
// Pool = st.FeeEpochTreasuryIncome / 2.
// Rounding leftovers and unearned shares stay in the treasury.
// Resets FeeEpochTreasuryIncome, FeeEpochTotalPower, and FeeEpochSignedPower.
func (s *State) DistributeFeeEpochPayout(epoch uint64) ([]ValidatorPayout, error) {
	pool := s.FeeEpochTreasuryIncome / 2
	totalPower := s.FeeEpochTotalPower

	if totalPower == 0 || pool == 0 || len(s.FeeEpochSignedPower) == 0 {
		s.FeeEpochTreasuryIncome = 0
		s.FeeEpochTotalPower = 0
		s.FeeEpochSignedPower = make(map[Address]uint64)
		return nil, nil
	}

	var ops []Address
	for op := range s.FeeEpochSignedPower {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[i] < ops[j]
	})

	shares := make(map[Address]uint64, len(ops))
	var sum uint64

	poolBig := new(big.Int).SetUint64(pool)
	totBig := new(big.Int).SetUint64(totalPower)

	for _, op := range ops {
		powerBig := new(big.Int).SetUint64(s.FeeEpochSignedPower[op])
		shareBig := new(big.Int).Div(new(big.Int).Mul(poolBig, powerBig), totBig)
		share := shareBig.Uint64()
		shares[op] = share
		sum += share
	}

	treasuryAcc := s.Get(s.TreasuryAddress)
	bal := treasuryAcc.Balance

	if sum > bal {
		balBig := new(big.Int).SetUint64(bal)
		sumBig := new(big.Int).SetUint64(sum)
		for _, op := range ops {
			curBig := new(big.Int).SetUint64(shares[op])
			scaled := new(big.Int).Div(new(big.Int).Mul(curBig, balBig), sumBig)
			shares[op] = scaled.Uint64()
		}
	}

	var payouts []ValidatorPayout
	for _, op := range ops {
		amt := shares[op]
		if amt == 0 {
			continue
		}

		treasuryAcc = s.Get(s.TreasuryAddress)
		if treasuryAcc.Balance < amt {
			amt = treasuryAcc.Balance
		}
		treasuryAcc.Balance -= amt
		s.Set(s.TreasuryAddress, treasuryAcc)

		opAcc := s.Get(op)
		opAcc.Balance += amt
		s.Set(op, opAcc)

		payouts = append(payouts, ValidatorPayout{
			Operator: op,
			Amount:   amt,
			Epoch:    epoch,
		})
	}

	s.FeeEpochTreasuryIncome = 0
	s.FeeEpochTotalPower = 0
	s.FeeEpochSignedPower = make(map[Address]uint64)

	return payouts, nil
}

// ValidatorByConsensusKey returns the validator registered with the given
// consensus public key (0x hex, any case).
func (s *State) ValidatorByConsensusKey(key string) (ValidatorRecord, bool) {
	norm, err := normalizeConsensusPubKey(key)
	if err != nil {
		return ValidatorRecord{}, false
	}
	op := s.validatorOperatorForKey(norm)
	if op == "" {
		return ValidatorRecord{}, false
	}
	return s.GetValidator(op)
}

// SlashForDoubleSign applies the double-sign penalty for a validator the
// consensus engine itself caught signing two conflicting votes: bps basis
// points of its self-bond and pending unbonding are burned, and it is
// tombstoned (never active again). Returns the amount burned, or false if
// the key isn't registered or the validator was already tombstoned (one
// offense, one penalty). Unlike a validator_evidence transaction there is
// no reporter to reward, so everything slashed is burned.
func (s *State) SlashForDoubleSign(consensusKey string, bps uint64) (uint64, bool) {
	rec, ok := s.ValidatorByConsensusKey(consensusKey)
	if !ok || rec.Tombstoned {
		return 0, false
	}
	updated, slashed := slashAndTombstone(rec, bps)
	s.mu.Lock()
	s.Validators[rec.Operator] = updated
	s.mu.Unlock()
	return slashed, true
}

// ReplaceValidatorSetSnapshot records set as the validator set currently
// in force. Under an external consensus engine this is the set last handed
// to the engine, kept in state (and so in Root) so every node agrees on it.
func (s *State) ReplaceValidatorSetSnapshot(set []ActiveValidator) {
	s.setValidatorSetSnapshot(append([]ActiveValidator(nil), set...))
}

var ErrGenesisValidator = errors.New("invalid genesis validator")

// BondGenesisValidator registers a validator in a genesis state by moving
// selfBond out of operator's genesis balance into its self-bond, so total
// supply is unchanged. Used to seed the first validator set of a new chain.
func (s *State) BondGenesisValidator(operator Address, consensusKey string, selfBond uint64, moniker, endpoint string) error {
	key, err := normalizeConsensusPubKey(consensusKey)
	if err != nil {
		return err
	}
	if selfBond == 0 {
		return ErrGenesisValidator
	}
	if err := validateValidatorProfile(moniker, endpoint); err != nil {
		return err
	}
	if s.validatorOperatorForKey(key) != "" {
		return ErrValidatorKeyInUse
	}
	if _, exists := s.GetValidator(operator); exists {
		return ErrGenesisValidator
	}
	acc := s.Get(operator)
	if acc.Balance < selfBond {
		return ErrInsufficientFunds
	}
	acc.Balance -= selfBond
	s.Set(operator, acc)
	s.mu.Lock()
	if s.Validators == nil {
		s.Validators = make(map[Address]ValidatorRecord)
	}
	s.Validators[operator] = ValidatorRecord{
		Operator: operator, ConsensusPubKey: key, Moniker: moniker, Endpoint: endpoint, SelfBond: selfBond,
	}
	s.mu.Unlock()
	return nil
}
