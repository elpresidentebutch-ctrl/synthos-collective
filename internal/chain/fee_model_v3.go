package chain

// FeeModelV3ActivationHeight is the block height at which the native EIP-1559 dynamic fee model v3 activates.
// Scheduled ~40,900 blocks from height ~339,100 (~5.2 days at ~11s/block).
const FeeModelV3ActivationHeight int64 = 380000

const (
	// FeeModelV3TargetBlockTxs is the target transaction count per block (50% utilization target).
	FeeModelV3TargetBlockTxs uint64 = 25

	// FeeModelV3MaxBlockTxs is the maximum transaction count allowed in a single block under v3.
	// With Target=25 and Denominator=8, a full block of 50 txs raises the base fee by exactly 12.5% (25 / (25 * 8) = 1/8).
	FeeModelV3MaxBlockTxs uint64 = 50

	// FeeModelV3InitialBaseFee is the initial base fee at FeeModelV3ActivationHeight (in SYN).
	FeeModelV3InitialBaseFee uint64 = 10

	// FeeModelV3MinBaseFee is the floor below which base fee cannot decay (in SYN).
	FeeModelV3MinBaseFee uint64 = 10

	// FeeModelV3MaxBaseFeeChangeDenominator is 8, allowing up to 12.5% base fee adjustment per block.
	FeeModelV3MaxBaseFeeChangeDenominator uint64 = 8
)

// CalculateNextBaseFee computes the deterministic EIP-1559 next base fee
// using strictly integer arithmetic based on the current block's transaction count.
func CalculateNextBaseFee(currentBaseFee uint64, txCount uint64) uint64 {
	if currentBaseFee == 0 {
		currentBaseFee = FeeModelV3InitialBaseFee
	}

	if txCount > FeeModelV3MaxBlockTxs {
		txCount = FeeModelV3MaxBlockTxs
	}

	if txCount == FeeModelV3TargetBlockTxs {
		return currentBaseFee
	}

	if txCount > FeeModelV3TargetBlockTxs {
		delta := txCount - FeeModelV3TargetBlockTxs
		// adjustment = currentBaseFee * delta / (Target * 8)
		adjustment := (currentBaseFee * delta) / (FeeModelV3TargetBlockTxs * FeeModelV3MaxBaseFeeChangeDenominator)
		if adjustment == 0 {
			adjustment = 1
		}
		return currentBaseFee + adjustment
	}

	// Under-utilized block (txCount < TargetBlockTxs): decay base fee
	delta := FeeModelV3TargetBlockTxs - txCount
	adjustment := (currentBaseFee * delta) / (FeeModelV3TargetBlockTxs * FeeModelV3MaxBaseFeeChangeDenominator)
	if currentBaseFee <= FeeModelV3MinBaseFee+adjustment {
		return FeeModelV3MinBaseFee
	}
	return currentBaseFee - adjustment
}

// RequiredFeeV3 computes the minimum required fee for a transaction under fee model v3.
// It enforces max(baseFee, min(1000, ceil(Amount / 1000))), with a minimum of FeeModelV3MinBaseFee.
func RequiredFeeV3(tx Tx, baseFee uint64) uint64 {
	if baseFee < FeeModelV3MinBaseFee {
		baseFee = FeeModelV3MinBaseFee
	}

	txType := metadataValue(tx.Metadata, "type")
	if tx.AssetID == "" && (txType == "" || txType == "transfer") {
		ceil := tx.Amount / 1000
		if tx.Amount%1000 != 0 {
			ceil++
		}
		if ceil > 1000 {
			ceil = 1000
		}
		if ceil > baseFee {
			return ceil
		}
	}
	return baseFee
}

// EffectiveBaseFee returns the active base fee for a given block height.
// Prior to FeeModelV3ActivationHeight, it returns 0.
// From FeeModelV3ActivationHeight onward, if FeeModelV3BaseFee is uninitialized,
// it returns FeeModelV3InitialBaseFee.
func (s *State) EffectiveBaseFee(height int64) uint64 {
	if height < FeeModelV3ActivationHeight {
		return 0
	}
	if s.FeeModelV3BaseFee == 0 {
		return FeeModelV3InitialBaseFee
	}
	return s.FeeModelV3BaseFee
}
