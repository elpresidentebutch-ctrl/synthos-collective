package chain

import "errors"

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

// ApplyBlockFees splits the fees of a block's successfully applied
// transactions between the proposer's address and the burn, the same way
// every legacy block does. An empty proposer address burns everything.
func ApplyBlockFees(st *State, appliedTxs []Tx, proposer Address) error {
	return applyBlockEconomics(st, appliedTxs, proposer)
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
