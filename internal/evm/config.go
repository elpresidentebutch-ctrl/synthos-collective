package evm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// MainnetEVMChainID is the EVM chain ID for SYNTHOS Collective mainnet (synthos-mainnet-1).
const MainnetEVMChainID uint64 = 20260702

// DevnetEVMChainID is the EVM chain ID for the isolated SYNTHOS EVM devnet (synthos-evm-devnet-1).
const DevnetEVMChainID uint64 = 20260799

// WeiPerSYN is the conversion factor between 0-decimal native SYN and 18-decimal EVM wei:
// 1 SYN = 10^18 wei = 1,000,000,000,000,000,000 wei.
var WeiPerSYN = uint256.NewInt(1_000_000_000_000_000_000)

// BlockGasLimit defines the conservative starting 10,000,000 gas limit per CometBFT block.
// May be increased in a future coordinated hard fork after public testnet soak testing.
const BlockGasLimit uint64 = 10_000_000

// TargetBlockGas defines the 50% block utilization target (5,000,000 gas) paired with BlockGasLimit.
const TargetBlockGas uint64 = 5_000_000

// StandardTransferGas is the canonical Ethereum intrinsic gas cost of a plain value transfer.
const StandardTransferGas uint64 = 21_000

// MinNativeBaseFeeSYN matches FeeModelV3MinBaseFee (10 SYN per native transfer).
const MinNativeBaseFeeSYN uint64 = 10

// Compiled-in EVM activation heights per CometBFT chain ID.
// Consensus invariant: activation height is ALWAYS compiled into the binary per network
// and NEVER exposed as a CLI flag, environment variable, or per-node config knob.
const (
	// MainnetChainIDComet is the CometBFT chain ID for SYNTHOS mainnet.
	MainnetChainIDComet = "synthos-mainnet-1"
	// DevnetChainIDComet is the CometBFT chain ID for the isolated EVM devnet.
	DevnetChainIDComet = "synthos-evm-devnet-1"

	// MainnetEVMActivationHeight is 0 (disabled) on feature/evm-foundation until Phase 5
	// coordinated hard-fork release following LGPL-3.0 legal sign-off, external audit, and testnet soak.
	MainnetEVMActivationHeight uint64 = 0

	// DevnetEVMActivationHeight enables EVM execution from block 1 on the isolated EVM devnet.
	DevnetEVMActivationHeight uint64 = 1
)

// ActivationHeightForChain returns the compiled-in EVM activation height for the given
// CometBFT chain ID and whether EVM activation is enabled for that chain ID in this build.
func ActivationHeightForChain(cometChainID string) (uint64, bool) {
	switch cometChainID {
	case MainnetChainIDComet:
		return MainnetEVMActivationHeight, MainnetEVMActivationHeight > 0
	case DevnetChainIDComet:
		return DevnetEVMActivationHeight, DevnetEVMActivationHeight > 0
	default:
		return 0, false
	}
}

// IsEVMActive reports whether EVM execution is active at blockHeight on cometChainID.
func IsEVMActive(cometChainID string, blockHeight uint64) bool {
	activationHeight, enabled := ActivationHeightForChain(cometChainID)
	return enabled && blockHeight >= activationHeight
}

// BaseFeePerGasFromNativeSYN converts SYNTHOS Fee Model V3's native base fee (whole SYN per
// standard transaction) into EVM base_fee_per_gas (wei per gas, 1 SYN = 10^18 wei):
//
//	base_fee_per_gas_wei = floor((max(nativeBaseFeeSYN, 10) * 10^18) / 21,000)
//
// At the 10 SYN floor, base_fee_per_gas is 476,190,476,190,476 wei/gas so a 21,000-gas transfer
// costs ~10 SYN (within 4,000 wei = 4e-15 SYN), preserving parity with native transfers.
func BaseFeePerGasFromNativeSYN(nativeBaseFeeSYN uint64) *uint256.Int {
	if nativeBaseFeeSYN < MinNativeBaseFeeSYN {
		nativeBaseFeeSYN = MinNativeBaseFeeSYN
	}
	totalWei := new(uint256.Int).Mul(uint256.NewInt(nativeBaseFeeSYN), WeiPerSYN)
	return new(uint256.Int).Div(totalWei, uint256.NewInt(StandardTransferGas))
}

// DefaultChainConfig returns the Ethereum Cancun+Prague-enabled ChainConfig for the given EVM chain ID.
func DefaultChainConfig(chainID *big.Int) *params.ChainConfig {
	zero := big.NewInt(0)
	zeroTime := uint64(0)

	return &params.ChainConfig{
		ChainID:             chainID,
		HomesteadBlock:      zero,
		DAOForkBlock:        nil,
		EIP150Block:         zero,
		EIP155Block:         zero,
		EIP158Block:         zero,
		ByzantiumBlock:      zero,
		ConstantinopleBlock: zero,
		PetersburgBlock:     zero,
		IstanbulBlock:       zero,
		MuirGlacierBlock:    zero,
		BerlinBlock:         zero,
		LondonBlock:         zero,
		ArrowGlacierBlock:   zero,
		GrayGlacierBlock:    zero,
		MergeNetsplitBlock:  zero,
		ShanghaiTime:        &zeroTime,
		CancunTime:          &zeroTime, // Cancun active from genesis: EIP-4844, EIP-1153, EIP-6780, EIP-5656
		PragueTime:          &zeroTime, // Prague (Pectra) active from genesis (requires go-ethereum >= v1.15): EIP-7702, EIP-2537, EIP-7623, EIP-2935
	}
}

// NativeToWei converts whole SYN tokens (uint64) into 18-decimal wei (*uint256.Int).
func NativeToWei(syn uint64) *uint256.Int {
	s := uint256.NewInt(syn)
	res := new(uint256.Int).Mul(s, WeiPerSYN)
	return res
}

// WeiToNative converts 18-decimal wei into whole SYN tokens, returning the whole SYN and remainder dust.
func WeiToNative(wei *uint256.Int) (uint64, *uint256.Int) {
	if wei == nil || wei.IsZero() {
		return 0, uint256.NewInt(0)
	}
	syn := new(uint256.Int).Div(wei, WeiPerSYN)
	rem := new(uint256.Int).Mod(wei, WeiPerSYN)
	return syn.Uint64(), rem
}

// ZeroAddress is 0x0000000000000000000000000000000000000000
var ZeroAddress = common.Address{}
