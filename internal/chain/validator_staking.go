package chain

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// -----------------------------------------------------------------------
// Permissionless validator staking (step 1: the on-chain registry).
//
// Today the validator roster comes from each node's config file
// (trusted_validators) plus an off-chain registry roster that only contains
// validators the operator approved by hand. That makes joining depend on
// one person and one server. This file is the first step toward replacing
// that with proof of stake: anyone can register as a validator by locking
// ("bonding") their own SYN in a signed transaction, and the validator set
// is then computed from chain state, identically on every node.
//
// Three transaction types, all applied only inside blocks (so every node
// sees the same result, the same way citizen staking and governance work):
//
//   validator_bond     lock tx.Amount SYN as self-bond. The first bond must
//                      carry metadata consensus_pub_key (the ed25519 key the
//                      validator will sign blocks with); optional moniker
//                      and endpoint metadata update the public record.
//   validator_unbond   start releasing tx.Amount of self-bond. It stops
//                      counting immediately but only becomes withdrawable
//                      after UnbondingBlocks blocks, so a validator can't
//                      misbehave and walk away with its stake in the same
//                      moment.
//   validator_withdraw return every matured unbonding entry to the
//                      operator's balance. tx.Amount is ignored (the
//                      transaction format requires it to be nonzero).
//
// Step 1 deliberately does NOT change who produces or approves blocks:
// consensus still uses the configured roster. ActiveValidatorSet computes
// what the stake-based set would be, so it can be observed on a testnet
// before step 2 wires it into block authorization and stake-weighted
// quorum.
//
// Everything here is inert until ValidatorStakingParams.EnabledFromHeight
// is set (identically on every node, like the other *_from_height
// switches). Before that height, or with it unset, a transaction with one
// of these types is processed exactly as it always has been (as an
// ordinary transfer), so merging this code cannot change the live chain.
// -----------------------------------------------------------------------

// ValidatorStakingParams are chain-wide rules for validator staking. They
// must be identical on every node sharing a chain.
type ValidatorStakingParams struct {
	// EnabledFromHeight is the first block height at which validator_*
	// transactions take effect. 0 disables validator staking entirely.
	EnabledFromHeight uint64 `json:"enabled_from_height"`
	// MinSelfBond is the smallest self-bond a registered validator may
	// hold. A partial unbond may not leave a validator below it (unbond
	// everything to exit instead).
	MinSelfBond uint64 `json:"min_self_bond"`
	// UnbondingBlocks is how many blocks unbonded SYN stays locked before
	// validator_withdraw can release it.
	UnbondingBlocks uint64 `json:"unbonding_blocks"`
	// MaxActiveValidators caps the active set (highest self-bond first).
	// 0 means no cap.
	MaxActiveValidators int `json:"max_active_validators"`

	// --- Step 2: stake-based block authorization and slashing ---

	// ConsensusFromHeight is the first block height whose authorization is
	// decided by stake instead of the configured validator roster: the
	// proposer must be in the active staked set, and approvals from more
	// than 2/3 of that set's stake are required. 0 = never. Must be later
	// than EnabledFromHeight so validators can bond first.
	ConsensusFromHeight uint64 `json:"consensus_from_height"`
	// EpochBlocks is how often the authorizing set is re-snapshotted from
	// chain state. Bonds and unbonds take effect at the next snapshot, so
	// the set can't change in the middle of an epoch.
	EpochBlocks uint64 `json:"epoch_blocks"`
	// SlashFractionBps is the share of a validator's stake (self-bond and
	// pending unbonding) destroyed when it is proven to have signed two
	// different blocks at the same height, in basis points (500 = 5%).
	SlashFractionBps uint64 `json:"slash_fraction_bps"`
	// ReporterRewardBps is the share of the slashed amount paid to whoever
	// submits the evidence, in basis points; the rest is burned.
	ReporterRewardBps uint64 `json:"reporter_reward_bps"`
}

// Validate rejects enabled params that would make staking meaningless or
// unsafe: a zero minimum lets anyone register for free (the fake-identity
// problem staking exists to prevent), and a zero unbonding period lets a
// validator misbehave and withdraw its stake in the same block.
func (p ValidatorStakingParams) Validate() error {
	if p.EnabledFromHeight == 0 {
		return nil
	}
	if p.MinSelfBond == 0 {
		return errors.New("validator_staking.min_self_bond must be greater than zero when staking is enabled")
	}
	if p.UnbondingBlocks == 0 {
		return errors.New("validator_staking.unbonding_blocks must be greater than zero when staking is enabled")
	}
	if p.MaxActiveValidators < 0 {
		return errors.New("validator_staking.max_active_validators must not be negative")
	}
	if p.ConsensusFromHeight == 0 {
		return nil
	}
	if p.ConsensusFromHeight <= p.EnabledFromHeight {
		return errors.New("validator_staking.consensus_from_height must be later than enabled_from_height, so validators can bond before stake decides block authorization")
	}
	if p.EpochBlocks == 0 {
		return errors.New("validator_staking.epoch_blocks must be greater than zero when stake consensus is enabled")
	}
	if p.SlashFractionBps == 0 || p.SlashFractionBps > 10_000 {
		return errors.New("validator_staking.slash_fraction_bps must be between 1 and 10000 when stake consensus is enabled (zero would make double-signing free)")
	}
	if p.ReporterRewardBps > 10_000 {
		return errors.New("validator_staking.reporter_reward_bps must not exceed 10000")
	}
	return nil
}

// ConsensusActiveAt reports whether a block at height is authorized by
// stake (subject to a non-empty authorizing set; see Chain.
// stakeAuthoritySetLocked).
func (p ValidatorStakingParams) ConsensusActiveAt(height uint64) bool {
	return p.ConsensusFromHeight > 0 && height >= p.ConsensusFromHeight
}

// isSnapshotHeight reports whether the authorizing set is re-snapshotted
// at the end of the block at height: the block just before
// ConsensusFromHeight, and every EpochBlocks blocks after that. The
// snapshot taken at the end of block h authorizes blocks h+1 through
// h+EpochBlocks.
func (p ValidatorStakingParams) isSnapshotHeight(height uint64) bool {
	if p.ConsensusFromHeight == 0 || p.EpochBlocks == 0 {
		return false
	}
	next := height + 1
	if next < p.ConsensusFromHeight {
		return false
	}
	return (next-p.ConsensusFromHeight)%p.EpochBlocks == 0
}

// EnabledAt reports whether validator_* transactions take effect in a block
// at height.
func (p ValidatorStakingParams) EnabledAt(height uint64) bool {
	return p.EnabledFromHeight > 0 && height >= p.EnabledFromHeight
}

// MaxUnbondingEntries bounds how many pending unbonding entries one
// validator may hold at once, so a validator can't grow chain state without
// limit by unbonding one coin at a time.
const MaxUnbondingEntries = 16

const (
	maxValidatorMonikerLen  = 64
	maxValidatorEndpointLen = 256
)

// UnbondingEntry is self-bond that has stopped counting toward the
// validator set and becomes withdrawable at CompleteAtHeight.
type UnbondingEntry struct {
	Amount           uint64 `json:"amount"`
	CompleteAtHeight uint64 `json:"complete_at_height"`
}

// ValidatorRecord is one registered validator, keyed in State.Validators by
// its operator address (the account that signs its bond transactions and
// receives withdrawals). ConsensusPubKey is the separate key it signs
// blocks with; it's fixed at first bond and unique across validators.
type ValidatorRecord struct {
	Operator        Address          `json:"operator"`
	ConsensusPubKey string           `json:"consensus_pub_key"`
	Moniker         string           `json:"moniker,omitempty"`
	Endpoint        string           `json:"endpoint,omitempty"`
	SelfBond        uint64           `json:"self_bond"`
	FirstBondHeight uint64           `json:"first_bond_height"`
	Unbonding       []UnbondingEntry `json:"unbonding,omitempty"`
	// Tombstoned is set permanently once the validator is proven to have
	// signed two different blocks at the same height. A tombstoned
	// validator is never active again and can't bond more; it can still
	// unbond and withdraw whatever stake the slash left it. Its record is
	// kept (even at zero stake) so the same operator can't re-register.
	Tombstoned bool `json:"tombstoned,omitempty"`
}

// ActiveValidator is one member of the stake-based validator set.
type ActiveValidator struct {
	Operator        Address `json:"operator"`
	ConsensusPubKey string  `json:"consensus_pub_key"`
	Moniker         string  `json:"moniker,omitempty"`
	Endpoint        string  `json:"endpoint,omitempty"`
	Power           uint64  `json:"power"`
}

var (
	ErrValidatorBondTooSmall     = errors.New("validator self-bond would be below the minimum")
	ErrValidatorKeyInUse         = errors.New("consensus public key is already registered to another validator")
	ErrValidatorKeyMismatch      = errors.New("consensus public key does not match this validator's registered key")
	ErrValidatorNotFound         = errors.New("no validator registered for this address")
	ErrValidatorInsufficientBond = errors.New("unbond amount exceeds self-bond")
	ErrValidatorTooManyUnbonding = errors.New("too many pending unbonding entries; withdraw matured ones first")
	ErrValidatorNothingMatured   = errors.New("no unbonding entries have matured yet")
	ErrValidatorAssetNotSYN      = errors.New("validator transactions must use native SYN")
	ErrValidatorTombstoned       = errors.New("validator is permanently removed for double-signing")
	ErrStakeConsensusInactive    = errors.New("stake-based consensus is not active at this height")
	ErrInvalidEvidence           = errors.New("invalid double-sign evidence")
)

var validatorTxTypes = map[string]bool{
	"validator_bond":     true,
	"validator_unbond":   true,
	"validator_withdraw": true,
	"validator_evidence": true,
}

// IsValidatorTxType reports whether txType is one of the validator staking
// transaction types.
func IsValidatorTxType(txType string) bool { return validatorTxTypes[txType] }

// normalizeConsensusPubKey accepts a hex ed25519 public key with or without
// a 0x prefix and returns it as lowercase "0x"-prefixed hex, so the same key
// can't be registered twice under different spellings.
func normalizeConsensusPubKey(raw string) (string, error) {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "0x")
	if trimmed == "" {
		return "", errors.New("missing consensus_pub_key metadata")
	}
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return "", fmt.Errorf("consensus_pub_key is not valid hex: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return "", fmt.Errorf("consensus_pub_key must be %d bytes, got %d", ed25519.PublicKeySize, len(b))
	}
	return "0x" + trimmed, nil
}

func validateValidatorProfile(moniker, endpoint string) error {
	if len(moniker) > maxValidatorMonikerLen {
		return fmt.Errorf("moniker longer than %d characters", maxValidatorMonikerLen)
	}
	if len(endpoint) > maxValidatorEndpointLen {
		return fmt.Errorf("endpoint longer than %d characters", maxValidatorEndpointLen)
	}
	if endpoint != "" && !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		return errors.New("endpoint must start with http:// or https://")
	}
	return nil
}

// applyValidatorTx applies one validator_* transaction at block height
// under params. Like applyCitizenGovernanceTx it checks the nonce itself,
// charges the fee and advances the nonce only on success, and never
// partially applies: every check runs before anything is written, and a
// returned error rejects the whole block anyway.
func (s *State) applyValidatorTx(tx Tx, txType string, height uint64, params ValidatorStakingParams) error {
	return s.applyValidatorTxCtx(tx, txType, validatorTxContext{height: height, params: params})
}

// validatorTxContext is everything about the enclosing block a validator
// transaction's effect depends on.
type validatorTxContext struct {
	height  uint64
	params  ValidatorStakingParams
	chainID string
}

func (s *State) applyValidatorTxCtx(tx Tx, txType string, ctx validatorTxContext) error {
	height, params := ctx.height, ctx.params
	if tx.AssetID != "" && tx.AssetID != "syn" {
		return ErrValidatorAssetNotSYN
	}
	if txType == "validator_evidence" {
		return s.applyValidatorEvidence(tx, ctx)
	}
	acc := s.Get(tx.From)
	if tx.Nonce != acc.Nonce {
		return errors.New("bad nonce")
	}

	s.mu.RLock()
	rec, exists := s.Validators[tx.From]
	s.mu.RUnlock()
	if exists {
		rec.Unbonding = append([]UnbondingEntry(nil), rec.Unbonding...)
	}

	switch txType {
	case "validator_bond":
		total, err := safeAdd(tx.Amount, tx.Fee)
		if err != nil {
			return errors.New("amount overflow detected")
		}
		if acc.Balance < total {
			return ErrInsufficientFunds
		}
		if exists && rec.Tombstoned {
			return ErrValidatorTombstoned
		}
		rawKey := metadataValue(tx.Metadata, "consensus_pub_key")
		if !exists {
			key, err := normalizeConsensusPubKey(rawKey)
			if err != nil {
				return err
			}
			if s.validatorOperatorForKey(key) != "" {
				return ErrValidatorKeyInUse
			}
			rec = ValidatorRecord{Operator: tx.From, ConsensusPubKey: key, FirstBondHeight: height}
		} else if rawKey != "" {
			key, err := normalizeConsensusPubKey(rawKey)
			if err != nil {
				return err
			}
			if key != rec.ConsensusPubKey {
				return ErrValidatorKeyMismatch
			}
		}
		moniker := metadataValue(tx.Metadata, "moniker")
		endpoint := metadataValue(tx.Metadata, "endpoint")
		if err := validateValidatorProfile(moniker, endpoint); err != nil {
			return err
		}
		if moniker != "" {
			rec.Moniker = moniker
		}
		if endpoint != "" {
			rec.Endpoint = endpoint
		}
		newBond, err := safeAdd(rec.SelfBond, tx.Amount)
		if err != nil {
			return errors.New("self-bond overflow detected")
		}
		if newBond < params.MinSelfBond {
			return ErrValidatorBondTooSmall
		}
		rec.SelfBond = newBond
		acc.Balance -= total

	case "validator_unbond":
		if !exists {
			return ErrValidatorNotFound
		}
		if tx.Amount > rec.SelfBond {
			return ErrValidatorInsufficientBond
		}
		remaining := rec.SelfBond - tx.Amount
		if remaining != 0 && remaining < params.MinSelfBond {
			return ErrValidatorBondTooSmall
		}
		if len(rec.Unbonding) >= MaxUnbondingEntries {
			return ErrValidatorTooManyUnbonding
		}
		if acc.Balance < tx.Fee {
			return ErrInsufficientFunds
		}
		completeAt, err := safeAdd(height, params.UnbondingBlocks)
		if err != nil {
			return errors.New("unbonding height overflow detected")
		}
		rec.SelfBond = remaining
		rec.Unbonding = append(rec.Unbonding, UnbondingEntry{Amount: tx.Amount, CompleteAtHeight: completeAt})
		acc.Balance -= tx.Fee

	case "validator_withdraw":
		if !exists {
			return ErrValidatorNotFound
		}
		var matured uint64
		pending := rec.Unbonding[:0]
		for _, e := range rec.Unbonding {
			if e.CompleteAtHeight <= height {
				sum, err := safeAdd(matured, e.Amount)
				if err != nil {
					return errors.New("withdraw amount overflow detected")
				}
				matured = sum
				continue
			}
			pending = append(pending, e)
		}
		if matured == 0 {
			return ErrValidatorNothingMatured
		}
		credited, err := safeAdd(acc.Balance, matured)
		if err != nil {
			return errors.New("balance overflow detected")
		}
		if credited < tx.Fee {
			return ErrInsufficientFunds
		}
		if len(pending) == 0 {
			pending = nil
		}
		rec.Unbonding = pending
		acc.Balance = credited - tx.Fee

	default:
		return fmt.Errorf("unhandled validator tx type %q", txType)
	}

	acc.Nonce++
	s.mu.Lock()
	if s.Validators == nil {
		s.Validators = make(map[Address]ValidatorRecord)
	}
	if rec.SelfBond == 0 && len(rec.Unbonding) == 0 && !rec.Tombstoned {
		delete(s.Validators, tx.From)
	} else {
		s.Validators[tx.From] = rec
	}
	s.mu.Unlock()
	s.Set(tx.From, acc)
	return nil
}

// validatorOperatorForKey returns the operator already registered with key,
// or "" if none.
func (s *State) validatorOperatorForKey(key string) Address {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for op, rec := range s.Validators {
		if rec.ConsensusPubKey == key {
			return op
		}
	}
	return ""
}

// GetValidator returns the validator registered for operator, if any.
func (s *State) GetValidator(operator Address) (ValidatorRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.Validators[operator]
	if ok {
		rec.Unbonding = append([]UnbondingEntry(nil), rec.Unbonding...)
	}
	return rec, ok
}

// ValidatorRecords returns a copy of every registered validator record.
func (s *State) ValidatorRecords() []ValidatorRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ValidatorRecord, 0, len(s.Validators))
	for _, rec := range s.Validators {
		rec.Unbonding = append([]UnbondingEntry(nil), rec.Unbonding...)
		out = append(out, rec)
	}
	return out
}

// ActiveValidatorSet returns the stake-based validator set implied by the
// current state: every validator whose self-bond meets params.MinSelfBond
// (and is nonzero), ordered by self-bond descending with ties broken by
// operator address, capped at params.MaxActiveValidators. It depends only
// on chain state and params, so every node computes the same set.
func (s *State) ActiveValidatorSet(params ValidatorStakingParams) []ActiveValidator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	minBond := params.MinSelfBond
	if minBond == 0 {
		minBond = 1
	}
	out := make([]ActiveValidator, 0, len(s.Validators))
	for op, rec := range s.Validators {
		if rec.SelfBond < minBond || rec.Tombstoned {
			continue
		}
		out = append(out, ActiveValidator{
			Operator:        op,
			ConsensusPubKey: rec.ConsensusPubKey,
			Moniker:         rec.Moniker,
			Endpoint:        rec.Endpoint,
			Power:           rec.SelfBond,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Power != out[j].Power {
			return out[i].Power > out[j].Power
		}
		return out[i].Operator < out[j].Operator
	})
	if params.MaxActiveValidators > 0 && len(out) > params.MaxActiveValidators {
		out = out[:params.MaxActiveValidators]
	}
	return out
}

// TotalValidatorLocked returns all SYN held by validator staking: active
// self-bond plus unbonding amounts not yet withdrawn. That SYN has left its
// owners' spendable balances, so it doesn't appear in TotalStake.
func (s *State) TotalValidatorLocked() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total uint64
	for _, rec := range s.Validators {
		total += rec.SelfBond
		for _, e := range rec.Unbonding {
			total += e.Amount
		}
	}
	return total
}

func cloneValidatorRecords(in map[Address]ValidatorRecord) map[Address]ValidatorRecord {
	out := make(map[Address]ValidatorRecord, len(in))
	for op, rec := range in {
		if rec.Unbonding != nil {
			rec.Unbonding = append([]UnbondingEntry(nil), rec.Unbonding...)
		}
		out[op] = rec
	}
	return out
}

// -----------------------------------------------------------------------
// Step 2: what stake-mode validators sign, and double-sign evidence.
//
// In stake mode proposers and voters sign messages that name the chain and
// the height, not just the block hash (the legacy QuorumApprovalMessage is
// "APPROVE|<hash>"). That makes equivocation provable from two signatures
// alone: the same key signing two different hashes for the same chain and
// height is proof of double-signing, with no need to ship the blocks. The
// chain ID keeps a signature made on a testnet from ever counting as
// evidence on mainnet.
// -----------------------------------------------------------------------

// StakeProposalMessage is what the proposer of a stake-mode block signs.
func StakeProposalMessage(chainID string, height uint64, blockHash string) []byte {
	return []byte(fmt.Sprintf("SYNTHOS/PROPOSE/v2|%s|%d|%s", chainID, height, blockHash))
}

// StakeApprovalMessage is what a validator signs to approve a stake-mode
// block.
func StakeApprovalMessage(chainID string, height uint64, blockHash string) []byte {
	return []byte(fmt.Sprintf("SYNTHOS/APPROVE/v2|%s|%d|%s", chainID, height, blockHash))
}

func stakeMessage(kind, chainID string, height uint64, blockHash string) ([]byte, error) {
	switch kind {
	case "propose":
		return StakeProposalMessage(chainID, height, blockHash), nil
	case "approve":
		return StakeApprovalMessage(chainID, height, blockHash), nil
	}
	return nil, fmt.Errorf("%w: kind must be \"propose\" or \"approve\"", ErrInvalidEvidence)
}

// isCanonicalBlockHash requires the exact form Block.ComputeHash produces
// ("0x" + 64 lowercase hex), so one block can't be presented as two
// "different" hashes by changing letter case.
func isCanonicalBlockHash(h string) bool {
	if len(h) != 66 || !strings.HasPrefix(h, "0x") {
		return false
	}
	for _, c := range h[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func verifyHexSig(pubHex string, msg []byte, sigHex string) bool {
	pub, err := hex.DecodeString(strings.TrimPrefix(pubHex, "0x"))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := decodeHexSig(sigHex)
	if err != nil {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}

// applyValidatorEvidence applies a validator_evidence transaction: proof
// that the validator owning consensus_pub_key signed two different block
// hashes of the same kind at the same height. Anyone may submit it (tx.From
// pays the fee and receives ReporterRewardBps of the slashed amount).
//
// Metadata: consensus_pub_key, kind ("propose" or "approve"),
// evidence_height, hash_a, sig_a, hash_b, sig_b.
//
// Effect, identical on every node: SlashFractionBps of the offender's
// self-bond and of every pending unbonding entry is removed, the reporter's
// share is credited, the rest is burned, and the validator is tombstoned
// and dropped from the current authorizing set immediately rather than at
// the next epoch. A validator can only be tombstoned once, so the same
// offense can't be charged twice.
func (s *State) applyValidatorEvidence(tx Tx, ctx validatorTxContext) error {
	p := ctx.params
	if !p.ConsensusActiveAt(ctx.height) {
		return ErrStakeConsensusInactive
	}
	reporter := s.Get(tx.From)
	if tx.Nonce != reporter.Nonce {
		return errors.New("bad nonce")
	}
	if reporter.Balance < tx.Fee {
		return ErrInsufficientFunds
	}

	key, err := normalizeConsensusPubKey(metadataValue(tx.Metadata, "consensus_pub_key"))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
	}
	kind := metadataValue(tx.Metadata, "kind")
	evHeight, err := parseTxMetadataUint64(tx.Metadata, "evidence_height")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
	}
	if evHeight < p.ConsensusFromHeight || evHeight > ctx.height {
		return fmt.Errorf("%w: evidence_height %d is outside stake consensus history", ErrInvalidEvidence, evHeight)
	}
	hashA := metadataValue(tx.Metadata, "hash_a")
	hashB := metadataValue(tx.Metadata, "hash_b")
	if !isCanonicalBlockHash(hashA) || !isCanonicalBlockHash(hashB) {
		return fmt.Errorf("%w: block hashes must be 0x followed by 64 lowercase hex characters", ErrInvalidEvidence)
	}
	if hashA == hashB {
		return fmt.Errorf("%w: the two hashes are the same block", ErrInvalidEvidence)
	}
	msgA, err := stakeMessage(kind, ctx.chainID, evHeight, hashA)
	if err != nil {
		return err
	}
	msgB, _ := stakeMessage(kind, ctx.chainID, evHeight, hashB)
	if !verifyHexSig(key, msgA, metadataValue(tx.Metadata, "sig_a")) ||
		!verifyHexSig(key, msgB, metadataValue(tx.Metadata, "sig_b")) {
		return fmt.Errorf("%w: signatures do not verify for that key, chain, height and kind", ErrInvalidEvidence)
	}

	operator := s.validatorOperatorForKey(key)
	if operator == "" {
		return ErrValidatorNotFound
	}
	s.mu.RLock()
	offender := s.Validators[operator]
	s.mu.RUnlock()
	if offender.Tombstoned {
		return ErrValidatorTombstoned
	}
	offender.Unbonding = append([]UnbondingEntry(nil), offender.Unbonding...)

	slashed := offender.SelfBond * p.SlashFractionBps / 10_000
	offender.SelfBond -= slashed
	for i := range offender.Unbonding {
		cut := offender.Unbonding[i].Amount * p.SlashFractionBps / 10_000
		offender.Unbonding[i].Amount -= cut
		slashed += cut
	}
	offender.Tombstoned = true
	reward := slashed * p.ReporterRewardBps / 10_000

	// Re-read the reporter in case it is the offender's own operator
	// account (self-reporting is allowed; it still loses the slash).
	reporter = s.Get(tx.From)
	credited, err := safeAdd(reporter.Balance, reward)
	if err != nil {
		return errors.New("balance overflow detected")
	}
	reporter.Balance = credited - tx.Fee
	reporter.Nonce++

	s.mu.Lock()
	s.Validators[operator] = offender
	if len(s.ValidatorSetSnapshot) > 0 {
		kept := make([]ActiveValidator, 0, len(s.ValidatorSetSnapshot))
		for _, v := range s.ValidatorSetSnapshot {
			if v.ConsensusPubKey != key {
				kept = append(kept, v)
			}
		}
		s.ValidatorSetSnapshot = kept
	}
	s.mu.Unlock()
	s.Set(tx.From, reporter)
	return nil
}

// AuthorizingValidatorSet returns a copy of the stake-based set currently
// authorizing blocks (the last epoch snapshot, minus anyone tombstoned
// since). Empty before stake consensus starts.
func (s *State) AuthorizingValidatorSet() []ActiveValidator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ActiveValidator(nil), s.ValidatorSetSnapshot...)
}

func (s *State) setValidatorSetSnapshot(set []ActiveValidator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(set) == 0 {
		s.ValidatorSetSnapshot = nil
		return
	}
	s.ValidatorSetSnapshot = set
}
