package evm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

func TestNativeToWeiAndWeiToNative(t *testing.T) {
	// 1 SYN = 10^18 wei
	syn := uint64(500)
	wei := NativeToWei(syn)
	expectedWei := new(uint256.Int).Mul(uint256.NewInt(500), uint256.NewInt(1_000_000_000_000_000_000))
	if !wei.Eq(expectedWei) {
		t.Fatalf("NativeToWei(500) = %s, want %s", wei.Dec(), expectedWei.Dec())
	}

	backSYN, dust := WeiToNative(wei)
	if backSYN != 500 {
		t.Fatalf("WeiToNative(%s) SYN = %d, want 500", wei.Dec(), backSYN)
	}
	if !dust.IsZero() {
		t.Fatalf("WeiToNative dust = %s, want 0", dust.Dec())
	}

	// Test with fractional dust
	fractionalWei := new(uint256.Int).Add(wei, uint256.NewInt(42))
	backSYN2, dust2 := WeiToNative(fractionalWei)
	if backSYN2 != 500 {
		t.Fatalf("WeiToNative with dust SYN = %d, want 500", backSYN2)
	}
	if !dust2.Eq(uint256.NewInt(42)) {
		t.Fatalf("WeiToNative dust = %s, want 42", dust2.Dec())
	}
}

func TestDefaultChainConfig(t *testing.T) {
	cfg := DefaultChainConfig(big.NewInt(int64(DevnetEVMChainID)))
	if cfg.ChainID.Uint64() != DevnetEVMChainID {
		t.Fatalf("ChainID = %d, want %d", cfg.ChainID.Uint64(), DevnetEVMChainID)
	}
	if !cfg.IsCancun(big.NewInt(0), 0) {
		t.Fatal("Cancun should be active from genesis (block 0, time 0)")
	}
	if !cfg.IsLondon(big.NewInt(0)) {
		t.Fatal("London (EIP-1559) should be active from genesis")
	}
	if !cfg.IsShanghai(big.NewInt(0), 0) {
		t.Fatal("Shanghai should be active from genesis")
	}
	if !cfg.IsPrague(big.NewInt(0), 0) {
		t.Fatal("Prague should be active from genesis")
	}
	rules := cfg.Rules(big.NewInt(0), true, 0)
	if !rules.IsPrague {
		t.Fatal("Rules.IsPrague should be true from genesis")
	}
	if _, err := vm.LookupInstructionSet(rules); err != nil {
		t.Fatalf("vm.LookupInstructionSet(Prague) failed: %v", err)
	}
}

func TestGasLimitsAndFeeFormula(t *testing.T) {
	if BlockGasLimit != 10_000_000 {
		t.Fatalf("BlockGasLimit = %d, want 10_000_000", BlockGasLimit)
	}
	if TargetBlockGas != 5_000_000 {
		t.Fatalf("TargetBlockGas = %d, want 5_000_000", TargetBlockGas)
	}

	// Floor: native 0 or 5 SYN clamps to MinNativeBaseFeeSYN (10 SYN)
	wantFloor := uint256.NewInt(476_190_476_190_476)
	for _, in := range []uint64{0, 5, 10} {
		got := BaseFeePerGasFromNativeSYN(in)
		if !got.Eq(wantFloor) {
			t.Fatalf("BaseFeePerGasFromNativeSYN(%d) = %s, want %s", in, got.Dec(), wantFloor.Dec())
		}
	}

	// Verify 21,000-gas transfer at floor equals ~10 SYN within 10,000 wei remainder
	tenSYN := NativeToWei(10)
	transferFee := new(uint256.Int).Mul(wantFloor, uint256.NewInt(StandardTransferGas))
	diff := new(uint256.Int).Sub(tenSYN, transferFee)
	if diff.Cmp(uint256.NewInt(StandardTransferGas)) >= 0 {
		t.Fatalf("21,000-gas fee diff from 10 SYN too large: %s wei", diff.Dec())
	}

	// Doubled native base fee (20 SYN) doubles per-gas wei price
	wantTwenty := uint256.NewInt(952_380_952_380_952)
	if got := BaseFeePerGasFromNativeSYN(20); !got.Eq(wantTwenty) {
		t.Fatalf("BaseFeePerGasFromNativeSYN(20) = %s, want %s", got.Dec(), wantTwenty.Dec())
	}
}

func TestCompiledInActivationHeights(t *testing.T) {
	if h, ok := ActivationHeightForChain(MainnetChainIDComet); ok || h != 0 {
		t.Fatalf("mainnet EVM activation must remain disabled (0, false) on Phase 1 branch, got (%d, %v)", h, ok)
	}
	if IsEVMActive(MainnetChainIDComet, 999_999_999) {
		t.Fatal("mainnet EVM must not activate while MainnetEVMActivationHeight == 0")
	}

	if h, ok := ActivationHeightForChain(DevnetChainIDComet); !ok || h != 1 {
		t.Fatalf("devnet activation = (%d, %v), want (1, true)", h, ok)
	}
	if IsEVMActive(DevnetChainIDComet, 0) {
		t.Fatal("devnet EVM should not be active at height 0")
	}
	if !IsEVMActive(DevnetChainIDComet, 1) {
		t.Fatal("devnet EVM should be active at height 1")
	}
	if h, ok := ActivationHeightForChain("unknown-chain"); ok || h != 0 {
		t.Fatalf("unknown chain activation = (%d, %v), want (0, false)", h, ok)
	}
}
