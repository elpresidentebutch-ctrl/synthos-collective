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

// BlockGasLimit defines the standard 30,000,000 gas limit per CometBFT block.
const BlockGasLimit uint64 = 30_000_000

// DefaultChainConfig returns the Ethereum Cancun-enabled ChainConfig for the given EVM chain ID.
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
		CancunTime:          &zeroTime, // Cancun active from genesis: EIP-4844, EIP-1153 (TSTORE/TLOAD), EIP-6780, EIP-5656 (MCOPY)
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
