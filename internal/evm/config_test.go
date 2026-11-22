package evm

import (
	"math/big"
	"testing"

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
}
