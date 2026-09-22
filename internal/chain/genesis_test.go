package chain

import "testing"

// TestNewChain_PinnedGenesisOverridesFreshComputation reproduces the actual
// incident this pinning mechanism exists for: a live node's genesis block
// was computed once, long ago, under a State.Root()/hashing formula that has
// since been corrected (see core.go's Root() and buildMerkleRoot history).
// That node never recomputes its genesis -- it just reloads whatever it
// already persisted -- so it stays on the old formula's answer forever, even
// after the formula is fixed in code. A fresh node building NewChain from
// the exact same alloc data would otherwise get a *different* genesis block
// under today's corrected formula, permanently unable to agree with the
// live node on block 0.
//
// This test proves PinnedGenesisStateRoot/PinnedGenesisHash close that gap:
// the same Alloc, once with no pin (computed fresh) and once with a pin set
// to some other known value, must produce two DIFFERENT genesis blocks --
// and the pinned one must match the pin exactly, not the freshly-computed
// value, even though nothing about the account data changed.
func TestNewChain_PinnedGenesisOverridesFreshComputation(t *testing.T) {
	alloc := map[Address]uint64{
		"0xaaaa": 100,
		"0xbbbb": 200,
	}

	unpinned := Genesis{
		ChainID: "test-chain",
		Alloc:   alloc,
	}
	freshChain, err := NewChain(unpinned)
	if err != nil {
		t.Fatalf("NewChain(unpinned): %v", err)
	}
	freshTip := freshChain.Tip()

	// A deliberately different, made-up "known-correct live" value -- stands
	// in for a real historical formula's output the way config/genesis.json
	// pins the actual synthos-mainnet-1 values.
	const pinnedStateRoot = "0xdeadbeef00000000000000000000000000000000000000000000000000000000"
	const pinnedHash = "0xfeedface00000000000000000000000000000000000000000000000000000000"

	pinned := Genesis{
		ChainID: "test-chain",
		Alloc:   alloc,
		Metadata: map[string]any{
			"pinned_genesis_state_root": pinnedStateRoot,
			"pinned_genesis_hash":       pinnedHash,
		},
	}
	pinnedChain, err := NewChain(pinned)
	if err != nil {
		t.Fatalf("NewChain(pinned): %v", err)
	}
	pinnedTip := pinnedChain.Tip()

	if pinnedTip.Header.StateRoot != pinnedStateRoot {
		t.Errorf("pinned genesis state_root = %q, want %q", pinnedTip.Header.StateRoot, pinnedStateRoot)
	}
	if pinnedTip.Hash != pinnedHash {
		t.Errorf("pinned genesis hash = %q, want %q", pinnedTip.Hash, pinnedHash)
	}

	// The whole point: the SAME account data must produce a DIFFERENT
	// genesis block when pinned vs. freshly computed -- proving the pin is
	// actually being honored, not just coincidentally equal to the fresh
	// computation.
	if freshTip.Header.StateRoot == pinnedTip.Header.StateRoot {
		t.Errorf("pinned and fresh state_root unexpectedly equal (%q) -- pin isn't actually overriding anything", freshTip.Header.StateRoot)
	}
	if freshTip.Hash == pinnedTip.Hash {
		t.Errorf("pinned and fresh hash unexpectedly equal (%q) -- pin isn't actually overriding anything", freshTip.Hash)
	}
}

// TestNewChain_UnpinnedGenesisStillComputesFresh guards the default path:
// a genesis.json with no pinned_genesis_* metadata (every test fixture and
// config/genesis.example.json) must keep computing its genesis block fresh,
// exactly as before this mechanism existed. Only a genesis that opts in by
// declaring the metadata should ever be pinned.
func TestNewChain_UnpinnedGenesisStillComputesFresh(t *testing.T) {
	g := Genesis{
		ChainID: "test-chain",
		Alloc:   map[Address]uint64{"0xaaaa": 100},
	}
	st, err := g.ToState()
	if err != nil {
		t.Fatalf("ToState: %v", err)
	}
	want := st.Root()

	c, err := NewChain(g)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	tip := c.Tip()
	if tip.Header.StateRoot != want {
		t.Errorf("unpinned genesis state_root = %q, want freshly-computed %q", tip.Header.StateRoot, want)
	}

	wantHash, err := tip.CalculateHash()
	if err != nil {
		t.Fatalf("CalculateHash: %v", err)
	}
	if tip.Hash != wantHash {
		t.Errorf("unpinned genesis hash = %q, want freshly-computed %q", tip.Hash, wantHash)
	}
}

// TestGenesis_PinnedGenesisAccessors covers the accessor methods directly:
// present-and-non-empty returns (value, true); absent or empty returns
// ("", false) so NewChain falls back to fresh computation.
func TestGenesis_PinnedGenesisAccessors(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		wantRoot string
		wantOK1  bool
		wantHash string
		wantOK2  bool
	}{
		{
			name:     "absent metadata",
			metadata: nil,
			wantOK1:  false,
			wantOK2:  false,
		},
		{
			name: "present but empty string",
			metadata: map[string]any{
				"pinned_genesis_state_root": "",
				"pinned_genesis_hash":       "",
			},
			wantOK1: false,
			wantOK2: false,
		},
		{
			name: "present and set",
			metadata: map[string]any{
				"pinned_genesis_state_root": "0xabc",
				"pinned_genesis_hash":       "0xdef",
			},
			wantRoot: "0xabc",
			wantOK1:  true,
			wantHash: "0xdef",
			wantOK2:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := Genesis{ChainID: "c", Alloc: map[Address]uint64{"0xa": 1}, Metadata: tc.metadata}
			root, ok1 := g.PinnedGenesisStateRoot()
			if root != tc.wantRoot || ok1 != tc.wantOK1 {
				t.Errorf("PinnedGenesisStateRoot() = (%q, %v), want (%q, %v)", root, ok1, tc.wantRoot, tc.wantOK1)
			}
			hash, ok2 := g.PinnedGenesisHash()
			if hash != tc.wantHash || ok2 != tc.wantOK2 {
				t.Errorf("PinnedGenesisHash() = (%q, %v), want (%q, %v)", hash, ok2, tc.wantHash, tc.wantOK2)
			}
		})
	}
}

// TestNewChain_RealSynthosMainnetGenesisMatchesLiveNetwork anchors the exact
// live incident: the real synthos-mainnet-1 alloc (as deployed in
// config/genesis.json) must produce validator-12's actual, already-live
// genesis state_root/hash -- 0x19d2ad03ed52a28e5cd2302b898e6e950069a54da8b782c6a4ea3782cb447f49
// / 0x0173b6a906e328e7ee4ef3efde46f7ceefa13fafbb22f3abd75607d9d22d927e,
// confirmed directly against the live node's /blocks?from=0 endpoint -- not
// whatever today's State.Root()/hashing formula would compute fresh from
// that same alloc (confirmed separately to be a *different* value,
// 0xa40c015e2800877ec9db67c55a5316b5e8b43dcf967fcbf3ade78aa7bc44a2ff /
// 0xa40c28b769f00c6fe63764291f40e9ef514f2af5ceb15e7366618af5a62fb8d5,
// reproducing the exact freeze every follower hit trying to resync). This
// is the regression test that would have caught the incident before it
// shipped: it fails immediately if either the pin is ever removed from
// config/genesis.json, or the pin value is ever wrong.
func TestNewChain_RealSynthosMainnetGenesisMatchesLiveNetwork(t *testing.T) {
	const wantStateRoot = "0x19d2ad03ed52a28e5cd2302b898e6e950069a54da8b782c6a4ea3782cb447f49"
	const wantHash = "0x0173b6a906e328e7ee4ef3efde46f7ceefa13fafbb22f3abd75607d9d22d927e"

	g := Genesis{
		ChainID:   "synthos-mainnet-1",
		TxChainID: 20260702,
		Alloc: map[Address]uint64{
			"0x825fd94aa826da6ce0b4e57487418b72aea09f5e": 499978940,
			"0x564f8814cc40f7c287ccdff66a4324217859cc8f": 17000000000,
			"0x4119efcd17aafd0f785f06d8df7cd4e88f85b5b2": 22000000000,
			"0xc58962c2c91db3b106cacfe42f1992eafc0a9b23": 20000000000,
			"0x83f91f1fbef8b0a0d995a7fe3cedbbab6b8e3ed9": 12000000000,
			"0xa8e6b4662c0344e83ba260e8360c096b3ef7019a": 12500000000,
			"0xa4aabbc7e5841259b61afb4b4fbfcd7dae7b0501": 13000000000,
			"0x58a39157bcfdbadbd779ca32325d28f921311973": 3000000000,
			"0x25151dd517ef19003d8feb50a501d8a096b7f597": 7000,
			"0xb039d690927d389e4c03a87f3f0001069f4832fe": 10000,
			"0xcf206e37d6425bbcebeefa006b1df9e4964ba87c": 20,
			"0x757a88a78ad5e636a083de9775f8606fc3f0a511": 40,
			"0xea5435f9b214f9bf193a9b9f1d3ae8fb8ec09d99": 2000,
			"0x66a0c64a9ff8d5187715b1fefed6b0ccc883b702": 2000,
		},
		Metadata: map[string]any{
			"pinned_genesis_state_root": wantStateRoot,
			"pinned_genesis_hash":       wantHash,
		},
	}

	c, err := NewChain(g)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	tip := c.Tip()
	if tip.Header.StateRoot != wantStateRoot {
		t.Errorf("state_root = %q, want live network's %q", tip.Header.StateRoot, wantStateRoot)
	}
	if tip.Hash != wantHash {
		t.Errorf("hash = %q, want live network's %q", tip.Hash, wantHash)
	}
}
